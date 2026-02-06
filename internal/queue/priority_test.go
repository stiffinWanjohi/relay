package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

// ============================================================================
// Priority Queue Tests
// ============================================================================

func TestPriorityQueueKey(t *testing.T) {
	tests := []struct {
		priority int
		expected string
	}{
		{1, priorityHighKey},
		{2, priorityHighKey},
		{3, priorityHighKey},
		{4, priorityNormalKey},
		{5, priorityNormalKey},
		{6, priorityNormalKey},
		{7, priorityNormalKey},
		{8, priorityLowKey},
		{9, priorityLowKey},
		{10, priorityLowKey},
	}

	for _, tc := range tests {
		result := priorityQueueKey(tc.priority)
		if result != tc.expected {
			t.Errorf("priorityQueueKey(%d) = %q, expected %q", tc.priority, result, tc.expected)
		}
	}
}

func TestQueue_EnqueueWithPriority(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Enqueue high priority
	highEventID := uuid.New()
	if err := q.EnqueueWithPriority(ctx, highEventID, 1); err != nil {
		t.Fatalf("EnqueueWithPriority high failed: %v", err)
	}

	// Enqueue normal priority
	normalEventID := uuid.New()
	if err := q.EnqueueWithPriority(ctx, normalEventID, 5); err != nil {
		t.Fatalf("EnqueueWithPriority normal failed: %v", err)
	}

	// Enqueue low priority
	lowEventID := uuid.New()
	if err := q.EnqueueWithPriority(ctx, lowEventID, 9); err != nil {
		t.Fatalf("EnqueueWithPriority low failed: %v", err)
	}

	// Verify messages are in correct queues
	if listLen(t, mr, priorityHighKey) != 1 {
		t.Error("expected 1 message in high priority queue")
	}
	if listLen(t, mr, priorityNormalKey) != 1 {
		t.Error("expected 1 message in normal priority queue")
	}
	if listLen(t, mr, priorityLowKey) != 1 {
		t.Error("expected 1 message in low priority queue")
	}
}

func TestQueue_EnqueueWithPriority_Normalization(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Test priority below range (should normalize to 1 -> high)
	if err := q.EnqueueWithPriority(ctx, uuid.New(), 0); err != nil {
		t.Fatalf("EnqueueWithPriority failed: %v", err)
	}
	if listLen(t, mr, priorityHighKey) != 1 {
		t.Error("expected priority 0 to normalize to high queue")
	}

	// Test priority above range (should normalize to 10 -> low)
	if err := q.EnqueueWithPriority(ctx, uuid.New(), 15); err != nil {
		t.Fatalf("EnqueueWithPriority failed: %v", err)
	}
	if listLen(t, mr, priorityLowKey) != 1 {
		t.Error("expected priority 15 to normalize to low queue")
	}
}

func TestQueue_DequeueWithPriority_PriorityOrder(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Reset starvation counter for predictable test
	starvationCounter = 0

	// Enqueue low, normal, high in that order
	lowEventID := uuid.New()
	normalEventID := uuid.New()
	highEventID := uuid.New()

	if err := q.EnqueueWithPriority(ctx, lowEventID, 9); err != nil {
		t.Fatalf("EnqueueWithPriority low failed: %v", err)
	}
	if err := q.EnqueueWithPriority(ctx, normalEventID, 5); err != nil {
		t.Fatalf("EnqueueWithPriority normal failed: %v", err)
	}
	if err := q.EnqueueWithPriority(ctx, highEventID, 1); err != nil {
		t.Fatalf("EnqueueWithPriority high failed: %v", err)
	}

	// First dequeue should get high priority
	msg1, err := q.DequeueWithPriority(ctx)
	if err != nil {
		t.Fatalf("First dequeue failed: %v", err)
	}
	if msg1.EventID != highEventID {
		t.Errorf("expected high priority event first, got %v", msg1.EventID)
	}
	if err := q.Ack(ctx, msg1); err != nil {
		t.Fatalf("Ack failed: %v", err)
	}

	// Second dequeue should get normal priority
	msg2, err := q.DequeueWithPriority(ctx)
	if err != nil {
		t.Fatalf("Second dequeue failed: %v", err)
	}
	if msg2.EventID != normalEventID {
		t.Errorf("expected normal priority event second, got %v", msg2.EventID)
	}
	if err := q.Ack(ctx, msg2); err != nil {
		t.Fatalf("Ack failed: %v", err)
	}

	// Third dequeue should get low priority
	msg3, err := q.DequeueWithPriority(ctx)
	if err != nil {
		t.Fatalf("Third dequeue failed: %v", err)
	}
	if msg3.EventID != lowEventID {
		t.Errorf("expected low priority event third, got %v", msg3.EventID)
	}
}

