package domain

import (
	"errors"
	"testing"
)

func TestDomainErrors_NotNil(t *testing.T) {
	domainErrors := []error{
		ErrEventNotFound,
		ErrDuplicateEvent,
		ErrEndpointNotFound,
		ErrNoSubscribedEndpoints,
		ErrEndpointDisabled,
		ErrEndpointPaused,
		ErrRateLimited,
		ErrInvalidEventStatus,
		ErrCircuitOpen,
		ErrQueueEmpty,
		ErrMessageNotFound,
		ErrDeliveryFailed,
		ErrInvalidDestination,
		ErrInvalidPayload,
		ErrInvalidIdempotencyKey,
		ErrTimeout,
		ErrConnectionFailed,
	}

	for _, err := range domainErrors {
		if err == nil {
			t.Error("domain error should not be nil")
		}
	}
}

func TestDomainErrors_ErrorMessages(t *testing.T) {
	tests := []struct {
		err      error
		contains string
	}{
		{ErrEventNotFound, "event not found"},
		{ErrDuplicateEvent, "duplicate event"},
		{ErrEndpointNotFound, "endpoint not found"},
		{ErrNoSubscribedEndpoints, "no endpoints subscribed"},
		{ErrEndpointDisabled, "endpoint is disabled"},
		{ErrEndpointPaused, "endpoint is paused"},
		{ErrRateLimited, "rate limit"},
		{ErrInvalidEventStatus, "invalid event status"},
		{ErrCircuitOpen, "circuit breaker"},
		{ErrQueueEmpty, "queue is empty"},
		{ErrMessageNotFound, "message not found"},
		{ErrDeliveryFailed, "delivery failed"},
		{ErrInvalidDestination, "invalid destination"},
		{ErrInvalidPayload, "invalid payload"},
		{ErrInvalidIdempotencyKey, "invalid idempotency key"},
		{ErrTimeout, "timed out"},
		{ErrConnectionFailed, "connection failed"},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			if tt.err.Error() == "" {
				t.Error("error message should not be empty")
			}
		})
	}
}

func TestDomainErrors_IsComparison(t *testing.T) {
	// Test that errors.Is works with sentinel errors
	tests := []struct {
		err    error
		target error
		want   bool
	}{
		{ErrEventNotFound, ErrEventNotFound, true},
		{ErrEventNotFound, ErrDuplicateEvent, false},
		{ErrQueueEmpty, ErrQueueEmpty, true},
		{ErrQueueEmpty, ErrMessageNotFound, false},
	}

	for _, tt := range tests {
		t.Run(tt.err.Error(), func(t *testing.T) {
			if got := errors.Is(tt.err, tt.target); got != tt.want {
				t.Errorf("errors.Is(%v, %v) = %v, want %v", tt.err, tt.target, got, tt.want)
			}
		})
	}
}

func TestValidationError(t *testing.T) {
	err := NewValidationError("destination", "URL is required")

	if err.Field != "destination" {
		t.Errorf("expected field 'destination', got %q", err.Field)
	}
	if err.Message != "URL is required" {
		t.Errorf("expected message 'URL is required', got %q", err.Message)
	}
}

func TestValidationError_Error(t *testing.T) {
	err := NewValidationError("payload", "cannot be empty")

	expected := "payload: cannot be empty"
	if err.Error() != expected {
		t.Errorf("expected %q, got %q", expected, err.Error())
	}
}

func TestValidationError_Interface(t *testing.T) {
	// Verify ValidationError implements error interface
	var _ error = NewValidationError("field", "message")

	err := NewValidationError("field", "message")
	if err.Error() != "field: message" {
		t.Errorf("unexpected error message: %s", err.Error())
	}
}

func TestValidationError_AsComparison(t *testing.T) {
	err := NewValidationError("url", "invalid format")

	var validationErr ValidationError
	if !errors.As(err, &validationErr) {
		t.Error("should be able to extract ValidationError")
	}

	if validationErr.Field != "url" {
		t.Errorf("expected field 'url', got %q", validationErr.Field)
	}
}

func TestValidationError_EmptyValues(t *testing.T) {
	err := NewValidationError("", "")

	if err.Field != "" {
		t.Error("field should be empty")
	}
	if err.Message != "" {
		t.Error("message should be empty")
	}
	if err.Error() != ": " {
		t.Errorf("unexpected error message: %q", err.Error())
	}
}

func TestValidationError_SpecialCharacters(t *testing.T) {
	err := NewValidationError("headers[X-Custom]", `value contains "quotes"`)

	if err.Field != "headers[X-Custom]" {
		t.Errorf("field should handle brackets: %q", err.Field)
	}
	if err.Message != `value contains "quotes"` {
		t.Errorf("message should handle quotes: %q", err.Message)
	}
}

func TestDomainErrors_Wrapping(t *testing.T) {
	// Test wrapping domain errors
	wrapped := errors.New("context: " + ErrEventNotFound.Error())

	// The wrapped error should contain the message
	if wrapped.Error() != "context: event not found" {
		t.Errorf("unexpected wrapped error: %s", wrapped.Error())
	}
}

func TestDomainErrors_Unique(t *testing.T) {
	// Ensure all domain errors are unique (no duplicates)
	errors := []error{
		ErrEventNotFound,
		ErrDuplicateEvent,
		ErrEndpointNotFound,
		ErrNoSubscribedEndpoints,
		ErrEndpointDisabled,
		ErrEndpointPaused,
		ErrRateLimited,
		ErrInvalidEventStatus,
		ErrCircuitOpen,
		ErrQueueEmpty,
		ErrMessageNotFound,
		ErrDeliveryFailed,
		ErrInvalidDestination,
		ErrInvalidPayload,
		ErrInvalidIdempotencyKey,
		ErrTimeout,
		ErrConnectionFailed,
	}

	seen := make(map[string]bool)
	for _, err := range errors {
		msg := err.Error()
		if seen[msg] {
			t.Errorf("duplicate error message: %s", msg)
		}
		seen[msg] = true
	}
}
