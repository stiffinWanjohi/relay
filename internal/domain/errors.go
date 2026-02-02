package domain

import "errors"

// Domain errors for the relay system.
var (
	// ErrEventNotFound is returned when an event cannot be found.
	ErrEventNotFound = errors.New("event not found")

	// ErrDuplicateEvent is returned when an event with the same idempotency key exists.
	ErrDuplicateEvent = errors.New("duplicate event: idempotency key already exists")

	// ErrEndpointNotFound is returned when an endpoint cannot be found.
	ErrEndpointNotFound = errors.New("endpoint not found")

	// ErrNoSubscribedEndpoints is returned when no endpoints subscribe to an event type.
	ErrNoSubscribedEndpoints = errors.New("no endpoints subscribed to event type")

	// ErrEndpointDisabled is returned when attempting to deliver to a disabled endpoint.
	ErrEndpointDisabled = errors.New("endpoint is disabled")

	// ErrEndpointPaused is returned when attempting to deliver to a paused endpoint.
	ErrEndpointPaused = errors.New("endpoint is paused")

	// ErrRateLimited is returned when the rate limit is exceeded for an endpoint.
	ErrRateLimited = errors.New("rate limit exceeded")

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

	// ErrEventTypeNotFound is returned when an event type cannot be found.
	ErrEventTypeNotFound = errors.New("event type not found")

	// ErrDuplicateEventType is returned when an event type with the same name exists.
	ErrDuplicateEventType = errors.New("event type already exists")

	// ErrInvalidJSONSchema is returned when a JSON schema is invalid.
	ErrInvalidJSONSchema = errors.New("invalid JSON schema")

	// ErrPayloadValidation is returned when a payload fails schema validation.
	ErrPayloadValidation = errors.New("payload validation failed")
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
