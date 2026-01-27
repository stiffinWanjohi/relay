package domain

import "errors"

// Domain errors for the relay system.
var (
	// ErrEventNotFound is returned when an event cannot be found.
	ErrEventNotFound = errors.New("event not found")

	// ErrDuplicateEvent is returned when an event with the same idempotency key exists.
	ErrDuplicateEvent = errors.New("duplicate event: idempotency key already exists")

	// ErrInvalidEventStatus is returned when an event status transition is invalid.
	ErrInvalidEventStatus = errors.New("invalid event status transition")

	// ErrCircuitOpen is returned when the circuit breaker is open for a destination.
	ErrCircuitOpen = errors.New("circuit breaker is open for destination")

	// ErrQueueEmpty is returned when attempting to dequeue from an empty queue.
	ErrQueueEmpty = errors.New("queue is empty")

	// ErrMessageNotFound is returned when a queue message cannot be found.
	ErrMessageNotFound = errors.New("queue message not found")

	// ErrDeliveryFailed is returned when webhook delivery fails.
	ErrDeliveryFailed = errors.New("webhook delivery failed")

	// ErrInvalidDestination is returned when the destination URL is invalid.
	ErrInvalidDestination = errors.New("invalid destination URL")

	// ErrInvalidPayload is returned when the payload is invalid.
	ErrInvalidPayload = errors.New("invalid payload")

	// ErrInvalidIdempotencyKey is returned when the idempotency key is invalid.
	ErrInvalidIdempotencyKey = errors.New("invalid idempotency key")

	// ErrTimeout is returned when an operation times out.
	ErrTimeout = errors.New("operation timed out")

	// ErrConnectionFailed is returned when a connection cannot be established.
	ErrConnectionFailed = errors.New("connection failed")
)

// ValidationError represents a validation error with field details.
type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string {
	return e.Field + ": " + e.Message
}

// NewValidationError creates a new validation error.
func NewValidationError(field, message string) ValidationError {
	return ValidationError{
		Field:   field,
		Message: message,
	}
}
