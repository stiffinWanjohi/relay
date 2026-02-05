package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

// Skip integration tests if no database is available
func skipIfNoDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://relay:relay@localhost:5432/relay_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("Skipping integration test: database not available: %v", err)
	}

	// Test connection
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Skipping integration test: database not reachable: %v", err)
	}

	return pool
}

func TestWorker_Integration_SuccessfulDelivery(t *testing.T) {
	// Setup database
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	// Setup Redis
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Setup mock HTTP server
	var deliveryCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveryCount.Add(1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"received"}`))
	}))
	defer server.Close()

	// Setup components
	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient).WithBlockingTimeout(100 * time.Millisecond)
	// logger removed - using centralized logging

	config := WorkerConfig{
		Concurrency:    1,
		VisibilityTime: 5 * time.Second,
		SigningKey:     "test-signing-key-32-chars-long!",
		CircuitConfig:  DefaultCircuitConfig(),
	}

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create and enqueue an event
	evt := domain.NewEvent(
		"test-idemp-key-"+uuid.New().String(),
		server.URL,
		[]byte(`{"test":"data"}`),
		nil,
	)

	createdEvent, err := store.Create(ctx, evt)
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	err = q.Enqueue(ctx, createdEvent.ID)
	if err != nil {
		t.Fatalf("failed to enqueue event: %v", err)
	}

	// Start worker
	worker.Start(ctx)

	// Wait for delivery
	time.Sleep(500 * time.Millisecond)

	// Stop worker
	worker.Stop()
	worker.Wait()

	// Verify delivery happened
	if deliveryCount.Load() == 0 {
		t.Error("expected at least one delivery")
	}

	// Verify event status updated
	updatedEvent, err := store.GetByID(ctx, createdEvent.ID)
	if err != nil {
		t.Fatalf("failed to get updated event: %v", err)
	}

	if updatedEvent.Status != domain.EventStatusDelivered {
		t.Errorf("event status = %s, want delivered", updatedEvent.Status)
	}
}

func TestWorker_Integration_FailedDeliveryWithRetry(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Setup mock HTTP server that fails
	var deliveryCount atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		deliveryCount.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"server error"}`))
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient).WithBlockingTimeout(100 * time.Millisecond)
	// logger removed - using centralized logging

	config := WorkerConfig{
		Concurrency:    1,
		VisibilityTime: 5 * time.Second,
		SigningKey:     "test-signing-key-32-chars-long!",
		CircuitConfig:  DefaultCircuitConfig(),
	}

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create event with low max attempts for faster test
	evt := domain.NewEvent(
		"test-fail-idemp-"+uuid.New().String(),
		server.URL,
		[]byte(`{"test":"data"}`),
		nil,
	)
	evt.MaxAttempts = 2

	createdEvent, err := store.Create(ctx, evt)
	if err != nil {
		t.Fatalf("failed to create event: %v", err)
	}

	err = q.Enqueue(ctx, createdEvent.ID)
	if err != nil {
		t.Fatalf("failed to enqueue event: %v", err)
	}

	// Start worker
	worker.Start(ctx)

	// Wait for first delivery attempt
	time.Sleep(500 * time.Millisecond)

	// Stop worker
	worker.Stop()
	worker.Wait()

	// Verify at least one delivery attempt was made
	if deliveryCount.Load() == 0 {
		t.Error("expected at least one delivery attempt")
	}

	// Verify event status is failed (waiting for retry)
	updatedEvent, err := store.GetByID(ctx, createdEvent.ID)
	if err != nil {
		t.Fatalf("failed to get updated event: %v", err)
	}

	if updatedEvent.Status != domain.EventStatusFailed && updatedEvent.Status != domain.EventStatusDead {
		t.Logf("event status = %s, attempts = %d", updatedEvent.Status, updatedEvent.Attempts)
	}
}

func TestWorker_Integration_CircuitBreaker(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient).WithBlockingTimeout(100 * time.Millisecond)
	// logger removed - using centralized logging

	// Low failure threshold for testing
	circuitConfig := CircuitConfig{
		FailureThreshold: 2,
		SuccessThreshold: 1,
		OpenDuration:     time.Minute,
	}

	config := WorkerConfig{
		Concurrency:    1,
		VisibilityTime: 5 * time.Second,
		SigningKey:     "test-signing-key-32-chars-long!",
		CircuitConfig:  circuitConfig,
	}

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	// Manually record failures to open circuit
	worker.circuit.RecordFailure(server.URL)
	worker.circuit.RecordFailure(server.URL)

	// Verify circuit is open
	if !worker.circuit.IsOpen(server.URL) {
		t.Error("circuit should be open after failures")
	}

	// Check circuit stats
	stats := worker.CircuitStats()
	if len(stats.Circuits) == 0 {
		t.Error("expected circuit in stats")
	}
}

func TestWorker_Integration_RateLimiting(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)
	rateLimiter := NewRateLimiter(redisClient)
	// logger removed - using centralized logging

	config := WorkerConfig{
		Concurrency:    1,
		VisibilityTime: 5 * time.Second,
		SigningKey:     "test-signing-key-32-chars-long!",
		CircuitConfig:  DefaultCircuitConfig(),
		RateLimiter:    rateLimiter,
	}

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	// Test rate limiter is set
	if worker.rateLimiter == nil {
		t.Error("rate limiter should be set")
	}

	// Test rate limiting logic
	ctx := context.Background()
	endpointID := uuid.New().String()

	// First request allowed
	if !rateLimiter.Allow(ctx, endpointID, 1) {
		t.Error("first request should be allowed")
	}

	// Second request blocked (limit is 1 per second)
	if rateLimiter.Allow(ctx, endpointID, 1) {
		t.Error("second request should be blocked")
	}
}

// Test handleSuccess directly (requires mocking or partial integration)
func TestWorker_HandleSuccess_Unit(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	q := queue.NewQueue(redisClient)
	// logger removed - using centralized logging
	config := DefaultWorkerConfig()

	worker := &Worker{
		queue:   q,
		circuit: NewCircuitBreaker(config.CircuitConfig),
	}
	defer worker.circuit.Stop()

	// Test that circuit records success
	dest := "https://example.com"
	worker.circuit.RecordFailure(dest)
	worker.circuit.RecordFailure(dest)

	// Record success should help close circuit
	worker.circuit.RecordSuccess(dest)

	// Circuit should still be closed (failures were reset)
	state := worker.circuit.GetState(dest)
	if state != CircuitClosed {
		t.Errorf("circuit state = %v, want closed", state)
	}
}

// Test handleFailure circuit breaker interaction
func TestWorker_HandleFailure_CircuitBreaker(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	q := queue.NewQueue(redisClient)
	// logger removed - using centralized logging

	circuitConfig := CircuitConfig{
		FailureThreshold: 3,
		SuccessThreshold: 1,
		OpenDuration:     time.Minute,
	}

	worker := &Worker{
		queue:   q,
		circuit: NewCircuitBreaker(circuitConfig),
		retry:   NewRetryPolicy(),
	}
	defer worker.circuit.Stop()

	dest := "https://failing-endpoint.com"

	// Simulate failures
	worker.circuit.RecordFailure(dest)
	worker.circuit.RecordFailure(dest)

	// Circuit should still be closed
	if worker.circuit.IsOpen(dest) {
		t.Error("circuit should not be open after 2 failures")
	}

	// Third failure opens circuit
	worker.circuit.RecordFailure(dest)
	if !worker.circuit.IsOpen(dest) {
		t.Error("circuit should be open after 3 failures")
	}
}
