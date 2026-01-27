package domain

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewEvent(t *testing.T) {
	idempotencyKey := "test-key-123"
	destination := "https://example.com/webhook"
	payload := json.RawMessage(`{"order_id": 123}`)
	headers := map[string]string{"Content-Type": "application/json"}

	evt := NewEvent(idempotencyKey, destination, payload, headers)

	if evt.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if evt.IdempotencyKey != idempotencyKey {
		t.Errorf("expected idempotency key %q, got %q", idempotencyKey, evt.IdempotencyKey)
	}
	if evt.Destination != destination {
		t.Errorf("expected destination %q, got %q", destination, evt.Destination)
	}
	if string(evt.Payload) != string(payload) {
		t.Errorf("expected payload %q, got %q", string(payload), string(evt.Payload))
	}
	if evt.Headers["Content-Type"] != "application/json" {
		t.Error("expected Content-Type header")
	}
	if evt.Status != EventStatusQueued {
		t.Errorf("expected status %q, got %q", EventStatusQueued, evt.Status)
	}
	if evt.Attempts != 0 {
		t.Errorf("expected 0 attempts, got %d", evt.Attempts)
	}
	if evt.MaxAttempts != 10 {
		t.Errorf("expected max attempts 10, got %d", evt.MaxAttempts)
	}
	if evt.NextAttemptAt == nil {
		t.Error("expected non-nil NextAttemptAt")
	}
	if evt.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if evt.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
}

func TestNewEvent_NilHeaders(t *testing.T) {
	evt := NewEvent("key", "https://example.com", json.RawMessage(`{}`), nil)

	if evt.Headers != nil {
		t.Error("expected nil headers")
	}
}

func TestNewEventForEndpoint(t *testing.T) {
	clientID := "client-123"
	eventType := "order.created"
	idempotencyKey := "order-123"
	endpoint := Endpoint{
		ID:               uuid.New(),
		ClientID:         clientID,
		URL:              "https://webhook.example.com/events",
		EventTypes:       []string{"order.created"},
		Status:           EndpointStatusActive,
		MaxRetries:       15,
		RetryBackoffMs:   500,
		RetryBackoffMax:  3600000,
		RetryBackoffMult: 1.5,
		TimeoutMs:        10000,
		CustomHeaders:    map[string]string{"X-Source": "relay"},
	}
	payload := json.RawMessage(`{"order_id": 456}`)
	headers := map[string]string{"Content-Type": "application/json"}

	evt := NewEventForEndpoint(clientID, eventType, idempotencyKey, endpoint, payload, headers)

	if evt.ClientID != clientID {
		t.Errorf("expected client ID %q, got %q", clientID, evt.ClientID)
	}
	if evt.EventType != eventType {
		t.Errorf("expected event type %q, got %q", eventType, evt.EventType)
	}
	if evt.EndpointID == nil || *evt.EndpointID != endpoint.ID {
		t.Error("expected endpoint ID to match")
	}
	if evt.Destination != endpoint.URL {
		t.Errorf("expected destination %q, got %q", endpoint.URL, evt.Destination)
	}
	if evt.MaxAttempts != endpoint.MaxRetries {
		t.Errorf("expected max attempts %d, got %d", endpoint.MaxRetries, evt.MaxAttempts)
	}
	// Check merged headers - event headers override endpoint headers
	if evt.Headers["X-Source"] != "relay" {
		t.Error("expected X-Source header from endpoint")
	}
	if evt.Headers["Content-Type"] != "application/json" {
		t.Error("expected Content-Type header from event")
	}
}

func TestNewEventForEndpoint_HeaderMerging(t *testing.T) {
	endpoint := Endpoint{
		ID:            uuid.New(),
		URL:           "https://example.com",
		MaxRetries:    10,
		CustomHeaders: map[string]string{"X-Custom": "endpoint-value", "X-Override": "endpoint"},
	}
	eventHeaders := map[string]string{"X-Override": "event", "X-Event": "value"}

	evt := NewEventForEndpoint("client", "type", "key", endpoint, json.RawMessage(`{}`), eventHeaders)

	// Event headers should override endpoint headers
	if evt.Headers["X-Override"] != "event" {
		t.Errorf("expected event header to override endpoint, got %q", evt.Headers["X-Override"])
	}
	if evt.Headers["X-Custom"] != "endpoint-value" {
		t.Error("expected endpoint header to be preserved")
	}
	if evt.Headers["X-Event"] != "value" {
		t.Error("expected event header to be included")
	}
}

