package event

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/stiffinWanjohi/relay/internal/domain"
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

// createTestClient creates a client in the database for testing endpoint operations
func createTestClient(t *testing.T, pool *pgxpool.Pool) string {
	t.Helper()
	ctx := context.Background()

	clientID := "test-client-" + uuid.New().String()
	_, err := pool.Exec(ctx, `
		INSERT INTO clients (id, name, email, webhook_url_patterns, max_events_per_day, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, NOW(), NOW())
	`, clientID, "Test Client", "test@example.com", []string{"https://example.com/*"}, 10000, true)
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

func TestNewStore(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	if store == nil {
		t.Fatal("expected non-nil store")
	}
	if store.pool != pool {
		t.Error("expected pool to be set")
	}
}

func TestStore_Create(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-idempotency-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "data"}`),
		map[string]string{"Content-Type": "application/json"},
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	created, err := store.Create(ctx, event)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if created.ID != event.ID {
		t.Errorf("expected ID %v, got %v", event.ID, created.ID)
	}
	if created.IdempotencyKey != event.IdempotencyKey {
		t.Errorf("expected idempotency key %s, got %s", event.IdempotencyKey, created.IdempotencyKey)
	}
	if created.Destination != event.Destination {
		t.Errorf("expected destination %s, got %s", event.Destination, created.Destination)
	}
	if created.Status != domain.EventStatusQueued {
		t.Errorf("expected status %s, got %s", domain.EventStatusQueued, created.Status)
	}
}

func TestStore_CreateWithOutbox(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-outbox-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "outbox"}`),
		map[string]string{"Content-Type": "application/json"},
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	created, err := store.CreateWithOutbox(ctx, event)
	if err != nil {
		t.Fatalf("CreateWithOutbox failed: %v", err)
	}

	if created.ID != event.ID {
		t.Errorf("expected ID %v, got %v", event.ID, created.ID)
	}

	// Verify outbox entry was created
	var outboxCount int
	err = pool.QueryRow(ctx, "SELECT COUNT(*) FROM outbox WHERE event_id = $1", created.ID).Scan(&outboxCount)
	if err != nil {
		t.Fatalf("failed to query outbox: %v", err)
	}
	if outboxCount != 1 {
		t.Errorf("expected 1 outbox entry, got %d", outboxCount)
	}
}

func TestStore_GetByID(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-getbyid-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "getbyid"}`),
		map[string]string{"X-Test": "header"},
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	_, err := store.Create(ctx, event)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.GetByID(ctx, event.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if retrieved.ID != event.ID {
		t.Errorf("expected ID %v, got %v", event.ID, retrieved.ID)
	}
	if retrieved.IdempotencyKey != event.IdempotencyKey {
		t.Errorf("expected idempotency key %s, got %s", event.IdempotencyKey, retrieved.IdempotencyKey)
	}
}

func TestStore_GetByID_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.GetByID(ctx, uuid.New())
	if err != domain.ErrEventNotFound {
		t.Errorf("expected ErrEventNotFound, got %v", err)
	}
}

func TestStore_GetByIdempotencyKey(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	idempotencyKey := "test-idem-key-" + uuid.New().String()
	event := domain.NewEvent(
		idempotencyKey,
		"https://example.com/webhook",
		json.RawMessage(`{"test": "idempotency"}`),
		nil,
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	_, err := store.Create(ctx, event)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.GetByIdempotencyKey(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("GetByIdempotencyKey failed: %v", err)
	}

	if retrieved.IdempotencyKey != idempotencyKey {
		t.Errorf("expected idempotency key %s, got %s", idempotencyKey, retrieved.IdempotencyKey)
	}
}

func TestStore_GetByIdempotencyKey_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.GetByIdempotencyKey(ctx, "nonexistent-key")
	if err != domain.ErrEventNotFound {
		t.Errorf("expected ErrEventNotFound, got %v", err)
	}
}

func TestStore_Update(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-update-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "update"}`),
		nil,
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	created, err := store.Create(ctx, event)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update the event
	created.Status = domain.EventStatusDelivered
	created.Attempts = 1
	now := time.Now().UTC()
	created.DeliveredAt = &now

	updated, err := store.Update(ctx, created)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if updated.Status != domain.EventStatusDelivered {
		t.Errorf("expected status %s, got %s", domain.EventStatusDelivered, updated.Status)
	}
	if updated.Attempts != 1 {
		t.Errorf("expected attempts 1, got %d", updated.Attempts)
	}
	if updated.DeliveredAt == nil {
		t.Error("expected delivered_at to be set")
	}
}

