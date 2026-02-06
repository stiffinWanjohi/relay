package graphql

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/stiffinWanjohi/relay/internal/alerting"
	"github.com/stiffinWanjohi/relay/internal/connector"
	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/metrics"
)

// validateScheduling validates and computes the scheduled delivery time from input parameters.
func validateScheduling(deliverAt *time.Time, delaySeconds *int) (*time.Time, error) {
	if deliverAt != nil && delaySeconds != nil {
		return nil, fmt.Errorf("cannot specify both deliverAt and delaySeconds")
	}

	if deliverAt != nil {
		if deliverAt.Before(time.Now()) {
			return nil, fmt.Errorf("deliverAt must be in the future")
		}
		if time.Until(*deliverAt) > domain.MaxScheduleDelay {
			return nil, fmt.Errorf("deliverAt cannot be more than 30 days in the future")
		}
		return deliverAt, nil
	}

	if delaySeconds != nil {
		if *delaySeconds < 0 {
			return nil, fmt.Errorf("delaySeconds must be positive")
		}
		if *delaySeconds > int(domain.MaxScheduleDelay.Seconds()) {
			return nil, fmt.Errorf("delaySeconds cannot exceed 30 days")
		}
		t := time.Now().Add(time.Duration(*delaySeconds) * time.Second)
		return &t, nil
	}

	return nil, nil
}

// timeNow is a variable so it can be mocked in tests
var timeNow = time.Now

// mapStringToAny converts a map[string]string to map[string]any for GraphQL
func mapStringToAny(m map[string]string) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// jsonToMap converts a json.RawMessage to map[string]any for GraphQL
func jsonToMap(data json.RawMessage) map[string]any {
	if data == nil {
		return nil
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		return nil
	}
	return v
}

// generateSecret generates a cryptographically secure random secret.
func generateSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
}

// validateFIFOConfig validates FIFO configuration for an endpoint.
// Returns an error if the configuration is invalid.
func validateFIFOConfig(fifo bool, partitionKey string) error {
	if !fifo && partitionKey != "" {
		return fmt.Errorf("fifoPartitionKey cannot be set when fifo is disabled")
	}

	if partitionKey != "" {
		if err := validateJSONPath(partitionKey); err != nil {
			return fmt.Errorf("invalid fifoPartitionKey: %w", err)
		}
	}

	return nil
}

// validateJSONPath validates a simple JSONPath expression.
// Supports paths like "$.field", "$.field.subfield", "$['field']"
func validateJSONPath(path string) error {
	if path == "" {
		return nil
	}

	// Must start with $
	if !strings.HasPrefix(path, "$") {
		return fmt.Errorf("JSONPath must start with '$', got: %s", path)
	}

	// Remove leading $
	remaining := path[1:]
	if remaining == "" {
		return nil // Just "$" is valid (root)
	}

	// Must be followed by . or [
	if remaining[0] != '.' && remaining[0] != '[' {
		return fmt.Errorf("JSONPath must use dot notation ($.field) or bracket notation ($['field']), got: %s", path)
	}

	// Validate the path segments
	i := 0
	for i < len(remaining) {
		switch remaining[i] {
		case '.':
			// Dot notation: .fieldname
			i++
			if i >= len(remaining) {
				return fmt.Errorf("JSONPath cannot end with '.': %s", path)
			}

			// Read field name
			start := i
			for i < len(remaining) && isValidFieldChar(remaining[i]) {
				i++
			}
			if i == start {
				return fmt.Errorf("JSONPath has empty field name after '.': %s", path)
			}
		case '[':
			// Bracket notation: ['fieldname'] or [0]
			i++
			if i >= len(remaining) {
				return fmt.Errorf("JSONPath has unclosed bracket: %s", path)
			}

			if remaining[i] == '\'' || remaining[i] == '"' {
				// String key: ['key'] or ["key"]
				quote := remaining[i]
				i++
				start := i
				for i < len(remaining) && remaining[i] != quote {
					i++
				}
				if i >= len(remaining) {
					return fmt.Errorf("JSONPath has unclosed string in bracket: %s", path)
				}
				if i == start {
					return fmt.Errorf("JSONPath has empty string key: %s", path)
				}
				i++ // Skip closing quote
			} else if remaining[i] >= '0' && remaining[i] <= '9' {
				// Array index: [0]
				for i < len(remaining) && remaining[i] >= '0' && remaining[i] <= '9' {
					i++
				}
			} else {
				return fmt.Errorf("JSONPath bracket must contain quoted string or array index: %s", path)
			}

			if i >= len(remaining) || remaining[i] != ']' {
				return fmt.Errorf("JSONPath has unclosed bracket: %s", path)
			}
			i++ // Skip ]
		default:
			return fmt.Errorf("unexpected character in JSONPath at position %d: %s", i+1, path)
		}
	}

	return nil
}

