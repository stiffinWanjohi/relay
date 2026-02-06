package delivery

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

// skipIfNoDatabase skips the test if PostgreSQL is not available
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

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Skipping integration test: database not reachable: %v", err)
	}

	return pool
}

// createTestClient creates a client in the database for testing
func createTestClient(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	clientID := "test-client-" + uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO clients (id, name, email, webhook_url_patterns, max_events_per_day, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`, clientID, "Test Client", "test@example.com", []string{"https://example.com/*", "http://*"}, 10000, true)
	if err != nil {
		t.Fatalf("Failed to create test client: %v", err)
	}

	return clientID
}

// cleanupTestClient removes a test client from the database
func cleanupTestClient(t *testing.T, pool *pgxpool.Pool, clientID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM endpoints WHERE client_id = $1", clientID)
	_, _ = pool.Exec(ctx, "DELETE FROM api_keys WHERE client_id = $1", clientID)
	_, _ = pool.Exec(ctx, "DELETE FROM clients WHERE id = $1", clientID)
}

// cleanupTestData removes test data from the database
func cleanupTestData(t *testing.T, pool *pgxpool.Pool, eventIDs []uuid.UUID, endpointIDs []uuid.UUID) {
	t.Helper()
	ctx := context.Background()

	for _, id := range eventIDs {
		_, _ = pool.Exec(ctx, "DELETE FROM delivery_attempts WHERE event_id = $1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM outbox WHERE event_id = $1", id)
		_, _ = pool.Exec(ctx, "DELETE FROM events WHERE id = $1", id)
	}

	for _, id := range endpointIDs {
		_, _ = pool.Exec(ctx, "DELETE FROM endpoints WHERE id = $1", id)
	}
}

// createTestEndpoint creates an endpoint in the database
func createTestEndpoint(t *testing.T, pool *pgxpool.Pool, clientID string, url string, fifo bool) domain.Endpoint {
	t.Helper()
	return createTestEndpointWithRateLimit(t, pool, clientID, url, fifo, 0)
}

// createTestEndpointWithRateLimit creates an endpoint with a specific rate limit
func createTestEndpointWithRateLimit(t *testing.T, pool *pgxpool.Pool, clientID string, url string, fifo bool, rateLimitPerSec int) domain.Endpoint {
	t.Helper()
	ctx := context.Background()

	endpoint := domain.Endpoint{
		ID:               uuid.New(),
		ClientID:         clientID,
		URL:              url,
		Status:           domain.EndpointStatusActive,
		MaxRetries:       3,
		RetryBackoffMs:   1000,
		RetryBackoffMult: 2.0,
		RetryBackoffMax:  60000,
		FIFO:             fifo,
		RateLimitPerSec:  rateLimitPerSec,
		CreatedAt:        time.Now(),
		UpdatedAt:        time.Now(),
	}

	_, err := pool.Exec(ctx, `
		INSERT INTO endpoints (id, client_id, url, status, max_retries, retry_backoff_ms, retry_backoff_mult, retry_backoff_max, fifo, rate_limit_per_sec, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, endpoint.ID, endpoint.ClientID, endpoint.URL, endpoint.Status, endpoint.MaxRetries,
		endpoint.RetryBackoffMs, endpoint.RetryBackoffMult, endpoint.RetryBackoffMax, endpoint.FIFO,
		endpoint.RateLimitPerSec, endpoint.CreatedAt, endpoint.UpdatedAt)
	if err != nil {
		t.Fatalf("Failed to create test endpoint: %v", err)
	}

	return endpoint
}

func TestWorker_GetEndpoint_WithRealStore(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", false)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Test GetEndpoint
	ep, err := worker.GetEndpoint(ctx, endpoint.ID.String())
	require.NoError(t, err)
	assert.Equal(t, endpoint.ID, ep.ID)
	assert.Equal(t, endpoint.URL, ep.URL)
	assert.Equal(t, endpoint.Status, ep.Status)
}

func TestWorker_GetEndpoint_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	ctx := context.Background()

	// Test GetEndpoint with non-existent ID
	_, err = worker.GetEndpoint(ctx, uuid.New().String())
	assert.Error(t, err)
}

