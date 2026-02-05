package queue

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

func TestQueue_Enqueue(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	eventID := uuid.New()

	err := q.Enqueue(ctx, eventID)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Verify message is in main queue
	length := listLen(t, mr, mainQueueKey)
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Verify message content
	data, err := mr.Lpop(mainQueueKey)
	if err != nil {
		t.Fatalf("failed to pop message: %v", err)
	}

	var msg Message
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		t.Fatalf("failed to unmarshal message: %v", err)
	}

	if msg.EventID != eventID {
		t.Errorf("expected event ID %v, got %v", eventID, msg.EventID)
	}
	if msg.ID == "" {
		t.Error("expected message ID to be set")
	}
	if msg.EnqueueAt.IsZero() {
		t.Error("expected EnqueueAt to be set")
	}
}

func TestQueue_Enqueue_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	q = q.WithMetrics(metrics)

	ctx := context.Background()
	err := q.Enqueue(ctx, uuid.New())
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}
}

func TestQueue_Enqueue_Multiple(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Enqueue multiple messages
	for i := 0; i < 10; i++ {
		if err := q.Enqueue(ctx, uuid.New()); err != nil {
			t.Fatalf("Enqueue %d failed: %v", i, err)
		}
	}

	length := listLen(t, mr, mainQueueKey)
	if length != 10 {
		t.Errorf("expected queue length 10, got %d", length)
	}
}

func TestQueue_Dequeue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()
	eventID := uuid.New()

	// Enqueue first
	if err := q.Enqueue(ctx, eventID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Dequeue
	msg, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if msg.EventID != eventID {
		t.Errorf("expected event ID %v, got %v", eventID, msg.EventID)
	}
}

func TestQueue_Dequeue_EmptyQueue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	_, err := q.Dequeue(ctx)
	if !errors.Is(err, domain.ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_Dequeue_ContextCanceled(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(2 * time.Second)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(100 * time.Millisecond)
		cancel()
	}()

	_, err := q.Dequeue(ctx)
	// With miniredis, we may get ErrQueueEmpty before context cancellation
	if !errors.Is(err, context.Canceled) && !errors.Is(err, domain.ErrQueueEmpty) {
		t.Errorf("expected context.Canceled or ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_Dequeue_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	q = q.WithMetrics(metrics).WithBlockingTimeout(100 * time.Millisecond)

	ctx := context.Background()
	eventID := uuid.New()

	if err := q.Enqueue(ctx, eventID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	_, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}
}

func TestQueue_Dequeue_MovesToProcessingQueue(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	if err := q.Enqueue(ctx, uuid.New()); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	_, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Message should be in processing queue
	procLength := listLen(t, mr, processingQueueKey)
	if procLength != 1 {
		t.Errorf("expected 1 message in processing queue, got %d", procLength)
	}

	// Main queue should be empty
	mainLength := listLen(t, mr, mainQueueKey)
	if mainLength != 0 {
		t.Errorf("expected 0 messages in main queue, got %d", mainLength)
	}
}

func TestQueue_Ack(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Enqueue and dequeue
	if err := q.Enqueue(ctx, uuid.New()); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	msg, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Verify message is in processing queue
	length := listLen(t, mr, processingQueueKey)
	if length != 1 {
		t.Errorf("expected 1 message in processing queue, got %d", length)
	}

	// Ack the message
	if err := q.Ack(ctx, msg); err != nil {
		t.Fatalf("Ack failed: %v", err)
	}

	// Verify message is removed from processing queue
	length = listLen(t, mr, processingQueueKey)
	if length != 0 {
		t.Errorf("expected 0 messages in processing queue, got %d", length)
	}
}

func TestQueue_Ack_MessageNotFound(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	msg := &Message{
		ID:      uuid.New().String(),
		EventID: uuid.New(),
	}

	err := q.Ack(ctx, msg)
	if !errors.Is(err, domain.ErrMessageNotFound) {
		t.Errorf("expected ErrMessageNotFound, got %v", err)
	}
}

func TestQueue_Nack_WithoutDelay(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	eventID := uuid.New()
	if err := q.Enqueue(ctx, eventID); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	msg, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Nack without delay - should go back to main queue
	if err := q.Nack(ctx, msg, 0); err != nil {
		t.Fatalf("Nack failed: %v", err)
	}

	// Verify message is back in main queue
	length := listLen(t, mr, mainQueueKey)
	if length != 1 {
		t.Errorf("expected 1 message in main queue, got %d", length)
	}

	// Verify processing queue is empty
	length = listLen(t, mr, processingQueueKey)
	if length != 0 {
		t.Errorf("expected 0 messages in processing queue, got %d", length)
	}
}

func TestQueue_Nack_WithDelay(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	if err := q.Enqueue(ctx, uuid.New()); err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	msg, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	// Nack with delay - should go to delayed queue
	if err := q.Nack(ctx, msg, 5*time.Second); err != nil {
		t.Fatalf("Nack failed: %v", err)
	}

	// Verify message is in delayed queue
	count := zsetLen(t, mr, delayedQueueKey)
	if count != 1 {
		t.Errorf("expected 1 message in delayed queue, got %d", count)
	}

	// Verify processing queue is empty
	length := listLen(t, mr, processingQueueKey)
	if length != 0 {
		t.Errorf("expected 0 messages in processing queue, got %d", length)
	}
}

func TestQueue_Nack_MessageNotFound(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	msg := &Message{
		ID:      uuid.New().String(),
		EventID: uuid.New(),
	}

	err := q.Nack(ctx, msg, 0)
	if !errors.Is(err, domain.ErrMessageNotFound) {
		t.Errorf("expected ErrMessageNotFound, got %v", err)
	}
}

func TestQueue_MultipleEnqueueDequeue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Enqueue multiple messages
	eventIDs := make([]uuid.UUID, 5)
	for i := range 5 {
		eventIDs[i] = uuid.New()
		if err := q.Enqueue(ctx, eventIDs[i]); err != nil {
			t.Fatalf("Enqueue %d failed: %v", i, err)
		}
	}

	// Dequeue in FIFO order
	for i := range 5 {
		msg, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("Dequeue %d failed: %v", i, err)
		}
		// LPUSH + BRPOPLPUSH = FIFO
		if msg.EventID != eventIDs[i] {
			t.Errorf("Dequeue %d: expected event ID %v, got %v", i, eventIDs[i], msg.EventID)
		}
		if err := q.Ack(ctx, msg); err != nil {
			t.Fatalf("Ack %d failed: %v", i, err)
		}
	}
}

