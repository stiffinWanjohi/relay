package delivery

import (
	"sync"
	"testing"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/logstream"
	"github.com/stiffinWanjohi/relay/internal/notification"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

func TestCircuitState_String(t *testing.T) {
	tests := []struct {
		name     string
		state    CircuitState
		expected string
	}{
		{"closed state", CircuitClosed, "closed"},
		{"open state", CircuitOpen, "open"},
		{"half-open state", CircuitHalfOpen, "half-open"},
		{"unknown state", CircuitState(99), "unknown"},
		{"negative state", CircuitState(-1), "unknown"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.state.String(); got != tt.expected {
				t.Errorf("CircuitState.String() = %v, want %v", got, tt.expected)
			}
		})
	}
}

func TestDefaultCircuitConfig(t *testing.T) {
	config := DefaultCircuitConfig()

	if config.FailureThreshold != 5 {
		t.Errorf("FailureThreshold = %d, want 5", config.FailureThreshold)
	}
	if config.SuccessThreshold != 3 {
		t.Errorf("SuccessThreshold = %d, want 3", config.SuccessThreshold)
	}
	if config.OpenDuration != 5*time.Minute {
		t.Errorf("OpenDuration = %v, want 5m", config.OpenDuration)
	}
}

func TestCircuitConfigFromEndpoint(t *testing.T) {
	defaultConfig := DefaultCircuitConfig()

	t.Run("nil endpoint returns default", func(t *testing.T) {
		config := CircuitConfigFromEndpoint(nil, defaultConfig)
		if config != defaultConfig {
			t.Error("expected default config for nil endpoint")
		}
	})

	t.Run("endpoint with custom threshold", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			CircuitThreshold: 10,
		}
		config := CircuitConfigFromEndpoint(endpoint, defaultConfig)
		if config.FailureThreshold != 10 {
			t.Errorf("FailureThreshold = %d, want 10", config.FailureThreshold)
		}
		if config.SuccessThreshold != defaultConfig.SuccessThreshold {
			t.Error("SuccessThreshold should remain default")
		}
	})

	t.Run("endpoint with custom reset time", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			CircuitResetMs: 60000, // 1 minute
		}
		config := CircuitConfigFromEndpoint(endpoint, defaultConfig)
		if config.OpenDuration != time.Minute {
			t.Errorf("OpenDuration = %v, want 1m", config.OpenDuration)
		}
	})

	t.Run("endpoint with zero threshold uses default", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			CircuitThreshold: 0,
		}
		config := CircuitConfigFromEndpoint(endpoint, defaultConfig)
		if config.FailureThreshold != defaultConfig.FailureThreshold {
			t.Error("zero threshold should use default")
		}
	})

	t.Run("endpoint with zero reset uses default", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			CircuitResetMs: 0,
		}
		config := CircuitConfigFromEndpoint(endpoint, defaultConfig)
		if config.OpenDuration != defaultConfig.OpenDuration {
			t.Error("zero reset should use default")
		}
	})

	t.Run("endpoint with both custom values", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			CircuitThreshold: 15,
			CircuitResetMs:   120000, // 2 minutes
		}
		config := CircuitConfigFromEndpoint(endpoint, defaultConfig)
		if config.FailureThreshold != 15 {
			t.Errorf("FailureThreshold = %d, want 15", config.FailureThreshold)
		}
		if config.OpenDuration != 2*time.Minute {
			t.Errorf("OpenDuration = %v, want 2m", config.OpenDuration)
		}
	})
}

func TestNewCircuitBreaker(t *testing.T) {
	config := DefaultCircuitConfig()
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	if cb.config != config {
		t.Error("config not set correctly")
	}
	if cb.circuits == nil {
		t.Error("circuits map not initialized")
	}
	if cb.cleanupStop == nil {
		t.Error("cleanupStop channel not initialized")
	}
	if cb.ttl != defaultCircuitTTL {
		t.Errorf("ttl = %v, want %v", cb.ttl, defaultCircuitTTL)
	}
}

func TestCircuitBreaker_WithMetrics(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	result := cb.WithMetrics(metrics)

	if result != cb {
		t.Error("WithMetrics should return same instance")
	}
	if cb.metrics != metrics {
		t.Error("metrics not set correctly")
	}
}

func TestCircuitBreaker_Stop(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())

	// Stop should close the channel without blocking
	done := make(chan struct{})
	go func() {
		cb.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Stop() blocked for too long")
	}
}

