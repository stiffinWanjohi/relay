package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestQueue_Stats(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add messages to different queues
	_, _ = mr.Lpush(mainQueueKey, "msg1")
	_, _ = mr.Lpush(mainQueueKey, "msg2")
	_, _ = mr.Lpush(mainQueueKey, "msg3")
	_, _ = mr.Lpush(processingQueueKey, "proc1")
	_, _ = mr.ZAdd(delayedQueueKey, 1.0, "delayed1")
	_, _ = mr.ZAdd(delayedQueueKey, 2.0, "delayed2")

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.Pending != 3 {
		t.Errorf("expected 3 pending, got %d", stats.Pending)
	}
	if stats.Processing != 1 {
		t.Errorf("expected 1 processing, got %d", stats.Processing)
	}
	if stats.Delayed != 2 {
		t.Errorf("expected 2 delayed, got %d", stats.Delayed)
	}
}

func TestQueue_Stats_Empty(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.Pending != 0 {
		t.Errorf("expected 0 pending, got %d", stats.Pending)
	}
	if stats.Processing != 0 {
		t.Errorf("expected 0 processing, got %d", stats.Processing)
	}
	if stats.Delayed != 0 {
		t.Errorf("expected 0 delayed, got %d", stats.Delayed)
	}
}

func TestQueue_Stats_LargeNumbers(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add many messages
	for range 100 {
		_, _ = mr.Lpush(mainQueueKey, "msg")
	}
	for range 50 {
		_, _ = mr.Lpush(processingQueueKey, "proc")
	}
	// Use unique members for sorted set (miniredis deduplicates by member value)
	for i := range 25 {
		_, _ = mr.ZAdd(delayedQueueKey, float64(i), fmt.Sprintf("delayed-%d", i))
	}

	stats, err := q.Stats(ctx)
	if err != nil {
		t.Fatalf("Stats failed: %v", err)
	}

	if stats.Pending != 100 {
		t.Errorf("expected 100 pending, got %d", stats.Pending)
	}
	if stats.Processing != 50 {
		t.Errorf("expected 50 processing, got %d", stats.Processing)
	}
	if stats.Delayed != 25 {
		t.Errorf("expected 25 delayed, got %d", stats.Delayed)
	}
}

func TestQueue_RecoverStaleMessages(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Manually add stale messages to processing queue
	staleMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now().Add(-10 * time.Minute), // Old message
	}
	staleData, _ := json.Marshal(staleMsg)
	_, _ = mr.Lpush(processingQueueKey, string(staleData))

	// Add a fresh message
	freshMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now(), // Fresh message
	}
	freshData, _ := json.Marshal(freshMsg)
	_, _ = mr.Lpush(processingQueueKey, string(freshData))

	// Recover stale messages (older than 5 minutes)
	recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	if recovered != 1 {
		t.Errorf("expected 1 recovered message, got %d", recovered)
	}

	// Verify stale message moved to main queue
	length := listLen(t, mr, mainQueueKey)
	if length != 1 {
		t.Errorf("expected 1 message in main queue, got %d", length)
	}

	// Verify fresh message still in processing queue
	length = listLen(t, mr, processingQueueKey)
	if length != 1 {
		t.Errorf("expected 1 message in processing queue, got %d", length)
	}
}

func TestQueue_RecoverStaleMessages_AllStale(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add multiple stale messages
	for i := range 5 {
		msg := Message{
			ID:        uuid.New().String(),
			EventID:   uuid.New(),
			EnqueueAt: time.Now().Add(-time.Duration(i+10) * time.Minute),
		}
		data, _ := json.Marshal(msg)
		_, _ = mr.Lpush(processingQueueKey, string(data))
	}

	recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	if recovered != 5 {
		t.Errorf("expected 5 recovered messages, got %d", recovered)
	}

	// All should be in main queue now
	mainLength := listLen(t, mr, mainQueueKey)
	if mainLength != 5 {
		t.Errorf("expected 5 messages in main queue, got %d", mainLength)
	}

	// Processing queue should be empty
	procLength := listLen(t, mr, processingQueueKey)
	if procLength != 0 {
		t.Errorf("expected 0 messages in processing queue, got %d", procLength)
	}
}

func TestQueue_RecoverStaleMessages_NoneStale(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add only fresh messages
	for range 3 {
		msg := Message{
			ID:        uuid.New().String(),
			EventID:   uuid.New(),
			EnqueueAt: time.Now().Add(-1 * time.Minute), // Only 1 minute old
		}
		data, _ := json.Marshal(msg)
		_, _ = mr.Lpush(processingQueueKey, string(data))
	}

	recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	if recovered != 0 {
		t.Errorf("expected 0 recovered messages, got %d", recovered)
	}

	// All should still be in processing queue
	procLength := listLen(t, mr, processingQueueKey)
	if procLength != 3 {
		t.Errorf("expected 3 messages in processing queue, got %d", procLength)
	}
}

func TestQueue_RecoverStaleMessages_InvalidJSON(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add invalid JSON to processing queue
	_, _ = mr.Lpush(processingQueueKey, "invalid json")

	// Should not fail, just skip invalid messages
	recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	if recovered != 0 {
		t.Errorf("expected 0 recovered messages, got %d", recovered)
	}
}

func TestQueue_RecoverStaleMessages_MixedValid(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add invalid JSON
	_, _ = mr.Lpush(processingQueueKey, "invalid json")

	// Add valid stale message
	staleMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now().Add(-10 * time.Minute),
	}
	staleData, _ := json.Marshal(staleMsg)
	_, _ = mr.Lpush(processingQueueKey, string(staleData))

	recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	// Should recover only the valid stale message
	if recovered != 1 {
		t.Errorf("expected 1 recovered message, got %d", recovered)
	}
}

func TestQueue_RecoverStaleMessages_Empty(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	if recovered != 0 {
		t.Errorf("expected 0 recovered messages, got %d", recovered)
	}
}

func TestQueue_RecoverStaleMessages_ShortThreshold(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add message that's only 2 minutes old
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now().Add(-2 * time.Minute),
	}
	data, _ := json.Marshal(msg)
	_, _ = mr.Lpush(processingQueueKey, string(data))

	// With 1 minute threshold, message should be recovered
	recovered, err := q.RecoverStaleMessages(ctx, 1*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	if recovered != 1 {
		t.Errorf("expected 1 recovered message, got %d", recovered)
	}
}

func TestQueue_RecoverStaleMessages_PreservesMessageContent(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	originalEventID := uuid.New()
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   originalEventID,
		ClientID:  "test-client",
		Priority:  5,
		EnqueueAt: time.Now().Add(-10 * time.Minute),
	}
	data, _ := json.Marshal(msg)
	_, _ = mr.Lpush(processingQueueKey, string(data))

	_, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
	if err != nil {
		t.Fatalf("RecoverStaleMessages failed: %v", err)
	}

	// Dequeue and verify content preserved
	recovered, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if recovered.EventID != originalEventID {
		t.Errorf("expected event ID %v, got %v", originalEventID, recovered.EventID)
	}
	if recovered.ClientID != "test-client" {
		t.Errorf("expected client ID 'test-client', got %s", recovered.ClientID)
	}
	if recovered.Priority != 5 {
		t.Errorf("expected priority 5, got %d", recovered.Priority)
	}
}
