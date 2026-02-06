package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

func integrationSetupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestIntegration_SenderDelivery(t *testing.T) {
	t.Parallel()

	t.Run("successful delivery to HTTP server", func(t *testing.T) {
		t.Parallel()

		var receivedRequests int32
		var receivedBody []byte

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&receivedRequests, 1)
			buf := make([]byte, 1024)
			n, _ := r.Body.Read(buf)
			receivedBody = buf[:n]
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status": "received"}`))
		}))
		defer server.Close()

		sender := NewSender("test-signing-key")

		event := domain.Event{
			ID:          uuid.New(),
			Destination: server.URL,
			Payload:     json.RawMessage(`{"event": "test", "data": 123}`),
			Headers:     map[string]string{"Content-Type": "application/json"},
		}

		result := sender.Send(context.Background(), event)

		assert.True(t, result.Success)
		assert.Equal(t, http.StatusOK, result.StatusCode)
		assert.Equal(t, int32(1), atomic.LoadInt32(&receivedRequests))
		assert.Contains(t, string(receivedBody), `"event":`)
	})

	t.Run("handles server error", func(t *testing.T) {
		t.Parallel()

		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error": "server error"}`))
		}))
		defer server.Close()

		sender := NewSender("")

		event := domain.Event{
			ID:          uuid.New(),
			Destination: server.URL,
			Payload:     json.RawMessage(`{}`),
		}

		result := sender.Send(context.Background(), event)

		assert.False(t, result.Success)
		assert.Equal(t, http.StatusInternalServerError, result.StatusCode)
	})
}

func TestIntegration_CircuitBreaker(t *testing.T) {
	t.Run("circuit breaker trips after failures", func(t *testing.T) {
		// Server that always fails
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
		}))
		defer server.Close()

		circuitConfig := DefaultCircuitConfig()
		circuitConfig.FailureThreshold = 3
		circuit := NewCircuitBreaker(circuitConfig)
		defer circuit.Stop()

		sender := NewSender("")
		circuitKey := extractHost(server.URL)

		// Make enough failures to trip the circuit
		for range 5 {
			event := domain.Event{
				ID:          uuid.New(),
				Destination: server.URL,
				Payload:     json.RawMessage(`{}`),
			}

			if !circuit.IsOpen(circuitKey) {
				result := sender.Send(context.Background(), event)
				if !result.Success {
					circuit.RecordFailure(circuitKey)
				}
			}
		}

		// Circuit should be open now
		state := circuit.GetState(circuitKey)
		assert.Equal(t, CircuitOpen, state, "circuit should be open after failures")

		// New requests should be blocked
		assert.True(t, circuit.IsOpen(circuitKey), "circuit should block requests when open")
	})

	t.Run("circuit breaker recovers after success", func(t *testing.T) {
		circuitConfig := DefaultCircuitConfig()
		circuitConfig.FailureThreshold = 2
		circuitConfig.OpenDuration = 50 * time.Millisecond
		circuit := NewCircuitBreaker(circuitConfig)
		defer circuit.Stop()

		circuitKey := "test-host"

		// Trip the circuit
		circuit.RecordFailure(circuitKey)
		circuit.RecordFailure(circuitKey)

		state := circuit.GetState(circuitKey)
		assert.Equal(t, CircuitOpen, state)

		// Wait for open duration
		time.Sleep(100 * time.Millisecond)

		// Should be half-open now and allow one request
		if !circuit.IsOpen(circuitKey) {
			circuit.RecordSuccess(circuitKey)
		}

		// Record enough successes to close
		for range circuitConfig.SuccessThreshold {
			circuit.RecordSuccess(circuitKey)
		}

		// After successes, circuit should close
		state = circuit.GetState(circuitKey)
		assert.Equal(t, CircuitClosed, state)
	})
}

func TestIntegration_RateLimiting(t *testing.T) {
	mr, client := integrationSetupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	t.Run("respects endpoint rate limits", func(t *testing.T) {
		var requestCount int32
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			atomic.AddInt32(&requestCount, 1)
			w.WriteHeader(http.StatusOK)
		}))
		defer server.Close()

		rateLimiter := NewRateLimiter(client)
		sender := NewSender("")

		host := extractHost(server.URL)
		rateLimit := 100 // High limit for test

		// Send multiple requests
		ctx := context.Background()

		for range 5 {
			event := domain.Event{
				ID:          uuid.New(),
				Destination: server.URL,
				Payload:     json.RawMessage(`{}`),
			}

			// Use Allow instead of Wait
			if rateLimiter.Allow(ctx, host, rateLimit) {
				result := sender.Send(ctx, event)
				assert.True(t, result.Success)
			}
		}

		// All requests should complete successfully
		assert.Equal(t, int32(5), atomic.LoadInt32(&requestCount))
	})
}