func TestCircuitBreaker_IsOpen_NewDestination(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// New destination should not be open
	if cb.IsOpen("https://example.com") {
		t.Error("new destination should not be open")
	}
}

func TestCircuitBreaker_IsOpen_ClosedCircuit(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	dest := "https://example.com"

	// Record a failure (but not enough to open)
	cb.RecordFailure(dest)

	if cb.IsOpen(dest) {
		t.Error("circuit should not be open with only 1 failure")
	}
}

func TestCircuitBreaker_IsOpen_OpenCircuit(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour, // Long duration so it stays open
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)

	if !cb.IsOpen(dest) {
		t.Error("circuit should be open after reaching threshold")
	}
}

func TestCircuitBreaker_IsOpen_TransitionToHalfOpen(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)
	if !cb.IsOpen(dest) {
		t.Error("circuit should be open")
	}

	// Wait for open duration to pass
	time.Sleep(20 * time.Millisecond)

	// IsOpen should now return false and transition to half-open
	if cb.IsOpen(dest) {
		t.Error("circuit should allow request through (half-open)")
	}

	// Verify state is half-open
	if state := cb.GetState(dest); state != CircuitHalfOpen {
		t.Errorf("state = %v, want half-open", state)
	}
}

func TestCircuitBreaker_RecordSuccess_NoCircuit(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// Recording success for non-existent circuit should not panic
	cb.RecordSuccess("https://example.com")

	// Circuit should not exist
	stats := cb.Stats()
	if len(stats.Circuits) != 0 {
		t.Error("no circuit should be created for success on new destination")
	}
}

func TestCircuitBreaker_RecordSuccess_ClosedCircuit(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 5,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Create circuit with some failures
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)

	// Record success - should reset failures
	cb.RecordSuccess(dest)

	// Verify failures were reset by checking we need full threshold again
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)
	if cb.IsOpen(dest) {
		t.Error("circuit should not be open (only 4 failures after reset)")
	}

	cb.RecordFailure(dest)
	if !cb.IsOpen(dest) {
		t.Error("circuit should be open after 5 failures")
	}
}

func TestCircuitBreaker_RecordSuccess_HalfOpenCircuit(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 2,
		OpenDuration:     10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)

	// Wait for half-open
	time.Sleep(20 * time.Millisecond)
	cb.IsOpen(dest) // Trigger transition

	// First success - still half-open
	cb.RecordSuccess(dest)
	if state := cb.GetState(dest); state != CircuitHalfOpen {
		t.Errorf("state = %v, want half-open after 1 success", state)
	}

	// Second success - should close
	cb.RecordSuccess(dest)
	if state := cb.GetState(dest); state != CircuitClosed {
		t.Errorf("state = %v, want closed after 2 successes", state)
	}
}

func TestCircuitBreaker_RecordSuccess_WithMetrics(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	cb.WithMetrics(metrics)

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)

	// Wait for half-open
	time.Sleep(20 * time.Millisecond)
	cb.IsOpen(dest)

	// Success should trigger metrics (state change from half-open to closed)
	cb.RecordSuccess(dest)

	// Verify state changed
	if state := cb.GetState(dest); state != CircuitClosed {
		t.Errorf("state = %v, want closed", state)
	}
}

func TestCircuitBreaker_RecordFailure_NewDestination(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	dest := "https://example.com"

	cb.RecordFailure(dest)

	stats := cb.Stats()
	if len(stats.Circuits) != 1 {
		t.Error("circuit should be created")
	}

	info, ok := stats.Circuits[dest]
	if !ok {
		t.Fatal("circuit not found in stats")
	}
	if info.Failures != 1 {
		t.Errorf("failures = %d, want 1", info.Failures)
	}
	if info.ConsecutiveErrors != 1 {
		t.Errorf("consecutive errors = %d, want 1", info.ConsecutiveErrors)
	}
}

func TestCircuitBreaker_RecordFailure_OpensCircuit(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Failures below threshold
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)
	if state := cb.GetState(dest); state != CircuitClosed {
		t.Errorf("state = %v, want closed after 2 failures", state)
	}

	// Third failure opens circuit
	cb.RecordFailure(dest)
	if state := cb.GetState(dest); state != CircuitOpen {
		t.Errorf("state = %v, want open after 3 failures", state)
	}
}

