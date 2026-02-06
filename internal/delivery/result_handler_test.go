package delivery

import (
	"context"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/logstream"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

func TestNewResultHandler(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	handler := NewResultHandler(nil, circuit, retry, nil, nil, logger)

	assert.NotNil(t, handler)
	assert.Same(t, circuit, handler.circuit)
	assert.Same(t, retry, handler.retry)
	assert.Same(t, logger, handler.logger)
}

func TestResultHandler_HandleSuccess(t *testing.T) {
	t.Parallel()

	t.Run("marks event as delivered", func(t *testing.T) {
		t.Parallel()

		circuit := NewCircuitBreaker(DefaultCircuitConfig())
		defer circuit.Stop()
		retry := NewRetryPolicy()
		logger := slog.Default()

		// Test with nil store - handler is nil-safe
		handler := NewResultHandler(nil, circuit, retry, nil, nil, logger)

		evt := domain.Event{
			ID:          uuid.New(),
			Status:      domain.EventStatusQueued,
			Attempts:    1,
			ClientID:    "client-123",
			Destination: "https://example.com/webhook",
		}

		endpoint := &domain.Endpoint{
			ID: uuid.New(),
		}

		result := domain.DeliveryResult{
			Success:    true,
			StatusCode: 200,
			DurationMs: 150,
		}

		circuitKey := "example.com"
		resultEvt := handler.HandleSuccess(context.Background(), evt, endpoint, circuitKey, result)

		// Event should be marked as delivered
		assert.Equal(t, domain.EventStatusDelivered, resultEvt.Status)
	})
}

func TestResultHandler_HandleSuccess_RecordsCircuitSuccess(t *testing.T) {
	t.Parallel()

	// Use a short open duration so we can test recovery
	config := DefaultCircuitConfig()
	config.FailureThreshold = 2
	config.SuccessThreshold = 1 // Need only 1 success to close
	config.OpenDuration = 10 * time.Millisecond
	circuit := NewCircuitBreaker(config)
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	handler := NewResultHandler(nil, circuit, retry, nil, nil, logger)

	// Trip the circuit first
	circuitKey := "test-host.example.com"
	circuit.RecordFailure(circuitKey)
	circuit.RecordFailure(circuitKey)

	// Circuit should be open
	assert.Equal(t, CircuitOpen, circuit.GetState(circuitKey))

	// Wait for reset time to pass so circuit transitions to half-open
	time.Sleep(15 * time.Millisecond)

	// GetState should transition to half-open
	state := circuit.GetState(circuitKey)
	assert.Equal(t, CircuitHalfOpen, state, "circuit should be half-open after reset time")

	// Now handle success to recover
	evt := domain.Event{
		ID:          uuid.New(),
		Destination: "https://test-host.example.com/webhook",
	}
	result := domain.DeliveryResult{Success: true, StatusCode: 200}

	handler.HandleSuccess(context.Background(), evt, nil, circuitKey, result)

	// Circuit should now be closed after success in half-open state
	state = circuit.GetState(circuitKey)
	assert.Equal(t, CircuitClosed, state, "circuit should be closed after success")
}

func TestResultHandler_HandleFailure(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		event            domain.Event
		endpoint         *domain.Endpoint
		result           domain.DeliveryResult
		expectedStatus   domain.EventStatus
		expectRetryDelay bool
	}{
		{
			name: "schedules retry when attempts remaining",
			event: domain.Event{
				ID:          uuid.New(),
				Status:      domain.EventStatusQueued,
				Attempts:    1,
				MaxAttempts: 5,
				ClientID:    "client-123",
				Destination: "https://example.com/webhook",
			},
			endpoint: nil,
			result: domain.DeliveryResult{
				Success:    false,
				StatusCode: 500,
				DurationMs: 100,
			},
			expectedStatus:   domain.EventStatusFailed,
			expectRetryDelay: true,
		},
		{
			name: "marks dead when max attempts exceeded",
			event: domain.Event{
				ID:          uuid.New(),
				Status:      domain.EventStatusQueued,
				Attempts:    5,
				MaxAttempts: 5,
				ClientID:    "client-456",
				Destination: "https://example.com/webhook",
			},
			endpoint: nil,
			result: domain.DeliveryResult{
				Success:    false,
				StatusCode: 503,
				DurationMs: 50,
			},
			expectedStatus:   domain.EventStatusDead,
			expectRetryDelay: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			circuit := NewCircuitBreaker(DefaultCircuitConfig())
			defer circuit.Stop()
			retry := NewRetryPolicy()
			logger := slog.Default()

			handler := NewResultHandler(nil, circuit, retry, nil, nil, logger)

			circuitKey := "example.com"
			resultEvt, delay := handler.HandleFailure(context.Background(), tt.event, tt.endpoint, circuitKey, tt.result)

			assert.Equal(t, tt.expectedStatus, resultEvt.Status)

			if tt.expectRetryDelay {
				assert.Greater(t, delay, time.Duration(0), "should have positive retry delay")
			} else {
				assert.Equal(t, time.Duration(0), delay, "should have no retry delay")
			}
		})
	}
}

