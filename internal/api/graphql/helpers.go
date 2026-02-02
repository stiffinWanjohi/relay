package graphql

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
)

// generateSecret generates a cryptographically secure random secret.
func generateSecret(length int) (string, error) {
	bytes := make([]byte, length)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return hex.EncodeToString(bytes), nil
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

	return &Endpoint{
		ID:               ep.ID.String(),
		ClientID:         ep.ClientID,
		URL:              ep.URL,
		Description:      description,
		EventTypes:       ep.EventTypes,
		Status:           domainEndpointStatusToGQL(ep.Status),
		Filter:           filter,
		Transformation:   transformation,
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
