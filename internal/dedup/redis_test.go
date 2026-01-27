package dedup

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/relay/internal/domain"
)

func setupTestChecker(t *testing.T) (*Checker, *miniredis.Miniredis) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	t.Cleanup(func() {
		client.Close()
		mr.Close()
	})

	return NewChecker(client), mr
}

func TestNewChecker(t *testing.T) {
	checker, _ := setupTestChecker(t)

	if checker.ttl != defaultTTL {
		t.Errorf("expected TTL %v, got %v", defaultTTL, checker.ttl)
	}
}

func TestChecker_WithTTL(t *testing.T) {
	checker, _ := setupTestChecker(t)
	customTTL := 1 * time.Hour

	checker2 := checker.WithTTL(customTTL)

	if checker2.ttl != customTTL {
		t.Errorf("expected TTL %v, got %v", customTTL, checker2.ttl)
	}
	// Verify original is unchanged
	if checker.ttl != defaultTTL {
		t.Errorf("original checker TTL should be unchanged")
	}
}

func TestChecker_Check_NotFound(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	result, err := checker.Check(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if result != uuid.Nil {
		t.Errorf("expected uuid.Nil, got %v", result)
	}
}

func TestChecker_Check_Found(t *testing.T) {
	checker, mr := setupTestChecker(t)
	ctx := context.Background()

	eventID := uuid.New()
	idempotencyKey := "test-key"

	// Set the key directly in Redis
	mr.Set(keyPrefix+idempotencyKey, eventID.String())

	result, err := checker.Check(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}

	if result != eventID {
		t.Errorf("expected %v, got %v", eventID, result)
	}
}

func TestChecker_Check_InvalidUUID(t *testing.T) {
	checker, mr := setupTestChecker(t)
	ctx := context.Background()

	idempotencyKey := "invalid-uuid-key"
	mr.Set(keyPrefix+idempotencyKey, "not-a-uuid")

	_, err := checker.Check(ctx, idempotencyKey)
	if err == nil {
		t.Error("expected error for invalid UUID")
	}
}

func TestChecker_Set_New(t *testing.T) {
	checker, mr := setupTestChecker(t)
	ctx := context.Background()

	eventID := uuid.New()
	idempotencyKey := "new-key"

	err := checker.Set(ctx, idempotencyKey, eventID)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify key was set
	value, err := mr.Get(keyPrefix + idempotencyKey)
	if err != nil {
		t.Fatalf("failed to get key: %v", err)
	}

	if value != eventID.String() {
		t.Errorf("expected %s, got %s", eventID.String(), value)
	}
}

func TestChecker_Set_SameEventID(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	eventID := uuid.New()
	idempotencyKey := "same-event-key"

	// Set first time
	err := checker.Set(ctx, idempotencyKey, eventID)
	if err != nil {
		t.Fatalf("First Set failed: %v", err)
	}

	// Set second time with same event ID - should succeed
	err = checker.Set(ctx, idempotencyKey, eventID)
	if err != nil {
		t.Fatalf("Second Set with same event ID should succeed: %v", err)
	}
}

func TestChecker_Set_DifferentEventID(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	eventID1 := uuid.New()
	eventID2 := uuid.New()
	idempotencyKey := "different-event-key"

	// Set first time
	err := checker.Set(ctx, idempotencyKey, eventID1)
	if err != nil {
		t.Fatalf("First Set failed: %v", err)
	}

	// Set second time with different event ID - should return ErrDuplicateEvent
	err = checker.Set(ctx, idempotencyKey, eventID2)
	if err != domain.ErrDuplicateEvent {
		t.Errorf("expected ErrDuplicateEvent, got %v", err)
	}
}

func TestChecker_CheckAndSet_New(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	eventID := uuid.New()
	idempotencyKey := "checkandset-new"

	existingID, err := checker.CheckAndSet(ctx, idempotencyKey, eventID)
	if err != nil {
		t.Fatalf("CheckAndSet failed: %v", err)
	}

	if existingID != uuid.Nil {
		t.Errorf("expected uuid.Nil for new key, got %v", existingID)
	}

	// Verify key was set
	result, err := checker.Check(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result != eventID {
		t.Errorf("expected %v, got %v", eventID, result)
	}
}

func TestChecker_CheckAndSet_Existing(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	eventID1 := uuid.New()
	eventID2 := uuid.New()
	idempotencyKey := "checkandset-existing"

	// Set first time
	_, err := checker.CheckAndSet(ctx, idempotencyKey, eventID1)
	if err != nil {
		t.Fatalf("First CheckAndSet failed: %v", err)
	}

	// Set second time with different event ID
	existingID, err := checker.CheckAndSet(ctx, idempotencyKey, eventID2)
	if err != nil {
		t.Fatalf("Second CheckAndSet failed: %v", err)
	}

	if existingID != eventID1 {
		t.Errorf("expected existing ID %v, got %v", eventID1, existingID)
	}
}

func TestChecker_Delete(t *testing.T) {
	checker, mr := setupTestChecker(t)
	ctx := context.Background()

	eventID := uuid.New()
	idempotencyKey := "delete-key"

	// Set the key
	err := checker.Set(ctx, idempotencyKey, eventID)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify key exists
	if !mr.Exists(keyPrefix + idempotencyKey) {
		t.Fatal("expected key to exist before delete")
	}

	// Delete the key
	err = checker.Delete(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("Delete failed: %v", err)
	}

	// Verify key is gone
	if mr.Exists(keyPrefix + idempotencyKey) {
		t.Error("expected key to be deleted")
	}
}

func TestChecker_Delete_Nonexistent(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	// Deleting nonexistent key should not error
	err := checker.Delete(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("Delete of nonexistent key should not error: %v", err)
	}
}

func TestChecker_Extend(t *testing.T) {
	checker, mr := setupTestChecker(t)
	ctx := context.Background()

	eventID := uuid.New()
	idempotencyKey := "extend-key"

	// Set the key
	err := checker.Set(ctx, idempotencyKey, eventID)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Get initial TTL
	initialTTL := mr.TTL(keyPrefix + idempotencyKey)

	// Fast forward time
	mr.FastForward(1 * time.Hour)

	// Extend TTL
	err = checker.Extend(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("Extend failed: %v", err)
	}

	// Get new TTL - should be reset to full TTL
	newTTL := mr.TTL(keyPrefix + idempotencyKey)

	// New TTL should be greater than what remained before (initialTTL - 1 hour)
	if newTTL <= initialTTL-1*time.Hour {
		t.Errorf("expected TTL to be extended, got %v", newTTL)
	}
}

func TestChecker_Extend_Nonexistent(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	// Extending nonexistent key should not error (Redis EXPIRE returns 0 but no error)
	err := checker.Extend(ctx, "nonexistent-key")
	if err != nil {
		t.Fatalf("Extend of nonexistent key should not error: %v", err)
	}
}

func TestChecker_ConcurrentSet(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	idempotencyKey := "concurrent-key"
	eventID := uuid.New()

	// Run multiple concurrent sets with the same event ID
	done := make(chan error, 10)
	for range 10 {
		go func() {
			done <- checker.Set(ctx, idempotencyKey, eventID)
		}()
	}

	// All should succeed since they use the same event ID
	for range 10 {
		if err := <-done; err != nil {
			t.Errorf("concurrent Set failed: %v", err)
		}
	}

	// Verify final state
	result, err := checker.Check(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result != eventID {
		t.Errorf("expected %v, got %v", eventID, result)
	}
}

func TestChecker_ConcurrentCheckAndSet_DifferentEventIDs(t *testing.T) {
	checker, _ := setupTestChecker(t)
	ctx := context.Background()

	idempotencyKey := "concurrent-checkandset-key"

	// Run multiple concurrent check-and-sets with different event IDs
	results := make(chan uuid.UUID, 10)
	for range 10 {
		go func() {
			eventID := uuid.New()
			existingID, _ := checker.CheckAndSet(ctx, idempotencyKey, eventID)
			if existingID == uuid.Nil {
				results <- eventID // This goroutine won the race
			} else {
				results <- existingID // Return the existing ID
			}
		}()
	}

	// Collect results - all should be the same (the winner's event ID)
	firstResult := <-results
	for range 9 {
		result := <-results
		if result != firstResult {
			t.Errorf("expected all results to be %v, got %v", firstResult, result)
		}
	}
}

func TestChecker_TTLExpiration(t *testing.T) {
	checker, mr := setupTestChecker(t)
	checker = checker.WithTTL(1 * time.Second)
	ctx := context.Background()

	eventID := uuid.New()
	idempotencyKey := "ttl-expire-key"

	err := checker.Set(ctx, idempotencyKey, eventID)
	if err != nil {
		t.Fatalf("Set failed: %v", err)
	}

	// Verify key exists
	result, err := checker.Check(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result != eventID {
		t.Errorf("expected %v, got %v", eventID, result)
	}

	// Fast forward past TTL
	mr.FastForward(2 * time.Second)

	// Key should be gone
	result, err = checker.Check(ctx, idempotencyKey)
	if err != nil {
		t.Fatalf("Check failed: %v", err)
	}
	if result != uuid.Nil {
		t.Errorf("expected uuid.Nil after TTL expiration, got %v", result)
	}
}

func TestKeyPrefix(t *testing.T) {
	if keyPrefix != "relay:idemp:" {
		t.Errorf("expected keyPrefix 'relay:idemp:', got %s", keyPrefix)
	}
}

func TestDefaultTTL(t *testing.T) {
	expected := 24 * time.Hour
	if defaultTTL != expected {
		t.Errorf("expected defaultTTL %v, got %v", expected, defaultTTL)
	}
}
