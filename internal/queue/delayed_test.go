package queue

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestQueue_EnqueueDelayed(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	eventID := uuid.New()
	delay := 5 * time.Second

	err := q.EnqueueDelayed(ctx, eventID, delay)
	if err != nil {
		t.Fatalf("EnqueueDelayed failed: %v", err)
	}

	// Verify message is in delayed queue (sorted set)
	count := zsetLen(t, mr, delayedQueueKey)
	if count != 1 {
		t.Errorf("expected 1 delayed message, got %d", count)
	}
}

func TestQueue_EnqueueDelayed_MultipleMessages(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	delays := []time.Duration{1 * time.Second, 5 * time.Second, 10 * time.Second}
	for _, delay := range delays {
		if err := q.EnqueueDelayed(ctx, uuid.New(), delay); err != nil {
			t.Fatalf("EnqueueDelayed failed: %v", err)
		}
	}

	count := zsetLen(t, mr, delayedQueueKey)
	if count != 3 {
		t.Errorf("expected 3 delayed messages, got %d", count)
	}
}

func TestQueue_EnqueueDelayed_CorrectScore(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	eventID := uuid.New()
	delay := 10 * time.Second

	before := time.Now().UTC()
	err := q.EnqueueDelayed(ctx, eventID, delay)
	after := time.Now().UTC()

	if err != nil {
		t.Fatalf("EnqueueDelayed failed: %v", err)
	}

	// Get the message from delayed queue
	members, err := mr.ZMembers(delayedQueueKey)
	if err != nil {
		t.Fatalf("failed to get zset members: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("expected 1 member, got %d", len(members))
	}

	var msg Message
	if err := json.Unmarshal([]byte(members[0]), &msg); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	// Verify the scheduled time is approximately correct
	expectedMin := before.Add(delay)
	expectedMax := after.Add(delay)

	if msg.EnqueueAt.Before(expectedMin) || msg.EnqueueAt.After(expectedMax) {
		t.Errorf("EnqueueAt %v not in expected range [%v, %v]",
			msg.EnqueueAt, expectedMin, expectedMax)
	}
}

func TestQueue_moveDelayedToMain(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Enqueue delayed messages that are ready now
	for i := 0; i < 3; i++ {
		msg := Message{
			ID:        uuid.New().String(),
			EventID:   uuid.New(),
			EnqueueAt: time.Now().Add(-1 * time.Second), // In the past
		}
		data, _ := json.Marshal(msg)
		score := float64(msg.EnqueueAt.Unix())
		_, _ = mr.ZAdd(delayedQueueKey, score, string(data))
	}

	// Move delayed to main
	if err := q.moveDelayedToMain(ctx); err != nil {
		t.Fatalf("moveDelayedToMain failed: %v", err)
	}

	// Verify messages moved to main queue
	length := listLen(t, mr, mainQueueKey)
	if length != 3 {
		t.Errorf("expected 3 messages in main queue, got %d", length)
	}

	// Verify delayed queue is empty
	count := zsetLen(t, mr, delayedQueueKey)
	if count != 0 {
		t.Errorf("expected 0 messages in delayed queue, got %d", count)
	}
}

func TestQueue_moveDelayedToMain_NotReady(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Enqueue delayed messages that are NOT ready
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now().Add(1 * time.Hour), // In the future
	}
	data, _ := json.Marshal(msg)
	score := float64(msg.EnqueueAt.Unix())
	_, _ = mr.ZAdd(delayedQueueKey, score, string(data))

	// Move delayed to main
	if err := q.moveDelayedToMain(ctx); err != nil {
		t.Fatalf("moveDelayedToMain failed: %v", err)
	}

	// Verify message stayed in delayed queue
	count := zsetLen(t, mr, delayedQueueKey)
	if count != 1 {
		t.Errorf("expected 1 message in delayed queue, got %d", count)
	}

	// Verify main queue is empty
	length := listLen(t, mr, mainQueueKey)
	if length != 0 {
		t.Errorf("expected 0 messages in main queue, got %d", length)
	}
}

