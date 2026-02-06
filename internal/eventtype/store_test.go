package eventtype

import (
	"context"
	"os"
	"testing"

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

// createTestClient creates a client in the database for testing
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

// cleanupTestClient removes a test client and related data from the database
func cleanupTestClient(t *testing.T, pool *pgxpool.Pool, clientID string) {
	t.Helper()
	ctx := context.Background()
	_, _ = pool.Exec(ctx, "DELETE FROM event_types WHERE client_id = $1", clientID)
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
		return
	}
	if store.pool != pool {
		t.Error("expected pool to be set")
	}
}

func TestStore_Create(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.NewEventType(clientID, "order.created", "Order creation events")

	created, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if created.ID != et.ID {
		t.Errorf("expected ID %v, got %v", et.ID, created.ID)
	}
	if created.ClientID != clientID {
		t.Errorf("expected client ID %s, got %s", clientID, created.ClientID)
	}
	if created.Name != "order.created" {
		t.Errorf("expected name 'order.created', got %s", created.Name)
	}
	if created.Description != "Order creation events" {
		t.Errorf("expected description 'Order creation events', got %s", created.Description)
	}
}

func TestStore_Create_WithSchema(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	schema := []byte(`{"type":"object","required":["order_id"],"properties":{"order_id":{"type":"string"}}}`)
	et := domain.NewEventType(clientID, "order.created", "Order events").
		WithSchema(schema, "1.0")

	created, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !created.HasSchema() {
		t.Error("expected event type to have schema")
	}
	if created.SchemaVersion != "1.0" {
		t.Errorf("expected schema version '1.0', got %s", created.SchemaVersion)
	}
	if string(created.Schema) != string(schema) {
		t.Errorf("expected schema %s, got %s", string(schema), string(created.Schema))
	}
}

func TestStore_Create_Duplicate(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.NewEventType(clientID, "order.created", "Order events")
	_, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Try to create duplicate
	et2 := domain.NewEventType(clientID, "order.created", "Duplicate")
	_, err = store.Create(ctx, et2)
	if err != domain.ErrDuplicateEventType {
		t.Errorf("expected ErrDuplicateEventType, got %v", err)
	}
}

func TestStore_GetByID(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.NewEventType(clientID, "order.created", "Order events")
	_, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.GetByID(ctx, et.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}

	if retrieved.ID != et.ID {
		t.Errorf("expected ID %v, got %v", et.ID, retrieved.ID)
	}
	if retrieved.Name != "order.created" {
		t.Errorf("expected name 'order.created', got %s", retrieved.Name)
	}
}

func TestStore_GetByID_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.GetByID(ctx, uuid.New())
	if err != domain.ErrEventTypeNotFound {
		t.Errorf("expected ErrEventTypeNotFound, got %v", err)
	}
}

func TestStore_GetByName(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.NewEventType(clientID, "user.registered", "User registration events")
	_, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	retrieved, err := store.GetByName(ctx, clientID, "user.registered")
	if err != nil {
		t.Fatalf("GetByName failed: %v", err)
	}

	if retrieved.ID != et.ID {
		t.Errorf("expected ID %v, got %v", et.ID, retrieved.ID)
	}
	if retrieved.Name != "user.registered" {
		t.Errorf("expected name 'user.registered', got %s", retrieved.Name)
	}
}

func TestStore_GetByName_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.GetByName(ctx, "nonexistent-client", "nonexistent.event")
	if err != domain.ErrEventTypeNotFound {
		t.Errorf("expected ErrEventTypeNotFound, got %v", err)
	}
}

func TestStore_List(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	// Create multiple event types
	eventNames := []string{"a.event", "b.event", "c.event"}
	for _, name := range eventNames {
		et := domain.NewEventType(clientID, name, "Description for "+name)
		_, err := store.Create(ctx, et)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	types, err := store.List(ctx, clientID, 10, 0)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}

	if len(types) != 3 {
		t.Errorf("expected 3 event types, got %d", len(types))
	}

	// Should be ordered by name ASC
	if types[0].Name != "a.event" {
		t.Errorf("expected first event 'a.event', got %s", types[0].Name)
	}
	if types[2].Name != "c.event" {
		t.Errorf("expected last event 'c.event', got %s", types[2].Name)
	}
}

