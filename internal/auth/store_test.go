package auth

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
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

func TestHashAPIKey(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int // length of hex-encoded SHA-256 hash
	}{
		{"empty string", "", 64},
		{"simple string", "test-api-key", 64},
		{"long string", "rly_" + strings.Repeat("a", 64), 64},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := hashAPIKey(tc.input)
			if len(result) != tc.expected {
				t.Errorf("expected hash length %d, got %d", tc.expected, len(result))
			}
		})
	}
}

func TestHashAPIKey_Deterministic(t *testing.T) {
	key := "my-secret-key"
	hash1 := hashAPIKey(key)
	hash2 := hashAPIKey(key)

	if hash1 != hash2 {
		t.Error("hash should be deterministic")
	}
}

func TestHashAPIKey_DifferentInputs(t *testing.T) {
	hash1 := hashAPIKey("key1")
	hash2 := hashAPIKey("key2")

	if hash1 == hash2 {
		t.Error("different inputs should produce different hashes")
	}
}

func TestGenerateAPIKey(t *testing.T) {
	key := generateAPIKey()

	// Should start with "rly_"
	if !strings.HasPrefix(key, "rly_") {
		t.Errorf("expected key to start with 'rly_', got %s", key)
	}

	// Should be rly_ + 64 hex chars = 68 chars total
	if len(key) != 68 {
		t.Errorf("expected key length 68, got %d", len(key))
	}
}

func TestGenerateAPIKey_Unique(t *testing.T) {
	keys := make(map[string]bool)

	for range 100 {
		key := generateAPIKey()
		if keys[key] {
			t.Error("generated duplicate key")
		}
		keys[key] = true
	}
}

func TestGenerateRandomHex(t *testing.T) {
	tests := []struct {
		byteLen     int
		expectedLen int // hex encoded length = byteLen * 2
	}{
		{16, 32},
		{32, 64},
		{64, 128},
	}

	for _, tc := range tests {
		result := generateRandomHex(tc.byteLen)
		if len(result) != tc.expectedLen {
			t.Errorf("generateRandomHex(%d) length = %d, expected %d", tc.byteLen, len(result), tc.expectedLen)
		}
	}
}

func TestGenerateRandomHex_ValidHex(t *testing.T) {
	result := generateRandomHex(16)

	// Should only contain hex characters
	for _, c := range result {
		isDigit := c >= '0' && c <= '9'
		isHexLetter := c >= 'a' && c <= 'f'
		if !isDigit && !isHexLetter {
			t.Errorf("invalid hex character: %c", c)
		}
	}
}

// Integration tests that require a database