func TestWorker_GetEvent_WithRealStore(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-get-event-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "data"}`),
		map[string]string{"Content-Type": "application/json"},
	)

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Test GetEvent
	retrieved, err := worker.GetEvent(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, created.ID, retrieved.ID)
	assert.Equal(t, created.IdempotencyKey, retrieved.IdempotencyKey)
}

func TestWorker_Deliver_WithRealStore(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, false)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-deliver-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "delivery"}`),
		map[string]string{"Content-Type": "application/json"},
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	logger := workerLog.With("test", true)

	// Test Deliver with real store
	result, err := worker.Deliver(ctx, created, &endpoint, logger)
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, http.StatusOK, result.StatusCode)

	// Verify event was updated in store
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, updated.Status)
}

func TestResultHandler_CreateDeliveryAttempt_WithRealStore(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := event.NewStore(pool)

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()

	handler := NewResultHandler(store, circuit, retry, nil, nil, workerLog)

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-attempt-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "attempt"}`),
		nil,
	)

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Test CreateDeliveryAttempt
	result := domain.DeliveryResult{
		Success:      true,
		StatusCode:   200,
		ResponseBody: `{"status": "ok"}`,
		DurationMs:   150,
	}

	created.Attempts = 1
	handler.CreateDeliveryAttempt(ctx, created, result)

	// Verify delivery attempt was created
	var attemptCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM delivery_attempts WHERE event_id = $1", created.ID).Scan(&attemptCount)
	require.NoError(t, err)
	assert.Equal(t, 1, attemptCount)
}

func TestResultHandler_HandleSuccess_WithRealStore(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := event.NewStore(pool)

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()

	handler := NewResultHandler(store, circuit, retry, nil, nil, workerLog)

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-success-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "success"}`),
		nil,
	)

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	result := domain.DeliveryResult{
		Success:    true,
		StatusCode: 200,
		DurationMs: 100,
	}

	// Test HandleSuccess
	updatedEvt := handler.HandleSuccess(ctx, created, nil, "example.com", result)
	assert.Equal(t, domain.EventStatusDelivered, updatedEvt.Status)

	// Verify event was updated in database
	retrieved, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, retrieved.Status)
}

func TestResultHandler_HandleFailure_WithRealStore(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := event.NewStore(pool)

	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	defer circuit.Stop()
	retry := NewRetryPolicy()

	handler := NewResultHandler(store, circuit, retry, nil, nil, workerLog)

	ctx := context.Background()

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", false)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	// Create test event with retries remaining
	evt := domain.NewEvent(
		"test-failure-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "failure"}`),
		nil,
	)
	evt.MaxAttempts = 5
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	result := domain.DeliveryResult{
		Success:    false,
		StatusCode: 500,
		DurationMs: 50,
	}

	// Test HandleFailure
	updatedEvt, delay := handler.HandleFailure(ctx, created, &endpoint, "example.com", result)
	assert.Equal(t, domain.EventStatusFailed, updatedEvt.Status)
	assert.Greater(t, delay, time.Duration(0))

	// Verify event was updated in database
	retrieved, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusFailed, retrieved.Status)
	assert.NotNil(t, retrieved.NextAttemptAt)
}

// ============================================================================
// FIFO Processor Integration Tests
// ============================================================================

func TestFIFOProcessor_ProcessOneFIFO_WithRealQueue(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server that succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and FIFO endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-fifo-process-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "fifo"}`),
		map[string]string{"Content-Type": "application/json"},
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue to FIFO queue
	err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
	require.NoError(t, err)

	// Process the message
	logger := workerLog.With("test", true)
	err = fifoProcessor.processOneFIFO(ctx, endpoint, "", logger)
	require.NoError(t, err)

	// Verify event was delivered
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, updated.Status)

	// Verify queue is empty
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)
}

func TestFIFOProcessor_ProcessOneFIFO_WithRetry(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server that fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "server error"}`))
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and FIFO endpoint with retries
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event with retries
	evt := domain.NewEvent(
		"test-fifo-retry-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "fifo-retry"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID
	evt.MaxAttempts = 3

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue to FIFO queue
	err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
	require.NoError(t, err)

	// Process the message (will fail and nack)
	logger := workerLog.With("test", true)
	err = fifoProcessor.processOneFIFO(ctx, endpoint, "", logger)
	require.NoError(t, err)

	// Verify event status is failed (but not dead yet since retries remain)
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusFailed, updated.Status)
	assert.NotNil(t, updated.NextAttemptAt)
}