func TestResultHandler_HandleFailure_RecordsCircuitFailure(t *testing.T) {
	t.Parallel()

	config := DefaultCircuitConfig()
	config.FailureThreshold = 3
	circuit := NewCircuitBreaker(config)
	defer circuit.Stop()

	retry := NewRetryPolicy()
	logger := slog.Default()

	handler := NewResultHandler(nil, circuit, retry, nil, nil, logger)

	circuitKey := "failing-host.example.com"
	evt := domain.Event{
		ID:          uuid.New(),
		Destination: "https://failing-host.example.com/webhook",
		MaxAttempts: 10,
	}
	result := domain.DeliveryResult{
		Success:    false,
		StatusCode: 500,
	}

	// Record multiple failures to trip circuit
	for range 3 {
		handler.HandleFailure(context.Background(), evt, nil, circuitKey, result)
	}

	// Circuit should be open after threshold
	state := circuit.GetState(circuitKey)
	assert.Equal(t, CircuitOpen, state, "circuit should be open after failures")
}

func TestResultHandler_CreateDeliveryAttempt(t *testing.T) {
	t.Parallel()

	t.Run("creates success attempt", func(t *testing.T) {
		t.Parallel()

		handler := NewResultHandler(nil, nil, nil, nil, nil, slog.Default())

		evt := domain.Event{
			ID:       uuid.New(),
			Attempts: 1,
		}
		result := domain.DeliveryResult{
			Success:      true,
			StatusCode:   200,
			ResponseBody: `{"status": "ok"}`,
			DurationMs:   150,
		}

		// Should not panic even with nil store
		handler.CreateDeliveryAttempt(context.Background(), evt, result)
	})

	t.Run("creates failure attempt", func(t *testing.T) {
		t.Parallel()

		handler := NewResultHandler(nil, nil, nil, nil, nil, slog.Default())

		evt := domain.Event{
			ID:       uuid.New(),
			Attempts: 3,
		}
		result := domain.DeliveryResult{
			Success:      false,
			StatusCode:   500,
			ResponseBody: "Internal Server Error",
			DurationMs:   50,
		}

		// Should not panic even with nil store
		handler.CreateDeliveryAttempt(context.Background(), evt, result)
	})
}

func TestResultHandler_RetryPolicy(t *testing.T) {
	t.Parallel()

	t.Run("uses default retry policy", func(t *testing.T) {
		t.Parallel()

		circuit := NewCircuitBreaker(DefaultCircuitConfig())
		defer circuit.Stop()
		retry := NewRetryPolicy()

		handler := NewResultHandler(nil, circuit, retry, nil, nil, slog.Default())

		// Handler should use the retry policy
		assert.NotNil(t, handler.retry)
	})

	t.Run("respects endpoint-specific retry config", func(t *testing.T) {
		t.Parallel()

		circuit := NewCircuitBreaker(DefaultCircuitConfig())
		defer circuit.Stop()
		retry := NewRetryPolicy()

		handler := NewResultHandler(nil, circuit, retry, nil, nil, slog.Default())

		evt := domain.Event{
			ID:          uuid.New(),
			Status:      domain.EventStatusQueued,
			Attempts:    1,
			MaxAttempts: 10,
		}

		endpoint := &domain.Endpoint{
			ID:               uuid.New(),
			MaxRetries:       5,
			RetryBackoffMs:   1000, // 1 second initial backoff
			RetryBackoffMult: 2.0,
			RetryBackoffMax:  60000, // 60 seconds max
		}

		result := domain.DeliveryResult{
			Success:    false,
			StatusCode: 500,
		}

		_, delay := handler.HandleFailure(context.Background(), evt, endpoint, "test-host", result)

		// Should get a retry delay since attempts < endpoint max retries
		assert.Greater(t, delay, time.Duration(0))
	})
}

func TestResultHandler_HandleSuccess_WithMetrics(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	// Use NoopMetricsProvider for testing
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	handler := NewResultHandler(nil, circuit, retry, metrics, nil, logger)

	evt := domain.Event{
		ID:          uuid.New(),
		Status:      domain.EventStatusQueued,
		Attempts:    1,
		ClientID:    "client-123",
		Destination: "https://example.com/webhook",
	}

	endpoint := &domain.Endpoint{
		ID: uuid.New(),
	}

	result := domain.DeliveryResult{
		Success:    true,
		StatusCode: 200,
		DurationMs: 150,
	}

	resultEvt := handler.HandleSuccess(context.Background(), evt, endpoint, "example.com", result)

	// Should record metrics without error
	assert.Equal(t, domain.EventStatusDelivered, resultEvt.Status)
}

