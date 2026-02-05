package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

func TestFIFOQueueKey(t *testing.T) {
	tests := []struct {
		endpointID   string
		partitionKey string
		expected     string
	}{
		{"endpoint-1", "", "relay:queue:fifo:endpoint-1"},
		{"endpoint-1", "customer-123", "relay:queue:fifo:endpoint-1:customer-123"},
		{"ep-abc", "order-456", "relay:queue:fifo:ep-abc:order-456"},
	}

	for _, tc := range tests {
		result := FIFOQueueKey(tc.endpointID, tc.partitionKey)
		if result != tc.expected {
			t.Errorf("FIFOQueueKey(%q, %q) = %q, expected %q",
				tc.endpointID, tc.partitionKey, result, tc.expected)
		}
	}
}

func TestFIFOLockKey(t *testing.T) {
	tests := []struct {
		endpointID   string
		partitionKey string
		expected     string
	}{
		{"endpoint-1", "", "relay:lock:fifo:endpoint-1"},
		{"endpoint-1", "customer-123", "relay:lock:fifo:endpoint-1:customer-123"},
		{"ep-abc", "order-456", "relay:lock:fifo:ep-abc:order-456"},
	}

	for _, tc := range tests {
		result := FIFOLockKey(tc.endpointID, tc.partitionKey)
		if result != tc.expected {
			t.Errorf("FIFOLockKey(%q, %q) = %q, expected %q",
				tc.endpointID, tc.partitionKey, result, tc.expected)
		}
	}
}

func TestFIFOInFlightKey(t *testing.T) {
	tests := []struct {
		endpointID   string
		partitionKey string
		expected     string
	}{
		{"endpoint-1", "", "relay:inflight:fifo:endpoint-1"},
		{"endpoint-1", "customer-123", "relay:inflight:fifo:endpoint-1:customer-123"},
	}

	for _, tc := range tests {
		result := FIFOInFlightKey(tc.endpointID, tc.partitionKey)
		if result != tc.expected {
			t.Errorf("FIFOInFlightKey(%q, %q) = %q, expected %q",
				tc.endpointID, tc.partitionKey, result, tc.expected)
		}
	}
}

func TestQueue_EnqueueFIFO(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"
	eventID := uuid.New()

	err := q.EnqueueFIFO(ctx, endpointID, "", eventID)
	if err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Verify message is in FIFO queue
	queueKey := FIFOQueueKey(endpointID, "")
	length := listLen(t, mr, queueKey)
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}
}

func TestQueue_EnqueueFIFO_WithPartitionKey(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"
	partitionKey := "customer-123"
	eventID := uuid.New()

	err := q.EnqueueFIFO(ctx, endpointID, partitionKey, eventID)
	if err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Verify message is in partitioned FIFO queue
	queueKey := FIFOQueueKey(endpointID, partitionKey)
	length := listLen(t, mr, queueKey)
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Verify other partition queue is empty
	otherQueueKey := FIFOQueueKey(endpointID, "other-partition")
	otherLength := listLen(t, mr, otherQueueKey)
	if otherLength != 0 {
		t.Errorf("expected other queue to be empty, got %d", otherLength)
	}
}

func TestQueue_EnqueueFIFO_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	q = q.WithMetrics(metrics)

	ctx := context.Background()
	err := q.EnqueueFIFO(ctx, "endpoint-1", "", uuid.New())
	if err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}
}

func TestQueue_DequeueFIFO(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"
	eventID := uuid.New()

	// Enqueue first
	if err := q.EnqueueFIFO(ctx, endpointID, "", eventID); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Dequeue
	msg, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	if msg.EventID != eventID {
		t.Errorf("expected event ID %v, got %v", eventID, msg.EventID)
	}
}

func TestQueue_DequeueFIFO_EmptyQueue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	_, err := q.DequeueFIFO(ctx, "empty-endpoint", "")
	if !errors.Is(err, domain.ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_DequeueFIFO_Locked(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue two messages
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// First dequeue should succeed
	_, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("First DequeueFIFO failed: %v", err)
	}

	// Second dequeue should fail (locked)
	_, err = q.DequeueFIFO(ctx, endpointID, "")
	if !errors.Is(err, domain.ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty (locked), got %v", err)
	}
}