func TestFIFOProcessor_ProcessOneFIFO_WithPartitionKey(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and FIFO endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()
	partitionKey := "user-123"

	// Create test event
	evt := domain.NewEvent(
		"test-fifo-partition-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"user_id": "123"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue with partition key
	err = q.EnqueueFIFO(ctx, endpoint.ID.String(), partitionKey, created.ID)
	require.NoError(t, err)

	// Process the message with partition key
	logger := workerLog.With("test", true)
	err = fifoProcessor.processOneFIFO(ctx, endpoint, partitionKey, logger)
	require.NoError(t, err)

	// Verify event was delivered
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, updated.Status)
	assert.Equal(t, 1, requestCount)
}

func TestFIFOProcessor_HandleOrphanedFIFOMessages_EndpointDisabled(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test events
	var eventIDs []uuid.UUID
	for range 3 {
		evt := domain.NewEvent(
			"test-orphan-"+uuid.New().String(),
			"https://example.com/webhook",
			json.RawMessage(`{"test": "orphan"}`),
			nil,
		)
		evt.EndpointID = &endpoint.ID
		evt.ClientID = clientID

		created, err := store.Create(ctx, evt)
		require.NoError(t, err)
		eventIDs = append(eventIDs, created.ID)

		// Enqueue to FIFO queue
		err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
		require.NoError(t, err)
	}
	defer cleanupTestData(t, pool, eventIDs, nil)

	// Mark endpoint as disabled
	disabledEndpoint := endpoint
	disabledEndpoint.Status = domain.EndpointStatusDisabled

	// Handle orphaned messages
	logger := workerLog.With("test", true)
	fifoProcessor.handleOrphanedFIFOMessages(ctx, endpoint.ID.String(), "", disabledEndpoint, logger)

	// Verify all events were marked as dead
	for _, eventID := range eventIDs {
		updated, err := store.GetByID(ctx, eventID)
		require.NoError(t, err)
		assert.Equal(t, domain.EventStatusDead, updated.Status)
	}

	// Verify queue is drained
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)
}

func TestFIFOProcessor_HandleOrphanedFIFOMessages_SwitchToStandard(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test events
	var eventIDs []uuid.UUID
	for range 2 {
		evt := domain.NewEvent(
			"test-switch-"+uuid.New().String(),
			"https://example.com/webhook",
			json.RawMessage(`{"test": "switch"}`),
			nil,
		)
		evt.EndpointID = &endpoint.ID
		evt.ClientID = clientID

		created, err := store.Create(ctx, evt)
		require.NoError(t, err)
		eventIDs = append(eventIDs, created.ID)

		// Enqueue to FIFO queue
		err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
		require.NoError(t, err)
	}
	defer cleanupTestData(t, pool, eventIDs, nil)

	// Endpoint switched to standard (FIFO=false, still active)
	standardEndpoint := endpoint
	standardEndpoint.FIFO = false
	standardEndpoint.Status = domain.EndpointStatusActive

	// Handle orphaned messages
	logger := workerLog.With("test", true)
	fifoProcessor.handleOrphanedFIFOMessages(ctx, endpoint.ID.String(), "", standardEndpoint, logger)

	// Verify FIFO queue is empty
	fifoLength, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), fifoLength)

	// Verify messages were moved to standard queue
	stats, err := q.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(2), stats.Pending)
}

func TestFIFOProcessor_ProcessOneFIFO_EventNotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and FIFO endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Enqueue non-existent event ID to FIFO queue
	nonExistentID := uuid.New()
	err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", nonExistentID)
	require.NoError(t, err)

	// Process - should ack and return nil (event not found)
	logger := workerLog.With("test", true)
	err = fifoProcessor.processOneFIFO(ctx, endpoint, "", logger)
	require.NoError(t, err)

	// Queue should be empty (message was acked)
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)
}

