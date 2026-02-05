package alerting

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/logging"
)

var log = logging.Component("alerting")

// Condition types for alert rules.
type ConditionType string

const (
	ConditionTypeFailureRate   ConditionType = "failure_rate"
	ConditionTypeLatency       ConditionType = "latency"
	ConditionTypeQueueDepth    ConditionType = "queue_depth"
	ConditionTypeErrorCount    ConditionType = "error_count"
	ConditionTypeSuccessRate   ConditionType = "success_rate"
	ConditionTypeDeliveryCount ConditionType = "delivery_count"
)

// Operator types for conditions.
type Operator string

const (
	OperatorGreaterThan        Operator = "gt"
	OperatorGreaterThanOrEqual Operator = "gte"
	OperatorLessThan           Operator = "lt"
	OperatorLessThanOrEqual    Operator = "lte"
	OperatorEqual              Operator = "eq"
	OperatorNotEqual           Operator = "ne"
)

// ActionType defines the type of action to take when an alert fires.
type ActionType string

const (
	ActionTypeSlack     ActionType = "slack"
	ActionTypeEmail     ActionType = "email"
	ActionTypeWebhook   ActionType = "webhook"
	ActionTypePagerDuty ActionType = "pagerduty"
)

// Errors
var (
	ErrInvalidCondition = errors.New("invalid condition")
	ErrInvalidAction    = errors.New("invalid action")
	ErrRuleNotFound     = errors.New("alert rule not found")
	ErrCooldownActive   = errors.New("alert is in cooldown period")
)

// Condition represents a condition that triggers an alert.
type Condition struct {
	Metric   ConditionType `json:"metric"`
	Operator Operator      `json:"operator"`
	Value    float64       `json:"value"`
	Window   Duration      `json:"window"` // Time window for evaluation (e.g., "5m")
}

// Duration is a wrapper for time.Duration that supports JSON marshaling.
type Duration time.Duration

func (d Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	dur, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d = Duration(dur)
	return nil
}

func (d Duration) Duration() time.Duration {
	return time.Duration(d)
}

// Action represents what happens when an alert fires.
type Action struct {
	Type    ActionType   `json:"type"`
	Config  ActionConfig `json:"config"`
	Message string       `json:"message,omitempty"` // Custom message template
}

// ActionConfig holds configuration for different action types.
type ActionConfig struct {
	// Slack
	WebhookURL string `json:"webhook_url,omitempty"`
	Channel    string `json:"channel,omitempty"`

	// Email
	To      []string `json:"to,omitempty"`
	Subject string   `json:"subject,omitempty"`

	// Webhook
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`

	// PagerDuty
	RoutingKey string `json:"routing_key,omitempty"`
	Severity   string `json:"severity,omitempty"` // critical, error, warning, info
}

// Rule represents an alert rule.
type Rule struct {
	ID          uuid.UUID `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description,omitempty"`
	Enabled     bool      `json:"enabled"`
	Condition   Condition `json:"condition"`
	Action      Action    `json:"action"`
	Cooldown    Duration  `json:"cooldown"` // Minimum time between alerts
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`

	// Runtime state (not persisted)
	lastFired time.Time
	mu        sync.Mutex
}

// NewRule creates a new alert rule.
func NewRule(name string, condition Condition, action Action) *Rule {
	now := time.Now().UTC()
	return &Rule{
		ID:        uuid.New(),
		Name:      name,
		Enabled:   true,
		Condition: condition,
		Action:    action,
		Cooldown:  Duration(15 * time.Minute), // Default 15 minute cooldown
		CreatedAt: now,
		UpdatedAt: now,
	}
}

// Validate validates the alert rule.
func (r *Rule) Validate() error {
	if r.Name == "" {
		return fmt.Errorf("%w: name is required", ErrInvalidCondition)
	}

	// Validate condition
	switch r.Condition.Metric {
	case ConditionTypeFailureRate, ConditionTypeLatency, ConditionTypeQueueDepth,
		ConditionTypeErrorCount, ConditionTypeSuccessRate, ConditionTypeDeliveryCount:
		// Valid
	default:
		return fmt.Errorf("%w: unknown metric %s", ErrInvalidCondition, r.Condition.Metric)
	}

	switch r.Condition.Operator {
	case OperatorGreaterThan, OperatorGreaterThanOrEqual, OperatorLessThan,
		OperatorLessThanOrEqual, OperatorEqual, OperatorNotEqual:
		// Valid
	default:
		return fmt.Errorf("%w: unknown operator %s", ErrInvalidCondition, r.Condition.Operator)
	}

	// Validate action
	switch r.Action.Type {
	case ActionTypeSlack:
		if r.Action.Config.WebhookURL == "" {
			return fmt.Errorf("%w: slack action requires webhook_url", ErrInvalidAction)
		}
	case ActionTypeEmail:
		if len(r.Action.Config.To) == 0 {
			return fmt.Errorf("%w: email action requires at least one recipient", ErrInvalidAction)
		}
	case ActionTypeWebhook:
		if r.Action.Config.URL == "" {
			return fmt.Errorf("%w: webhook action requires url", ErrInvalidAction)
		}
	case ActionTypePagerDuty:
		if r.Action.Config.RoutingKey == "" {
			return fmt.Errorf("%w: pagerduty action requires routing_key", ErrInvalidAction)
		}
	default:
		return fmt.Errorf("%w: unknown action type %s", ErrInvalidAction, r.Action.Type)
	}

	return nil
}

// Evaluate evaluates the condition against the given value.
func (r *Rule) Evaluate(value float64) bool {
	switch r.Condition.Operator {
	case OperatorGreaterThan:
		return value > r.Condition.Value
	case OperatorGreaterThanOrEqual:
		return value >= r.Condition.Value
	case OperatorLessThan:
		return value < r.Condition.Value
	case OperatorLessThanOrEqual:
		return value <= r.Condition.Value
	case OperatorEqual:
		return value == r.Condition.Value
	case OperatorNotEqual:
		return value != r.Condition.Value
	default:
		return false
	}
}

// CanFire checks if the rule can fire (not in cooldown).
func (r *Rule) CanFire() bool {
	r.mu.Lock()
	defer r.mu.Unlock()

	if r.lastFired.IsZero() {
		return true
	}
	return time.Since(r.lastFired) > r.Cooldown.Duration()
}

// MarkFired marks the rule as having fired.
func (r *Rule) MarkFired() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastFired = time.Now()
}

// LastFiredAt returns when the rule last fired.
func (r *Rule) LastFiredAt() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastFired
}

// Alert represents a fired alert.
type Alert struct {
	ID        uuid.UUID `json:"id"`
	RuleID    uuid.UUID `json:"rule_id"`
	RuleName  string    `json:"rule_name"`
	Metric    string    `json:"metric"`
	Value     float64   `json:"value"`
	Threshold float64   `json:"threshold"`
	Message   string    `json:"message"`
	FiredAt   time.Time `json:"fired_at"`
}

// MetricsProvider provides metrics for alert evaluation.
type MetricsProvider interface {
	GetFailureRate(ctx context.Context, window time.Duration) (float64, error)
	GetAverageLatency(ctx context.Context, window time.Duration) (float64, error)
	GetQueueDepth(ctx context.Context) (int64, error)
	GetErrorCount(ctx context.Context, window time.Duration) (int64, error)
	GetSuccessRate(ctx context.Context, window time.Duration) (float64, error)
	GetDeliveryCount(ctx context.Context, window time.Duration) (int64, error)
}