func TestStore_Update_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.Event{
		ID:          uuid.New(),
		Destination: "https://example.com",
		Status:      domain.EventStatusQueued,
	}

	_, err := store.Update(ctx, event)
	if err != domain.ErrEventNotFound {
		t.Errorf("expected ErrEventNotFound, got %v", err)
	}
}

func TestStore_ListByStatus(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	var eventIDs []uuid.UUID

	// Create multiple events
	for i := range 3 {
		event := domain.NewEvent(
			"test-list-"+uuid.New().String(),
			"https://example.com/webhook",
			json.RawMessage(`{"index": `+string(rune('0'+i))+`}`),
			nil,
		)
		eventIDs = append(eventIDs, event.ID)

		_, err := store.Create(ctx, event)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	defer cleanupTestData(t, pool, eventIDs, nil)

	events, err := store.ListByStatus(ctx, domain.EventStatusQueued, 10, 0)
	if err != nil {
		t.Fatalf("ListByStatus failed: %v", err)
	}

	if len(events) < 3 {
		t.Errorf("expected at least 3 events, got %d", len(events))
	}
}

func TestStore_ListReadyForDelivery(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-ready-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "ready"}`),
		nil,
	)
	event.NextAttemptAt = nil // Should be ready immediately

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	_, err := store.Create(ctx, event)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	events, err := store.ListReadyForDelivery(ctx, 10)
	if err != nil {
		t.Fatalf("ListReadyForDelivery failed: %v", err)
	}

	found := false
	for _, e := range events {
		if e.ID == event.ID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected to find the created event in ready list")
	}
}

func TestStore_CreateDeliveryAttempt(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-attempt-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "attempt"}`),
		nil,
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	_, err := store.Create(ctx, event)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	attempt := domain.DeliveryAttempt{
		ID:            uuid.New(),
		EventID:       event.ID,
		StatusCode:    200,
		ResponseBody:  "OK",
		DurationMs:    150,
		AttemptNumber: 1,
		AttemptedAt:   time.Now().UTC(),
	}

	created, err := store.CreateDeliveryAttempt(ctx, attempt)
	if err != nil {
		t.Fatalf("CreateDeliveryAttempt failed: %v", err)
	}

	if created.ID != attempt.ID {
		t.Errorf("expected ID %v, got %v", attempt.ID, created.ID)
	}
	if created.StatusCode != 200 {
		t.Errorf("expected status code 200, got %d", created.StatusCode)
	}
}

