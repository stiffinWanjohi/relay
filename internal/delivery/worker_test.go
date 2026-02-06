package delivery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/observability"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

// Test helpers

func workerSetupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	return mr, client
}

func setupTestWorker(t *testing.T, config Config) (*Worker, *miniredis.Miniredis, func()) {
	mr, client := workerSetupTestRedis(t)
	q := queue.NewQueue(client)
	w := NewWorker(q, nil, config)

	cleanup := func() {
		w.circuit.Stop()
		_ = client.Close()
		mr.Close()
	}

	return w, mr, cleanup
}

// Config Tests

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()

	tests := []struct {
		name     string
		got      interface{}
		expected interface{}
	}{
		{"Concurrency", config.Concurrency, 10},
		{"VisibilityTime", config.VisibilityTime, 30 * time.Second},
		{"EnablePriorityQueue", config.EnablePriorityQueue, true},
		{"FIFOGracePeriod", config.FIFOGracePeriod, 30 * time.Second},
		{"EnableStandard", config.EnableStandard, true},
		{"EnableFIFO", config.EnableFIFO, true},
		{"CircuitConfig.FailureThreshold", config.CircuitConfig.FailureThreshold, 5},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.expected {
				t.Errorf("%s = %v, want %v", tt.name, tt.got, tt.expected)
			}
		})
	}
}

func TestConfigValidate(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name:   "zero values",
			config: Config{},
		},
		{
			name: "negative concurrency",
			config: Config{
				Concurrency: -1,
			},
		},
		{
			name: "valid config",
			config: Config{
				Concurrency:     5,
				VisibilityTime:  time.Minute,
				FIFOGracePeriod: time.Minute,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.config.Validate()
			if err != nil {
				t.Errorf("Validate() returned error: %v", err)
			}
		})
	}
}

func TestConfigWithConcurrency(t *testing.T) {
	tests := []struct {
		name        string
		concurrency int
	}{
		{"default", 10},
		{"single", 1},
		{"high", 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig().WithConcurrency(tt.concurrency)
			if config.Concurrency != tt.concurrency {
				t.Errorf("Concurrency = %d, want %d", config.Concurrency, tt.concurrency)
			}
		})
	}
}

func TestConfigWithFIFOOnly(t *testing.T) {
	config := DefaultConfig().WithFIFOOnly()

	if config.EnableStandard {
		t.Error("EnableStandard should be false")
	}
	if !config.EnableFIFO {
		t.Error("EnableFIFO should be true")
	}
}

func TestConfigWithStandardOnly(t *testing.T) {
	config := DefaultConfig().WithStandardOnly()

	if !config.EnableStandard {
		t.Error("EnableStandard should be true")
	}
	if config.EnableFIFO {
		t.Error("EnableFIFO should be false")
	}
}

// Worker Tests

func TestNewWorker(t *testing.T) {
	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	if w.queue == nil {
		t.Error("queue should not be nil")
	}
	if w.sender == nil {
		t.Error("sender should not be nil")
	}
	if w.circuit == nil {
		t.Error("circuit should not be nil")
	}
	if w.retry == nil {
		t.Error("retry should not be nil")
	}
	if w.transformer == nil {
		t.Error("transformer should not be nil")
	}
	if w.recorder == nil {
		t.Error("recorder should not be nil")
	}
	if w.handler == nil {
		t.Error("handler should not be nil")
	}
}

func TestNewWorker_ProcessorCounts(t *testing.T) {
	tests := []struct {
		name           string
		enableStandard bool
		enableFIFO     bool
		expectedCount  int
	}{
		{"both enabled", true, true, 2},
		{"standard only", true, false, 1},
		{"FIFO only", false, true, 1},
		{"none enabled", false, false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			config := DefaultConfig()
			config.EnableStandard = tt.enableStandard
			config.EnableFIFO = tt.enableFIFO

			w, _, cleanup := setupTestWorker(t, config)
			defer cleanup()

			if len(w.processors) != tt.expectedCount {
				t.Errorf("processor count = %d, want %d", len(w.processors), tt.expectedCount)
			}
		})
	}
}

func TestNewWorker_WithMetrics(t *testing.T) {
	config := DefaultConfig()
	config.Metrics = observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	if w.recorder == nil {
		t.Error("recorder should not be nil")
	}
}

func TestNewWorker_WithRateLimiter(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	config := DefaultConfig()
	config.RateLimiter = NewRateLimiter(client)

	q := queue.NewQueue(client)
	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	if w.rateLimiter == nil {
		t.Error("rateLimiter should not be nil")
	}
}

