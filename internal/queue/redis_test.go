package queue

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

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

// Helper to get list length
func listLen(t *testing.T, mr *miniredis.Miniredis, key string) int {
	t.Helper()
	list, err := mr.List(key)
	if err != nil {
		return 0
	}
	return len(list)
}

// Helper to get sorted set size
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
}

func TestQueue_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	q2 := q.WithMetrics(metrics)

	if q2.metrics != metrics {
		t.Error("expected metrics to be set")
	}
	// Verify other fields are preserved
	if q2.visibilityTimeout != q.visibilityTimeout {
		t.Error("visibility timeout not preserved")
	}
	if q2.blockingTimeout != q.blockingTimeout {
		t.Error("blocking timeout not preserved")
	}
}

func TestQueue_WithVisibilityTimeout(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	customTimeout := 5 * time.Minute

	q2 := q.WithVisibilityTimeout(customTimeout)

	if q2.visibilityTimeout != customTimeout {
		t.Errorf("expected visibility timeout %v, got %v", customTimeout, q2.visibilityTimeout)
	}
	// Verify other fields are preserved
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
	// Verify other fields are preserved
	if q2.visibilityTimeout != q.visibilityTimeout {
		t.Error("visibility timeout not preserved")
	}
}

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
	// Metrics are called but we can't easily verify with noop provider
}

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
	// because miniredis truncates timeouts < 1s to 1s
	// In production with real Redis, this would return context.Canceled
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

func TestQueue_moveDelayedToMain(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Enqueue delayed messages that are ready now
	for range 3 {
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

func TestQueue_EnqueueForClient(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	clientID := "client-123"
	eventID := uuid.New()

	err := q.EnqueueForClient(ctx, clientID, eventID)
	if err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}

	// Verify message is in client queue
	clientKey := clientQueuePrefix + clientID
	length := listLen(t, mr, clientKey)
	if length != 1 {
		t.Errorf("expected queue length 1, got %d", length)
	}

	// Verify client is in active clients set
	members, err := mr.SMembers(activeClientsKey)
	if err != nil {
		t.Fatalf("failed to get active clients: %v", err)
	}
	found := false
	for _, m := range members {
		if m == clientID {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected client to be in active clients set")
	}
}

func TestQueue_EnqueueForClient_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	q = q.WithMetrics(metrics)

	ctx := context.Background()
	err := q.EnqueueForClient(ctx, "client-1", uuid.New())
	if err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}
}

func TestQueue_DequeueFromClient(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()
	clientID := "client-123"
	eventID := uuid.New()

	// Enqueue for client
	if err := q.EnqueueForClient(ctx, clientID, eventID); err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}

	// Dequeue from client
	msg, err := q.DequeueFromClient(ctx, clientID)
	if err != nil {
		t.Fatalf("DequeueFromClient failed: %v", err)
	}

	if msg.EventID != eventID {
		t.Errorf("expected event ID %v, got %v", eventID, msg.EventID)
	}
	if msg.ClientID != clientID {
		t.Errorf("expected client ID %s, got %s", clientID, msg.ClientID)
	}
}

func TestQueue_DequeueFromClient_EmptyQueue(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	_, err := q.DequeueFromClient(ctx, "nonexistent-client")
	if !errors.Is(err, domain.ErrQueueEmpty) {
		t.Errorf("expected ErrQueueEmpty, got %v", err)
	}
}

func TestQueue_DequeueFromClient_RemovesFromActiveClients(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()
	clientID := "client-123"

	// Enqueue single message
	if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}

	// Verify client is active
	members, _ := mr.SMembers(activeClientsKey)
	if len(members) != 1 {
		t.Errorf("expected 1 active client, got %d", len(members))
	}

	// Dequeue - should remove from active clients since queue is now empty
	_, err := q.DequeueFromClient(ctx, clientID)
	if err != nil {
		t.Fatalf("DequeueFromClient failed: %v", err)
	}

	// Verify client is removed from active clients
	members, _ = mr.SMembers(activeClientsKey)
	if len(members) != 0 {
		t.Errorf("expected 0 active clients, got %d", len(members))
	}
}

