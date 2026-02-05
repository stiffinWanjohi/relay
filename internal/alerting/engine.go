package alerting

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/smtp"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/google/uuid"
)

// Engine evaluates alert rules and fires actions.
type Engine struct {
	rules            map[uuid.UUID]*Rule
	mu               sync.RWMutex
	metrics          MetricsProvider
	store            *Store // Optional persistence store
	clientID         string // Client ID for persistence (default: "default")
	evaluateInterval time.Duration
	httpClient       *http.Client

	// Alert history
	alertHistory []Alert
	historyMu    sync.RWMutex
	maxHistory   int

	// SMTP config for email alerts
	smtpHost     string
	smtpPort     int
	smtpUsername string
	smtpPassword string
	smtpFrom     string

	// Shutdown
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// EngineConfig holds configuration for the alert engine.
type EngineConfig struct {
	EvaluateInterval time.Duration
	MaxHistory       int
	SMTPHost         string
	SMTPPort         int
	SMTPUsername     string
	SMTPPassword     string
	SMTPFrom         string
}

// DefaultEngineConfig returns the default engine configuration.
func DefaultEngineConfig() EngineConfig {
	return EngineConfig{
		EvaluateInterval: 1 * time.Minute,
		MaxHistory:       1000,
	}
}

// NewEngine creates a new alert engine.
func NewEngine(metrics MetricsProvider, cfg EngineConfig) *Engine {
	return &Engine{
		rules:            make(map[uuid.UUID]*Rule),
		metrics:          metrics,
		clientID:         "default", // Default client ID for system-wide alerts
		evaluateInterval: cfg.EvaluateInterval,
		httpClient: &http.Client{
			Timeout: 10 * time.Second,
		},
		alertHistory: make([]Alert, 0, cfg.MaxHistory),
		maxHistory:   cfg.MaxHistory,
		smtpHost:     cfg.SMTPHost,
		smtpPort:     cfg.SMTPPort,
		smtpUsername: cfg.SMTPUsername,
		smtpPassword: cfg.SMTPPassword,
		smtpFrom:     cfg.SMTPFrom,
		stopCh:       make(chan struct{}),
	}
}

// WithStore sets a persistence store for the engine.
func (e *Engine) WithStore(store *Store) *Engine {
	e.store = store
	return e
}

// WithClientID sets the client ID for persistence.
func (e *Engine) WithClientID(clientID string) *Engine {
	e.clientID = clientID
	return e
}

// LoadRules loads rules from the persistence store for the engine's client.
func (e *Engine) LoadRules(ctx context.Context) error {
	if e.store == nil {
		return nil
	}

	rules, err := e.store.LoadRules(ctx, e.clientID)
	if err != nil {
		return err
	}

	e.mu.Lock()
	for _, rule := range rules {
		e.rules[rule.ID] = rule
	}
	e.mu.Unlock()

	log.Info("loaded alert rules", "count", len(rules))
	return nil
}

// AddRule adds a rule to the engine.
func (e *Engine) AddRule(rule *Rule) error {
	if err := rule.Validate(); err != nil {
		return err
	}

	e.mu.Lock()
	e.rules[rule.ID] = rule
	e.mu.Unlock()

	// Persist to store
	if e.store != nil {
		if err := e.store.SaveRule(context.Background(), e.clientID, rule); err != nil {
			log.Error("failed to persist rule", "rule_id", rule.ID, "error", err)
		}
	}

	log.Info("alert rule added", "rule_id", rule.ID, "name", rule.Name)
	return nil
}

// RemoveRule removes a rule from the engine.
func (e *Engine) RemoveRule(id uuid.UUID) error {
	e.mu.Lock()
	if _, ok := e.rules[id]; !ok {
		e.mu.Unlock()
		return ErrRuleNotFound
	}
	delete(e.rules, id)
	e.mu.Unlock()

	// Remove from store
	if e.store != nil {
		if err := e.store.DeleteRule(context.Background(), id); err != nil {
			log.Error("failed to delete rule from store", "rule_id", id, "error", err)
		}
	}

	log.Info("alert rule removed", "rule_id", id)
	return nil
}

// GetRule returns a rule by ID.
func (e *Engine) GetRule(id uuid.UUID) (*Rule, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rule, ok := e.rules[id]
	if !ok {
		return nil, ErrRuleNotFound
	}
	return rule, nil
}

// ListRules returns all rules.
func (e *Engine) ListRules() []*Rule {
	e.mu.RLock()
	defer e.mu.RUnlock()

	rules := make([]*Rule, 0, len(e.rules))
	for _, rule := range e.rules {
		rules = append(rules, rule)
	}
	return rules
}

// UpdateRule updates an existing rule.
func (e *Engine) UpdateRule(rule *Rule) error {
	if err := rule.Validate(); err != nil {
		return err
	}

	e.mu.Lock()
	if _, ok := e.rules[rule.ID]; !ok {
		e.mu.Unlock()
		return ErrRuleNotFound
	}

	rule.UpdatedAt = time.Now().UTC()
	e.rules[rule.ID] = rule
	e.mu.Unlock()

	// Persist to store
	if e.store != nil {
		if err := e.store.SaveRule(context.Background(), e.clientID, rule); err != nil {
			log.Error("failed to persist updated rule", "rule_id", rule.ID, "error", err)
		}
	}

	log.Info("alert rule updated", "rule_id", rule.ID, "name", rule.Name)
	return nil
}

// EnableRule enables a rule.
func (e *Engine) EnableRule(id uuid.UUID) error {
	e.mu.Lock()
	rule, ok := e.rules[id]
	if !ok {
		e.mu.Unlock()
		return ErrRuleNotFound
	}
	rule.Enabled = true
	rule.UpdatedAt = time.Now().UTC()
	e.mu.Unlock()

	// Persist to store
	if e.store != nil {
		if err := e.store.SaveRule(context.Background(), e.clientID, rule); err != nil {
			log.Error("failed to persist enabled rule", "rule_id", id, "error", err)
		}
	}

	return nil
}

// DisableRule disables a rule.
func (e *Engine) DisableRule(id uuid.UUID) error {
	e.mu.Lock()
	rule, ok := e.rules[id]
	if !ok {
		e.mu.Unlock()
		return ErrRuleNotFound
	}
	rule.Enabled = false
	rule.UpdatedAt = time.Now().UTC()
	e.mu.Unlock()

	// Persist to store
	if e.store != nil {
		if err := e.store.SaveRule(context.Background(), e.clientID, rule); err != nil {
			log.Error("failed to persist disabled rule", "rule_id", id, "error", err)
		}
	}

	return nil
}

// Start starts the alert evaluation loop.
func (e *Engine) Start(ctx context.Context) {
	e.wg.Add(1)
	go func() {
		defer e.wg.Done()
		ticker := time.NewTicker(e.evaluateInterval)
		defer ticker.Stop()

		log.Info("alert engine started", "interval", e.evaluateInterval)

		for {
			select {
			case <-ctx.Done():
				log.Info("alert engine stopping (context cancelled)")
				return
			case <-e.stopCh:
				log.Info("alert engine stopping")
				return
			case <-ticker.C:
				e.evaluateAll(ctx)
			}
		}
	}()
}

// Stop stops the alert engine.
func (e *Engine) Stop() {
	close(e.stopCh)
	e.wg.Wait()
}

// evaluateAll evaluates all enabled rules.
func (e *Engine) evaluateAll(ctx context.Context) {
	e.mu.RLock()
	rules := make([]*Rule, 0, len(e.rules))
	for _, rule := range e.rules {
		if rule.Enabled {
			rules = append(rules, rule)
		}
	}
	e.mu.RUnlock()

	for _, rule := range rules {
		if err := e.evaluateRule(ctx, rule); err != nil {
			log.Error("failed to evaluate rule", "rule_id", rule.ID, "error", err)
		}
	}
}

// evaluateRule evaluates a single rule.
func (e *Engine) evaluateRule(ctx context.Context, rule *Rule) error {
	// Get metric value
	value, err := e.getMetricValue(ctx, rule.Condition.Metric, rule.Condition.Window.Duration())
	if err != nil {
		return fmt.Errorf("failed to get metric: %w", err)
	}

	// Check if condition is met
	if !rule.Evaluate(value) {
		return nil
	}

	// Check cooldown
	if !rule.CanFire() {
		log.Debug("alert in cooldown", "rule_id", rule.ID, "name", rule.Name)
		return nil
	}

	// Fire alert
	alert := e.createAlert(rule, value)
	if err := e.fireAction(ctx, rule, alert); err != nil {
		log.Error("failed to fire alert action", "rule_id", rule.ID, "error", err)
		return err
	}

	// Mark as fired and store in history
	rule.MarkFired()
	e.addToHistory(alert)

	log.Warn("alert fired",
		"rule_id", rule.ID,
		"name", rule.Name,
		"metric", rule.Condition.Metric,
		"value", value,
		"threshold", rule.Condition.Value,
	)

	return nil
}

// getMetricValue gets the current value for a metric.
func (e *Engine) getMetricValue(ctx context.Context, metric ConditionType, window time.Duration) (float64, error) {
	if e.metrics == nil {
		return 0, fmt.Errorf("metrics provider not configured")
	}

	switch metric {
	case ConditionTypeFailureRate:
		return e.metrics.GetFailureRate(ctx, window)
	case ConditionTypeLatency:
		return e.metrics.GetAverageLatency(ctx, window)
	case ConditionTypeQueueDepth:
		depth, err := e.metrics.GetQueueDepth(ctx)
		return float64(depth), err
	case ConditionTypeErrorCount:
		count, err := e.metrics.GetErrorCount(ctx, window)
		return float64(count), err
	case ConditionTypeSuccessRate:
		return e.metrics.GetSuccessRate(ctx, window)
	case ConditionTypeDeliveryCount:
		count, err := e.metrics.GetDeliveryCount(ctx, window)
		return float64(count), err
	default:
		return 0, fmt.Errorf("unknown metric: %s", metric)
	}
}

// createAlert creates an alert from a rule and value.
func (e *Engine) createAlert(rule *Rule, value float64) Alert {
	message := e.formatMessage(rule, value)
	return Alert{
		ID:        uuid.New(),
		RuleID:    rule.ID,
		RuleName:  rule.Name,
		Metric:    string(rule.Condition.Metric),
		Value:     value,
		Threshold: rule.Condition.Value,
		Message:   message,
		FiredAt:   time.Now().UTC(),
	}
}

// formatMessage formats the alert message using the template.
func (e *Engine) formatMessage(rule *Rule, value float64) string {
	if rule.Action.Message == "" {
		// Default message
		return fmt.Sprintf("[%s] %s: %s is %.2f (threshold: %s %.2f)",
			rule.Name,
			rule.Condition.Metric,
			rule.Condition.Metric,
			value,
			rule.Condition.Operator,
			rule.Condition.Value,
		)
	}

	// Parse and execute template
	tmpl, err := template.New("message").Parse(rule.Action.Message)
	if err != nil {
		return rule.Action.Message
	}

	var buf bytes.Buffer
	data := map[string]any{
		"rule_name": rule.Name,
		"metric":    rule.Condition.Metric,
		"value":     value,
		"threshold": rule.Condition.Value,
		"operator":  rule.Condition.Operator,
		"fired_at":  time.Now().UTC().Format(time.RFC3339),
	}
	if err := tmpl.Execute(&buf, data); err != nil {
		return rule.Action.Message
	}
	return buf.String()
}

// fireAction fires the action for an alert.
func (e *Engine) fireAction(ctx context.Context, rule *Rule, alert Alert) error {
	switch rule.Action.Type {
	case ActionTypeSlack:
		return e.fireSlack(ctx, rule, alert)
	case ActionTypeEmail:
		return e.fireEmail(ctx, rule, alert)
	case ActionTypeWebhook:
		return e.fireWebhook(ctx, rule, alert)
	case ActionTypePagerDuty:
		return e.firePagerDuty(ctx, rule, alert)
	default:
		return fmt.Errorf("unknown action type: %s", rule.Action.Type)
	}
}

// fireSlack sends an alert to Slack.
func (e *Engine) fireSlack(ctx context.Context, rule *Rule, alert Alert) error {
	payload := map[string]any{
		"text": alert.Message,
	}
	if rule.Action.Config.Channel != "" {
		payload["channel"] = rule.Action.Config.Channel
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rule.Action.Config.WebhookURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned %d", resp.StatusCode)
	}

	log.Debug("slack alert sent", "rule_id", rule.ID)
	return nil
}

// fireEmail sends an alert via email.
func (e *Engine) fireEmail(ctx context.Context, rule *Rule, alert Alert) error {
	if e.smtpHost == "" {
		return fmt.Errorf("SMTP not configured")
	}

	subject := rule.Action.Config.Subject
	if subject == "" {
		subject = fmt.Sprintf("[Alert] %s", rule.Name)
	}

	addr := fmt.Sprintf("%s:%d", e.smtpHost, e.smtpPort)

	msg := strings.Builder{}
	fmt.Fprintf(&msg, "From: %s\r\n", e.smtpFrom)
	fmt.Fprintf(&msg, "To: %s\r\n", strings.Join(rule.Action.Config.To, ", "))
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(alert.Message)

	var auth smtp.Auth
	if e.smtpUsername != "" && e.smtpPassword != "" {
		auth = smtp.PlainAuth("", e.smtpUsername, e.smtpPassword, e.smtpHost)
	}

	if err := smtp.SendMail(addr, auth, e.smtpFrom, rule.Action.Config.To, []byte(msg.String())); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	log.Debug("email alert sent", "rule_id", rule.ID, "to", rule.Action.Config.To)
	return nil
}

// fireWebhook sends an alert to a webhook URL.
func (e *Engine) fireWebhook(ctx context.Context, rule *Rule, alert Alert) error {
	body, _ := json.Marshal(alert)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, rule.Action.Config.URL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	for k, v := range rule.Action.Config.Headers {
		req.Header.Set(k, v)
	}

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook returned %d", resp.StatusCode)
	}

	log.Debug("webhook alert sent", "rule_id", rule.ID, "url", rule.Action.Config.URL)
	return nil
}

