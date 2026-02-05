package queue

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

// Per-client queue support for fair scheduling

const (
	clientQueuePrefix = "relay:queue:client:"
	activeClientsKey  = "relay:queue:active_clients"
)

func clientQueueKey(clientID string) string {
	return clientQueuePrefix + clientID
}

// EnqueueForClient adds an event to a client-specific queue.
// This enables fair scheduling across multiple tenants.
func (q *Queue) EnqueueForClient(ctx context.Context, clientID string, eventID uuid.UUID) error {
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		ClientID:  clientID,
		EnqueueAt: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("failed to marshal client message", "client_id", clientID, "event_id", eventID, "error", err)
		return err
	}

	pipe := q.client.Pipeline()
	pipe.LPush(ctx, clientQueueKey(clientID), data)
	pipe.SAdd(ctx, activeClientsKey, clientID) // Track active clients
	if _, err := pipe.Exec(ctx); err != nil {
		log.Error("failed to enqueue client message", "client_id", clientID, "event_id", eventID, "error", err)
		return err
	}

	log.Debug("message enqueued for client",
		"client_id", clientID,
		"event_id", eventID,
		"message_id", msg.ID,
	)

	if q.metrics != nil {
		q.metrics.QueueEnqueued(ctx)
	}

	return nil
}

// DequeueFromClient retrieves a message from a specific client's queue.
func (q *Queue) DequeueFromClient(ctx context.Context, clientID string) (*Message, error) {
	result, err := q.client.BRPopLPush(ctx, clientQueueKey(clientID), processingQueueKey, q.blockingTimeout).Result()
	if errors.Is(err, redis.Nil) {
		return nil, domain.ErrQueueEmpty
	}
	if err != nil {
		log.Error("failed to dequeue from client queue", "client_id", clientID, "error", err)
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		log.Error("failed to unmarshal client message", "client_id", clientID, "error", err)
		return nil, err
	}

	// Check if client queue is now empty
	length, _ := q.client.LLen(ctx, clientQueueKey(clientID)).Result()
	if length == 0 {
		// Remove from active clients set
		q.client.SRem(ctx, activeClientsKey, clientID)
		log.Debug("client queue emptied, removed from active clients", "client_id", clientID)
	}

	log.Debug("message dequeued from client queue",
		"client_id", clientID,
		"event_id", msg.EventID,
		"message_id", msg.ID,
	)

	if q.metrics != nil {
		q.metrics.QueueDequeued(ctx)
	}

	return &msg, nil
}

// GetActiveClients returns a list of clients with pending events.
func (q *Queue) GetActiveClients(ctx context.Context) ([]string, error) {
	clients, err := q.client.SMembers(ctx, activeClientsKey).Result()
	if err != nil {
		log.Error("failed to get active clients", "error", err)
		return nil, err
	}
	return clients, nil
}

// GetClientQueueLength returns the number of pending events for a client.
func (q *Queue) GetClientQueueLength(ctx context.Context, clientID string) (int64, error) {
	length, err := q.client.LLen(ctx, clientQueueKey(clientID)).Result()
	if err != nil {
		log.Error("failed to get client queue length", "client_id", clientID, "error", err)
		return 0, err
	}
	return length, nil
}

// ClientStats returns queue statistics per client.
func (q *Queue) ClientStats(ctx context.Context) (map[string]int64, error) {
	clients, err := q.GetActiveClients(ctx)
	if err != nil {
		return nil, err
	}

	stats := make(map[string]int64)
	for _, clientID := range clients {
		length, _ := q.GetClientQueueLength(ctx, clientID)
		stats[clientID] = length
	}

	return stats, nil
}
