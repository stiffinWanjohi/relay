package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewDeliveryAttempt(t *testing.T) {
	eventID := uuid.New()
	attemptNumber := 3

	attempt := NewDeliveryAttempt(eventID, attemptNumber)

	if attempt.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if attempt.EventID != eventID {
		t.Errorf("expected event ID %v, got %v", eventID, attempt.EventID)
	}
	if attempt.AttemptNumber != attemptNumber {
		t.Errorf("expected attempt number %d, got %d", attemptNumber, attempt.AttemptNumber)
	}
	if attempt.AttemptedAt.IsZero() {
		t.Error("expected non-zero AttemptedAt")
	}
	if attempt.StatusCode != 0 {
		t.Errorf("expected status code 0, got %d", attempt.StatusCode)
	}
	if attempt.ResponseBody != "" {
		t.Errorf("expected empty response body, got %q", attempt.ResponseBody)
	}
	if attempt.Error != "" {
		t.Errorf("expected empty error, got %q", attempt.Error)
	}
	if attempt.DurationMs != 0 {
		t.Errorf("expected duration 0, got %d", attempt.DurationMs)
	}
}

func TestDeliveryAttempt_WithSuccess(t *testing.T) {
	eventID := uuid.New()
	original := NewDeliveryAttempt(eventID, 1)
	time.Sleep(time.Millisecond) // Ensure some time passes

	success := original.WithSuccess(200, `{"status": "ok"}`, 150)

	if success.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", success.StatusCode)
	}
	if success.ResponseBody != `{"status": "ok"}` {
		t.Errorf("expected response body, got %q", success.ResponseBody)
	}
	if success.Error != "" {
		t.Errorf("expected empty error, got %q", success.Error)
	}
	if success.DurationMs != 150 {
		t.Errorf("expected duration 150, got %d", success.DurationMs)
	}
	// Preserved fields
	if success.ID != original.ID {
		t.Error("ID should be preserved")
	}
	if success.EventID != original.EventID {
		t.Error("EventID should be preserved")
	}
	if success.AttemptNumber != original.AttemptNumber {
		t.Error("AttemptNumber should be preserved")
	}
	if !success.AttemptedAt.Equal(original.AttemptedAt) {
		t.Error("AttemptedAt should be preserved")
	}
}

func TestDeliveryAttempt_WithFailure(t *testing.T) {
	eventID := uuid.New()
	original := NewDeliveryAttempt(eventID, 2)

	failure := original.WithFailure(500, "Internal Server Error", "server error", 5000)

	if failure.StatusCode != 500 {
		t.Errorf("expected status code 500, got %d", failure.StatusCode)
	}
	if failure.ResponseBody != "Internal Server Error" {
		t.Errorf("expected response body, got %q", failure.ResponseBody)
	}
	if failure.Error != "server error" {
		t.Errorf("expected error 'server error', got %q", failure.Error)
	}
	if failure.DurationMs != 5000 {
		t.Errorf("expected duration 5000, got %d", failure.DurationMs)
	}
	// Preserved fields
	if failure.ID != original.ID {
		t.Error("ID should be preserved")
	}
	if failure.EventID != original.EventID {
		t.Error("EventID should be preserved")
	}
}

func TestDeliveryAttempt_WithFailure_NoStatusCode(t *testing.T) {
	original := NewDeliveryAttempt(uuid.New(), 1)

	// Network error - no status code
	failure := original.WithFailure(0, "", "connection refused", 100)

	if failure.StatusCode != 0 {
		t.Errorf("expected status code 0, got %d", failure.StatusCode)
	}
	if failure.Error != "connection refused" {
		t.Errorf("expected error, got %q", failure.Error)
	}
}

