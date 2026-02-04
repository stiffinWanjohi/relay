package graphql

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
)

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
		if remaining[i] == '.' {
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
		} else if remaining[i] == '[' {
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
		} else {
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
