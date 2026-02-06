package queue

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

// PriorityStats holds statistics for priority queues.
type PriorityStats struct {
	High    int64
	Normal  int64
	Low     int64
	Delayed int64
}

// priorityQueueKey returns the queue key for a given priority level.
func priorityQueueKey(priority int) string {
	switch {
	case priority <= 3:
		return priorityHighKey
	case priority <= 7:
		return priorityNormalKey
	default:
		return priorityLowKey
	}
}

// priorityName returns a human-readable name for a priority level.
func priorityName(priority int) string {
	switch {
	case priority <= 3:
		return "high"
	case priority <= 7:
		return "normal"
	default:
		return "low"
	}
}

// EnqueueWithPriority adds an event to the appropriate priority queue.
func (q *Queue) EnqueueWithPriority(ctx context.Context, eventID uuid.UUID, priority int) error {
	// Normalize priority to valid range
	if priority < 1 {
		priority = 1
	} else if priority > 10 {
		priority = 10
	}

	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		Priority:  priority,
		EnqueueAt: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("failed to marshal priority message", "event_id", eventID, "error", err)
		return err
	}

	queueKey := priorityQueueKey(priority)
	if err := q.client.LPush(ctx, queueKey, data).Err(); err != nil {
		log.Error("failed to enqueue priority message",
			"event_id", eventID,
			"priority", priority,
			"queue", queueKey,
			"error", err,
		)
		return err
	}

	log.Debug("message enqueued with priority",
		"event_id", eventID,
		"message_id", msg.ID,
		"priority", priority,
		"priority_level", priorityName(priority),
	)

	if q.metrics != nil {
		q.metrics.QueueEnqueued(ctx)
	}

	return nil
}

// EnqueueDelayedWithPriority adds an event to the delayed queue with priority info.
func (q *Queue) EnqueueDelayedWithPriority(ctx context.Context, eventID uuid.UUID, priority int, delay time.Duration) error {
	// Normalize priority to valid range
	if priority < 1 {
		priority = 1
	} else if priority > 10 {
		priority = 10
	}

	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		Priority:  priority,
		EnqueueAt: time.Now().UTC().Add(delay),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("failed to marshal delayed priority message", "event_id", eventID, "error", err)
		return err
	}

	score := float64(msg.EnqueueAt.Unix())
	if err := q.client.ZAdd(ctx, delayedQueueKey, redis.Z{Score: score, Member: data}).Err(); err != nil {
		log.Error("failed to enqueue delayed priority message",
			"event_id", eventID,
			"priority", priority,
			"delay", delay,
			"error", err,
		)
		return err
	}

	log.Debug("message enqueued with priority and delay",
		"event_id", eventID,
		"message_id", msg.ID,
		"priority", priority,
		"delay", delay,
		"scheduled_at", msg.EnqueueAt,
	)

	return nil
}

// starvationCounter tracks how many messages have been dequeued from high priority
// to implement weighted fair queuing (process some low priority periodically)
var starvationCounter uint64

