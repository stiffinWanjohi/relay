package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewEndpoint(t *testing.T) {
	clientID := "client-123"
	url := "https://webhook.example.com/events"
	eventTypes := []string{"order.created", "order.updated"}

	ep := NewEndpoint(clientID, url, eventTypes)

	if ep.ID == uuid.Nil {
		t.Error("expected non-nil UUID")
	}
	if ep.ClientID != clientID {
		t.Errorf("expected client ID %q, got %q", clientID, ep.ClientID)
	}
	if ep.URL != url {
		t.Errorf("expected URL %q, got %q", url, ep.URL)
	}
	if len(ep.EventTypes) != 2 {
		t.Errorf("expected 2 event types, got %d", len(ep.EventTypes))
	}
	if ep.Status != EndpointStatusActive {
		t.Errorf("expected status %q, got %q", EndpointStatusActive, ep.Status)
	}

	// Check defaults
	if ep.MaxRetries != DefaultMaxRetries {
		t.Errorf("expected max retries %d, got %d", DefaultMaxRetries, ep.MaxRetries)
	}
	if ep.RetryBackoffMs != DefaultRetryBackoffMs {
		t.Errorf("expected retry backoff %d, got %d", DefaultRetryBackoffMs, ep.RetryBackoffMs)
	}
	if ep.RetryBackoffMax != DefaultRetryBackoffMax {
		t.Errorf("expected retry backoff max %d, got %d", DefaultRetryBackoffMax, ep.RetryBackoffMax)
	}
	if ep.RetryBackoffMult != DefaultRetryBackoffMult {
		t.Errorf("expected retry backoff mult %f, got %f", DefaultRetryBackoffMult, ep.RetryBackoffMult)
	}
	if ep.TimeoutMs != DefaultTimeoutMs {
		t.Errorf("expected timeout %d, got %d", DefaultTimeoutMs, ep.TimeoutMs)
	}
	if ep.CircuitThreshold != DefaultCircuitThreshold {
		t.Errorf("expected circuit threshold %d, got %d", DefaultCircuitThreshold, ep.CircuitThreshold)
	}
	if ep.CircuitResetMs != DefaultCircuitResetMs {
		t.Errorf("expected circuit reset %d, got %d", DefaultCircuitResetMs, ep.CircuitResetMs)
	}
	if ep.CustomHeaders == nil {
		t.Error("expected non-nil custom headers map")
	}
	if ep.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
	if ep.UpdatedAt.IsZero() {
		t.Error("expected non-zero UpdatedAt")
	}
}

func TestEndpoint_IsActive(t *testing.T) {
	tests := []struct {
		status EndpointStatus
		active bool
	}{
		{EndpointStatusActive, true},
		{EndpointStatusPaused, false},
		{EndpointStatusDisabled, false},
	}

	for _, tt := range tests {
		t.Run(string(tt.status), func(t *testing.T) {
			ep := Endpoint{Status: tt.status}
			if got := ep.IsActive(); got != tt.active {
				t.Errorf("IsActive() = %v, want %v", got, tt.active)
			}
		})
	}
}

func TestEndpoint_SubscribesTo(t *testing.T) {
	tests := []struct {
		name       string
		eventTypes []string
		eventType  string
		want       bool
	}{
		{"exact match", []string{"order.created"}, "order.created", true},
		{"no match", []string{"order.created"}, "order.updated", false},
		{"multiple types match", []string{"order.created", "order.updated"}, "order.updated", true},
		{"wildcard", []string{"*"}, "anything.here", true},
		{"wildcard with others", []string{"order.created", "*"}, "user.signup", true},
		{"empty types (wildcard)", []string{}, "any.event", true},
		{"nil types (wildcard)", nil, "any.event", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ep := Endpoint{EventTypes: tt.eventTypes}
			if got := ep.SubscribesTo(tt.eventType); got != tt.want {
				t.Errorf("SubscribesTo(%q) = %v, want %v", tt.eventType, got, tt.want)
			}
		})
	}
}