func TestFIFOProcessor_ProcessOneFIFO_RateLimited(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	// Set up rate limiter to block requests
	rateLimiter := NewRateLimiter(redisClient)
	worker.rateLimiter = rateLimiter

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and FIFO endpoint WITH rate limit enabled
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	// Create endpoint with rate limit of 1 request per second
	endpoint := createTestEndpointWithRateLimit(t, pool, clientID, "https://example.com/webhook", true, 1)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-fifo-ratelimit-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "ratelimit"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue to FIFO queue
	err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
	require.NoError(t, err)

	// Exhaust the rate limit using endpoint ID (which is the rate limit key)
	for range 10 {
		rateLimiter.Allow(ctx, endpoint.ID.String(), 1) // use up the limit
	}

	// Process - should nack due to rate limit
	logger := workerLog.With("test", true)
	err = fifoProcessor.processOneFIFO(ctx, endpoint, "", logger)
	require.NoError(t, err)

	// Message should be nacked back to queue
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), length)
}

func TestFIFOProcessor_ProcessOneFIFO_CircuitOpen(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create server that always fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	// Configure circuit breaker to trip quickly
	circuitConfig := DefaultCircuitConfig()
	circuitConfig.FailureThreshold = 1

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = true
	config.CircuitConfig = circuitConfig

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and FIFO endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Trip the circuit using endpoint ID (which is used as circuit key in Deliver)
	worker.circuit.RecordFailure(endpoint.ID.String())
	worker.circuit.RecordFailure(endpoint.ID.String())

	// Create test event
	evt := domain.NewEvent(
		"test-fifo-circuit-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "circuit"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue to FIFO queue
	err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
	require.NoError(t, err)

	// Process - should nack due to circuit open
	logger := workerLog.With("test", true)
	err = fifoProcessor.processOneFIFO(ctx, endpoint, "", logger)
	require.NoError(t, err)

	// Message should be nacked back to queue
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), length)
}

func TestFIFOProcessor_HandleOrphanedFIFOMessages_MoveError(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Endpoint switched to standard but no messages in queue (edge case - no error but 0 moved)
	standardEndpoint := endpoint
	standardEndpoint.FIFO = false
	standardEndpoint.Status = domain.EndpointStatusActive

	// Handle orphaned messages with empty queue
	logger := workerLog.With("test", true)
	fifoProcessor.handleOrphanedFIFOMessages(ctx, endpoint.ID.String(), "", standardEndpoint, logger)

	// Should complete without error - no messages to move
	stats, err := q.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.Pending)
}

func TestFIFOProcessor_MarkQueuedEventsAsFailed(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create and enqueue multiple events
	var eventIDs []uuid.UUID
	for i := range 5 {
		evt := domain.NewEvent(
			"test-fail-queued-"+uuid.New().String(),
			"https://example.com/webhook",
			json.RawMessage(`{"index": `+string(rune('0'+i))+`}`),
			nil,
		)
		evt.EndpointID = &endpoint.ID
		evt.ClientID = clientID

		created, err := store.Create(ctx, evt)
		require.NoError(t, err)
		eventIDs = append(eventIDs, created.ID)

		err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
		require.NoError(t, err)
	}
	defer cleanupTestData(t, pool, eventIDs, nil)

	// Verify queue has messages
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(5), length)

	// Mark all queued events as failed
	logger := workerLog.With("test", true)
	fifoProcessor.markQueuedEventsAsFailed(ctx, endpoint.ID.String(), "", logger)

	// Verify queue is empty
	length, err = q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)

	// Verify all events are marked dead
	for _, eventID := range eventIDs {
		updated, err := store.GetByID(ctx, eventID)
		require.NoError(t, err)
		assert.Equal(t, domain.EventStatusDead, updated.Status)
	}
}

// ============================================================================
// Standard Processor Integration Tests
// ============================================================================