// isValidFieldChar returns true if the character is valid in a JSONPath field name.
func isValidFieldChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_' || c == '-'
}

// Helper functions for converting between domain and GraphQL types

func domainEventToGQL(evt domain.Event) *Event {
	var payload map[string]any
	if len(evt.Payload) > 0 {
		_ = json.Unmarshal(evt.Payload, &payload)
	}

	var headers map[string]any
	if len(evt.Headers) > 0 {
		headers = make(map[string]any)
		for k, v := range evt.Headers {
			headers[k] = v
		}
	}

	var endpointID *string
	if evt.EndpointID != nil {
		id := evt.EndpointID.String()
		endpointID = &id
	}

	var clientID, eventType *string
	if evt.ClientID != "" {
		clientID = &evt.ClientID
	}
	if evt.EventType != "" {
		eventType = &evt.EventType
	}

	return &Event{
		ID:             evt.ID.String(),
		IdempotencyKey: evt.IdempotencyKey,
		ClientID:       clientID,
		EventType:      eventType,
		EndpointID:     endpointID,
		Destination:    evt.Destination,
		Payload:        payload,
		Headers:        headers,
		Status:         domainStatusToGQL(evt.Status),
		Priority:       evt.Priority,
		ScheduledAt:    evt.ScheduledAt,
		Attempts:       evt.Attempts,
		MaxAttempts:    evt.MaxAttempts,
		NextAttemptAt:  evt.NextAttemptAt,
		DeliveredAt:    evt.DeliveredAt,
		CreatedAt:      evt.CreatedAt,
		UpdatedAt:      evt.UpdatedAt,
	}
}

func domainAttemptToGQL(a domain.DeliveryAttempt) DeliveryAttempt {
	result := DeliveryAttempt{
		ID:            a.ID.String(),
		EventID:       a.EventID.String(),
		DurationMs:    int(a.DurationMs),
		AttemptNumber: a.AttemptNumber,
		AttemptedAt:   a.AttemptedAt,
	}

	if a.StatusCode != 0 {
		sc := a.StatusCode
		result.StatusCode = &sc
	}

	if a.ResponseBody != "" {
		result.ResponseBody = &a.ResponseBody
	}

	if a.Error != "" {
		result.Error = &a.Error
	}

	return result
}

func domainEndpointToGQL(ep domain.Endpoint) *Endpoint {
	var headers map[string]any
	if len(ep.CustomHeaders) > 0 {
		headers = make(map[string]any)
		for k, v := range ep.CustomHeaders {
			headers[k] = v
		}
	}

	var description *string
	if ep.Description != "" {
		description = &ep.Description
	}

	// Convert filter bytes to JSON map
	var filter map[string]any
	if len(ep.Filter) > 0 {
		_ = json.Unmarshal(ep.Filter, &filter)
	}

	// Convert transformation to pointer
	var transformation *string
	if ep.Transformation != "" {
		transformation = &ep.Transformation
	}

	// Convert FIFO partition key to pointer
	var fifoPartitionKey *string
	if ep.FIFOPartitionKey != "" {
		fifoPartitionKey = &ep.FIFOPartitionKey
	}

	return &Endpoint{
		ID:               ep.ID.String(),
		ClientID:         ep.ClientID,
		URL:              ep.URL,
		Description:      description,
		EventTypes:       ep.EventTypes,
		Status:           domainEndpointStatusToGQL(ep.Status),
		Filter:           filter,
		Transformation:   transformation,
		Fifo:             ep.FIFO,
		FifoPartitionKey: fifoPartitionKey,
		MaxRetries:       ep.MaxRetries,
		RetryBackoffMs:   ep.RetryBackoffMs,
		RetryBackoffMax:  ep.RetryBackoffMax,
		RetryBackoffMult: ep.RetryBackoffMult,
		TimeoutMs:        ep.TimeoutMs,
		RateLimitPerSec:  ep.RateLimitPerSec,
		CircuitThreshold: ep.CircuitThreshold,
		CircuitResetMs:   ep.CircuitResetMs,
		CustomHeaders:    headers,
		HasCustomSecret:  ep.HasCustomSecret(),
		SecretRotatedAt:  ep.SecretRotatedAt,
		CreatedAt:        ep.CreatedAt,
		UpdatedAt:        ep.UpdatedAt,
	}
}