func TestQueue_DequeueFIFO_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	q = q.WithMetrics(metrics)

	ctx := context.Background()
	endpointID := "endpoint-1"

	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	_, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}
}

func TestQueue_AckFIFO(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue and dequeue
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	_, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	// Verify locked
	locked, _ := q.IsFIFOLocked(ctx, endpointID, "")
	if !locked {
		t.Error("expected queue to be locked after dequeue")
	}

	// Ack
	if err := q.AckFIFO(ctx, endpointID, ""); err != nil {
		t.Fatalf("AckFIFO failed: %v", err)
	}

	// Verify unlocked
	locked, _ = q.IsFIFOLocked(ctx, endpointID, "")
	if locked {
		t.Error("expected queue to be unlocked after ack")
	}
}

func TestQueue_NackFIFO_NoDelay(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"
	eventID := uuid.New()

	// Enqueue and dequeue
	if err := q.EnqueueFIFO(ctx, endpointID, "", eventID); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	msg, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	// Nack without delay
	if err := q.NackFIFO(ctx, endpointID, "", msg, 0); err != nil {
		t.Fatalf("NackFIFO failed: %v", err)
	}

	// Verify message is back in queue
	queueKey := FIFOQueueKey(endpointID, "")
	length := listLen(t, mr, queueKey)
	if length != 1 {
		t.Errorf("expected 1 message in queue, got %d", length)
	}

	// Verify unlocked (can dequeue again)
	msg2, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("Second DequeueFIFO failed: %v", err)
	}
	if msg2.EventID != eventID {
		t.Errorf("expected same event ID %v, got %v", eventID, msg2.EventID)
	}
}

func TestQueue_NackFIFO_WithDelay(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue and dequeue
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	msg, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	// Nack with delay
	if err := q.NackFIFO(ctx, endpointID, "", msg, 10*time.Second); err != nil {
		t.Fatalf("NackFIFO failed: %v", err)
	}

	// Verify message is back in queue
	queueKey := FIFOQueueKey(endpointID, "")
	length := listLen(t, mr, queueKey)
	if length != 1 {
		t.Errorf("expected 1 message in queue, got %d", length)
	}

	// Verify still locked (can't dequeue due to delay)
	lockKey := FIFOLockKey(endpointID, "")
	exists := mr.Exists(lockKey)
	if !exists {
		t.Error("expected lock to exist with TTL")
	}
}

func TestQueue_GetFIFOQueueLength(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue several messages
	for i := 0; i < 5; i++ {
		if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
			t.Fatalf("EnqueueFIFO failed: %v", err)
		}
	}

	length, err := q.GetFIFOQueueLength(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("GetFIFOQueueLength failed: %v", err)
	}

	if length != 5 {
		t.Errorf("expected length 5, got %d", length)
	}
}

func TestQueue_GetFIFOQueueLength_Empty(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	length, err := q.GetFIFOQueueLength(ctx, "nonexistent", "")
	if err != nil {
		t.Fatalf("GetFIFOQueueLength failed: %v", err)
	}

	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
	}
}

func TestQueue_IsFIFOLocked(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Initially not locked
	locked, err := q.IsFIFOLocked(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("IsFIFOLocked failed: %v", err)
	}
	if locked {
		t.Error("expected not locked initially")
	}

	// Enqueue and dequeue to acquire lock
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}
	if _, err := q.DequeueFIFO(ctx, endpointID, ""); err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	// Now should be locked
	locked, err = q.IsFIFOLocked(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("IsFIFOLocked failed: %v", err)
	}
	if !locked {
		t.Error("expected locked after dequeue")
	}
}