func TestStandardProcessor_ProcessOne_Success(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status": "ok"}`))
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, false)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-standard-process-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "standard"}`),
		map[string]string{"Content-Type": "application/json"},
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue to standard queue
	err = q.Enqueue(ctx, created.ID)
	require.NoError(t, err)

	// Process the message
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Verify event was delivered
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, updated.Status)
}

func TestStandardProcessor_ProcessOne_WithPriorityQueue(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, false)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-priority-process-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "priority"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue with priority
	err = q.EnqueueWithPriority(ctx, created.ID, 5) // high priority
	require.NoError(t, err)

	// Process the message
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Verify event was delivered
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, updated.Status)
}

func TestStandardProcessor_ProcessOne_WithRetry(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server that fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, false)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event with retries
	evt := domain.NewEvent(
		"test-standard-retry-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "retry"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID
	evt.MaxAttempts = 3

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue to standard queue
	err = q.Enqueue(ctx, created.ID)
	require.NoError(t, err)

	// Process the message (will fail and nack)
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Verify event status is failed with retry scheduled
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusFailed, updated.Status)
	assert.NotNil(t, updated.NextAttemptAt)
}

func TestStandardProcessor_ProcessOne_EmptyQueue(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	ctx := context.Background()

	// Process with empty queue
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)

	// Should return ErrQueueEmpty
	assert.ErrorIs(t, err, domain.ErrQueueEmpty)
}

func TestStandardProcessor_ProcessOne_MissingEvent(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	ctx := context.Background()

	// Enqueue non-existent event ID
	nonExistentID := uuid.New()
	err = q.Enqueue(ctx, nonExistentID)
	require.NoError(t, err)

	// Process - should ack and continue (event not found)
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)

	// Should complete without error (acks the message)
	require.NoError(t, err)

	// Queue should be empty
	stats, err := q.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(0), stats.Pending)
}

func TestStandardProcessor_ProcessOne_RateLimited(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	// Set up rate limiter
	rateLimiter := NewRateLimiter(redisClient)
	worker.rateLimiter = rateLimiter

	standardProcessor := NewStandardProcessor(worker, config)

	// Create test client and endpoint WITH rate limit enabled
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	// Create endpoint with rate limit of 1 request per second
	endpoint := createTestEndpointWithRateLimit(t, pool, clientID, "https://example.com/webhook", false, 1)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event
	evt := domain.NewEvent(
		"test-standard-ratelimit-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "ratelimit"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue
	err = q.Enqueue(ctx, created.ID)
	require.NoError(t, err)

	// Exhaust the rate limit using endpoint ID
	for range 10 {
		rateLimiter.Allow(ctx, endpoint.ID.String(), 1) // use up the limit
	}

	// Process - should nack due to rate limit
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Message should be nacked to delayed queue
	stats, err := q.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Delayed)
}

func TestStandardProcessor_ProcessOne_CircuitOpen(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	// Configure circuit to trip after 1 failure
	circuitConfig := DefaultCircuitConfig()
	circuitConfig.FailureThreshold = 1

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false
	config.CircuitConfig = circuitConfig

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, false)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Trip the circuit using endpoint ID (which is used as circuit key in Deliver)
	worker.circuit.RecordFailure(endpoint.ID.String())
	worker.circuit.RecordFailure(endpoint.ID.String())

	// Create test event
	evt := domain.NewEvent(
		"test-standard-circuit-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "circuit"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue
	err = q.Enqueue(ctx, created.ID)
	require.NoError(t, err)

	// Process - should nack due to circuit open
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Message should be nacked to delayed queue
	stats, err := q.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Delayed)
}

func TestStandardProcessor_ProcessOne_NoEndpoint(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	ctx := context.Background()

	// Create test event WITHOUT endpoint (nil EndpointID)
	evt := domain.NewEvent(
		"test-no-endpoint-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "no-endpoint"}`),
		nil,
	)
	// Don't set EndpointID - leave it nil

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue
	err = q.Enqueue(ctx, created.ID)
	require.NoError(t, err)

	// Process - should work without endpoint
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Verify event was delivered
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, updated.Status)
}

func TestStandardProcessor_ProcessOne_EndpointDeleted(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create test server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, false)

	ctx := context.Background()

	// Create test event with valid endpoint
	evt := domain.NewEvent(
		"test-endpoint-deleted-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "endpoint-deleted"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue
	err = q.Enqueue(ctx, created.ID)
	require.NoError(t, err)

	// Delete the endpoint AFTER creating the event (simulates endpoint deletion while event pending)
	_, err = pool.Exec(ctx, "DELETE FROM endpoints WHERE id = $1", endpoint.ID)
	require.NoError(t, err)

	// Process - should work even with deleted endpoint (ErrEndpointNotFound is handled gracefully)
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Verify event was delivered (with nil endpoint)
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusDelivered, updated.Status)
}