// DequeueWithPriority retrieves a message from the priority queues using weighted fair queuing.
// Uses a weighted approach: for every 10 high-priority messages, process at least 1 from
// normal queue, and for every 20, process at least 1 from low queue.
// This prevents starvation of low-priority messages while still prioritizing high-priority ones.
func (q *Queue) DequeueWithPriority(ctx context.Context) (*Message, error) {
	// First, move any delayed messages that are ready to their priority queues
	if err := q.moveDelayedToPriority(ctx); err != nil {
		log.Warn("failed to move delayed messages to priority queues", "error", err)
		return nil, err
	}

	// Check for context cancellation before blocking
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Increment starvation counter atomically
	counter := atomic.AddUint64(&starvationCounter, 1) - 1

	// Weighted fair queuing:
	// - Every 20th dequeue, try low priority first (5% of traffic)
	// - Every 10th dequeue, try normal priority first (10% of traffic)
	// - Otherwise, strict priority order
	var queueOrder []string
	if counter%20 == 19 {
		// Give low priority a chance
		queueOrder = []string{priorityLowKey, priorityNormalKey, priorityHighKey}
	} else if counter%10 == 9 {
		// Give normal priority a chance
		queueOrder = []string{priorityNormalKey, priorityHighKey, priorityLowKey}
	} else {
		// Standard priority order
		queueOrder = []string{priorityHighKey, priorityNormalKey, priorityLowKey}
	}

	for _, queueKey := range queueOrder {
		// Non-blocking check
		result, err := q.client.RPopLPush(ctx, queueKey, processingQueueKey).Result()
		if err == nil {
			var msg Message
			if err := json.Unmarshal([]byte(result), &msg); err != nil {
				log.Error("failed to unmarshal priority message", "queue", queueKey, "error", err)
				return nil, err
			}

			log.Debug("message dequeued from priority queue",
				"event_id", msg.EventID,
				"message_id", msg.ID,
				"priority", msg.Priority,
				"queue", queueKey,
			)

			if q.metrics != nil {
				q.metrics.QueueDequeued(ctx)
			}
			return &msg, nil
		}
		if !errors.Is(err, redis.Nil) {
			log.Error("failed to dequeue from priority queue", "queue", queueKey, "error", err)
			return nil, err
		}
	}

	// All priority queues are empty, fall back to main queue with blocking
	result, err := q.client.BRPopLPush(ctx, mainQueueKey, processingQueueKey, q.blockingTimeout).Result()
	if errors.Is(err, redis.Nil) {
		return nil, domain.ErrQueueEmpty
	}
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		log.Error("failed to dequeue from main queue", "error", err)
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		log.Error("failed to unmarshal message from main queue", "error", err)
		return nil, err
	}

	log.Debug("message dequeued from main queue", "event_id", msg.EventID, "message_id", msg.ID)

	if q.metrics != nil {
		q.metrics.QueueDequeued(ctx)
	}

	return &msg, nil
}

// moveDelayedToPriority moves ready delayed messages to their priority queues.
func (q *Queue) moveDelayedToPriority(ctx context.Context) error {
	now := formatFloat(float64(time.Now().UTC().Unix()))

	// Get messages that are ready
	messages, err := q.client.ZRangeByScore(ctx, delayedQueueKey, &redis.ZRangeBy{
		Min:   "-inf",
		Max:   now,
		Count: int64(delayedBatchLimit),
	}).Result()
	if err != nil {
		log.Error("failed to get ready delayed messages", "error", err)
		return err
	}

	if len(messages) == 0 {
		return nil
	}

	pipe := q.client.Pipeline()
	movedCount := 0

	for _, msgData := range messages {
		var msg Message
		if err := json.Unmarshal([]byte(msgData), &msg); err != nil {
			log.Warn("failed to unmarshal delayed message, skipping", "error", err)
			continue
		}

		// Determine target queue based on priority
		var targetQueue string
		if msg.Priority > 0 {
			targetQueue = priorityQueueKey(msg.Priority)
		} else {
			targetQueue = mainQueueKey // No priority info, use main queue
		}

		pipe.RPush(ctx, targetQueue, msgData)
		pipe.ZRem(ctx, delayedQueueKey, msgData)
		movedCount++
	}

	_, err = pipe.Exec(ctx)
	if err != nil {
		log.Error("failed to move delayed messages to priority queues", "error", err)
		return err
	}

	if movedCount > 0 {
		log.Debug("moved delayed messages to priority queues", "count", movedCount)
	}

	return nil
}

// GetPriorityStats returns the current depth of each priority queue.
func (q *Queue) GetPriorityStats(ctx context.Context) (PriorityStats, error) {
	pipe := q.client.Pipeline()
	highLen := pipe.LLen(ctx, priorityHighKey)
	normalLen := pipe.LLen(ctx, priorityNormalKey)
	lowLen := pipe.LLen(ctx, priorityLowKey)
	delayedLen := pipe.ZCard(ctx, delayedQueueKey)

	if _, err := pipe.Exec(ctx); err != nil {
		log.Error("failed to get priority queue stats", "error", err)
		return PriorityStats{}, err
	}

	return PriorityStats{
		High:    highLen.Val(),
		Normal:  normalLen.Val(),
		Low:     lowLen.Val(),
		Delayed: delayedLen.Val(),
	}, nil
}