func TestCircuitBreaker_RecordFailure_HalfOpenToOpen(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 3,
		OpenDuration:     10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)

	// Wait for half-open
	time.Sleep(20 * time.Millisecond)
	cb.IsOpen(dest) // Trigger transition to half-open

	if state := cb.GetState(dest); state != CircuitHalfOpen {
		t.Errorf("state = %v, want half-open", state)
	}

	// Any failure in half-open returns to open
	cb.RecordFailure(dest)
	if state := cb.GetState(dest); state != CircuitOpen {
		t.Errorf("state = %v, want open after failure in half-open", state)
	}
}

func TestCircuitBreaker_RecordFailure_WithMetrics(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	cb.WithMetrics(metrics)

	dest := "https://example.com"

	// Failure that opens circuit should trigger metrics
	cb.RecordFailure(dest)

	if state := cb.GetState(dest); state != CircuitOpen {
		t.Errorf("state = %v, want open", state)
	}
}

func TestCircuitBreaker_GetState_NewDestination(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	state := cb.GetState("https://example.com")
	if state != CircuitClosed {
		t.Errorf("state = %v, want closed for new destination", state)
	}
}

func TestCircuitBreaker_GetState_TransitionsOpenToHalfOpen(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)
	if state := cb.GetState(dest); state != CircuitOpen {
		t.Errorf("state = %v, want open", state)
	}

	// Wait for open duration
	time.Sleep(20 * time.Millisecond)

	// GetState should also trigger transition
	if state := cb.GetState(dest); state != CircuitHalfOpen {
		t.Errorf("state = %v, want half-open", state)
	}
}

func TestCircuitBreaker_Reset(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)
	if !cb.IsOpen(dest) {
		t.Error("circuit should be open")
	}

	// Reset
	cb.Reset(dest)

	// Should be closed
	if cb.IsOpen(dest) {
		t.Error("circuit should not be open after reset")
	}

	// Should not appear in stats
	stats := cb.Stats()
	if _, ok := stats.Circuits[dest]; ok {
		t.Error("circuit should be removed from stats after reset")
	}
}

func TestCircuitBreaker_Stats(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest1 := "https://example1.com"
	dest2 := "https://example2.com"
	dest3 := "https://example3.com"

	// Create different states
	cb.RecordFailure(dest1)
	cb.RecordFailure(dest1) // Open

	cb.RecordFailure(dest2) // Closed with 1 failure

	cb.RecordFailure(dest3)
	cb.RecordSuccess(dest3) // Closed with 0 failures

	stats := cb.Stats()

	if len(stats.Circuits) != 3 {
		t.Errorf("expected 3 circuits, got %d", len(stats.Circuits))
	}

	info1 := stats.Circuits[dest1]
	if info1.State != "open" {
		t.Errorf("dest1 state = %s, want open", info1.State)
	}
	if info1.Failures != 2 {
		t.Errorf("dest1 failures = %d, want 2", info1.Failures)
	}

	info2 := stats.Circuits[dest2]
	if info2.State != "closed" {
		t.Errorf("dest2 state = %s, want closed", info2.State)
	}
	if info2.Failures != 1 {
		t.Errorf("dest2 failures = %d, want 1", info2.Failures)
	}

	info3 := stats.Circuits[dest3]
	if info3.State != "closed" {
		t.Errorf("dest3 state = %s, want closed", info3.State)
	}
	if info3.Failures != 0 {
		t.Errorf("dest3 failures = %d, want 0", info3.Failures)
	}
}

func TestCircuitBreaker_Cleanup(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// Set very short TTL for testing
	cb.ttl = 10 * time.Millisecond

	dest := "https://example.com"

	// Create a closed circuit
	cb.RecordFailure(dest)
	cb.RecordSuccess(dest)

	// Verify circuit exists
	stats := cb.Stats()
	if len(stats.Circuits) != 1 {
		t.Error("circuit should exist")
	}

	// Wait for TTL to pass and trigger cleanup
	time.Sleep(20 * time.Millisecond)
	cb.cleanup()

	// Circuit should be removed (it was closed and idle)
	stats = cb.Stats()
	if len(stats.Circuits) != 0 {
		t.Error("idle closed circuit should be cleaned up")
	}
}

