package domain

import (
	"encoding/json"
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

// Event represents a webhook event to be delivered.
type Event struct {
	ID             uuid.UUID
	IdempotencyKey string
	Destination    string
	Payload        json.RawMessage
	Headers        map[string]string
	Status         EventStatus
	Attempts       int
	MaxAttempts    int
	NextAttemptAt  *time.Time
	DeliveredAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

// NewEvent creates a new event with default values.
func NewEvent(idempotencyKey, destination string, payload json.RawMessage, headers map[string]string) Event {
	now := time.Now().UTC()
	return Event{
		ID:             uuid.New(),
		IdempotencyKey: idempotencyKey,
		Destination:    destination,
		Payload:        payload,
		Headers:        headers,
		Status:         EventStatusQueued,
		Attempts:       0,
		MaxAttempts:    10,
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
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusDelivering,
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
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusDelivered,
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
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusFailed,
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
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         EventStatusDead,
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
		Destination:    e.Destination,
		Payload:        e.Payload,
		Headers:        e.Headers,
		Status:         e.Status,
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