func TestQueue_ReleaseFIFOLock(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue and dequeue to acquire lock
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}
	if _, err := q.DequeueFIFO(ctx, endpointID, ""); err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	// Verify locked
	locked, _ := q.IsFIFOLocked(ctx, endpointID, "")
	if !locked {
		t.Error("expected locked")
	}

	// Force release
	if err := q.ReleaseFIFOLock(ctx, endpointID, ""); err != nil {
		t.Fatalf("ReleaseFIFOLock failed: %v", err)
	}

	// Verify unlocked
	locked, _ = q.IsFIFOLocked(ctx, endpointID, "")
	if locked {
		t.Error("expected unlocked after force release")
	}
}

func TestQueue_FIFO_OrderPreservation(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue multiple messages in order
	eventIDs := make([]uuid.UUID, 5)
	for i := range 5 {
		eventIDs[i] = uuid.New()
		if err := q.EnqueueFIFO(ctx, endpointID, "", eventIDs[i]); err != nil {
			t.Fatalf("EnqueueFIFO %d failed: %v", i, err)
		}
	}

	// Dequeue and verify order (FIFO)
	for i := range 5 {
		msg, err := q.DequeueFIFO(ctx, endpointID, "")
		if err != nil {
			t.Fatalf("DequeueFIFO %d failed: %v", i, err)
		}
		if msg.EventID != eventIDs[i] {
			t.Errorf("Dequeue %d: expected event ID %v, got %v", i, eventIDs[i], msg.EventID)
		}
		if err := q.AckFIFO(ctx, endpointID, ""); err != nil {
			t.Fatalf("AckFIFO %d failed: %v", i, err)
		}
	}
}

func TestQueue_FIFO_PartitionIsolation(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue to partition A
	eventA := uuid.New()
	if err := q.EnqueueFIFO(ctx, endpointID, "partition-A", eventA); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Enqueue to partition B
	eventB := uuid.New()
	if err := q.EnqueueFIFO(ctx, endpointID, "partition-B", eventB); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Dequeue from partition A (should get eventA, lock partition A)
	msgA, err := q.DequeueFIFO(ctx, endpointID, "partition-A")
	if err != nil {
		t.Fatalf("DequeueFIFO A failed: %v", err)
	}
	if msgA.EventID != eventA {
		t.Errorf("expected event A %v, got %v", eventA, msgA.EventID)
	}

	// Dequeue from partition B (should succeed despite A being locked)
	msgB, err := q.DequeueFIFO(ctx, endpointID, "partition-B")
	if err != nil {
		t.Fatalf("DequeueFIFO B failed: %v", err)
	}
	if msgB.EventID != eventB {
		t.Errorf("expected event B %v, got %v", eventB, msgB.EventID)
	}

	// Both partitions now locked independently
	lockedA, _ := q.IsFIFOLocked(ctx, endpointID, "partition-A")
	lockedB, _ := q.IsFIFOLocked(ctx, endpointID, "partition-B")
	if !lockedA || !lockedB {
		t.Error("expected both partitions to be locked")
	}
}

func TestQueue_FIFO_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	q = q.WithMetrics(metrics)

	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue with metrics
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Dequeue with metrics
	_, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}
}

func TestQueue_GetFIFOQueueStats(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"
	partitionKey := "partition-1"

	// Enqueue some messages
	for i := 0; i < 3; i++ {
		if err := q.EnqueueFIFO(ctx, endpointID, partitionKey, uuid.New()); err != nil {
			t.Fatalf("EnqueueFIFO failed: %v", err)
		}
	}

	stats, err := q.GetFIFOQueueStats(ctx, endpointID, partitionKey)
	if err != nil {
		t.Fatalf("GetFIFOQueueStats failed: %v", err)
	}

	if stats.EndpointID != endpointID {
		t.Errorf("expected endpoint ID %s, got %s", endpointID, stats.EndpointID)
	}
	if stats.PartitionKey != partitionKey {
		t.Errorf("expected partition key %s, got %s", partitionKey, stats.PartitionKey)
	}
	if stats.QueueLength != 3 {
		t.Errorf("expected queue length 3, got %d", stats.QueueLength)
	}
	if stats.IsLocked {
		t.Error("expected not locked")
	}
	if stats.HasInFlight {
		t.Error("expected no in-flight")
	}
}