func TestFIFOProcessor_ProcessOneFIFO_RetryWithDelay(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create server that fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and FIFO endpoint with long retry backoff
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, true)
	// Update endpoint to have longer retry backoff (10 seconds)
	_, err = pool.Exec(context.Background(),
		"UPDATE endpoints SET retry_backoff_ms = $1 WHERE id = $2",
		10000, endpoint.ID)
	require.NoError(t, err)
	endpoint.RetryBackoffMs = 10000
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event with retries
	evt := domain.NewEvent(
		"test-fifo-retry-delay-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "retry-delay"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID
	evt.MaxAttempts = 5

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue to FIFO queue
	err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", created.ID)
	require.NoError(t, err)

	// Process - should fail and nack with delay
	logger := workerLog.With("test", true)
	err = fifoProcessor.processOneFIFO(ctx, endpoint, "", logger)
	require.NoError(t, err)

	// Message should be nacked back to queue (with delay)
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(1), length)

	// Verify event has NextAttemptAt set in the future
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusFailed, updated.Status)
	assert.NotNil(t, updated.NextAttemptAt)
	assert.True(t, updated.NextAttemptAt.After(time.Now()))
}

func TestStandardProcessor_ProcessOne_RetryWithDelay(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	// Create server that fails
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = true
	config.EnableFIFO = false
	config.EnablePriorityQueue = false

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	standardProcessor := NewStandardProcessor(worker, config)

	// Create test client and endpoint with long retry backoff
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, server.URL, false)
	// Update endpoint to have longer retry backoff (10 seconds)
	_, err = pool.Exec(context.Background(),
		"UPDATE endpoints SET retry_backoff_ms = $1 WHERE id = $2",
		10000, endpoint.ID)
	require.NoError(t, err)
	endpoint.RetryBackoffMs = 10000
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Create test event with retries
	evt := domain.NewEvent(
		"test-standard-retry-delay-"+uuid.New().String(),
		server.URL,
		json.RawMessage(`{"test": "retry-delay"}`),
		nil,
	)
	evt.EndpointID = &endpoint.ID
	evt.ClientID = clientID
	evt.MaxAttempts = 5

	created, err := store.Create(ctx, evt)
	require.NoError(t, err)
	defer cleanupTestData(t, pool, []uuid.UUID{created.ID}, nil)

	// Enqueue
	err = q.Enqueue(ctx, created.ID)
	require.NoError(t, err)

	// Process - should fail and nack with delay
	logger := workerLog.With("test", true)
	err = standardProcessor.processOne(ctx, logger)
	require.NoError(t, err)

	// Message should be nacked to delayed queue
	stats, err := q.Stats(ctx)
	require.NoError(t, err)
	assert.Equal(t, int64(1), stats.Delayed)

	// Verify event has NextAttemptAt set in the future
	updated, err := store.GetByID(ctx, created.ID)
	require.NoError(t, err)
	assert.Equal(t, domain.EventStatusFailed, updated.Status)
	assert.NotNil(t, updated.NextAttemptAt)
	assert.True(t, updated.NextAttemptAt.After(time.Now()))
}

func TestFIFOProcessor_MarkQueuedEventsAsFailed_EventNotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	mr, err := miniredis.Run()
	require.NoError(t, err)
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, store, config)
	defer worker.circuit.Stop()

	fifoProcessor := NewFIFOProcessor(worker, config)

	// Create test client and endpoint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := createTestEndpoint(t, pool, clientID, "https://example.com/webhook", true)
	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	ctx := context.Background()

	// Enqueue non-existent event IDs to FIFO queue
	for range 3 {
		nonExistentID := uuid.New()
		err = q.EnqueueFIFO(ctx, endpoint.ID.String(), "", nonExistentID)
		require.NoError(t, err)
	}

	// Verify queue has messages
	length, err := q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(3), length)

	// Mark all queued events as failed - should handle missing events gracefully
	logger := workerLog.With("test", true)
	fifoProcessor.markQueuedEventsAsFailed(ctx, endpoint.ID.String(), "", logger)

	// Queue should be empty (drained even though events not found)
	length, err = q.GetFIFOQueueLength(ctx, endpoint.ID.String(), "")
	require.NoError(t, err)
	assert.Equal(t, int64(0), length)
}