func TestQueue_moveDelayedToMain_PartialMove(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add one ready and one not ready
	readyMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now().Add(-1 * time.Second),
	}
	readyData, _ := json.Marshal(readyMsg)
	_, _ = mr.ZAdd(delayedQueueKey, float64(readyMsg.EnqueueAt.Unix()), string(readyData))

	notReadyMsg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now().Add(1 * time.Hour),
	}
	notReadyData, _ := json.Marshal(notReadyMsg)
	_, _ = mr.ZAdd(delayedQueueKey, float64(notReadyMsg.EnqueueAt.Unix()), string(notReadyData))

	// Move delayed to main
	if err := q.moveDelayedToMain(ctx); err != nil {
		t.Fatalf("moveDelayedToMain failed: %v", err)
	}

	// Verify only ready message moved
	mainLength := listLen(t, mr, mainQueueKey)
	if mainLength != 1 {
		t.Errorf("expected 1 message in main queue, got %d", mainLength)
	}

	delayedCount := zsetLen(t, mr, delayedQueueKey)
	if delayedCount != 1 {
		t.Errorf("expected 1 message in delayed queue, got %d", delayedCount)
	}
}

func TestQueue_RemoveFromDelayed(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	eventID := uuid.New()
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		EnqueueAt: time.Now().Add(1 * time.Hour),
	}
	data, _ := json.Marshal(msg)
	_, _ = mr.ZAdd(delayedQueueKey, float64(msg.EnqueueAt.Unix()), string(data))

	// Verify it's there
	count := zsetLen(t, mr, delayedQueueKey)
	if count != 1 {
		t.Fatalf("expected 1 message in delayed queue, got %d", count)
	}

	// Remove it
	if err := q.RemoveFromDelayed(ctx, eventID); err != nil {
		t.Fatalf("RemoveFromDelayed failed: %v", err)
	}

	// Verify it's gone
	count = zsetLen(t, mr, delayedQueueKey)
	if count != 0 {
		t.Errorf("expected 0 messages in delayed queue, got %d", count)
	}
}

func TestQueue_RemoveFromDelayed_NotFound(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	// Try to remove non-existent event
	err := q.RemoveFromDelayed(ctx, uuid.New())

	// Should not error - not found is not an error
	if err != nil {
		t.Errorf("expected no error for not found, got %v", err)
	}
}

func TestQueue_RemoveFromDelayed_MultipleMessages(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add multiple messages
	eventIDs := make([]uuid.UUID, 3)
	for i := 0; i < 3; i++ {
		eventIDs[i] = uuid.New()
		msg := Message{
			ID:        uuid.New().String(),
			EventID:   eventIDs[i],
			EnqueueAt: time.Now().Add(time.Duration(i+1) * time.Hour),
		}
		data, _ := json.Marshal(msg)
		_, _ = mr.ZAdd(delayedQueueKey, float64(msg.EnqueueAt.Unix()), string(data))
	}

	// Remove the middle one
	if err := q.RemoveFromDelayed(ctx, eventIDs[1]); err != nil {
		t.Fatalf("RemoveFromDelayed failed: %v", err)
	}

	// Verify 2 remain
	count := zsetLen(t, mr, delayedQueueKey)
	if count != 2 {
		t.Errorf("expected 2 messages in delayed queue, got %d", count)
	}

	// Verify the right ones remain
	members, _ := mr.ZMembers(delayedQueueKey)
	for _, m := range members {
		var msg Message
		_ = json.Unmarshal([]byte(m), &msg)
		if msg.EventID == eventIDs[1] {
			t.Error("removed event should not be in delayed queue")
		}
	}
}

func TestFormatFloat(t *testing.T) {
	tests := []struct {
		input    float64
		expected string
	}{
		{1234567890.0, "1234567890"},
		{0.0, "0"},
		{-123.0, "-123"},
		{9999999999.0, "9999999999"},
	}

	for _, tc := range tests {
		result := formatFloat(tc.input)
		if result != tc.expected {
			t.Errorf("formatFloat(%v) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

func TestQueue_EnqueueDelayed_ZeroDelay(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Zero delay should still work (scheduled for now)
	err := q.EnqueueDelayed(ctx, uuid.New(), 0)
	if err != nil {
		t.Fatalf("EnqueueDelayed with zero delay failed: %v", err)
	}

	count := zsetLen(t, mr, delayedQueueKey)
	if count != 1 {
		t.Errorf("expected 1 message in delayed queue, got %d", count)
	}
}