func TestQueue_DequeueFromClient_WithMetrics(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	q = q.WithMetrics(metrics).WithBlockingTimeout(100 * time.Millisecond)

	ctx := context.Background()
	clientID := "client-1"

	if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}

	_, err := q.DequeueFromClient(ctx, clientID)
	if err != nil {
		t.Fatalf("DequeueFromClient failed: %v", err)
	}
}

func TestQueue_GetActiveClients(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Add clients to active set
	_, _ = mr.SAdd(activeClientsKey, "client-1", "client-2", "client-3")

	clients, err := q.GetActiveClients(ctx)
	if err != nil {
		t.Fatalf("GetActiveClients failed: %v", err)
	}

	if len(clients) != 3 {
		t.Errorf("expected 3 clients, got %d", len(clients))
	}
}

func TestQueue_GetClientQueueLength(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	clientID := "client-123"

	// Add messages to client queue
	clientKey := clientQueuePrefix + clientID
	_, _ = mr.Lpush(clientKey, "msg1")
	_, _ = mr.Lpush(clientKey, "msg2")
	_, _ = mr.Lpush(clientKey, "msg3")

	length, err := q.GetClientQueueLength(ctx, clientID)
	if err != nil {
		t.Fatalf("GetClientQueueLength failed: %v", err)
	}

	if length != 3 {
		t.Errorf("expected length 3, got %d", length)
	}
}

func TestQueue_ClientStats(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Setup multiple clients with queues
	_, _ = mr.SAdd(activeClientsKey, "client-1", "client-2")
	_, _ = mr.Lpush(clientQueuePrefix+"client-1", "msg1")
	_, _ = mr.Lpush(clientQueuePrefix+"client-1", "msg2")
	_, _ = mr.Lpush(clientQueuePrefix+"client-2", "msg1")

	stats, err := q.ClientStats(ctx)
	if err != nil {
		t.Fatalf("ClientStats failed: %v", err)
	}

	if len(stats) != 2 {
		t.Errorf("expected 2 clients in stats, got %d", len(stats))
	}
	if stats["client-1"] != 2 {
		t.Errorf("expected client-1 to have 2 messages, got %d", stats["client-1"])
	}
	if stats["client-2"] != 1 {
		t.Errorf("expected client-2 to have 1 message, got %d", stats["client-2"])
	}
}

func TestClientQueueKey(t *testing.T) {
	clientID := "test-client"
	expected := clientQueuePrefix + clientID

	result := clientQueueKey(clientID)
	if result != expected {
		t.Errorf("expected %s, got %s", expected, result)
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
	}

	for _, tc := range tests {
		result := formatFloat(tc.input)
		if result != tc.expected {
			t.Errorf("formatFloat(%v) = %s, expected %s", tc.input, result, tc.expected)
		}
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

	// Enqueue concurrently
	done := make(chan struct{})
	for range 10 {
		go func() {
			for range 10 {
				_ = q.Enqueue(ctx, uuid.New())
			}
			done <- struct{}{}
		}()
	}

	// Wait for all goroutines
	for range 10 {
		<-done
	}

	// Verify all messages enqueued
	length := listLen(t, mr, mainQueueKey)
	if length != 100 {
		t.Errorf("expected 100 messages, got %d", length)
	}
}

func TestQueue_ChainedWithMethods(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	q2 := q.
		WithMetrics(metrics).
		WithVisibilityTimeout(2 * time.Minute).
		WithBlockingTimeout(5 * time.Second)

	if q2.metrics != metrics {
		t.Error("metrics not preserved in chain")
	}
	if q2.visibilityTimeout != 2*time.Minute {
		t.Error("visibility timeout not preserved in chain")
	}
	if q2.blockingTimeout != 5*time.Second {
		t.Error("blocking timeout not preserved in chain")
	}
}

// ============================================================================
// FIFO Queue Tests
// ============================================================================

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
	for range 5 {
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