func TestCircuitBreaker_Cleanup_KeepsOpenCircuits(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	cb.ttl = 10 * time.Millisecond

	dest := "https://example.com"

	// Create an open circuit
	cb.RecordFailure(dest)

	// Wait for TTL and cleanup
	time.Sleep(20 * time.Millisecond)
	cb.cleanup()

	// Open circuit should NOT be removed
	stats := cb.Stats()
	if len(stats.Circuits) != 1 {
		t.Error("open circuit should not be cleaned up")
	}
}

func TestCircuitBreaker_ConcurrentAccess(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	dest := "https://example.com"
	var wg sync.WaitGroup

	// Concurrent failures
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cb.RecordFailure(dest)
		}
	}()

	// Concurrent successes
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cb.RecordSuccess(dest)
		}
	}()

	// Concurrent state checks
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cb.IsOpen(dest)
			cb.GetState(dest)
		}
	}()

	// Concurrent stats
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			cb.Stats()
		}
	}()

	// Concurrent resets
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 10; i++ {
			cb.Reset(dest)
			time.Sleep(time.Millisecond)
		}
	}()

	wg.Wait()

	// Should complete without panic or deadlock
}

func TestCircuitBreaker_MultipleDestinations(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest1 := "https://example1.com"
	dest2 := "https://example2.com"

	// Open circuit for dest1 only
	cb.RecordFailure(dest1)

	if !cb.IsOpen(dest1) {
		t.Error("dest1 should be open")
	}
	if cb.IsOpen(dest2) {
		t.Error("dest2 should not be open")
	}

	// Now open dest2
	cb.RecordFailure(dest2)

	if !cb.IsOpen(dest2) {
		t.Error("dest2 should now be open")
	}

	// Reset dest1 only
	cb.Reset(dest1)

	if cb.IsOpen(dest1) {
		t.Error("dest1 should not be open after reset")
	}
	if !cb.IsOpen(dest2) {
		t.Error("dest2 should still be open")
	}
}

func TestCircuitBreaker_LastAccessUpdated(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	dest := "https://example.com"

	// Create circuit
	cb.RecordFailure(dest)

	// Get initial access time
	cb.mu.Lock()
	c := cb.circuits[dest]
	initialAccess := c.lastAccess
	cb.mu.Unlock()

	time.Sleep(10 * time.Millisecond)

	// IsOpen should update lastAccess
	cb.IsOpen(dest)

	cb.mu.Lock()
	afterIsOpen := cb.circuits[dest].lastAccess
	cb.mu.Unlock()

	if !afterIsOpen.After(initialAccess) {
		t.Error("lastAccess should be updated after IsOpen")
	}

	time.Sleep(10 * time.Millisecond)

	// RecordSuccess should update lastAccess
	cb.RecordSuccess(dest)

	cb.mu.Lock()
	afterSuccess := cb.circuits[dest].lastAccess
	cb.mu.Unlock()

	if !afterSuccess.After(afterIsOpen) {
		t.Error("lastAccess should be updated after RecordSuccess")
	}

	time.Sleep(10 * time.Millisecond)

	// RecordFailure should update lastAccess
	cb.RecordFailure(dest)

	cb.mu.Lock()
	afterFailure := cb.circuits[dest].lastAccess
	cb.mu.Unlock()

	if !afterFailure.After(afterSuccess) {
		t.Error("lastAccess should be updated after RecordFailure")
	}
}

func TestCircuitInfo_Fields(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 3,
		SuccessThreshold: 2,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// Record some failures
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)

	stats := cb.Stats()
	info := stats.Circuits[dest]

	if info.State != "closed" {
		t.Errorf("State = %s, want closed", info.State)
	}
	if info.Failures != 2 {
		t.Errorf("Failures = %d, want 2", info.Failures)
	}
	if info.Successes != 0 {
		t.Errorf("Successes = %d, want 0", info.Successes)
	}
	if info.ConsecutiveErrors != 2 {
		t.Errorf("ConsecutiveErrors = %d, want 2", info.ConsecutiveErrors)
	}
	if info.LastStateChange.IsZero() {
		t.Error("LastStateChange should not be zero")
	}
}