func TestQueue_GetFIFOQueueStats_WithLock(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue and dequeue to create lock
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}
	if _, err := q.DequeueFIFO(ctx, endpointID, ""); err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	stats, err := q.GetFIFOQueueStats(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("GetFIFOQueueStats failed: %v", err)
	}

	if !stats.IsLocked {
		t.Error("expected locked")
	}
	if !stats.HasInFlight {
		t.Error("expected in-flight")
	}
}

func TestQueue_DrainFIFOQueue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue messages
	eventIDs := make([]uuid.UUID, 5)
	for i := 0; i < 5; i++ {
		eventIDs[i] = uuid.New()
		if err := q.EnqueueFIFO(ctx, endpointID, "", eventIDs[i]); err != nil {
			t.Fatalf("EnqueueFIFO failed: %v", err)
		}
	}

	// Drain queue
	messages, err := q.DrainFIFOQueue(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DrainFIFOQueue failed: %v", err)
	}

	if len(messages) != 5 {
		t.Errorf("expected 5 messages, got %d", len(messages))
	}

	// Verify queue is empty
	length, _ := q.GetFIFOQueueLength(ctx, endpointID, "")
	if length != 0 {
		t.Errorf("expected queue to be empty, got length %d", length)
	}
}

func TestQueue_DrainFIFOQueue_Empty(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	messages, err := q.DrainFIFOQueue(ctx, "nonexistent", "")
	if err != nil {
		t.Fatalf("DrainFIFOQueue failed: %v", err)
	}

	if len(messages) != 0 {
		t.Errorf("expected 0 messages, got %d", len(messages))
	}
}

func TestQueue_MoveFIFOToStandardQueue(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue to FIFO
	for i := 0; i < 3; i++ {
		if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
			t.Fatalf("EnqueueFIFO failed: %v", err)
		}
	}

	// Move to standard queue
	moved, err := q.MoveFIFOToStandardQueue(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("MoveFIFOToStandardQueue failed: %v", err)
	}

	if moved != 3 {
		t.Errorf("expected 3 moved, got %d", moved)
	}

	// Verify FIFO queue is empty
	fifoLength, _ := q.GetFIFOQueueLength(ctx, endpointID, "")
	if fifoLength != 0 {
		t.Errorf("expected FIFO queue empty, got %d", fifoLength)
	}

	// Verify standard queue has messages
	mainLength := listLen(t, mr, mainQueueKey)
	if mainLength != 3 {
		t.Errorf("expected 3 in main queue, got %d", mainLength)
	}
}

func TestParseFIFOQueueKey(t *testing.T) {
	tests := []struct {
		key          string
		endpointID   string
		partitionKey string
		ok           bool
	}{
		{"relay:queue:fifo:endpoint-1", "endpoint-1", "", true},
		{"relay:queue:fifo:endpoint-1:partition-1", "endpoint-1", "partition-1", true},
		{"relay:queue:fifo:", "", "", false},
		{"relay:queue:fifo", "", "", false},
		{"invalid", "", "", false},
	}

	for _, tc := range tests {
		endpointID, partitionKey, ok := ParseFIFOQueueKey(tc.key)
		if ok != tc.ok {
			t.Errorf("ParseFIFOQueueKey(%q) ok = %v, expected %v", tc.key, ok, tc.ok)
		}
		if ok && endpointID != tc.endpointID {
			t.Errorf("ParseFIFOQueueKey(%q) endpointID = %q, expected %q", tc.key, endpointID, tc.endpointID)
		}
		if ok && partitionKey != tc.partitionKey {
			t.Errorf("ParseFIFOQueueKey(%q) partitionKey = %q, expected %q", tc.key, partitionKey, tc.partitionKey)
		}
	}
}