func domainStatusToGQL(s domain.EventStatus) EventStatus {
	switch s {
	case domain.EventStatusQueued:
		return EventStatusQueued
	case domain.EventStatusDelivering:
		return EventStatusDelivering
	case domain.EventStatusDelivered:
		return EventStatusDelivered
	case domain.EventStatusFailed:
		return EventStatusFailed
	case domain.EventStatusDead:
		return EventStatusDead
	default:
		return EventStatusQueued
	}
}

func gqlStatusToDomain(s EventStatus) domain.EventStatus {
	switch s {
	case EventStatusQueued:
		return domain.EventStatusQueued
	case EventStatusDelivering:
		return domain.EventStatusDelivering
	case EventStatusDelivered:
		return domain.EventStatusDelivered
	case EventStatusFailed:
		return domain.EventStatusFailed
	case EventStatusDead:
		return domain.EventStatusDead
	default:
		return domain.EventStatusQueued
	}
}

func domainEndpointStatusToGQL(s domain.EndpointStatus) EndpointStatus {
	switch s {
	case domain.EndpointStatusActive:
		return EndpointStatusActive
	case domain.EndpointStatusPaused:
		return EndpointStatusPaused
	case domain.EndpointStatusDisabled:
		return EndpointStatusDisabled
	default:
		return EndpointStatusActive
	}
}

func gqlEndpointStatusToDomain(s EndpointStatus) domain.EndpointStatus {
	switch s {
	case EndpointStatusActive:
		return domain.EndpointStatusActive
	case EndpointStatusPaused:
		return domain.EndpointStatusPaused
	case EndpointStatusDisabled:
		return domain.EndpointStatusDisabled
	default:
		return domain.EndpointStatusActive
	}
}

func storeBatchResultToGQL(result *event.BatchRetryResult) *BatchRetryResult {
	succeeded := make([]Event, len(result.Succeeded))
	for i, evt := range result.Succeeded {
		succeeded[i] = *domainEventToGQL(evt)
	}

	failed := make([]BatchRetryError, len(result.Failed))
	for i, f := range result.Failed {
		failed[i] = BatchRetryError{
			EventID: f.EventID.String(),
			Error:   f.Error,
		}
	}

	return &BatchRetryResult{
		Succeeded:      succeeded,
		Failed:         failed,
		TotalRequested: len(result.Succeeded) + len(result.Failed),
		TotalSucceeded: len(result.Succeeded),
	}
}

func domainEventTypeToGQL(et domain.EventType) *EventType {
	var description, schemaVersion *string
	if et.Description != "" {
		description = &et.Description
	}
	if et.SchemaVersion != "" {
		schemaVersion = &et.SchemaVersion
	}

	var schema map[string]any
	if len(et.Schema) > 0 {
		_ = json.Unmarshal(et.Schema, &schema)
	}

	return &EventType{
		ID:            et.ID.String(),
		ClientID:      et.ClientID,
		Name:          et.Name,
		Description:   description,
		Schema:        schema,
		SchemaVersion: schemaVersion,
		CreatedAt:     et.CreatedAt,
		UpdatedAt:     et.UpdatedAt,
	}
}

// granularityToDuration converts a TimeGranularity enum to a time.Duration.
func granularityToDuration(g TimeGranularity) time.Duration {
	switch g {
	case TimeGranularityMinute:
		return time.Minute
	case TimeGranularityHour:
		return time.Hour
	case TimeGranularityDay:
		return 24 * time.Hour
	case TimeGranularityWeek:
		return 7 * 24 * time.Hour
	default:
		return time.Hour // default to hourly
	}
}