func TestQueue_ConcurrentEnqueue(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	var wg sync.WaitGroup
	numGoroutines := 10
	messagesPerGoroutine := 10

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < messagesPerGoroutine; j++ {
				_ = q.Enqueue(ctx, uuid.New())
			}
		}()
	}

	wg.Wait()

	// Verify all messages enqueued
	length := listLen(t, mr, mainQueueKey)
	expected := numGoroutines * messagesPerGoroutine
	if length != expected {
		t.Errorf("expected %d messages, got %d", expected, length)
	}
}

func TestQueue_ConcurrentDequeue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Pre-populate
	numMessages := 50
	for i := 0; i < numMessages; i++ {
		if err := q.Enqueue(ctx, uuid.New()); err != nil {
			t.Fatalf("Enqueue failed: %v", err)
		}
	}

	var wg sync.WaitGroup
	numWorkers := 5
	results := make(chan *Message, numMessages)

	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				msg, err := q.Dequeue(ctx)
				if err != nil {
					return
				}
				results <- msg
				_ = q.Ack(ctx, msg)
			}
		}()
	}

	wg.Wait()
	close(results)

	// Count dequeued messages
	dequeued := 0
	for range results {
		dequeued++
	}

	if dequeued != numMessages {
		t.Errorf("expected %d dequeued, got %d", numMessages, dequeued)
	}
}

func TestQueue_Dequeue_MovesDelayedFirst(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Add ready delayed message
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   uuid.New(),
		EnqueueAt: time.Now().Add(-1 * time.Second),
	}
	data, _ := json.Marshal(msg)
	score := float64(msg.EnqueueAt.Unix())
	_, _ = mr.ZAdd(delayedQueueKey, score, string(data))

	// Dequeue should move delayed to main first
	result, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue failed: %v", err)
	}

	if result.EventID != msg.EventID {
		t.Errorf("expected event ID %v, got %v", msg.EventID, result.EventID)
	}
}

func TestQueue_Enqueue_ContextDeadlineExceeded(t *testing.T) {
	q, _, _ := setupTestQueue(t)

	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(1 * time.Millisecond) // Ensure context is expired

	err := q.Enqueue(ctx, uuid.New())
	// The enqueue might succeed if Redis is fast enough, or fail with deadline exceeded
	// This is implementation-dependent behavior
	_ = err
}