func TestStore_GetDeliveryAttempts(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-get-attempts-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "attempts"}`),
		nil,
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	_, err := store.Create(ctx, event)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Create multiple attempts
	for i := range 3 {
		attempt := domain.DeliveryAttempt{
			ID:            uuid.New(),
			EventID:       event.ID,
			StatusCode:    500,
			Error:         "server error",
			DurationMs:    100,
			AttemptNumber: i + 1,
			AttemptedAt:   time.Now().UTC(),
		}
		_, err := store.CreateDeliveryAttempt(ctx, attempt)
		if err != nil {
			t.Fatalf("CreateDeliveryAttempt failed: %v", err)
		}
	}

	attempts, err := store.GetDeliveryAttempts(ctx, event.ID)
	if err != nil {
		t.Fatalf("GetDeliveryAttempts failed: %v", err)
	}

	if len(attempts) != 3 {
		t.Errorf("expected 3 attempts, got %d", len(attempts))
	}
}

func TestStore_GetQueueStats(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	stats, err := store.GetQueueStats(ctx)
	if err != nil {
		t.Fatalf("GetQueueStats failed: %v", err)
	}

	// Just verify the query works - actual counts depend on DB state
	if stats.Queued < 0 || stats.Delivered < 0 || stats.Failed < 0 {
		t.Error("expected non-negative stats")
	}
}

func TestStore_OutboxOperations(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-outbox-ops-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "outbox"}`),
		nil,
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	created, err := store.CreateWithOutbox(ctx, event)
	if err != nil {
		t.Fatalf("CreateWithOutbox failed: %v", err)
	}

	// Get unprocessed outbox entries
	entries, err := store.GetUnprocessedOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("GetUnprocessedOutbox failed: %v", err)
	}

	var outboxEntry *OutboxEntry
	for _, e := range entries {
		if e.EventID == created.ID {
			outboxEntry = &e
			break
		}
	}
	if outboxEntry == nil {
		t.Fatal("expected to find outbox entry")
	}

	// Mark as processed
	err = store.MarkOutboxProcessed(ctx, outboxEntry.ID)
	if err != nil {
		t.Fatalf("MarkOutboxProcessed failed: %v", err)
	}

	// Verify it's no longer in unprocessed
	entries, err = store.GetUnprocessedOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("GetUnprocessedOutbox failed: %v", err)
	}

	for _, e := range entries {
		if e.EventID == created.ID {
			t.Error("expected outbox entry to be marked as processed")
		}
	}
}