// computeStatsFromRecords computes analytics stats from a slice of delivery records.
func computeStatsFromRecords(records []metrics.DeliveryRecord, period time.Time) *AnalyticsStats {
	stats := &AnalyticsStats{
		Period: period.Format(time.RFC3339),
	}

	if len(records) == 0 {
		return stats
	}

	var totalLatency int64
	var minLatency, maxLatency int64 = -1, 0
	latencies := make([]int64, 0, len(records))

	for _, r := range records {
		stats.TotalCount++
		latencies = append(latencies, r.LatencyMs)
		totalLatency += r.LatencyMs

		if minLatency == -1 || r.LatencyMs < minLatency {
			minLatency = r.LatencyMs
		}
		if r.LatencyMs > maxLatency {
			maxLatency = r.LatencyMs
		}

		switch r.Outcome {
		case metrics.OutcomeSuccess:
			stats.SuccessCount++
		case metrics.OutcomeFailure:
			stats.FailureCount++
		case metrics.OutcomeTimeout:
			stats.TimeoutCount++
		}
	}

	if stats.TotalCount > 0 {
		stats.SuccessRate = float64(stats.SuccessCount) / float64(stats.TotalCount)
		stats.FailureRate = float64(stats.FailureCount+stats.TimeoutCount) / float64(stats.TotalCount)
		stats.AvgLatencyMs = float64(totalLatency) / float64(stats.TotalCount)
		stats.MinLatencyMs = int(minLatency)
		stats.MaxLatencyMs = int(maxLatency)

		// Calculate percentiles
		slices.Sort(latencies)
		stats.P50LatencyMs = float64(latencies[len(latencies)*50/100])
		stats.P95LatencyMs = float64(latencies[len(latencies)*95/100])
		p99Index := len(latencies) * 99 / 100
		if p99Index >= len(latencies) {
			p99Index = len(latencies) - 1
		}
		stats.P99LatencyMs = float64(latencies[p99Index])
	}

	return stats
}

// Alerting type conversion helpers

func parseDurationOrDefault(s string, def time.Duration) time.Duration {
	d, err := time.ParseDuration(s)
	if err != nil {
		return def
	}
	return d
}

func gqlMetricToAlertingMetric(m AlertMetric) alerting.ConditionType {
	switch m {
	case AlertMetricFailureRate:
		return alerting.ConditionTypeFailureRate
	case AlertMetricSuccessRate:
		return alerting.ConditionTypeSuccessRate
	case AlertMetricLatency:
		return alerting.ConditionTypeLatency
	case AlertMetricQueueDepth:
		return alerting.ConditionTypeQueueDepth
	case AlertMetricErrorCount:
		return alerting.ConditionTypeErrorCount
	case AlertMetricDeliveryCount:
		return alerting.ConditionTypeDeliveryCount
	default:
		return alerting.ConditionTypeFailureRate
	}
}

func gqlOperatorToAlertingOperator(o AlertOperator) alerting.Operator {
	switch o {
	case AlertOperatorGt:
		return alerting.OperatorGreaterThan
	case AlertOperatorGte:
		return alerting.OperatorGreaterThanOrEqual
	case AlertOperatorLt:
		return alerting.OperatorLessThan
	case AlertOperatorLte:
		return alerting.OperatorLessThanOrEqual
	case AlertOperatorEq:
		return alerting.OperatorEqual
	case AlertOperatorNe:
		return alerting.OperatorNotEqual
	default:
		return alerting.OperatorGreaterThan
	}
}

func gqlActionTypeToAlertingActionType(t AlertActionType) alerting.ActionType {
	switch t {
	case AlertActionTypeSLACk:
		return alerting.ActionTypeSlack
	case AlertActionTypeEmail:
		return alerting.ActionTypeEmail
	case AlertActionTypeWebhook:
		return alerting.ActionTypeWebhook
	case AlertActionTypePagerduty:
		return alerting.ActionTypePagerDuty
	default:
		return alerting.ActionTypeSlack
	}
}

