package queue

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

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

func TestQueue_EnqueueForClient_Multiple(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()
	clientID := "client-123"

	for i := 0; i < 5; i++ {
		if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
			t.Fatalf("EnqueueForClient %d failed: %v", i, err)
		}
	}

	clientKey := clientQueuePrefix + clientID
	length := listLen(t, mr, clientKey)
	if length != 5 {
		t.Errorf("expected queue length 5, got %d", length)
	}
}

func TestQueue_EnqueueForClient_MultipleClients(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	clients := []string{"client-1", "client-2", "client-3"}
	for _, clientID := range clients {
		if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
			t.Fatalf("EnqueueForClient failed: %v", err)
		}
	}

	// Verify all clients are active
	members, _ := mr.SMembers(activeClientsKey)
	if len(members) != 3 {
		t.Errorf("expected 3 active clients, got %d", len(members))
	}

	// Verify each client has their own queue
	for _, clientID := range clients {
		clientKey := clientQueuePrefix + clientID
		length := listLen(t, mr, clientKey)
		if length != 1 {
			t.Errorf("client %s: expected queue length 1, got %d", clientID, length)
		}
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

func TestQueue_DequeueFromClient_KeepsActiveWithRemainingMessages(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()
	clientID := "client-123"

	// Enqueue two messages
	if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}
	if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}

	// Dequeue one
	_, err := q.DequeueFromClient(ctx, clientID)
	if err != nil {
		t.Fatalf("DequeueFromClient failed: %v", err)
	}

	// Verify client is still active (one message remains)
	members, _ := mr.SMembers(activeClientsKey)
	if len(members) != 1 {
		t.Errorf("expected 1 active client, got %d", len(members))
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

func TestQueue_DequeueFromClient_MovesToProcessingQueue(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()
	clientID := "client-123"

	if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
		t.Fatalf("EnqueueForClient failed: %v", err)
	}

	_, err := q.DequeueFromClient(ctx, clientID)
	if err != nil {
		t.Fatalf("DequeueFromClient failed: %v", err)
	}

	// Message should be in processing queue
	procLength := listLen(t, mr, processingQueueKey)
	if procLength != 1 {
		t.Errorf("expected 1 message in processing queue, got %d", procLength)
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

func TestQueue_GetActiveClients_Empty(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	clients, err := q.GetActiveClients(ctx)
	if err != nil {
		t.Fatalf("GetActiveClients failed: %v", err)
	}

	if len(clients) != 0 {
		t.Errorf("expected 0 clients, got %d", len(clients))
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

func TestQueue_GetClientQueueLength_NonExistent(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	length, err := q.GetClientQueueLength(ctx, "nonexistent-client")
	if err != nil {
		t.Fatalf("GetClientQueueLength failed: %v", err)
	}

	if length != 0 {
		t.Errorf("expected length 0, got %d", length)
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

func TestQueue_ClientStats_Empty(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	ctx := context.Background()

	stats, err := q.ClientStats(ctx)
	if err != nil {
		t.Fatalf("ClientStats failed: %v", err)
	}

	if len(stats) != 0 {
		t.Errorf("expected 0 clients in stats, got %d", len(stats))
	}
}

func TestClientQueueKey(t *testing.T) {
	tests := []struct {
		clientID string
		expected string
	}{
		{"client-1", "relay:queue:client:client-1"},
		{"abc", "relay:queue:client:abc"},
		{"client-with-dashes", "relay:queue:client:client-with-dashes"},
	}

	for _, tc := range tests {
		result := clientQueueKey(tc.clientID)
		if result != tc.expected {
			t.Errorf("clientQueueKey(%q) = %q, expected %q", tc.clientID, result, tc.expected)
		}
	}
}

func TestQueue_ClientQueue_FairScheduling(t *testing.T) {
	q, _, _ := setupTestQueue(t)
	q = q.WithBlockingTimeout(100 * time.Millisecond)
	ctx := context.Background()

	// Enqueue different amounts for different clients
	clients := []string{"client-1", "client-2", "client-3"}
	for _, clientID := range clients {
		for i := 0; i < 3; i++ {
			if err := q.EnqueueForClient(ctx, clientID, uuid.New()); err != nil {
				t.Fatalf("EnqueueForClient failed: %v", err)
			}
		}
	}

	// Round-robin dequeue from each client
	for round := 0; round < 3; round++ {
		for _, clientID := range clients {
			msg, err := q.DequeueFromClient(ctx, clientID)
			if err != nil {
				t.Fatalf("DequeueFromClient failed: %v", err)
			}
			if msg.ClientID != clientID {
				t.Errorf("expected client %s, got %s", clientID, msg.ClientID)
			}
		}
	}
}

func TestQueue_ClientQueue_Concurrent(t *testing.T) {
	q, mr, _ := setupTestQueue(t)
	ctx := context.Background()

	// Concurrent enqueue for multiple clients
	var wg sync.WaitGroup
	clients := []string{"client-1", "client-2", "client-3"}

	for _, clientID := range clients {
		wg.Add(1)
		go func(cid string) {
			defer wg.Done()
			for i := 0; i < 10; i++ {
				_ = q.EnqueueForClient(ctx, cid, uuid.New())
			}
		}(clientID)
	}

	wg.Wait()

	// Verify totals
	for _, clientID := range clients {
		clientKey := clientQueuePrefix + clientID
		length := listLen(t, mr, clientKey)
		if length != 10 {
			t.Errorf("client %s: expected 10 messages, got %d", clientID, length)
		}
	}
}