// firePagerDuty sends an alert to PagerDuty.
func (e *Engine) firePagerDuty(ctx context.Context, rule *Rule, alert Alert) error {
	severity := rule.Action.Config.Severity
	if severity == "" {
		severity = "warning"
	}

	payload := map[string]any{
		"routing_key":  rule.Action.Config.RoutingKey,
		"event_action": "trigger",
		"payload": map[string]any{
			"summary":   alert.Message,
			"severity":  severity,
			"source":    "relay",
			"timestamp": alert.FiredAt.Format(time.RFC3339),
			"custom_details": map[string]any{
				"rule_name": alert.RuleName,
				"metric":    alert.Metric,
				"value":     alert.Value,
				"threshold": alert.Threshold,
			},
		},
	}

	body, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://events.pagerduty.com/v2/enqueue", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 {
		return fmt.Errorf("pagerduty returned %d", resp.StatusCode)
	}

	log.Debug("pagerduty alert sent", "rule_id", rule.ID)
	return nil
}

// addToHistory adds an alert to the history.
func (e *Engine) addToHistory(alert Alert) {
	e.historyMu.Lock()
	defer e.historyMu.Unlock()

	e.alertHistory = append(e.alertHistory, alert)
	if len(e.alertHistory) > e.maxHistory {
		e.alertHistory = e.alertHistory[1:]
	}
}

// GetAlertHistory returns the alert history.
func (e *Engine) GetAlertHistory(limit int) []Alert {
	e.historyMu.RLock()
	defer e.historyMu.RUnlock()

	if limit <= 0 || limit > len(e.alertHistory) {
		limit = len(e.alertHistory)
	}

	// Return most recent first
	result := make([]Alert, limit)
	for i := 0; i < limit; i++ {
		result[i] = e.alertHistory[len(e.alertHistory)-1-i]
	}
	return result
}

// EvaluateNow forces immediate evaluation of all rules.
func (e *Engine) EvaluateNow(ctx context.Context) {
	e.evaluateAll(ctx)
}