func TestStore_ValidateAPIKey_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.ValidateAPIKey(ctx, "nonexistent-key")
	if err != ErrInvalidAPIKey {
		t.Errorf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestStore_GetClient_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	_, err := store.GetClient(ctx, "nonexistent-client-id")
	if err != ErrUnauthorized {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestStore_RevokeAPIKey_NotFound(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Use a valid UUID format that doesn't exist
	nonExistentID := "00000000-0000-0000-0000-000000000000"
	err := store.RevokeAPIKey(ctx, nonExistentID)
	if err != ErrInvalidAPIKey {
		t.Errorf("expected ErrInvalidAPIKey, got %v", err)
	}
}

func TestStore_ListAPIKeys_EmptyClient(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	keys, err := store.ListAPIKeys(ctx, "nonexistent-client")
	if err != nil {
		t.Fatalf("ListAPIKeys failed: %v", err)
	}

	if len(keys) != 0 {
		t.Errorf("expected 0 keys for nonexistent client, got %d", len(keys))
	}
}

func TestStore_FullWorkflow(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	// Generate a unique client ID
	clientID := "test-client-" + generateRandomHex(8)

	// Cleanup at the end
	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM api_keys WHERE client_id = $1", clientID)
		_, _ = pool.Exec(ctx, "DELETE FROM clients WHERE id = $1", clientID)
	}()

	// Create client
	client := Client{
		ID:                 clientID,
		Name:               "Test Client",
		Email:              "test@example.com",
		WebhookURLPatterns: []string{"https://example.com/*"},
		MaxEventsPerDay:    10000,
		IsActive:           true,
	}

	createdClient, err := store.CreateClient(ctx, client)
	if err != nil {
		t.Fatalf("CreateClient failed: %v", err)
	}

	if createdClient.ID != clientID {
		t.Errorf("expected client ID %s, got %s", clientID, createdClient.ID)
	}

	// Get client
	retrievedClient, err := store.GetClient(ctx, clientID)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}

	if retrievedClient.Name != "Test Client" {
		t.Errorf("expected name 'Test Client', got %s", retrievedClient.Name)
	}

	// Create API key
	expires := time.Now().Add(24 * time.Hour)
	rawKey, apiKey, err := store.CreateAPIKey(ctx, clientID, "Test Key", []string{"read", "write"}, 1000, &expires)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	if !strings.HasPrefix(rawKey, "rly_") {
		t.Errorf("expected raw key to start with 'rly_', got %s", rawKey[:4])
	}

	if apiKey.ClientID != clientID {
		t.Errorf("expected client ID %s, got %s", clientID, apiKey.ClientID)
	}

	if apiKey.Name != "Test Key" {
		t.Errorf("expected name 'Test Key', got %s", apiKey.Name)
	}

	// Validate API key
	validatedClientID, err := store.ValidateAPIKey(ctx, rawKey)
	if err != nil {
		t.Fatalf("ValidateAPIKey failed: %v", err)
	}

	if validatedClientID != clientID {
		t.Errorf("expected client ID %s, got %s", clientID, validatedClientID)
	}

	// List API keys
	keys, err := store.ListAPIKeys(ctx, clientID)
	if err != nil {
		t.Fatalf("ListAPIKeys failed: %v", err)
	}

	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}

	// Revoke API key
	err = store.RevokeAPIKey(ctx, apiKey.ID)
	if err != nil {
		t.Fatalf("RevokeAPIKey failed: %v", err)
	}

	// Validate should now fail
	_, err = store.ValidateAPIKey(ctx, rawKey)
	if err != ErrInvalidAPIKey {
		t.Errorf("expected ErrInvalidAPIKey after revocation, got %v", err)
	}
}

func TestStore_ValidateAPIKey_ExpiredKey(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	clientID := "test-client-expired-" + generateRandomHex(8)

	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM api_keys WHERE client_id = $1", clientID)
		_, _ = pool.Exec(ctx, "DELETE FROM clients WHERE id = $1", clientID)
	}()

	// Create client
	client := Client{
		ID:       clientID,
		Name:     "Expired Test Client",
		Email:    "expired@example.com",
		IsActive: true,
	}

	_, err := store.CreateClient(ctx, client)
	if err != nil {
		t.Fatalf("CreateClient failed: %v", err)
	}

	// Create API key that already expired
	expired := time.Now().Add(-1 * time.Hour)
	rawKey, _, err := store.CreateAPIKey(ctx, clientID, "Expired Key", nil, 1000, &expired)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	// Validate should fail due to expiration
	_, err = store.ValidateAPIKey(ctx, rawKey)
	if err != ErrInvalidAPIKey {
		t.Errorf("expected ErrInvalidAPIKey for expired key, got %v", err)
	}
}

func TestStore_ValidateAPIKey_InactiveKey(t *testing.T) {
	pool := skipIfNoDatabase(t)
	defer pool.Close()

	store := NewStore(pool)
	ctx := context.Background()

	clientID := "test-client-inactive-" + generateRandomHex(8)

	defer func() {
		_, _ = pool.Exec(ctx, "DELETE FROM api_keys WHERE client_id = $1", clientID)
		_, _ = pool.Exec(ctx, "DELETE FROM clients WHERE id = $1", clientID)
	}()

	// Create client
	client := Client{
		ID:       clientID,
		Name:     "Inactive Test Client",
		Email:    "inactive@example.com",
		IsActive: true,
	}

	_, err := store.CreateClient(ctx, client)
	if err != nil {
		t.Fatalf("CreateClient failed: %v", err)
	}

	// Create and then revoke API key
	rawKey, apiKey, err := store.CreateAPIKey(ctx, clientID, "Soon Inactive Key", nil, 1000, nil)
	if err != nil {
		t.Fatalf("CreateAPIKey failed: %v", err)
	}

	// Revoke to make inactive
	err = store.RevokeAPIKey(ctx, apiKey.ID)
	if err != nil {
		t.Fatalf("RevokeAPIKey failed: %v", err)
	}

	// Validate should fail due to inactive status
	_, err = store.ValidateAPIKey(ctx, rawKey)
	if err != ErrInvalidAPIKey {
		t.Errorf("expected ErrInvalidAPIKey for inactive key, got %v", err)
	}
}
