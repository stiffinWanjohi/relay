package domain

import (
	"time"

	"github.com/google/uuid"
)

// DeliveryAttempt represents a single attempt to deliver an event.
type DeliveryAttempt struct {
	ID            uuid.UUID
	EventID       uuid.UUID
	StatusCode    int
	ResponseBody  string
	Error         string
	DurationMs    int64
	AttemptNumber int
	AttemptedAt   time.Time
}

// NewDeliveryAttempt creates a new delivery attempt record.
func NewDeliveryAttempt(eventID uuid.UUID, attemptNumber int) DeliveryAttempt {
	return DeliveryAttempt{
		ID:            uuid.New(),
		EventID:       eventID,
		AttemptNumber: attemptNumber,
		AttemptedAt:   time.Now().UTC(),
	}
}

// WithSuccess records a successful delivery attempt.
func (d DeliveryAttempt) WithSuccess(statusCode int, responseBody string, durationMs int64) DeliveryAttempt {
	return DeliveryAttempt{
		ID:            d.ID,
		EventID:       d.EventID,
		StatusCode:    statusCode,
		ResponseBody:  responseBody,
		Error:         "",
		DurationMs:    durationMs,
		AttemptNumber: d.AttemptNumber,
		AttemptedAt:   d.AttemptedAt,
	}
}

// WithFailure records a failed delivery attempt.
func (d DeliveryAttempt) WithFailure(statusCode int, responseBody string, err string, durationMs int64) DeliveryAttempt {
	return DeliveryAttempt{
		ID:            d.ID,
		EventID:       d.EventID,
		StatusCode:    statusCode,
		ResponseBody:  responseBody,
		Error:         err,
		DurationMs:    durationMs,
		AttemptNumber: d.AttemptNumber,
		AttemptedAt:   d.AttemptedAt,
	}
}

// IsSuccess returns true if the delivery attempt was successful.
func (d DeliveryAttempt) IsSuccess() bool {
	return d.StatusCode >= 200 && d.StatusCode < 300 && d.Error == ""
}

// DeliveryResult represents the outcome of a delivery attempt.
type DeliveryResult struct {
	Success      bool
	StatusCode   int
	ResponseBody string
	Error        error
	DurationMs   int64
}

// NewSuccessResult creates a successful delivery result.
func NewSuccessResult(statusCode int, responseBody string, durationMs int64) DeliveryResult {
	return DeliveryResult{
		Success:      true,
		StatusCode:   statusCode,
		ResponseBody: responseBody,
		DurationMs:   durationMs,
	}
}

// NewFailureResult creates a failed delivery result.
func NewFailureResult(statusCode int, responseBody string, err error, durationMs int64) DeliveryResult {
	return DeliveryResult{
		Success:      false,
		StatusCode:   statusCode,
		ResponseBody: responseBody,
		Error:        err,
		DurationMs:   durationMs,
	}
}