func TestDeliveryAttempt_IsSuccess(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		err        string
		want       bool
	}{
		{"200 OK", 200, "", true},
		{"201 Created", 201, "", true},
		{"204 No Content", 204, "", true},
		{"299 Edge case", 299, "", true},
		{"200 with error", 200, "some error", false},
		{"300 Redirect", 300, "", false},
		{"400 Bad Request", 400, "", false},
		{"404 Not Found", 404, "", false},
		{"500 Server Error", 500, "", false},
		{"0 No status", 0, "connection error", false},
		{"0 No status no error", 0, "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attempt := DeliveryAttempt{
				StatusCode: tt.statusCode,
				Error:      tt.err,
			}
			if got := attempt.IsSuccess(); got != tt.want {
				t.Errorf("IsSuccess() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDeliveryAttempt_Immutability(t *testing.T) {
	original := NewDeliveryAttempt(uuid.New(), 1)
	originalID := original.ID
	originalStatusCode := original.StatusCode

	_ = original.WithSuccess(200, "OK", 100)
	_ = original.WithFailure(500, "Error", "error", 200)

	if original.ID != originalID {
		t.Error("ID was mutated")
	}
	if original.StatusCode != originalStatusCode {
		t.Error("StatusCode was mutated")
	}
}

func TestNewSuccessResult(t *testing.T) {
	result := NewSuccessResult(200, `{"ok": true}`, 150)

	if !result.Success {
		t.Error("expected Success to be true")
	}
	if result.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", result.StatusCode)
	}
	if result.ResponseBody != `{"ok": true}` {
		t.Errorf("expected response body, got %q", result.ResponseBody)
	}
	if result.Error != nil {
		t.Errorf("expected nil error, got %v", result.Error)
	}
	if result.DurationMs != 150 {
		t.Errorf("expected duration 150, got %d", result.DurationMs)
	}
}

func TestNewFailureResult(t *testing.T) {
	err := errors.New("connection refused")
	result := NewFailureResult(0, "", err, 5000)

	if result.Success {
		t.Error("expected Success to be false")
	}
	if result.StatusCode != 0 {
		t.Errorf("expected status code 0, got %d", result.StatusCode)
	}
	if result.ResponseBody != "" {
		t.Errorf("expected empty response body, got %q", result.ResponseBody)
	}
	if result.Error != err {
		t.Errorf("expected error %v, got %v", err, result.Error)
	}
	if result.DurationMs != 5000 {
		t.Errorf("expected duration 5000, got %d", result.DurationMs)
	}
}

func TestNewFailureResult_WithStatusCode(t *testing.T) {
	err := errors.New("server error")
	result := NewFailureResult(503, "Service Unavailable", err, 100)

	if result.Success {
		t.Error("expected Success to be false")
	}
	if result.StatusCode != 503 {
		t.Errorf("expected status code 503, got %d", result.StatusCode)
	}
	if result.ResponseBody != "Service Unavailable" {
		t.Errorf("expected response body, got %q", result.ResponseBody)
	}
	if result.Error != err {
		t.Errorf("expected error, got %v", result.Error)
	}
}

func TestDeliveryResult_NilError(t *testing.T) {
	result := NewFailureResult(500, "Error", nil, 100)

	if result.Success {
		t.Error("expected Success to be false")
	}
	if result.Error != nil {
		t.Error("expected nil error")
	}
}

func TestDeliveryAttempt_VariousStatusCodes(t *testing.T) {
	attempt := NewDeliveryAttempt(uuid.New(), 1)

	// Test various HTTP status codes
	statusCodes := []struct {
		code    int
		success bool
	}{
		{100, false}, // Informational
		{199, false},
		{200, true}, // Success range
		{201, true},
		{202, true},
		{204, true},
		{299, true},
		{300, false}, // Redirect
		{301, false},
		{302, false},
		{400, false}, // Client error
		{401, false},
		{403, false},
		{404, false},
		{429, false},
		{500, false}, // Server error
		{502, false},
		{503, false},
		{504, false},
	}

	for _, tc := range statusCodes {
		result := attempt.WithSuccess(tc.code, "", 100)
		if result.IsSuccess() != tc.success {
			t.Errorf("status %d: IsSuccess() = %v, want %v", tc.code, result.IsSuccess(), tc.success)
		}
	}
}

func TestDeliveryAttempt_LargeDuration(t *testing.T) {
	attempt := NewDeliveryAttempt(uuid.New(), 1)

	// Test with large duration (30 second timeout)
	success := attempt.WithSuccess(200, "OK", 30000)
	if success.DurationMs != 30000 {
		t.Errorf("expected duration 30000, got %d", success.DurationMs)
	}
}

func TestDeliveryAttempt_LargeResponseBody(t *testing.T) {
	attempt := NewDeliveryAttempt(uuid.New(), 1)

	// Large response body
	largeBody := make([]byte, 10000)
	for i := range largeBody {
		largeBody[i] = 'x'
	}

	success := attempt.WithSuccess(200, string(largeBody), 100)
	if len(success.ResponseBody) != 10000 {
		t.Errorf("expected response body length 10000, got %d", len(success.ResponseBody))
	}
}
