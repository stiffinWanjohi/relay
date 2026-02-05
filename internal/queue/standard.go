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

// Enqueue adds an event to the queue for immediate processing.
func (q *Queue) Enqueue(ctx context.Context, eventID uuid.UUID) error {
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		EnqueueAt: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("failed to marshal message", "event_id", eventID, "error", err)
		return err
	}

	if err := q.client.LPush(ctx, mainQueueKey, data).Err(); err != nil {
		log.Error("failed to enqueue message", "event_id", eventID, "queue", mainQueueKey, "error", err)
		return err
	}

	log.Debug("message enqueued", "event_id", eventID, "message_id", msg.ID, "queue", mainQueueKey)

	if q.metrics != nil {
		q.metrics.QueueEnqueued(ctx)
	}

	return nil
}

// Dequeue retrieves a message from the queue with visibility timeout.
// Returns ErrQueueEmpty if no messages are available.
func (q *Queue) Dequeue(ctx context.Context) (*Message, error) {
	// First, move any delayed messages that are ready
	if err := q.moveDelayedToMain(ctx); err != nil {
		log.Warn("failed to move delayed messages", "error", err)
		return nil, err
	}

	// Check for context cancellation before blocking
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Use BRPOPLPUSH for atomic dequeue
	result, err := q.client.BRPopLPush(ctx, mainQueueKey, processingQueueKey, q.blockingTimeout).Result()
	if errors.Is(err, redis.Nil) {
		return nil, domain.ErrQueueEmpty
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		log.Error("failed to dequeue message", "error", err)
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		log.Error("failed to unmarshal message", "error", err)
		return nil, err
	}

	log.Debug("message dequeued", "event_id", msg.EventID, "message_id", msg.ID)

	if q.metrics != nil {
		q.metrics.QueueDequeued(ctx)
	}

	return &msg, nil
}

// Ack acknowledges successful processing of a message.
func (q *Queue) Ack(ctx context.Context, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("failed to marshal message for ack", "message_id", msg.ID, "error", err)
		return err
	}

	removed, err := q.client.LRem(ctx, processingQueueKey, 1, data).Result()
	if err != nil {
		log.Error("failed to ack message", "message_id", msg.ID, "error", err)
		return err
	}
	if removed == 0 {
		log.Warn("message not found for ack", "message_id", msg.ID, "event_id", msg.EventID)
		return domain.ErrMessageNotFound
	}

	log.Debug("message acked", "event_id", msg.EventID, "message_id", msg.ID)
	return nil
}

// Nack returns a message to the queue for reprocessing with optional delay.
func (q *Queue) Nack(ctx context.Context, msg *Message, delay time.Duration) error {
	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("failed to marshal message for nack", "message_id", msg.ID, "error", err)
		return err
	}

	// Remove from processing queue and verify it existed
	removed, err := q.client.LRem(ctx, processingQueueKey, 1, data).Result()
	if err != nil {
		log.Error("failed to remove message from processing", "message_id", msg.ID, "error", err)
		return err
	}
	if removed == 0 {
		log.Warn("message not found for nack", "message_id", msg.ID, "event_id", msg.EventID)
		return domain.ErrMessageNotFound
	}

	// Re-enqueue with delay if specified
	if delay > 0 {
		log.Debug("message nacked with delay", "event_id", msg.EventID, "message_id", msg.ID, "delay", delay)
		return q.EnqueueDelayed(ctx, msg.EventID, delay)
	}

	// Otherwise, add back to main queue
	if err := q.client.RPush(ctx, mainQueueKey, data).Err(); err != nil {
		log.Error("failed to requeue message", "message_id", msg.ID, "error", err)
		return err
	}

	log.Debug("message nacked", "event_id", msg.EventID, "message_id", msg.ID)
	return nil
}