func TestIntegration_Transformer(t *testing.T) {
	t.Parallel()

	t.Run("passes through when no transformation", func(t *testing.T) {
		t.Parallel()

		transformer := NewTransformer()

		event := domain.Event{
			ID:          uuid.New(),
			Destination: "https://example.com/webhook",
			Headers:     map[string]string{"X-Original": "value"},
			Payload:     json.RawMessage(`{"original": true}`),
		}

		// No endpoint transformation
		result, err := transformer.Apply(context.Background(), event, nil, nil)

		require.NoError(t, err)
		assert.Equal(t, event.Destination, result.Destination)
		assert.Equal(t, event.Headers, result.Headers)
		assert.JSONEq(t, string(event.Payload), string(result.Payload))
	})

	t.Run("passes through with empty transformation", func(t *testing.T) {
		t.Parallel()

		transformer := NewTransformer()

		event := domain.Event{
			ID:          uuid.New(),
			Destination: "https://example.com/webhook",
			Payload:     json.RawMessage(`{}`),
		}

		endpoint := &domain.Endpoint{
			ID:             uuid.New(),
			Transformation: "", // Empty
		}

		result, err := transformer.Apply(context.Background(), event, endpoint, nil)

		require.NoError(t, err)
		assert.Equal(t, event.Destination, result.Destination)
	})
}

func TestIntegration_WorkerLifecycle(t *testing.T) {
	mr, client := integrationSetupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	t.Run("worker starts and stops cleanly", func(t *testing.T) {
		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 2
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)

		ctx, cancel := context.WithCancel(context.Background())
		worker.Start(ctx)

		// Give it time to start
		time.Sleep(50 * time.Millisecond)

		// Cancel and stop
		cancel()
		worker.Stop()
	})

	t.Run("worker with both processors", func(t *testing.T) {
		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 1
		config.EnableStandard = true
		config.EnableFIFO = true

		worker := NewWorker(q, nil, config)

		ctx, cancel := context.WithCancel(context.Background())
		worker.Start(ctx)

		time.Sleep(50 * time.Millisecond)

		cancel()
		worker.Stop()
	})

	t.Run("worker with no processors", func(t *testing.T) {
		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.EnableStandard = false
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)

		ctx, cancel := context.WithCancel(context.Background())
		worker.Start(ctx)

		time.Sleep(50 * time.Millisecond)

		cancel()
		worker.Stop()
	})
}

func TestIntegration_QueueOperations(t *testing.T) {
	mr, client := integrationSetupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	t.Run("queue handles enqueue and dequeue", func(t *testing.T) {
		q := queue.NewQueue(client)

		ctx := context.Background()
		eventID := uuid.New()

		// Enqueue takes uuid.UUID
		err := q.Enqueue(ctx, eventID)
		require.NoError(t, err)

		// Dequeue
		dequeued, err := q.Dequeue(ctx)
		require.NoError(t, err)
		assert.Equal(t, eventID, dequeued.EventID)

		// Ack
		err = q.Ack(ctx, dequeued)
		require.NoError(t, err)
	})
}

func TestIntegration_RetryPolicy(t *testing.T) {
	t.Parallel()

	t.Run("calculates exponential backoff", func(t *testing.T) {
		t.Parallel()

		policy := NewRetryPolicy()

		delay1 := policy.NextRetryDelay(1)
		delay2 := policy.NextRetryDelay(2)
		delay3 := policy.NextRetryDelay(3)

		// Each delay should be greater than the previous
		assert.Greater(t, delay2, delay1)
		assert.Greater(t, delay3, delay2)
	})

	t.Run("max attempts is reasonable", func(t *testing.T) {
		t.Parallel()

		policy := NewRetryPolicy()
		maxAttempts := policy.MaxAttempts()

		assert.Greater(t, maxAttempts, 0)
		assert.LessOrEqual(t, maxAttempts, 20) // Should be reasonable
	})
}

func TestIntegration_UtilityFunctions(t *testing.T) {
	t.Parallel()

	t.Run("extractHost parses URLs correctly", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			url      string
			expected string
		}{
			{"https://example.com/webhook", "example.com"},
			{"http://api.service.com:8080/path", "api.service.com:8080"},
			{"https://localhost:3000", "localhost:3000"},
		}

		for _, tt := range tests {
			result := extractHost(tt.url)
			assert.Equal(t, tt.expected, result)
		}
	})

	t.Run("classifyFailureReason categorizes errors", func(t *testing.T) {
		t.Parallel()

		tests := []struct {
			result   domain.DeliveryResult
			expected string
		}{
			{
				result:   domain.DeliveryResult{StatusCode: 500},
				expected: "server_error",
			},
			{
				result:   domain.DeliveryResult{StatusCode: 400},
				expected: "client_error",
			},
			{
				result:   domain.DeliveryResult{StatusCode: 0},
				expected: "unknown",
			},
		}

		for _, tt := range tests {
			result := classifyFailureReason(tt.result)
			assert.Equal(t, tt.expected, result)
		}
	})
}