// Test the cleanup goroutine actually runs
func TestCircuitBreaker_CleanupLoop(t *testing.T) {
	cb := &CircuitBreaker{
		config:      DefaultCircuitConfig(),
		circuits:    make(map[string]*circuit),
		ttl:         10 * time.Millisecond,
		cleanupStop: make(chan struct{}),
	}

	// Create a circuit that should be cleaned up
	cb.circuits["test"] = &circuit{
		state:           CircuitClosed,
		lastAccess:      time.Now().Add(-time.Hour), // Old access time
		lastStateChange: time.Now(),
	}

	// Start cleanup loop with very short interval for testing
	go func() {
		ticker := time.NewTicker(5 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-cb.cleanupStop:
				return
			case <-ticker.C:
				cb.cleanup()
			}
		}
	}()

	// Wait for cleanup to run
	time.Sleep(20 * time.Millisecond)

	cb.Stop()

	// Verify cleanup happened
	cb.mu.Lock()
	count := len(cb.circuits)
	cb.mu.Unlock()

	if count != 0 {
		t.Error("cleanup loop should have removed the idle circuit")
	}
}

func TestCircuitBreaker_WithNotifier(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// Create a mock notification service
	notifier := &notification.Service{}

	result := cb.WithNotifier(notifier, true, true)

	if result != cb {
		t.Error("WithNotifier should return same instance")
	}
	if cb.notifier != notifier {
		t.Error("notifier not set correctly")
	}
	if !cb.notifyTrip {
		t.Error("notifyTrip should be true")
	}
	if !cb.notifyRecover {
		t.Error("notifyRecover should be true")
	}
}

func TestCircuitBreaker_WithNotifier_DisabledNotifications(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	notifier := &notification.Service{}

	cb.WithNotifier(notifier, false, false)

	if cb.notifyTrip {
		t.Error("notifyTrip should be false")
	}
	if cb.notifyRecover {
		t.Error("notifyRecover should be false")
	}
}

func TestCircuitBreaker_WithDeliveryLogger(t *testing.T) {
	cb := NewCircuitBreaker(DefaultCircuitConfig())
	defer cb.Stop()

	// Create a mock delivery logger (nil hub is fine for this test)
	logger := logstream.NewDeliveryLogger(nil)

	result := cb.WithDeliveryLogger(logger)

	if result != cb {
		t.Error("WithDeliveryLogger should return same instance")
	}
	if cb.deliveryLogger != logger {
		t.Error("deliveryLogger not set correctly")
	}
}

func TestCircuitBreaker_RecordFailure_WithDeliveryLogger(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	// Set up delivery logger with nil hub (safe for testing)
	logger := logstream.NewDeliveryLogger(nil)
	cb.WithDeliveryLogger(logger)

	dest := "https://example.com"

	// Failure that opens circuit should log to delivery stream
	cb.RecordFailure(dest)

	if state := cb.GetState(dest); state != CircuitOpen {
		t.Errorf("state = %v, want open", state)
	}
}

func TestCircuitBreaker_RecordSuccess_WithDeliveryLogger(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	// Set up delivery logger with nil hub (safe for testing)
	logger := logstream.NewDeliveryLogger(nil)
	cb.WithDeliveryLogger(logger)

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)

	// Wait for half-open
	time.Sleep(20 * time.Millisecond)
	cb.IsOpen(dest)

	// Success should trigger delivery logging (state change from half-open to closed)
	cb.RecordSuccess(dest)

	// Verify state changed
	if state := cb.GetState(dest); state != CircuitClosed {
		t.Errorf("state = %v, want closed", state)
	}
}

func TestCircuitBreaker_RecordFailure_WithNotifier(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     time.Hour,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	// Set up notifier (nil service is safe for testing - it checks internally)
	notifier := &notification.Service{}
	cb.WithNotifier(notifier, true, true)

	dest := "https://example.com"

	// Failure that opens circuit should attempt notification
	cb.RecordFailure(dest)

	if state := cb.GetState(dest); state != CircuitOpen {
		t.Errorf("state = %v, want open", state)
	}
}

func TestCircuitBreaker_RecordSuccess_WithNotifier(t *testing.T) {
	config := CircuitConfig{
		FailureThreshold: 1,
		SuccessThreshold: 1,
		OpenDuration:     10 * time.Millisecond,
	}
	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	// Set up notifier
	notifier := &notification.Service{}
	cb.WithNotifier(notifier, true, true)

	dest := "https://example.com"

	// Open the circuit
	cb.RecordFailure(dest)

	// Wait for half-open
	time.Sleep(20 * time.Millisecond)
	cb.IsOpen(dest)

	// Success should attempt recovery notification
	cb.RecordSuccess(dest)

	// Verify state changed
	if state := cb.GetState(dest); state != CircuitClosed {
		t.Errorf("state = %v, want closed", state)
	}
}