func gqlActionConfigToAlertingConfig(input *AlertActionInput) alerting.ActionConfig {
	cfg := alerting.ActionConfig{}
	if input.WebhookURL != nil {
		cfg.WebhookURL = *input.WebhookURL
		cfg.URL = *input.WebhookURL
	}
	if input.Channel != nil {
		cfg.Channel = *input.Channel
	}
	if input.To != nil {
		cfg.To = input.To
	}
	if input.Subject != nil {
		cfg.Subject = *input.Subject
	}
	if input.RoutingKey != nil {
		cfg.RoutingKey = *input.RoutingKey
	}
	if input.Severity != nil {
		cfg.Severity = *input.Severity
	}
	return cfg
}

func alertingMetricToGQL(m alerting.ConditionType) AlertMetric {
	switch m {
	case alerting.ConditionTypeFailureRate:
		return AlertMetricFailureRate
	case alerting.ConditionTypeSuccessRate:
		return AlertMetricSuccessRate
	case alerting.ConditionTypeLatency:
		return AlertMetricLatency
	case alerting.ConditionTypeQueueDepth:
		return AlertMetricQueueDepth
	case alerting.ConditionTypeErrorCount:
		return AlertMetricErrorCount
	case alerting.ConditionTypeDeliveryCount:
		return AlertMetricDeliveryCount
	default:
		return AlertMetricFailureRate
	}
}

func alertingOperatorToGQL(o alerting.Operator) AlertOperator {
	switch o {
	case alerting.OperatorGreaterThan:
		return AlertOperatorGt
	case alerting.OperatorGreaterThanOrEqual:
		return AlertOperatorGte
	case alerting.OperatorLessThan:
		return AlertOperatorLt
	case alerting.OperatorLessThanOrEqual:
		return AlertOperatorLte
	case alerting.OperatorEqual:
		return AlertOperatorEq
	case alerting.OperatorNotEqual:
		return AlertOperatorNe
	default:
		return AlertOperatorGt
	}
}

func alertingActionTypeToGQL(t alerting.ActionType) AlertActionType {
	switch t {
	case alerting.ActionTypeSlack:
		return AlertActionTypeSLACk
	case alerting.ActionTypeEmail:
		return AlertActionTypeEmail
	case alerting.ActionTypeWebhook:
		return AlertActionTypeWebhook
	case alerting.ActionTypePagerDuty:
		return AlertActionTypePagerduty
	default:
		return AlertActionTypeSLACk
	}
}

func alertingRuleToGQL(rule *alerting.Rule) *AlertRule {
	var desc *string
	if rule.Description != "" {
		desc = &rule.Description
	}

	var lastFired *time.Time
	if !rule.LastFiredAt().IsZero() {
		t := rule.LastFiredAt()
		lastFired = &t
	}

	return &AlertRule{
		ID:          rule.ID.String(),
		Name:        rule.Name,
		Description: desc,
		Enabled:     rule.Enabled,
		Condition: &AlertCondition{
			Metric:   alertingMetricToGQL(rule.Condition.Metric),
			Operator: alertingOperatorToGQL(rule.Condition.Operator),
			Value:    rule.Condition.Value,
			Window:   rule.Condition.Window.Duration().String(),
		},
		Action: &AlertAction{
			Type:       alertingActionTypeToGQL(rule.Action.Type),
			WebhookURL: strPtrOrNil(rule.Action.Config.WebhookURL),
			Channel:    strPtrOrNil(rule.Action.Config.Channel),
			To:         rule.Action.Config.To,
			Subject:    strPtrOrNil(rule.Action.Config.Subject),
			RoutingKey: strPtrOrNil(rule.Action.Config.RoutingKey),
			Severity:   strPtrOrNil(rule.Action.Config.Severity),
			Message:    strPtrOrNil(rule.Action.Message),
		},
		Cooldown:    rule.Cooldown.Duration().String(),
		LastFiredAt: lastFired,
		CreatedAt:   rule.CreatedAt,
		UpdatedAt:   rule.UpdatedAt,
	}
}

