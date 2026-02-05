package queue

import (
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/observability"
)

// setupTestQueue creates a test queue with miniredis for unit testing.
func setupTestQueue(t *testing.T) (*Queue, *miniredis.Miniredis, *redis.Client) {
	t.Helper()

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	t.Cleanup(func() {
		_ = client.Close()
		mr.Close()
	})

	return NewQueue(client), mr, client
}

// listLen is a helper to get list length from miniredis.
func listLen(t *testing.T, mr *miniredis.Miniredis, key string) int {
	t.Helper()
	list, err := mr.List(key)
	if err != nil {
		return 0
	}
	return len(list)
}

// zsetLen is a helper to get sorted set size from miniredis.
func zsetLen(t *testing.T, mr *miniredis.Miniredis, key string) int {
	t.Helper()
	members, err := mr.ZMembers(key)
	if err != nil {
		return 0
	}
	return len(members)
}

func TestNewQueue(t *testing.T) {
	q, _, _ := setupTestQueue(t)

	if q.visibilityTimeout != defaultVisibilityTimeout {
		t.Errorf("expected visibility timeout %v, got %v", defaultVisibilityTimeout, q.visibilityTimeout)
	}
	if q.blockingTimeout != defaultBlockingTimeout {
		t.Errorf("expected blocking timeout %v, got %v", defaultBlockingTimeout, q.blockingTimeout)
	}
	if q.metrics != nil {
		t.Error("expected nil metrics by default")
	}
	if q.client == nil {
		t.Error("expected client to be set")
	}
}

func TestQueue_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	q2 := q.WithMetrics(metrics)

	if q2.metrics != metrics {
		t.Error("expected metrics to be set")
	}
	if q2.visibilityTimeout != q.visibilityTimeout {
		t.Error("visibility timeout not preserved")
	}
	if q2.blockingTimeout != q.blockingTimeout {
		t.Error("blocking timeout not preserved")
	}
	if q2.fifoConfig.LockExpiration != q.fifoConfig.LockExpiration {
		t.Error("FIFO config not preserved")
	}
}

func TestQueue_WithVisibilityTimeout(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	customTimeout := 5 * time.Minute

	q2 := q.WithVisibilityTimeout(customTimeout)

	if q2.visibilityTimeout != customTimeout {
		t.Errorf("expected visibility timeout %v, got %v", customTimeout, q2.visibilityTimeout)
	}
	if q2.blockingTimeout != q.blockingTimeout {
		t.Error("blocking timeout not preserved")
	}
}

func TestQueue_WithBlockingTimeout(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	customTimeout := 5 * time.Second

	q2 := q.WithBlockingTimeout(customTimeout)

	if q2.blockingTimeout != customTimeout {
		t.Errorf("expected blocking timeout %v, got %v", customTimeout, q2.blockingTimeout)
	}
	if q2.visibilityTimeout != q.visibilityTimeout {
		t.Error("visibility timeout not preserved")
	}
}

func TestQueue_WithFIFOConfig(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	customConfig := FIFOConfig{LockExpiration: 10 * time.Minute}

	q2 := q.WithFIFOConfig(customConfig)

	if q2.fifoConfig.LockExpiration != customConfig.LockExpiration {
		t.Errorf("expected FIFO lock expiration %v, got %v",
			customConfig.LockExpiration, q2.fifoConfig.LockExpiration)
	}
	if q2.visibilityTimeout != q.visibilityTimeout {
		t.Error("visibility timeout not preserved")
	}
}

func TestQueue_ChainedWithMethods(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	q2 := q.
		WithMetrics(metrics).
		WithVisibilityTimeout(2 * time.Minute).
		WithBlockingTimeout(5 * time.Second).
		WithFIFOConfig(FIFOConfig{LockExpiration: 10 * time.Minute})

	if q2.metrics != metrics {
		t.Error("metrics not preserved in chain")
	}
	if q2.visibilityTimeout != 2*time.Minute {
		t.Error("visibility timeout not preserved in chain")
	}
	if q2.blockingTimeout != 5*time.Second {
		t.Error("blocking timeout not preserved in chain")
	}
	if q2.fifoConfig.LockExpiration != 10*time.Minute {
		t.Error("FIFO config not preserved in chain")
	}
}

func TestDefaultFIFOConfig(t *testing.T) {
	config := DefaultFIFOConfig()

	if config.LockExpiration != DefaultFIFOLockExpiration {
		t.Errorf("expected lock expiration %v, got %v",
			DefaultFIFOLockExpiration, config.LockExpiration)
	}
}

func TestMessage_Fields(t *testing.T) {
	msg := Message{
		ID:       "test-id",
		EventID:  [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		ClientID: "client-123",
		Priority: 5,
	}

	if msg.ID != "test-id" {
		t.Errorf("expected ID 'test-id', got %s", msg.ID)
	}
	if msg.ClientID != "client-123" {
		t.Errorf("expected ClientID 'client-123', got %s", msg.ClientID)
	}
	if msg.Priority != 5 {
		t.Errorf("expected Priority 5, got %d", msg.Priority)
	}
}

func TestStats_Fields(t *testing.T) {
	stats := Stats{
		Pending:    10,
		Processing: 5,
		Delayed:    3,
	}

	if stats.Pending != 10 {
		t.Errorf("expected Pending 10, got %d", stats.Pending)
	}
	if stats.Processing != 5 {
		t.Errorf("expected Processing 5, got %d", stats.Processing)
	}
	if stats.Delayed != 3 {
		t.Errorf("expected Delayed 3, got %d", stats.Delayed)
	}
}
