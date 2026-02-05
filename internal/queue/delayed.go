package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// EnqueueDelayed adds an event to the queue for delayed processing.
func (q *Queue) EnqueueDelayed(ctx context.Context, eventID uuid.UUID, delay time.Duration) error {
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		EnqueueAt: time.Now().UTC().Add(delay),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		log.Error("failed to marshal delayed message", "event_id", eventID, "error", err)
		return err
	}

	score := float64(msg.EnqueueAt.Unix())
	if err := q.client.ZAdd(ctx, delayedQueueKey, redis.Z{Score: score, Member: data}).Err(); err != nil {
		log.Error("failed to enqueue delayed message", "event_id", eventID, "delay", delay, "error", err)
		return err
	}

	log.Debug("message enqueued with delay",
		"event_id", eventID,
		"message_id", msg.ID,
		"delay", delay,
		"scheduled_at", msg.EnqueueAt,
	)

	return nil
}

// Lua script for atomic move from delayed to main queue
// This prevents race conditions where multiple workers might move the same message
var moveDelayedScript = redis.NewScript(`
	local delayed_key = KEYS[1]
	local main_key = KEYS[2]
	local now = ARGV[1]
	local limit = tonumber(ARGV[2])

	local messages = redis.call('ZRANGEBYSCORE', delayed_key, '-inf', now, 'LIMIT', 0, limit)

	if #messages == 0 then
		return 0
	end

	for i, msg in ipairs(messages) do
		redis.call('RPUSH', main_key, msg)
		redis.call('ZREM', delayed_key, msg)
	end

	return #messages
`)

// moveDelayedToMain moves delayed messages that are ready to the main queue.
// Uses a Lua script to ensure atomicity and prevent race conditions.
func (q *Queue) moveDelayedToMain(ctx context.Context) error {
	now := formatFloat(float64(time.Now().UTC().Unix()))

	result, err := moveDelayedScript.Run(ctx, q.client,
		[]string{delayedQueueKey, mainQueueKey},
		now,
		delayedBatchLimit,
	).Result()

	// Ignore NOSCRIPT error on first run (script will be loaded automatically)
	if err != nil && !errors.Is(err, redis.Nil) {
		log.Error("failed to move delayed messages to main queue", "error", err)
		return err
	}

	if moved, ok := result.(int64); ok && moved > 0 {
		log.Debug("moved delayed messages to main queue", "count", moved)
	}

	return nil
}

// RemoveFromDelayed removes a specific event from the delayed queue.
// This is used when cancelling a scheduled event.
func (q *Queue) RemoveFromDelayed(ctx context.Context, eventID uuid.UUID) error {
	// We need to find and remove the message with the matching event ID
	// Since ZSET members are serialized Message structs, we need to scan and remove
	messages, err := q.client.ZRange(ctx, delayedQueueKey, 0, -1).Result()
	if err != nil {
		log.Error("failed to scan delayed queue", "error", err)
		return err
	}

	for _, msgData := range messages {
		var msg Message
		if err := json.Unmarshal([]byte(msgData), &msg); err != nil {
			continue
		}
		if msg.EventID == eventID {
			if err := q.client.ZRem(ctx, delayedQueueKey, msgData).Err(); err != nil {
				log.Error("failed to remove from delayed queue", "event_id", eventID, "error", err)
				return err
			}
			log.Debug("removed event from delayed queue", "event_id", eventID)
			return nil
		}
	}

	log.Debug("event not found in delayed queue", "event_id", eventID)
	return nil // Not found is not an error
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 0, 64)
}