func TestResultHandler_HandleSuccess_WithDeliveryLogger(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	// Create delivery logger with nil hub (safe for testing)
	deliveryLogger := logstream.NewDeliveryLogger(nil)

	handler := NewResultHandler(nil, circuit, retry, nil, deliveryLogger, logger)

	evt := domain.Event{
		ID:          uuid.New(),
		Status:      domain.EventStatusQueued,
		Attempts:    1,
		ClientID:    "client-123",
		EventType:   "test.event",
		Destination: "https://example.com/webhook",
	}

	endpoint := &domain.Endpoint{
		ID: uuid.New(),
	}

	result := domain.DeliveryResult{
		Success:    true,
		StatusCode: 200,
		DurationMs: 150,
	}

	resultEvt := handler.HandleSuccess(context.Background(), evt, endpoint, "example.com", result)

	// Should log to delivery stream without error
	assert.Equal(t, domain.EventStatusDelivered, resultEvt.Status)
}

func TestResultHandler_HandleSuccess_WithNilEndpoint(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	deliveryLogger := logstream.NewDeliveryLogger(nil)

	handler := NewResultHandler(nil, circuit, retry, nil, deliveryLogger, logger)

	evt := domain.Event{
		ID:          uuid.New(),
		Status:      domain.EventStatusQueued,
		Attempts:    1,
		Destination: "https://example.com/webhook",
	}

	result := domain.DeliveryResult{
		Success:    true,
		StatusCode: 200,
		DurationMs: 150,
	}

	// Should handle nil endpoint gracefully
	resultEvt := handler.HandleSuccess(context.Background(), evt, nil, "example.com", result)

	assert.Equal(t, domain.EventStatusDelivered, resultEvt.Status)
}

func TestResultHandler_HandleFailure_WithMetrics(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	handler := NewResultHandler(nil, circuit, retry, metrics, nil, logger)

	evt := domain.Event{
		ID:          uuid.New(),
		Status:      domain.EventStatusQueued,
		Attempts:    1,
		MaxAttempts: 5,
		ClientID:    "client-123",
		Destination: "https://example.com/webhook",
	}

	result := domain.DeliveryResult{
		Success:    false,
		StatusCode: 500,
		DurationMs: 100,
	}

	resultEvt, delay := handler.HandleFailure(context.Background(), evt, nil, "example.com", result)

	// Should record metrics without error
	assert.Equal(t, domain.EventStatusFailed, resultEvt.Status)
	assert.Greater(t, delay, time.Duration(0))
}

func TestResultHandler_HandleFailure_WithDeliveryLogger(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	deliveryLogger := logstream.NewDeliveryLogger(nil)

	handler := NewResultHandler(nil, circuit, retry, nil, deliveryLogger, logger)

	evt := domain.Event{
		ID:          uuid.New(),
		Status:      domain.EventStatusQueued,
		Attempts:    1,
		MaxAttempts: 5,
		ClientID:    "client-123",
		EventType:   "test.event",
		Destination: "https://example.com/webhook",
	}

	endpoint := &domain.Endpoint{
		ID:               uuid.New(),
		MaxRetries:       5,
		RetryBackoffMs:   1000,
		RetryBackoffMult: 2.0,
		RetryBackoffMax:  60000,
	}

	result := domain.DeliveryResult{
		Success:    false,
		StatusCode: 500,
		DurationMs: 100,
		Error:      assert.AnError,
	}

	resultEvt, delay := handler.HandleFailure(context.Background(), evt, endpoint, "example.com", result)

	// Should log to delivery stream without error
	assert.Equal(t, domain.EventStatusFailed, resultEvt.Status)
	assert.Greater(t, delay, time.Duration(0))
}

func TestResultHandler_HandleFailure_WithMetricsRetry(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	handler := NewResultHandler(nil, circuit, retry, metrics, nil, logger)

	evt := domain.Event{
		ID:          uuid.New(),
		Status:      domain.EventStatusQueued,
		Attempts:    1,
		MaxAttempts: 5,
		ClientID:    "client-123",
	}

	result := domain.DeliveryResult{
		Success:    false,
		StatusCode: 500,
	}

	// This should trigger retry metrics recording
	_, delay := handler.HandleFailure(context.Background(), evt, nil, "example.com", result)

	assert.Greater(t, delay, time.Duration(0))
}

func TestResultHandler_HandleFailure_DeadWithDeliveryLogger(t *testing.T) {
	t.Parallel()

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()
	logger := slog.Default()

	deliveryLogger := logstream.NewDeliveryLogger(nil)

	handler := NewResultHandler(nil, circuit, retry, nil, deliveryLogger, logger)

	evt := domain.Event{
		ID:          uuid.New(),
		Status:      domain.EventStatusQueued,
		Attempts:    5,
		MaxAttempts: 5, // No more retries
		ClientID:    "client-123",
		EventType:   "test.event",
		Destination: "https://example.com/webhook",
	}

	result := domain.DeliveryResult{
		Success:    false,
		StatusCode: 500,
		DurationMs: 100,
	}

	resultEvt, delay := handler.HandleFailure(context.Background(), evt, nil, "example.com", result)

	// Should be marked as dead with no retry
	assert.Equal(t, domain.EventStatusDead, resultEvt.Status)
	assert.Equal(t, time.Duration(0), delay)
}