func TestStore_MarkOutboxFailed(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-outbox-fail-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "fail"}`),
		nil,
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	created, err := store.CreateWithOutbox(ctx, event)
	if err != nil {
		t.Fatalf("CreateWithOutbox failed: %v", err)
	}

	entries, err := store.GetUnprocessedOutbox(ctx, 10)
	if err != nil {
		t.Fatalf("GetUnprocessedOutbox failed: %v", err)
	}

	var outboxEntry *OutboxEntry
	for _, e := range entries {
		if e.EventID == created.ID {
			outboxEntry = &e
			break
		}
	}
	if outboxEntry == nil {
		t.Fatal("expected to find outbox entry")
	}

	// Mark as failed
	err = store.MarkOutboxFailed(ctx, outboxEntry.ID, "test error")
	if err != nil {
		t.Fatalf("MarkOutboxFailed failed: %v", err)
	}

	// Verify attempts incremented and error recorded
	var attempts int
	var lastError *string
	err = pool.QueryRow(ctx, "SELECT attempts, last_error FROM outbox WHERE id = $1", outboxEntry.ID).Scan(&attempts, &lastError)
	if err != nil {
		t.Fatalf("failed to query outbox: %v", err)
	}
	if attempts != 1 {
		t.Errorf("expected attempts 1, got %d", attempts)
	}
	if lastError == nil || *lastError != "test error" {
		t.Errorf("expected last_error 'test error', got %v", lastError)
	}
}

func TestStore_ClaimAndGetOutbox(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	event := domain.NewEvent(
		"test-claim-"+uuid.New().String(),
		"https://example.com/webhook",
		json.RawMessage(`{"test": "claim"}`),
		nil,
	)

	defer cleanupTestData(t, pool, []uuid.UUID{event.ID}, nil)

	_, err := store.CreateWithOutbox(ctx, event)
	if err != nil {
		t.Fatalf("CreateWithOutbox failed: %v", err)
	}

	// Claim entries
	entries, err := store.ClaimAndGetOutbox(ctx, "worker-1", 10, 5*time.Minute)
	if err != nil {
		t.Fatalf("ClaimAndGetOutbox failed: %v", err)
	}

	if len(entries) == 0 {
		t.Skip("no entries to claim (may have been claimed by another test)")
	}
}

func TestStore_CleanupProcessedOutbox(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Just verify the function works
	deleted, err := store.CleanupProcessedOutbox(ctx, 24*time.Hour)
	if err != nil {
		t.Fatalf("CleanupProcessedOutbox failed: %v", err)
	}

	if deleted < 0 {
		t.Error("expected non-negative deleted count")
	}
}

func TestStore_CreateEndpoint(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := domain.NewEndpoint(
		clientID,
		"https://example.com/webhook",
		[]string{"order.created", "order.updated"},
	)

	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	created, err := store.CreateEndpoint(ctx, endpoint)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	if created.ID != endpoint.ID {
		t.Errorf("expected ID %v, got %v", endpoint.ID, created.ID)
	}
	if created.URL != endpoint.URL {
		t.Errorf("expected URL %s, got %s", endpoint.URL, created.URL)
	}
	if len(created.EventTypes) != 2 {
		t.Errorf("expected 2 event types, got %d", len(created.EventTypes))
	}
}

func TestStore_UpdateEndpoint(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := domain.NewEndpoint(
		clientID,
		"https://example.com/webhook",
		[]string{"order.created"},
	)

	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	created, err := store.CreateEndpoint(ctx, endpoint)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	// Update the endpoint
	created.URL = "https://updated.example.com/webhook"
	created.Status = domain.EndpointStatusPaused

	updated, err := store.UpdateEndpoint(ctx, created)
	if err != nil {
		t.Fatalf("UpdateEndpoint failed: %v", err)
	}

	if updated.URL != "https://updated.example.com/webhook" {
		t.Errorf("expected updated URL, got %s", updated.URL)
	}
	if updated.Status != domain.EndpointStatusPaused {
		t.Errorf("expected status paused, got %s", updated.Status)
	}
}

func TestStore_UpdateEndpoint_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	endpoint := domain.Endpoint{
		ID:       uuid.New(),
		ClientID: "test",
		URL:      "https://example.com",
		Status:   domain.EndpointStatusActive,
	}

	_, err := store.UpdateEndpoint(ctx, endpoint)
	if err != domain.ErrEndpointNotFound {
		t.Errorf("expected ErrEndpointNotFound, got %v", err)
	}
}

func TestStore_GetEndpointByID(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := domain.NewEndpoint(
		clientID,
		"https://example.com/webhook",
		[]string{"*"},
	)

	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	_, err := store.CreateEndpoint(ctx, endpoint)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	retrieved, err := store.GetEndpointByID(ctx, endpoint.ID)
	if err != nil {
		t.Fatalf("GetEndpointByID failed: %v", err)
	}

	if retrieved.ID != endpoint.ID {
		t.Errorf("expected ID %v, got %v", endpoint.ID, retrieved.ID)
	}
}

func TestStore_GetEndpointByID_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.GetEndpointByID(ctx, uuid.New())
	if err != domain.ErrEndpointNotFound {
		t.Errorf("expected ErrEndpointNotFound, got %v", err)
	}
}

func TestStore_ListEndpointsByClient(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	var endpointIDs []uuid.UUID

	// Create multiple endpoints for the same client
	for range 3 {
		endpoint := domain.NewEndpoint(
			clientID,
			"https://example.com/webhook/"+uuid.New().String(),
			[]string{"order.created"},
		)
		endpointIDs = append(endpointIDs, endpoint.ID)

		_, err := store.CreateEndpoint(ctx, endpoint)
		if err != nil {
			t.Fatalf("CreateEndpoint failed: %v", err)
		}
	}

	defer cleanupTestData(t, pool, nil, endpointIDs)

	endpoints, err := store.ListEndpointsByClient(ctx, clientID, 10, 0)
	if err != nil {
		t.Fatalf("ListEndpointsByClient failed: %v", err)
	}

	if len(endpoints) != 3 {
		t.Errorf("expected 3 endpoints, got %d", len(endpoints))
	}
}

func TestStore_FindActiveEndpointsByEventType(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	var endpointIDs []uuid.UUID

	// Create endpoint subscribed to specific event
	ep1 := domain.NewEndpoint(clientID, "https://example.com/webhook1", []string{"order.created"})
	endpointIDs = append(endpointIDs, ep1.ID)
	_, err := store.CreateEndpoint(ctx, ep1)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	// Create endpoint subscribed to wildcard
	ep2 := domain.NewEndpoint(clientID, "https://example.com/webhook2", []string{"*"})
	endpointIDs = append(endpointIDs, ep2.ID)
	_, err = store.CreateEndpoint(ctx, ep2)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	// Create endpoint subscribed to different event
	ep3 := domain.NewEndpoint(clientID, "https://example.com/webhook3", []string{"user.created"})
	endpointIDs = append(endpointIDs, ep3.ID)
	_, err = store.CreateEndpoint(ctx, ep3)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	defer cleanupTestData(t, pool, nil, endpointIDs)

	// Find endpoints for order.created
	endpoints, err := store.FindActiveEndpointsByEventType(ctx, clientID, "order.created")
	if err != nil {
		t.Fatalf("FindActiveEndpointsByEventType failed: %v", err)
	}

	// Should find ep1 (specific) and ep2 (wildcard), not ep3
	if len(endpoints) != 2 {
		t.Errorf("expected 2 endpoints, got %d", len(endpoints))
	}
}

func TestStore_DeleteEndpoint(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	endpoint := domain.NewEndpoint(
		clientID,
		"https://example.com/webhook",
		[]string{"order.created"},
	)

	_, err := store.CreateEndpoint(ctx, endpoint)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	err = store.DeleteEndpoint(ctx, endpoint.ID)
	if err != nil {
		t.Fatalf("DeleteEndpoint failed: %v", err)
	}

	// Verify deletion
	_, err = store.GetEndpointByID(ctx, endpoint.ID)
	if err != domain.ErrEndpointNotFound {
		t.Errorf("expected ErrEndpointNotFound after deletion, got %v", err)
	}
}

func TestStore_DeleteEndpoint_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	err := store.DeleteEndpoint(ctx, uuid.New())
	if err != domain.ErrEndpointNotFound {
		t.Errorf("expected ErrEndpointNotFound, got %v", err)
	}
}

func TestStore_CountEndpointsByClient(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	var endpointIDs []uuid.UUID

	// Create endpoints
	for range 2 {
		endpoint := domain.NewEndpoint(clientID, "https://example.com/webhook/"+uuid.New().String(), []string{"*"})
		endpointIDs = append(endpointIDs, endpoint.ID)
		_, err := store.CreateEndpoint(ctx, endpoint)
		if err != nil {
			t.Fatalf("CreateEndpoint failed: %v", err)
		}
	}

	defer cleanupTestData(t, pool, nil, endpointIDs)

	count, err := store.CountEndpointsByClient(ctx, clientID)
	if err != nil {
		t.Fatalf("CountEndpointsByClient failed: %v", err)
	}

	if count != 2 {
		t.Errorf("expected count 2, got %d", count)
	}
}

func TestStore_CreateEventWithFanout(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	var endpointIDs []uuid.UUID
	var eventIDs []uuid.UUID

	// Create two endpoints subscribed to the same event type
	ep1 := domain.NewEndpoint(clientID, "https://example.com/webhook1", []string{"order.created"})
	endpointIDs = append(endpointIDs, ep1.ID)
	_, err := store.CreateEndpoint(ctx, ep1)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	ep2 := domain.NewEndpoint(clientID, "https://example.com/webhook2", []string{"order.created"})
	endpointIDs = append(endpointIDs, ep2.ID)
	_, err = store.CreateEndpoint(ctx, ep2)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	defer func() {
		cleanupTestData(t, pool, eventIDs, endpointIDs)
	}()

	// Create event with fanout
	payload := json.RawMessage(`{"order_id": "123"}`)
	headers := map[string]string{"Content-Type": "application/json"}

	events, err := store.CreateEventWithFanout(ctx, clientID, "order.created", "test-fanout-"+uuid.New().String(), payload, headers)
	if err != nil {
		t.Fatalf("CreateEventWithFanout failed: %v", err)
	}

	for _, e := range events {
		eventIDs = append(eventIDs, e.ID)
	}

	if len(events) != 2 {
		t.Errorf("expected 2 events (fanout to 2 endpoints), got %d", len(events))
	}

	// Verify each event has different endpoint
	endpoints := make(map[uuid.UUID]bool)
	for _, e := range events {
		if e.EndpointID != nil {
			endpoints[*e.EndpointID] = true
		}
	}
	if len(endpoints) != 2 {
		t.Errorf("expected events to have 2 different endpoints, got %d", len(endpoints))
	}
}

func TestStore_CreateEventWithFanout_NoEndpoints(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	clientID := "nonexistent-client-" + uuid.New().String()
	payload := json.RawMessage(`{"test": "data"}`)

	_, err := store.CreateEventWithFanout(ctx, clientID, "order.created", "test-"+uuid.New().String(), payload, nil)
	if err != domain.ErrNoSubscribedEndpoints {
		t.Errorf("expected ErrNoSubscribedEndpoints, got %v", err)
	}
}

func TestStore_ListEventsByEndpoint(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	// Create endpoint
	endpoint := domain.NewEndpoint(clientID, "https://example.com/webhook", []string{"*"})
	_, err := store.CreateEndpoint(ctx, endpoint)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	var eventIDs []uuid.UUID
	defer func() {
		cleanupTestData(t, pool, eventIDs, []uuid.UUID{endpoint.ID})
	}()

	// Create events with fanout to get events assigned to this endpoint
	for range 2 {
		events, err := store.CreateEventWithFanout(ctx, clientID, "test.event", "test-"+uuid.New().String(), json.RawMessage(`{}`), nil)
		if err != nil {
			t.Fatalf("CreateEventWithFanout failed: %v", err)
		}
		for _, e := range events {
			eventIDs = append(eventIDs, e.ID)
		}
	}

	// List events by endpoint
	events, err := store.ListEventsByEndpoint(ctx, endpoint.ID, 10, 0)
	if err != nil {
		t.Fatalf("ListEventsByEndpoint failed: %v", err)
	}

	if len(events) != 2 {
		t.Errorf("expected 2 events, got %d", len(events))
	}
}

func TestStore_GetEndpointStats(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Create a test client first to satisfy foreign key constraint
	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	// Create endpoint
	endpoint := domain.NewEndpoint(clientID, "https://example.com/webhook", []string{"*"})
	_, err := store.CreateEndpoint(ctx, endpoint)
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	defer cleanupTestData(t, pool, nil, []uuid.UUID{endpoint.ID})

	stats, err := store.GetEndpointStats(ctx, endpoint.ID)
	if err != nil {
		t.Fatalf("GetEndpointStats failed: %v", err)
	}

	// Just verify the query works
	if stats.TotalEvents < 0 {
		t.Error("expected non-negative total events")
	}
}

func TestNullString(t *testing.T) {
	tests := []struct {
		input    string
		expected *string
	}{
		{"", nil},
		{"hello", ptrString("hello")},
		{"test-client", ptrString("test-client")},
	}

	for _, tc := range tests {
		result := nullString(tc.input)
		if tc.expected == nil {
			if result != nil {
				t.Errorf("nullString(%q) = %v, expected nil", tc.input, *result)
			}
		} else {
			if result == nil {
				t.Errorf("nullString(%q) = nil, expected %v", tc.input, *tc.expected)
			} else if *result != *tc.expected {
				t.Errorf("nullString(%q) = %v, expected %v", tc.input, *result, *tc.expected)
			}
		}
	}
}

func ptrString(s string) *string {
	return &s
}