func TestEvent_ShouldRetry(t *testing.T) {
	tests := []struct {
		name        string
		attempts    int
		maxAttempts int
		status      EventStatus
		want        bool
	}{
		{"zero attempts", 0, 10, EventStatusQueued, true},
		{"under max", 5, 10, EventStatusFailed, true},
		{"at max", 10, 10, EventStatusFailed, false},
		{"over max", 11, 10, EventStatusFailed, false},
		{"dead status", 5, 10, EventStatusDead, false},
		{"one below max", 9, 10, EventStatusFailed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			evt := Event{
				Attempts:    tt.attempts,
				MaxAttempts: tt.maxAttempts,
				Status:      tt.status,
			}
			if got := evt.ShouldRetry(); got != tt.want {
				t.Errorf("ShouldRetry() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEvent_MarkDelivering(t *testing.T) {
	original := NewEvent("key", "https://example.com", json.RawMessage(`{}`), nil)
	original.Attempts = 3

	delivering := original.MarkDelivering()

	if delivering.Status != EventStatusDelivering {
		t.Errorf("expected status %q, got %q", EventStatusDelivering, delivering.Status)
	}
	if delivering.ID != original.ID {
		t.Error("ID should be preserved")
	}
	if delivering.Attempts != original.Attempts {
		t.Error("Attempts should be preserved")
	}
	if delivering.UpdatedAt.Before(original.CreatedAt) {
		t.Error("UpdatedAt should not be before CreatedAt")
	}
	// Original should be unchanged (immutability)
	if original.Status != EventStatusQueued {
		t.Error("original event should be unchanged")
	}
}

func TestEvent_MarkDelivered(t *testing.T) {
	original := NewEvent("key", "https://example.com", json.RawMessage(`{}`), nil)
	delivering := original.MarkDelivering()

	delivered := delivering.MarkDelivered()

	if delivered.Status != EventStatusDelivered {
		t.Errorf("expected status %q, got %q", EventStatusDelivered, delivered.Status)
	}
	if delivered.DeliveredAt == nil {
		t.Error("DeliveredAt should be set")
	}
	if delivered.NextAttemptAt != nil {
		t.Error("NextAttemptAt should be nil for delivered events")
	}
}

func TestEvent_MarkFailed(t *testing.T) {
	original := NewEvent("key", "https://example.com", json.RawMessage(`{}`), nil)
	nextAttempt := time.Now().Add(5 * time.Minute)

	failed := original.MarkFailed(nextAttempt)

	if failed.Status != EventStatusFailed {
		t.Errorf("expected status %q, got %q", EventStatusFailed, failed.Status)
	}
	if failed.NextAttemptAt == nil {
		t.Error("NextAttemptAt should be set")
	}
	if !failed.NextAttemptAt.Equal(nextAttempt) {
		t.Errorf("expected NextAttemptAt %v, got %v", nextAttempt, *failed.NextAttemptAt)
	}
}

func TestEvent_MarkDead(t *testing.T) {
	original := NewEvent("key", "https://example.com", json.RawMessage(`{}`), nil)
	original.Attempts = 10

	dead := original.MarkDead()

	if dead.Status != EventStatusDead {
		t.Errorf("expected status %q, got %q", EventStatusDead, dead.Status)
	}
	if dead.NextAttemptAt != nil {
		t.Error("NextAttemptAt should be nil for dead events")
	}
}

func TestEvent_IncrementAttempts(t *testing.T) {
	original := NewEvent("key", "https://example.com", json.RawMessage(`{}`), nil)
	if original.Attempts != 0 {
		t.Fatal("expected initial attempts to be 0")
	}

	incremented := original.IncrementAttempts()
	if incremented.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", incremented.Attempts)
	}

	// Original unchanged
	if original.Attempts != 0 {
		t.Error("original should be unchanged")
	}

	// Chain increments
	incremented2 := incremented.IncrementAttempts()
	if incremented2.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", incremented2.Attempts)
	}
}

func TestEvent_IsTerminal(t *testing.T) {
	tests := []struct {
		status   EventStatus
		terminal bool
	}{
		{EventStatusQueued, false},
		{EventStatusDelivering, false},
		{EventStatusFailed, false},
		{EventStatusDelivered, true},
		{EventStatusDead, true},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			evt := Event{Status: tt.status}
			if got := evt.IsTerminal(); got != tt.terminal {
				t.Errorf("IsTerminal() = %v, want %v", got, tt.terminal)
			}
		})
	}
}