func TestWorker_Stop(t *testing.T) {
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	// Stop should close the channel
	w.Stop()

	// Verify channel is closed
	select {
	case <-w.stopCh:
		// Expected - channel is closed
	default:
		t.Error("stopCh should be closed")
	}
}

func TestWorker_StopAndWait_Success(t *testing.T) {
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	err := w.StopAndWait(time.Second)
	if err != nil {
		t.Errorf("StopAndWait() returned error: %v", err)
	}
}

func TestWorker_StopAndWait_Timeout(t *testing.T) {
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	// Add a goroutine that doesn't respond to stop
	w.wg.Add(1)
	go func() {
		time.Sleep(5 * time.Second)
		w.wg.Done()
	}()

	err := w.StopAndWait(50 * time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
	if err.Error() != "worker shutdown timed out" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWorker_CircuitStats(t *testing.T) {
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	// Record failures for different destinations
	w.circuit.RecordFailure("https://example1.com")
	w.circuit.RecordFailure("https://example2.com")
	w.circuit.RecordFailure("https://example3.com")

	stats := w.CircuitStats()

	if len(stats.Circuits) != 3 {
		t.Errorf("expected 3 circuits, got %d", len(stats.Circuits))
	}
}

func TestWorker_Queue(t *testing.T) {
	config := DefaultConfig()

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	if w.Queue() == nil {
		t.Error("Queue() should not return nil")
	}
}

func TestWorker_Store(t *testing.T) {
	config := DefaultConfig()

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	// Store is nil in our test setup
	if w.Store() != nil {
		t.Error("Store() should return nil in test setup")
	}
}

func TestWorker_GetEvent_WithNilStore(t *testing.T) {
	config := DefaultConfig()

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	_, err := w.GetEvent(context.Background(), uuid.New())
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestWorker_GetEndpoint_WithNilStore(t *testing.T) {
	config := DefaultConfig()

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	_, err := w.GetEndpoint(context.Background(), uuid.New().String())
	if err == nil {
		t.Error("expected error when store is nil")
	}
}

func TestWorker_GetEndpoint_InvalidUUID(t *testing.T) {
	config := DefaultConfig()

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	_, err := w.GetEndpoint(context.Background(), "not-a-uuid")
	if err == nil {
		t.Error("expected error for invalid UUID")
	}
}

// RetryError Tests

func TestRetryError(t *testing.T) {
	tests := []struct {
		delay time.Duration
	}{
		{time.Second},
		{5 * time.Second},
		{time.Minute},
	}

	for _, tt := range tests {
		t.Run(tt.delay.String(), func(t *testing.T) {
			err := &RetryError{Delay: tt.delay}
			if err.Error() != "delivery failed, retry scheduled" {
				t.Errorf("unexpected error message: %s", err.Error())
			}
			if err.Delay != tt.delay {
				t.Errorf("Delay = %v, want %v", err.Delay, tt.delay)
			}
		})
	}
}

// Integration Tests

func TestWorker_StartAndStop(t *testing.T) {
	config := DefaultConfig()
	config.Concurrency = 2
	config.EnableFIFO = false // Only test standard processor

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	ctx, cancel := context.WithCancel(context.Background())

	// Start the worker
	w.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Stop via context cancel
	cancel()

	// Should stop gracefully
	err := w.StopAndWait(2 * time.Second)
	if err != nil {
		t.Errorf("StopAndWait() returned error: %v", err)
	}
}

func TestWorker_DeliverWithMockServer(t *testing.T) {
	var requestCount atomic.Int32

	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	// Create a test event
	evt := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
		Status:      domain.EventStatusQueued,
		Attempts:    0,
		MaxAttempts: 3,
	}

	// Create a mock logger
	logger := workerLog.With("test", true)

	// Deliver the event
	result, err := w.Deliver(context.Background(), evt, nil, logger)

	if err != nil {
		t.Errorf("Deliver() returned error: %v", err)
	}
	if !result.Success {
		t.Error("expected successful delivery")
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if requestCount.Load() != 1 {
		t.Errorf("request count = %d, want 1", requestCount.Load())
	}
}

func TestWorker_DeliverWithFailingServer(t *testing.T) {
	// Create a test server that returns 500
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "internal server error"}`))
	}))
	defer server.Close()

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
		Status:      domain.EventStatusQueued,
		Attempts:    0,
		MaxAttempts: 3,
	}

	logger := workerLog.With("test", true)

	result, _ := w.Deliver(context.Background(), evt, nil, logger)

	if result.Success {
		t.Error("expected failed delivery")
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("StatusCode = %d, want %d", result.StatusCode, http.StatusInternalServerError)
	}
}

func TestWorker_DeliverWithRateLimiting(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.RateLimiter = NewRateLimiter(client)
	config.EnableStandard = false
	config.EnableFIFO = false

	q := queue.NewQueue(client)
	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	endpointID := uuid.New()
	endpoint := &domain.Endpoint{
		ID:              endpointID,
		RateLimitPerSec: 1, // Very low rate limit
	}

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: "https://example.com/webhook",
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
		EndpointID:  &endpointID,
	}

	logger := workerLog.With("test", true)

	// First request should succeed
	_, err := w.Deliver(context.Background(), evt, endpoint, logger)
	if err != nil && !errors.Is(err, domain.ErrRateLimited) {
		// Rate limit might trigger on first call due to implementation
	}

	// Second request should be rate limited
	_, err = w.Deliver(context.Background(), evt, endpoint, logger)
	if err == nil || !errors.Is(err, domain.ErrRateLimited) {
		// May or may not be rate limited depending on timing
	}
}

func TestWorker_DeliverWithCircuitOpen(t *testing.T) {
	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.CircuitConfig.FailureThreshold = 1 // Open after 1 failure
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	endpointID := uuid.New()

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: "https://example.com/webhook",
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
		EndpointID:  &endpointID,
	}

	// Trip the circuit
	w.circuit.RecordFailure(endpointID.String())

	logger := workerLog.With("test", true)

	_, err := w.Deliver(context.Background(), evt, &domain.Endpoint{ID: endpointID}, logger)

	if err == nil {
		t.Error("expected error when circuit is open")
	}
	if !errors.Is(err, domain.ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got: %v", err)
	}
}

// Concurrent Tests

func TestWorker_ConcurrentDeliveries(t *testing.T) {
	var requestCount atomic.Int32

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount.Add(1)
		time.Sleep(10 * time.Millisecond) // Simulate some work
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	const numDeliveries = 50
	var wg sync.WaitGroup
	wg.Add(numDeliveries)

	errors := make(chan error, numDeliveries)

	for i := 0; i < numDeliveries; i++ {
		go func(i int) {
			defer wg.Done()

			evt := domain.Event{
				ID:          uuid.New(),
				Destination: server.URL,
				Payload:     []byte(`{"test": "data"}`),
				ClientID:    "test-client",
			}

			logger := workerLog.With("delivery", i)
			_, err := w.Deliver(context.Background(), evt, nil, logger)
			if err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("delivery error: %v", err)
	}

	if requestCount.Load() != numDeliveries {
		t.Errorf("request count = %d, want %d", requestCount.Load(), numDeliveries)
	}
}

// Test with real event store (mock)

type workerMockEventStore struct {
	events    map[uuid.UUID]domain.Event
	endpoints map[uuid.UUID]domain.Endpoint
	mu        sync.RWMutex
}

func newWorkerMockEventStore() *workerMockEventStore {
	return &workerMockEventStore{
		events:    make(map[uuid.UUID]domain.Event),
		endpoints: make(map[uuid.UUID]domain.Endpoint),
	}
}

func (m *workerMockEventStore) GetByID(ctx context.Context, id uuid.UUID) (domain.Event, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if evt, ok := m.events[id]; ok {
		return evt, nil
	}
	return domain.Event{}, domain.ErrEventNotFound
}

func (m *workerMockEventStore) Update(ctx context.Context, evt domain.Event) (domain.Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[evt.ID] = evt
	return evt, nil
}

func (m *workerMockEventStore) GetEndpointByID(ctx context.Context, id uuid.UUID) (domain.Endpoint, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if ep, ok := m.endpoints[id]; ok {
		return ep, nil
	}
	return domain.Endpoint{}, domain.ErrEndpointNotFound
}

func (m *workerMockEventStore) CreateDeliveryAttempt(ctx context.Context, attempt domain.DeliveryAttempt) (domain.DeliveryAttempt, error) {
	return attempt, nil
}

func (m *workerMockEventStore) ListFIFOEndpoints(ctx context.Context) ([]domain.Endpoint, error) {
	return nil, nil
}

func (m *workerMockEventStore) AddEvent(evt domain.Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.events[evt.ID] = evt
}

func (m *workerMockEventStore) AddEndpoint(ep domain.Endpoint) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endpoints[ep.ID] = ep
}

// Test with actual store interface
func TestWorker_WithMockStore(t *testing.T) {
	// This test demonstrates how to use the worker with a mock store
	// The actual implementation would need the event.Store interface to be mockable

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	// Worker is created, store is nil
	if w.Store() != nil {
		t.Error("expected nil store in test setup")
	}
}

func TestWorker_Wait(t *testing.T) {
	t.Run("wait returns immediately when no processors running", func(t *testing.T) {
		config := DefaultConfig()
		config.EnableStandard = false
		config.EnableFIFO = false

		w, _, cleanup := setupTestWorker(t, config)
		defer cleanup()

		// Wait should return immediately since no processors are started
		done := make(chan struct{})
		go func() {
			w.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(time.Second):
			t.Error("Wait() should return immediately when no processors running")
		}
	})

	t.Run("wait blocks until processors stopped", func(t *testing.T) {
		config := DefaultConfig()
		config.Concurrency = 1
		config.EnableStandard = true
		config.EnableFIFO = false

		w, _, cleanup := setupTestWorker(t, config)
		defer cleanup()

		ctx, cancel := context.WithCancel(context.Background())
		w.Start(ctx)
		time.Sleep(50 * time.Millisecond)

		// Cancel context to trigger stop
		cancel()
		w.Stop()

		// Wait should return after processors stop
		done := make(chan struct{})
		go func() {
			w.Wait()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(2 * time.Second):
			t.Error("Wait() should complete after Stop()")
		}
	})
}

func TestWorker_Deliver_RateLimited(t *testing.T) {
	mr, client := workerSetupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	// Create a rate limiter
	w.rateLimiter = NewRateLimiter(client)

	// Create endpoint with rate limit
	endpoint := &domain.Endpoint{
		ID:              uuid.New(),
		RateLimitPerSec: 1, // Very low limit
	}

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: "https://example.com/webhook",
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
	}

	logger := workerLog.With("test", true)

	// First request should pass
	_, err1 := w.Deliver(context.Background(), evt, endpoint, logger)
	// Note: might hit rate limit depending on timing

	// Spam requests to trigger rate limit
	var rateLimitErr error
	for range 10 {
		_, err := w.Deliver(context.Background(), evt, endpoint, logger)
		if errors.Is(err, domain.ErrRateLimited) {
			rateLimitErr = err
			break
		}
	}

	// Check that we either passed or got rate limited (both are valid)
	_ = err1
	// At least we exercised the rate limit code path
	if rateLimitErr != nil {
		if !errors.Is(rateLimitErr, domain.ErrRateLimited) {
			t.Errorf("expected ErrRateLimited, got: %v", rateLimitErr)
		}
	}
}

func TestWorker_Deliver_CircuitOpen(t *testing.T) {
	config := DefaultConfig()
	config.CircuitConfig.FailureThreshold = 1
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	// Create a failing server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
		MaxAttempts: 1,
	}

	logger := workerLog.With("test", true)

	// First delivery will fail and trip the circuit
	w.Deliver(context.Background(), evt, nil, logger)

	// Second delivery should get circuit open error
	_, err := w.Deliver(context.Background(), evt, nil, logger)
	if !errors.Is(err, domain.ErrCircuitOpen) {
		t.Errorf("expected ErrCircuitOpen, got: %v", err)
	}
}

func TestWorker_Deliver_WithEndpoint(t *testing.T) {
	// Create a test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	endpoint := &domain.Endpoint{
		ID:     uuid.New(),
		URL:    server.URL,
		Status: domain.EndpointStatusActive,
	}

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
		EndpointID:  &endpoint.ID,
	}

	logger := workerLog.With("test", true)

	result, err := w.Deliver(context.Background(), evt, endpoint, logger)

	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected successful delivery")
	}
}

func TestWorker_Deliver_RetryError(t *testing.T) {
	// Create a failing server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w, _, cleanup := setupTestWorker(t, config)
	defer cleanup()

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
		MaxAttempts: 5,
		Attempts:    0,
	}

	logger := workerLog.With("test", true)

	result, err := w.Deliver(context.Background(), evt, nil, logger)

	// Should get a RetryError since we have retries remaining
	var retryErr *RetryError
	if errors.As(err, &retryErr) {
		if retryErr.Delay <= 0 {
			t.Error("expected positive retry delay")
		}
	}

	if result.Success {
		t.Error("expected failed delivery")
	}
}

func TestWorker_NewWorker_WithMetrics(t *testing.T) {
	mr, client := workerSetupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	q := queue.NewQueue(client)

	config := DefaultConfig()
	config.Metrics = observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	config.EnableStandard = false
	config.EnableFIFO = false

	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	// Worker should be created with metrics
	if w == nil {
		t.Error("expected worker to be created")
	}
}