func TestStore_List_Pagination(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	// Create 5 event types
	for i := range 5 {
		et := domain.NewEventType(clientID, "event."+string(rune('a'+i)), "Description")
		_, err := store.Create(ctx, et)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	// Get first page
	page1, err := store.List(ctx, clientID, 2, 0)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(page1) != 2 {
		t.Errorf("expected 2 event types on page 1, got %d", len(page1))
	}

	// Get second page
	page2, err := store.List(ctx, clientID, 2, 2)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(page2) != 2 {
		t.Errorf("expected 2 event types on page 2, got %d", len(page2))
	}

	// Get third page
	page3, err := store.List(ctx, clientID, 2, 4)
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(page3) != 1 {
		t.Errorf("expected 1 event type on page 3, got %d", len(page3))
	}
}

func TestStore_Count(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	// Create event types
	for i := range 3 {
		et := domain.NewEventType(clientID, "event."+string(rune('a'+i)), "Description")
		_, err := store.Create(ctx, et)
		if err != nil {
			t.Fatalf("Create failed: %v", err)
		}
	}

	count, err := store.Count(ctx, clientID)
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	if count != 3 {
		t.Errorf("expected count 3, got %d", count)
	}
}

func TestStore_Count_Empty(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	count, err := store.Count(ctx, "nonexistent-client")
	if err != nil {
		t.Fatalf("Count failed: %v", err)
	}

	if count != 0 {
		t.Errorf("expected count 0, got %d", count)
	}
}

func TestStore_Update(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.NewEventType(clientID, "order.created", "Original description")
	created, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Update with new description and schema
	schema := []byte(`{"type":"object"}`)
	updated := created.WithDescription("Updated description").WithSchema(schema, "1.0")

	result, err := store.Update(ctx, updated)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if result.Description != "Updated description" {
		t.Errorf("expected description 'Updated description', got %s", result.Description)
	}
	if !result.HasSchema() {
		t.Error("expected event type to have schema")
	}
	if result.SchemaVersion != "1.0" {
		t.Errorf("expected schema version '1.0', got %s", result.SchemaVersion)
	}
}

func TestStore_Update_ClearSchema(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	schema := []byte(`{"type":"object"}`)
	et := domain.NewEventType(clientID, "order.created", "Description").
		WithSchema(schema, "1.0")
	created, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	if !created.HasSchema() {
		t.Error("expected event type to have schema initially")
	}

	// Clear the schema
	cleared := created.ClearSchema()
	result, err := store.Update(ctx, cleared)
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	if result.HasSchema() {
		t.Error("expected event type to not have schema after clearing")
	}
}

func TestStore_Update_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.EventType{
		ID:   uuid.New(),
		Name: "nonexistent",
	}

	_, err := store.Update(ctx, et)
	if err != domain.ErrEventTypeNotFound {
		t.Errorf("expected ErrEventTypeNotFound, got %v", err)
	}
}

func TestStore_Delete(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.NewEventType(clientID, "order.created", "Order events")
	_, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	err = store.Delete(ctx, et.ID)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify deletion
	_, err = store.GetByID(ctx, et.ID)
	if err != domain.ErrEventTypeNotFound {
		t.Errorf("expected ErrEventTypeNotFound after deletion, got %v", err)
	}
}

func TestStore_Delete_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	err := store.Delete(ctx, uuid.New())
	if err != domain.ErrEventTypeNotFound {
		t.Errorf("expected ErrEventTypeNotFound, got %v", err)
	}
}

func TestStore_Exists(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID)

	store := NewStore(pool)
	ctx := context.Background()

	et := domain.NewEventType(clientID, "order.created", "Order events")
	_, err := store.Create(ctx, et)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	exists, err := store.Exists(ctx, clientID, "order.created")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if !exists {
		t.Error("expected event type to exist")
	}
}

func TestStore_Exists_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	exists, err := store.Exists(ctx, "nonexistent-client", "nonexistent.event")
	if err != nil {
		t.Fatalf("Exists failed: %v", err)
	}
	if exists {
		t.Error("expected event type to not exist")
	}
}

func TestStore_ClientIsolation(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	clientID1 := createTestClient(t, pool)
	clientID2 := createTestClient(t, pool)
	defer cleanupTestClient(t, pool, clientID1)
	defer cleanupTestClient(t, pool, clientID2)

	store := NewStore(pool)
	ctx := context.Background()

	// Create event type for client 1
	et1 := domain.NewEventType(clientID1, "order.created", "Client 1 events")
	_, err := store.Create(ctx, et1)
	if err != nil {
		t.Fatalf("Create failed: %v", err)
	}

	// Create event type with same name for client 2
	et2 := domain.NewEventType(clientID2, "order.created", "Client 2 events")
	_, err = store.Create(ctx, et2)
	if err != nil {
		t.Fatalf("Create for client 2 failed: %v", err)
	}

	// Verify client isolation - each client sees only their own types
	types1, err := store.List(ctx, clientID1, 10, 0)
	if err != nil {
		t.Fatalf("List for client 1 failed: %v", err)
	}
	if len(types1) != 1 {
		t.Errorf("expected 1 event type for client 1, got %d", len(types1))
	}
	if types1[0].Description != "Client 1 events" {
		t.Error("client 1 saw client 2's event type")
	}

	types2, err := store.List(ctx, clientID2, 10, 0)
	if err != nil {
		t.Fatalf("List for client 2 failed: %v", err)
	}
	if len(types2) != 1 {
		t.Errorf("expected 1 event type for client 2, got %d", len(types2))
	}
	if types2[0].Description != "Client 2 events" {
		t.Error("client 2 saw client 1's event type")
	}

	// GetByName should also be isolated
	retrieved1, err := store.GetByName(ctx, clientID1, "order.created")
	if err != nil {
		t.Fatalf("GetByName for client 1 failed: %v", err)
	}
	if retrieved1.Description != "Client 1 events" {
		t.Error("GetByName returned wrong client's event type")
	}
}

func TestNullString(t *testing.T) {
	tests := []struct {
		input    string
		expected *string
	}{
		{"", nil},
		{"hello", ptrString("hello")},
		{"test value", ptrString("test value")},
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