func TestQueue_DequeueWithPriority_Empty(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	_, err := q.DequeueWithPriority(ctx)
	if !errors.Is(err, domain.ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_DequeueWithPriority_FallsBackToMainQueue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Enqueue to main queue (not priority queue)
	eventID := uuid.New()
	if err := q.Enqueue(ctx, eventID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// DequeueWithPriority should fall back to main queue
	msg, err := q.DequeueWithPriority(ctx)
	if err != nil {
		t.Fatalf("DequeueWithPriority failed: %v", err)
	}
	if msg.EventID != eventID {
		t.Errorf("expected event from main queue, got %v", msg.EventID)
	}
}

func TestQueue_EnqueueDelayedWithPriority(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	eventID := uuid.New()
	priority := 2
	delay := 5 * time.Second

	if err := q.EnqueueDelayedWithPriority(ctx, eventID, priority, delay); err != nil {
		t.Fatalf("EnqueueDelayedWithPriority failed: %v", err)
	}

	// Verify message is in delayed queue
	count := zsetLen(t, mr, delayedQueueKey)
	if count != 1 {
		t.Errorf("expected 1 delayed message, got %d", count)
	}

	// Verify priority is stored in the message
	members, _ := mr.ZMembers(delayedQueueKey)
	if len(members) > 0 {
		var msg Message
		if err := json.Unmarshal([]byte(members[0]), &msg); err == nil {
			if msg.Priority != priority {
				t.Errorf("expected priority %d, got %d", priority, msg.Priority)
			}
		}
	}
}

func TestQueue_GetPriorityStats(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add messages to different priority queues
	_, _ = mr.Lpush(priorityHighKey, "msg1")
	_, _ = mr.Lpush(priorityHighKey, "msg2")
	_, _ = mr.Lpush(priorityNormalKey, "msg1")
	_, _ = mr.Lpush(priorityNormalKey, "msg2")
	_, _ = mr.Lpush(priorityNormalKey, "msg3")
	_, _ = mr.Lpush(priorityLowKey, "msg1")
	_, _ = mr.ZAdd(delayedQueueKey, 1.0, "delayed1")
	_, _ = mr.ZAdd(delayedQueueKey, 2.0, "delayed2")

	stats, err := q.GetPriorityStats(ctx)
	if err != nil {
		t.Fatalf("GetPriorityStats failed: %v", err)
	}

	if stats.High != 2 {
		t.Errorf("expected 2 high priority, got %d", stats.High)
	}
	if stats.Normal != 3 {
		t.Errorf("expected 3 normal priority, got %d", stats.Normal)
	}
	if stats.Low != 1 {
		t.Errorf("expected 1 low priority, got %d", stats.Low)
	}
	if stats.Delayed != 2 {
		t.Errorf("expected 2 delayed, got %d", stats.Delayed)
	}
}

func TestQueue_moveDelayedToPriority(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add ready delayed messages with different priorities
	highMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		Priority:  2,
		EnqueueAt: time.Now().Add(-1 * time.Second),
	}
	lowMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		Priority:  9,
		EnqueueAt: time.Now().Add(-1 * time.Second),
	}
	noPriorityMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		Priority:  0, // No priority set
		EnqueueAt: time.Now().Add(-1 * time.Second),
	}

	highData, _ := json.Marshal(highMsg)
	lowData, _ := json.Marshal(lowMsg)
	noPriorityData, _ := json.Marshal(noPriorityMsg)

	_, _ = mr.ZAdd(delayedQueueKey, float64(highMsg.EnqueueAt.Unix()), string(highData))
	_, _ = mr.ZAdd(delayedQueueKey, float64(lowMsg.EnqueueAt.Unix()), string(lowData))
	_, _ = mr.ZAdd(delayedQueueKey, float64(noPriorityMsg.EnqueueAt.Unix()), string(noPriorityData))

	// Move delayed to priority queues
	if err := q.moveDelayedToPriority(ctx); err != nil {
		t.Fatalf("moveDelayedToPriority failed: %v", err)
	}

	// Verify messages went to correct queues
	if listLen(t, mr, priorityHighKey) != 1 {
		t.Error("expected high priority message in high queue")
	}
	if listLen(t, mr, priorityLowKey) != 1 {
		t.Error("expected low priority message in low queue")
	}
	if listLen(t, mr, mainQueueKey) != 1 {
		t.Error("expected no-priority message in main queue")
	}

	// Verify delayed queue is empty
	if zsetLen(t, mr, delayedQueueKey) != 0 {
		t.Error("expected delayed queue to be empty")
	}
}

func TestQueue_DequeueWithPriority_StarvationPrevention(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Reset starvation counter
	starvationCounter = 0

	// Add many messages to all queues
	for range 25 {
		_ = q.EnqueueWithPriority(ctx, uuid.New(), 1) // High
		_ = q.EnqueueWithPriority(ctx, uuid.New(), 5) // Normal
		_ = q.EnqueueWithPriority(ctx, uuid.New(), 9) // Low
	}

	// Dequeue 20 times and count which queues we pulled from
	highCount := 0
	normalCount := 0
	lowCount := 0

	for i := range 20 {
		msg, err := q.DequeueWithPriority(ctx)
		if err != nil {
			t.Fatalf("Dequeue %d failed: %v", i, err)
		}

		switch msg.Priority {
		case 1:
			highCount++
		case 5:
			normalCount++
		case 9:
			lowCount++
		}

		_ = q.Ack(ctx, msg)
	}

	// With starvation prevention:
	// - Every 10th should try normal first (2 out of 20)
	// - Every 20th should try low first (1 out of 20)
	// So we should see some normal and low messages processed even though high has messages

	// The exact distribution depends on the implementation, but we should see at least 1 low
	// message processed due to starvation prevention
	if lowCount == 0 && normalCount == 0 {
		t.Error("starvation prevention should have allowed at least some normal/low priority messages")
	}

	t.Logf("Distribution: high=%d, normal=%d, low=%d", highCount, normalCount, lowCount)
}