func TestEvent_Replay(t *testing.T) {
	original := NewEvent("key", "https://example.com", json.RawMessage(`{"data": "test"}`), map[string]string{"X-Test": "value"})
	original = original.IncrementAttempts().IncrementAttempts().IncrementAttempts()
	original = original.MarkDead()

	replayed := original.Replay()

	if replayed.Status != EventStatusQueued {
		t.Errorf("expected status %q, got %q", EventStatusQueued, replayed.Status)
	}
	if replayed.Attempts != 0 {
		t.Errorf("expected 0 attempts, got %d", replayed.Attempts)
	}
	if replayed.DeliveredAt != nil {
		t.Error("DeliveredAt should be nil")
	}
	if replayed.NextAttemptAt == nil {
		t.Error("NextAttemptAt should be set")
	}
	// Preserved fields
	if replayed.ID != original.ID {
		t.Error("ID should be preserved")
	}
	if replayed.IdempotencyKey != original.IdempotencyKey {
		t.Error("IdempotencyKey should be preserved")
	}
	if replayed.Destination != original.Destination {
		t.Error("Destination should be preserved")
	}
	if string(replayed.Payload) != string(original.Payload) {
		t.Error("Payload should be preserved")
	}
	if replayed.MaxAttempts != original.MaxAttempts {
		t.Error("MaxAttempts should be preserved")
	}
}

func TestEventStatus_Values(t *testing.T) {
	// Ensure status values match expected strings for database/API consistency
	tests := []struct {
		status EventStatus
		want   string
	}{
		{EventStatusQueued, "queued"},
		{EventStatusDelivering, "delivering"},
		{EventStatusDelivered, "delivered"},
		{EventStatusFailed, "failed"},
		{EventStatusDead, "dead"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("expected %q, got %q", tt.want, string(tt.status))
			}
		})
	}
}

func TestEvent_Immutability(t *testing.T) {
	// Verify that all mutation methods return new instances
	original := NewEvent("key", "https://example.com", json.RawMessage(`{}`), map[string]string{"X-Test": "original"})
	originalID := original.ID
	originalStatus := original.Status
	originalAttempts := original.Attempts

	_ = original.MarkDelivering()
	_ = original.MarkDelivered()
	_ = original.MarkFailed(time.Now())
	_ = original.MarkDead()
	_ = original.IncrementAttempts()
	_ = original.Replay()

	// Original should be completely unchanged
	if original.ID != originalID {
		t.Error("ID was mutated")
	}
	if original.Status != originalStatus {
		t.Error("Status was mutated")
	}
	if original.Attempts != originalAttempts {
		t.Error("Attempts was mutated")
	}
}

func TestEvent_StateTransitions(t *testing.T) {
	// Test realistic state transition flow
	evt := NewEvent("order-123", "https://webhook.example.com", json.RawMessage(`{"order_id": 123}`), nil)

	// Initial state
	if evt.Status != EventStatusQueued {
		t.Fatalf("expected initial status queued, got %s", evt.Status)
	}

	// Worker picks up event
	evt = evt.MarkDelivering()
	evt = evt.IncrementAttempts()
	if evt.Status != EventStatusDelivering {
		t.Errorf("expected delivering, got %s", evt.Status)
	}
	if evt.Attempts != 1 {
		t.Errorf("expected 1 attempt, got %d", evt.Attempts)
	}

	// First attempt fails
	evt = evt.MarkFailed(time.Now().Add(time.Second))
	if evt.Status != EventStatusFailed {
		t.Errorf("expected failed, got %s", evt.Status)
	}
	if !evt.ShouldRetry() {
		t.Error("should be retryable")
	}

	// Retry
	evt = evt.MarkDelivering()
	evt = evt.IncrementAttempts()
	if evt.Attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", evt.Attempts)
	}

	// Success
	evt = evt.MarkDelivered()
	if evt.Status != EventStatusDelivered {
		t.Errorf("expected delivered, got %s", evt.Status)
	}
	if evt.DeliveredAt == nil {
		t.Error("DeliveredAt should be set")
	}
	if evt.IsTerminal() != true {
		t.Error("delivered event should be terminal")
	}
}

func TestEvent_ExhaustRetries(t *testing.T) {
	evt := NewEvent("key", "https://example.com", json.RawMessage(`{}`), nil)
	evt.MaxAttempts = 3

	// Exhaust all retries
	for i := 0; i < 3; i++ {
		evt = evt.IncrementAttempts()
		evt = evt.MarkFailed(time.Now().Add(time.Second))
	}

	if evt.ShouldRetry() {
		t.Error("should not be retryable after exhausting attempts")
	}

	// Mark as dead
	evt = evt.MarkDead()
	if evt.Status != EventStatusDead {
		t.Errorf("expected dead, got %s", evt.Status)
	}
	if !evt.IsTerminal() {
		t.Error("dead event should be terminal")
	}
}