func TestEndpoint_Pause(t *testing.T) {
	original := NewEndpoint("client", "https://example.com", []string{"order.created"})
	original.Description = "Test endpoint"

	paused := original.Pause()

	if paused.Status != EndpointStatusPaused {
		t.Errorf("expected status %q, got %q", EndpointStatusPaused, paused.Status)
	}
	// Check immutability
	if original.Status != EndpointStatusActive {
		t.Error("original should be unchanged")
	}
	// Check fields preserved
	if paused.ID != original.ID {
		t.Error("ID should be preserved")
	}
	if paused.Description != original.Description {
		t.Error("Description should be preserved")
	}
	if paused.URL != original.URL {
		t.Error("URL should be preserved")
	}
}

func TestEndpoint_Resume(t *testing.T) {
	original := NewEndpoint("client", "https://example.com", []string{"order.created"})
	paused := original.Pause()

	resumed := paused.Resume()

	if resumed.Status != EndpointStatusActive {
		t.Errorf("expected status %q, got %q", EndpointStatusActive, resumed.Status)
	}
	if paused.Status != EndpointStatusPaused {
		t.Error("paused should be unchanged")
	}
}

func TestEndpoint_Disable(t *testing.T) {
	original := NewEndpoint("client", "https://example.com", []string{"order.created"})

	disabled := original.Disable()

	if disabled.Status != EndpointStatusDisabled {
		t.Errorf("expected status %q, got %q", EndpointStatusDisabled, disabled.Status)
	}
	if original.Status != EndpointStatusActive {
		t.Error("original should be unchanged")
	}
}

func TestEndpoint_WithRetryConfig(t *testing.T) {
	original := NewEndpoint("client", "https://example.com", []string{"order.created"})

	configured := original.WithRetryConfig(20, 500, 7200000, 1.5)

	if configured.MaxRetries != 20 {
		t.Errorf("expected max retries 20, got %d", configured.MaxRetries)
	}
	if configured.RetryBackoffMs != 500 {
		t.Errorf("expected backoff ms 500, got %d", configured.RetryBackoffMs)
	}
	if configured.RetryBackoffMax != 7200000 {
		t.Errorf("expected backoff max 7200000, got %d", configured.RetryBackoffMax)
	}
	if configured.RetryBackoffMult != 1.5 {
		t.Errorf("expected backoff mult 1.5, got %f", configured.RetryBackoffMult)
	}
	// Original unchanged
	if original.MaxRetries != DefaultMaxRetries {
		t.Error("original should be unchanged")
	}
}

func TestEndpoint_WithTimeout(t *testing.T) {
	original := NewEndpoint("client", "https://example.com", []string{"order.created"})

	configured := original.WithTimeout(60000)

	if configured.TimeoutMs != 60000 {
		t.Errorf("expected timeout 60000, got %d", configured.TimeoutMs)
	}
	if original.TimeoutMs != DefaultTimeoutMs {
		t.Error("original should be unchanged")
	}
}

func TestEndpoint_WithRateLimit(t *testing.T) {
	original := NewEndpoint("client", "https://example.com", []string{"order.created"})

	configured := original.WithRateLimit(100)

	if configured.RateLimitPerSec != 100 {
		t.Errorf("expected rate limit 100, got %d", configured.RateLimitPerSec)
	}
	if original.RateLimitPerSec != 0 {
		t.Error("original should be unchanged")
	}
}

