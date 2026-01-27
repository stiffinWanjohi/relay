package graphql

import (
	"encoding/json"

	"github.com/relay/internal/domain"
)

// Helper functions for converting between domain and GraphQL types

func domainEventToGQL(evt domain.Event) *Event {
	var payload map[string]any
	if len(evt.Payload) > 0 {
		json.Unmarshal(evt.Payload, &payload)
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

	return &Endpoint{
		ID:               ep.ID.String(),
		ClientID:         ep.ClientID,
		URL:              ep.URL,
		Description:      description,
		EventTypes:       ep.EventTypes,
		Status:           domainEndpointStatusToGQL(ep.Status),
		MaxRetries:       ep.MaxRetries,
		RetryBackoffMs:   ep.RetryBackoffMs,
		RetryBackoffMax:  ep.RetryBackoffMax,
		RetryBackoffMult: ep.RetryBackoffMult,
		TimeoutMs:        ep.TimeoutMs,
		RateLimitPerSec:  ep.RateLimitPerSec,
		CircuitThreshold: ep.CircuitThreshold,
		CircuitResetMs:   ep.CircuitResetMs,
		CustomHeaders:    headers,
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