func TestIndexOf(t *testing.T) {
	tests := []struct {
		s        string
		c        byte
		expected int
	}{
		{"hello", 'l', 2},
		{"hello", 'o', 4},
		{"hello", 'x', -1},
		{"", 'a', -1},
		{"a:b:c", ':', 1},
	}

	for _, tc := range tests {
		result := indexOf(tc.s, tc.c)
		if result != tc.expected {
			t.Errorf("indexOf(%q, %c) = %d, expected %d", tc.s, tc.c, result, tc.expected)
		}
	}
}

func TestFIFOQueueStats_Fields(t *testing.T) {
	stats := FIFOQueueStats{
		EndpointID:   "ep-1",
		PartitionKey: "pk-1",
		QueueLength:  10,
		IsLocked:     true,
		HasInFlight:  true,
	}

	if stats.EndpointID != "ep-1" {
		t.Errorf("expected EndpointID 'ep-1', got %s", stats.EndpointID)
	}
	if stats.PartitionKey != "pk-1" {
		t.Errorf("expected PartitionKey 'pk-1', got %s", stats.PartitionKey)
	}
	if stats.QueueLength != 10 {
		t.Errorf("expected QueueLength 10, got %d", stats.QueueLength)
	}
	if !stats.IsLocked {
		t.Error("expected IsLocked true")
	}
	if !stats.HasInFlight {
		t.Error("expected HasInFlight true")
	}
}

func TestQueue_RecoverFIFOInFlight(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Simulate an in-flight message (as if worker crashed)
	eventID := uuid.New()
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		EnqueueAt: time.Now().UTC(),
	}

	// Manually set in-flight data
	inflightKey := FIFOInFlightKey(endpointID, "")
	lockKey := FIFOLockKey(endpointID, "")

	data, _ := json.Marshal(msg)
	mr.Set(inflightKey, string(data))
	mr.Set(lockKey, "locked")

	// Recover the in-flight message
	recovered, err := q.RecoverFIFOInFlight(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("RecoverFIFOInFlight failed: %v", err)
	}

	if recovered == nil {
		t.Fatal("expected recovered message, got nil")
	}
	if recovered.EventID != eventID {
		t.Errorf("expected event ID %v, got %v", eventID, recovered.EventID)
	}

	// Verify message is back in queue
	queueKey := FIFOQueueKey(endpointID, "")
	length := listLen(t, mr, queueKey)
	if length != 1 {
		t.Errorf("expected 1 message in queue, got %d", length)
	}

	// Verify lock and in-flight are cleared
	if mr.Exists(lockKey) {
		t.Error("expected lock to be cleared")
	}
	if mr.Exists(inflightKey) {
		t.Error("expected in-flight to be cleared")
	}
}

func TestQueue_RecoverFIFOInFlight_NoInFlight(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	recovered, err := q.RecoverFIFOInFlight(ctx, "nonexistent", "")
	if err != nil {
		t.Fatalf("RecoverFIFOInFlight failed: %v", err)
	}

	if recovered != nil {
		t.Errorf("expected nil, got %+v", recovered)
	}
}

func TestQueue_FIFO_MultipleEndpoints(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	endpoints := []string{"ep-1", "ep-2", "ep-3"}
	eventsByEndpoint := make(map[string]uuid.UUID)

	// Enqueue to each endpoint
	for _, ep := range endpoints {
		eventID := uuid.New()
		eventsByEndpoint[ep] = eventID
		if err := q.EnqueueFIFO(ctx, ep, "", eventID); err != nil {
			t.Fatalf("EnqueueFIFO failed: %v", err)
		}
	}

	// Dequeue from each (they should be independent)
	for _, ep := range endpoints {
		msg, err := q.DequeueFIFO(ctx, ep, "")
		if err != nil {
			t.Fatalf("DequeueFIFO %s failed: %v", ep, err)
		}
		if msg.EventID != eventsByEndpoint[ep] {
			t.Errorf("endpoint %s: expected %v, got %v", ep, eventsByEndpoint[ep], msg.EventID)
		}
	}
}