func TestEndpoint_GetTimeoutDuration(t *testing.T) {
	tests := []struct {
		timeoutMs int
		want      time.Duration
	}{
		{30000, 30 * time.Second},
		{1000, time.Second},
		{500, 500 * time.Millisecond},
		{60000, time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.want.String(), func(t *testing.T) {
			ep := Endpoint{TimeoutMs: tt.timeoutMs}
			if got := ep.GetTimeoutDuration(); got != tt.want {
				t.Errorf("GetTimeoutDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEndpoint_GetCircuitResetDuration(t *testing.T) {
	tests := []struct {
		resetMs int
		want    time.Duration
	}{
		{300000, 5 * time.Minute},
		{60000, time.Minute},
		{1000, time.Second},
	}

	for _, tt := range tests {
		t.Run(tt.want.String(), func(t *testing.T) {
			ep := Endpoint{CircuitResetMs: tt.resetMs}
			if got := ep.GetCircuitResetDuration(); got != tt.want {
				t.Errorf("GetCircuitResetDuration() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEndpointStatus_Values(t *testing.T) {
	tests := []struct {
		status EndpointStatus
		want   string
	}{
		{EndpointStatusActive, "active"},
		{EndpointStatusPaused, "paused"},
		{EndpointStatusDisabled, "disabled"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			if string(tt.status) != tt.want {
				t.Errorf("expected %q, got %q", tt.want, string(tt.status))
			}
		})
	}
}

func TestEndpoint_Immutability(t *testing.T) {
	original := NewEndpoint("client", "https://example.com", []string{"order.created"})
	originalID := original.ID
	originalStatus := original.Status
	originalMaxRetries := original.MaxRetries

	_ = original.Pause()
	_ = original.Resume()
	_ = original.Disable()
	_ = original.WithRetryConfig(20, 500, 7200000, 1.5)
	_ = original.WithTimeout(60000)
	_ = original.WithRateLimit(100)

	if original.ID != originalID {
		t.Error("ID was mutated")
	}
	if original.Status != originalStatus {
		t.Error("Status was mutated")
	}
	if original.MaxRetries != originalMaxRetries {
		t.Error("MaxRetries was mutated")
	}
}

func TestEndpoint_StateTransitions(t *testing.T) {
	ep := NewEndpoint("client", "https://example.com", []string{"*"})

	// Active -> Paused -> Active
	if !ep.IsActive() {
		t.Error("new endpoint should be active")
	}

	paused := ep.Pause()
	if paused.IsActive() {
		t.Error("paused endpoint should not be active")
	}

	resumed := paused.Resume()
	if !resumed.IsActive() {
		t.Error("resumed endpoint should be active")
	}

	// Active -> Disabled
	disabled := resumed.Disable()
	if disabled.IsActive() {
		t.Error("disabled endpoint should not be active")
	}
}

func TestEndpoint_ChainedConfiguration(t *testing.T) {
	ep := NewEndpoint("client", "https://example.com", []string{"order.*"}).
		WithRetryConfig(15, 200, 1800000, 2.5).
		WithTimeout(10000).
		WithRateLimit(50)

	if ep.MaxRetries != 15 {
		t.Errorf("expected max retries 15, got %d", ep.MaxRetries)
	}
	if ep.RetryBackoffMs != 200 {
		t.Errorf("expected backoff ms 200, got %d", ep.RetryBackoffMs)
	}
	if ep.TimeoutMs != 10000 {
		t.Errorf("expected timeout 10000, got %d", ep.TimeoutMs)
	}
	if ep.RateLimitPerSec != 50 {
		t.Errorf("expected rate limit 50, got %d", ep.RateLimitPerSec)
	}
}

func TestDefaultConstants(t *testing.T) {
	// Verify default constants are sensible
	if DefaultMaxRetries != 10 {
		t.Errorf("expected default max retries 10, got %d", DefaultMaxRetries)
	}
	if DefaultRetryBackoffMs != 1000 {
		t.Errorf("expected default retry backoff 1000ms, got %d", DefaultRetryBackoffMs)
	}
	if DefaultRetryBackoffMax != 86400000 { // 24 hours
		t.Errorf("expected default retry backoff max 86400000ms (24h), got %d", DefaultRetryBackoffMax)
	}
	if DefaultRetryBackoffMult != 2.0 {
		t.Errorf("expected default retry backoff mult 2.0, got %f", DefaultRetryBackoffMult)
	}
	if DefaultTimeoutMs != 30000 { // 30 seconds
		t.Errorf("expected default timeout 30000ms (30s), got %d", DefaultTimeoutMs)
	}
	if DefaultCircuitThreshold != 5 {
		t.Errorf("expected default circuit threshold 5, got %d", DefaultCircuitThreshold)
	}
	if DefaultCircuitResetMs != 300000 { // 5 minutes
		t.Errorf("expected default circuit reset 300000ms (5m), got %d", DefaultCircuitResetMs)
	}
}