func strPtrOrNil(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func alertingAlertToGQL(alert alerting.Alert) Alert {
	return Alert{
		ID:        alert.ID.String(),
		RuleID:    alert.RuleID.String(),
		RuleName:  alert.RuleName,
		Metric:    alert.Metric,
		Value:     alert.Value,
		Threshold: alert.Threshold,
		Message:   alert.Message,
		FiredAt:   alert.FiredAt,
	}
}

// Connector type conversion helpers

func gqlConnectorTypeToConnector(t ConnectorType) connector.ConnectorType {
	switch t {
	case ConnectorTypeSLACk:
		return connector.ConnectorTypeSlack
	case ConnectorTypeDiscord:
		return connector.ConnectorTypeDiscord
	case ConnectorTypeTeams:
		return connector.ConnectorTypeTeams
	case ConnectorTypeEmail:
		return connector.ConnectorTypeEmail
	case ConnectorTypeWebhook:
		return connector.ConnectorTypeWebhook
	default:
		return connector.ConnectorTypeWebhook
	}
}

func connectorTypeToGQL(t connector.ConnectorType) ConnectorType {
	switch t {
	case connector.ConnectorTypeSlack:
		return ConnectorTypeSLACk
	case connector.ConnectorTypeDiscord:
		return ConnectorTypeDiscord
	case connector.ConnectorTypeTeams:
		return ConnectorTypeTeams
	case connector.ConnectorTypeEmail:
		return ConnectorTypeEmail
	case connector.ConnectorTypeWebhook:
		return ConnectorTypeWebhook
	default:
		return ConnectorTypeWebhook
	}
}

func gqlConnectorConfigToConnector(input *ConnectorConfigInput) connector.Config {
	if input == nil {
		return connector.Config{}
	}

	cfg := connector.Config{}
	if input.WebhookURL != nil {
		cfg.WebhookURL = *input.WebhookURL
	}
	if input.Channel != nil {
		cfg.Channel = *input.Channel
	}
	if input.Username != nil {
		cfg.Username = *input.Username
	}
	if input.IconEmoji != nil {
		cfg.IconEmoji = *input.IconEmoji
	}
	if input.IconURL != nil {
		cfg.IconURL = *input.IconURL
	}
	if input.SMTPHost != nil {
		cfg.SMTPHost = *input.SMTPHost
	}
	if input.SMTPPort != nil {
		cfg.SMTPPort = *input.SMTPPort
	}
	if input.SMTPUsername != nil {
		cfg.SMTPUsername = *input.SMTPUsername
	}
	if input.SMTPPassword != nil {
		cfg.SMTPPassword = *input.SMTPPassword
	}
	if input.FromEmail != nil {
		cfg.FromEmail = *input.FromEmail
	}
	if input.ToEmails != nil {
		cfg.ToEmails = input.ToEmails
	}
	return cfg
}

func gqlConnectorTemplateToConnector(input *ConnectorTemplateInput) connector.Template {
	if input == nil {
		return connector.Template{}
	}

	tmpl := connector.Template{}
	if input.Text != nil {
		tmpl.Text = *input.Text
	}
	if input.Title != nil {
		tmpl.Title = *input.Title
	}
	if input.Body != nil {
		tmpl.Body = *input.Body
	}
	if input.Color != nil {
		tmpl.Color = *input.Color
	}
	if input.Subject != nil {
		tmpl.Subject = *input.Subject
	}
	return tmpl
}

func connectorToGQL(name string, c *connector.Connector) *Connector {
	var smtpPort *int
	if c.Config.SMTPPort > 0 {
		smtpPort = &c.Config.SMTPPort
	}

	return &Connector{
		Name: name,
		Type: connectorTypeToGQL(c.Type),
		Config: &ConnectorConfig{
			WebhookURL: strPtrOrNil(c.Config.WebhookURL),
			Channel:    strPtrOrNil(c.Config.Channel),
			Username:   strPtrOrNil(c.Config.Username),
			IconEmoji:  strPtrOrNil(c.Config.IconEmoji),
			IconURL:    strPtrOrNil(c.Config.IconURL),
			SMTPHost:   strPtrOrNil(c.Config.SMTPHost),
			SMTPPort:   smtpPort,
			FromEmail:  strPtrOrNil(c.Config.FromEmail),
			ToEmails:   c.Config.ToEmails,
		},
		Template: &ConnectorTemplate{
			Text:    strPtrOrNil(c.Template.Text),
			Title:   strPtrOrNil(c.Template.Title),
			Body:    strPtrOrNil(c.Template.Body),
			Color:   strPtrOrNil(c.Template.Color),
			Subject: strPtrOrNil(c.Template.Subject),
		},
	}
}
