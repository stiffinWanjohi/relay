package domain

import (
	"encoding/json"
	"maps"
	"time"

	"github.com/google/uuid"
)

// EventStatus represents the current state of an event in the delivery pipeline.
type EventStatus string

const (
	EventStatusQueued     EventStatus = "queued"
	EventStatusDelivering EventStatus = "delivering"
	EventStatusDelivered  EventStatus = "delivered"
	EventStatusFailed     EventStatus = "failed"
	EventStatusDead       EventStatus = "dead"
)

// Priority constants
const (
	PriorityHighest = 1
	PriorityHigh    = 3
	PriorityNormal  = 5
	PriorityLow     = 7
	PriorityLowest  = 10

	DefaultPriority = PriorityNormal
)

// MaxScheduleDelay is the maximum time an event can be scheduled in the future (30 days)
const MaxScheduleDelay = 30 * 24 * time.Hour

// Event represents a webhook event to be delivered.
type Event struct {
	ID             uuid.UUID
	IdempotencyKey string
	ClientID       string     // Tenant identifier
	EventType      string     // Event type for routing (e.g., "order.created")
	EndpointID     *uuid.UUID // Target endpoint (nil for legacy direct destination)
	Destination    string     // Webhook URL (populated from endpoint or direct)
	Payload        json.RawMessage
	Headers        map[string]string
	Status         EventStatus
	Priority       int        // 1-10, lower = higher priority, default 5
	ScheduledAt    *time.Time // When to deliver (nil = immediate)
	Attempts       int
	MaxAttempts    int
	NextAttemptAt  *time.Time
	DeliveredAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewEvent creates a new event with default values (legacy direct destination mode).
func NewEvent(idempotencyKey, destination string, payload json.RawMessage, headers map[string]string) Event {
	now := time.Now().UTC()
	return Event{
		ID:             uuid.New(),
		IdempotencyKey: idempotencyKey,
		Destination:    destination,
		Payload:        payload,
		Headers:        headers,
		Status:         EventStatusQueued,
		Priority:       DefaultPriority,
		Attempts:       0,
		MaxAttempts:    10,
		NextAttemptAt:  &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// NewEventWithOptions creates a new event with custom priority and scheduling.
func NewEventWithOptions(idempotencyKey, destination string, payload json.RawMessage, headers map[string]string, priority int, scheduledAt *time.Time) Event {
	now := time.Now().UTC()
	if priority < 1 || priority > 10 {
		priority = DefaultPriority
	}
	return Event{
		ID:             uuid.New(),
		IdempotencyKey: idempotencyKey,
		Destination:    destination,
		Payload:        payload,
		Headers:        headers,
		Status:         EventStatusQueued,
		Priority:       priority,
		ScheduledAt:    scheduledAt,
		Attempts:       0,
		MaxAttempts:    10,
		NextAttemptAt:  &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// NewEventForEndpoint creates a new event targeting a specific endpoint.
func NewEventForEndpoint(clientID, eventType, idempotencyKey string, endpoint Endpoint, payload json.RawMessage, headers map[string]string) Event {
	return NewEventForEndpointWithOptions(clientID, eventType, idempotencyKey, endpoint, payload, headers, DefaultPriority, nil)
}

// NewEventForEndpointWithOptions creates a new event targeting a specific endpoint with custom priority and scheduling.
func NewEventForEndpointWithOptions(clientID, eventType, idempotencyKey string, endpoint Endpoint, payload json.RawMessage, headers map[string]string, priority int, scheduledAt *time.Time) Event {
	now := time.Now().UTC()
	endpointID := endpoint.ID

	// Merge custom headers from endpoint with event headers
	mergedHeaders := make(map[string]string)
	maps.Copy(mergedHeaders, endpoint.CustomHeaders)
	maps.Copy(mergedHeaders, headers) // Event headers override endpoint headers

	if priority < 1 || priority > 10 {
		priority = DefaultPriority
	}

	return Event{
		ID:             uuid.New(),
		IdempotencyKey: idempotencyKey,
		ClientID:       clientID,
		EventType:      eventType,
		EndpointID:     &endpointID,
		Destination:    endpoint.URL,
		Payload:        payload,
		Headers:        mergedHeaders,
		Status:         EventStatusQueued,
		Priority:       priority,
		ScheduledAt:    scheduledAt,
		Attempts:       0,
		MaxAttempts:    endpoint.MaxRetries,
		NextAttemptAt:  &now,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
}

// ShouldRetry returns true if the event should be retried.
func (e Event) ShouldRetry() bool {
	return e.Attempts < e.MaxAttempts && e.Status != EventStatusDead
}

// MarkDelivering transitions the event to delivering status.
func (e Event) MarkDelivering() Event {
	return Event{
		ID:             e.ID,
		IdempotencyKey: e.IdempotencyKey,
		ClientID:       e.ClientID,
		EventType:      e.EventType,
		EndpointID:     e.EndpointID,
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusDelivering,
		Priority:       e.Priority,
		ScheduledAt:    e.ScheduledAt,
		Attempts:       e.Attempts,
		MaxAttempts:    e.MaxAttempts,
		NextAttemptAt:  e.NextAttemptAt,
		DeliveredAt:    e.DeliveredAt,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      time.Now().UTC(),
	}
}

// MarkDelivered transitions the event to delivered status.
func (e Event) MarkDelivered() Event {
	now := time.Now().UTC()
	return Event{
		ID:             e.ID,
		IdempotencyKey: e.IdempotencyKey,
		ClientID:       e.ClientID,
		EventType:      e.EventType,
		EndpointID:     e.EndpointID,
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusDelivered,
		Priority:       e.Priority,
		ScheduledAt:    e.ScheduledAt,
		Attempts:       e.Attempts,
		MaxAttempts:    e.MaxAttempts,
		NextAttemptAt:  nil,
		DeliveredAt:    &now,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      now,
	}
}

// MarkFailed transitions the event to failed status with next retry time.
func (e Event) MarkFailed(nextAttemptAt time.Time) Event {
	return Event{
		ID:             e.ID,
		IdempotencyKey: e.IdempotencyKey,
		ClientID:       e.ClientID,
		EventType:      e.EventType,
		EndpointID:     e.EndpointID,
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusFailed,
		Priority:       e.Priority,
		ScheduledAt:    e.ScheduledAt,
		Attempts:       e.Attempts,
		MaxAttempts:    e.MaxAttempts,
		NextAttemptAt:  &nextAttemptAt,
		DeliveredAt:    e.DeliveredAt,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      time.Now().UTC(),
	}
}

// MarkDead transitions the event to dead letter status.
func (e Event) MarkDead() Event {
	return Event{
		ID:             e.ID,
		IdempotencyKey: e.IdempotencyKey,
		ClientID:       e.ClientID,
		EventType:      e.EventType,
		EndpointID:     e.EndpointID,
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusDead,
		Priority:       e.Priority,
		ScheduledAt:    e.ScheduledAt,
		Attempts:       e.Attempts,
		MaxAttempts:    e.MaxAttempts,
		NextAttemptAt:  nil,
		DeliveredAt:    e.DeliveredAt,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      time.Now().UTC(),
	}
}

// IncrementAttempts returns a new event with incremented attempt count.
func (e Event) IncrementAttempts() Event {
	return Event{
		ID:             e.ID,
		IdempotencyKey: e.IdempotencyKey,
		ClientID:       e.ClientID,
		EventType:      e.EventType,
		EndpointID:     e.EndpointID,
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         e.Status,
		Priority:       e.Priority,
		ScheduledAt:    e.ScheduledAt,
		Attempts:       e.Attempts + 1,
		MaxAttempts:    e.MaxAttempts,
		NextAttemptAt:  e.NextAttemptAt,
		DeliveredAt:    e.DeliveredAt,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      time.Now().UTC(),
	}
}

// IsTerminal returns true if the event is in a terminal state.
func (e Event) IsTerminal() bool {
	return e.Status == EventStatusDelivered || e.Status == EventStatusDead
}

// Replay resets the event for redelivery (only valid for dead or failed events).
func (e Event) Replay() Event {
	now := time.Now().UTC()
	return Event{
		ID:             e.ID,
		IdempotencyKey: e.IdempotencyKey,
		ClientID:       e.ClientID,
		EventType:      e.EventType,
		EndpointID:     e.EndpointID,
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusQueued,
		Priority:       e.Priority,
		ScheduledAt:    nil, // Clear scheduling on replay for immediate delivery
		Attempts:       0,
		MaxAttempts:    e.MaxAttempts,
		NextAttemptAt:  &now,
		DeliveredAt:    nil,
		CreatedAt:      e.CreatedAt,
		UpdatedAt:      now,
	}
}

// IsScheduled returns true if the event is scheduled for future delivery.
func (e Event) IsScheduled() bool {
	return e.ScheduledAt != nil && e.ScheduledAt.After(time.Now())
}

// ValidatePriority checks if a priority value is valid (1-10).
func ValidatePriority(priority int) bool {
	return priority >= 1 && priority <= 10
}
