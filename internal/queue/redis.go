package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/relay/internal/domain"
)

const (
	// Queue keys
	mainQueueKey       = "relay:queue:main"
	processingQueueKey = "relay:queue:processing"
	delayedQueueKey    = "relay:queue:delayed"

	// Default visibility timeout (how long a message stays invisible to other consumers)
	defaultVisibilityTimeout = 30 * time.Second

	// Default blocking timeout (how long to wait for a message before returning)
	defaultBlockingTimeout = 1 * time.Second

	// Batch limit for moving delayed messages
	delayedBatchLimit = 100
)

// Message represents a queue message.
type Message struct {
	ID        string    `json:"id"`
	EventID   uuid.UUID `json:"event_id"`
	EnqueueAt time.Time `json:"enqueue_at"`
}

// Queue provides reliable message queuing with visibility timeout.
type Queue struct {
	client            *redis.Client
	visibilityTimeout time.Duration
	blockingTimeout   time.Duration
}

// NewQueue creates a new Redis-backed queue.
func NewQueue(client *redis.Client) *Queue {
	return &Queue{
		client:            client,
		visibilityTimeout: defaultVisibilityTimeout,
		blockingTimeout:   defaultBlockingTimeout,
	}
}

// WithVisibilityTimeout sets a custom visibility timeout.
func (q *Queue) WithVisibilityTimeout(timeout time.Duration) *Queue {
	return &Queue{
		client:            q.client,
		visibilityTimeout: timeout,
		blockingTimeout:   q.blockingTimeout,
	}
}

// WithBlockingTimeout sets a custom blocking timeout for dequeue operations.
func (q *Queue) WithBlockingTimeout(timeout time.Duration) *Queue {
	return &Queue{
		client:            q.client,
		visibilityTimeout: q.visibilityTimeout,
		blockingTimeout:   timeout,
	}
}

// Enqueue adds an event to the queue for immediate processing.
func (q *Queue) Enqueue(ctx context.Context, eventID uuid.UUID) error {
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		EnqueueAt: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return q.client.LPush(ctx, mainQueueKey, data).Err()
}

// EnqueueDelayed adds an event to the queue for delayed processing.
func (q *Queue) EnqueueDelayed(ctx context.Context, eventID uuid.UUID, delay time.Duration) error {
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		EnqueueAt: time.Now().UTC().Add(delay),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	score := float64(msg.EnqueueAt.Unix())
	return q.client.ZAdd(ctx, delayedQueueKey, redis.Z{Score: score, Member: data}).Err()
}

// Dequeue retrieves a message from the queue with visibility timeout.
// Returns ErrQueueEmpty if no messages are available.
func (q *Queue) Dequeue(ctx context.Context) (*Message, error) {
	// First, move any delayed messages that are ready
	if err := q.moveDelayedToMain(ctx); err != nil {
		return nil, err
	}

	// Use short blocking timeout to allow context cancellation checks
	// Loop until we get a message or context is cancelled
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Use BRPOPLPUSH for atomic dequeue
		// This moves the message to the processing queue
		result, err := q.client.BRPopLPush(ctx, mainQueueKey, processingQueueKey, q.blockingTimeout).Result()
		if errors.Is(err, redis.Nil) {
			// No message available, return empty error (caller handles backoff)
			return nil, domain.ErrQueueEmpty
		}
		if err != nil {
			// Check if it's a context cancellation
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}
			return nil, err
		}

		var msg Message
		if err := json.Unmarshal([]byte(result), &msg); err != nil {
			return nil, err
		}

		return &msg, nil
	}
}

// Ack acknowledges successful processing of a message.
func (q *Queue) Ack(ctx context.Context, msg *Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	removed, err := q.client.LRem(ctx, processingQueueKey, 1, data).Result()
	if err != nil {
		return err
	}
	if removed == 0 {
		return domain.ErrMessageNotFound
	}
	return nil
}

// Nack returns a message to the queue for reprocessing with optional delay.
func (q *Queue) Nack(ctx context.Context, msg *Message, delay time.Duration) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	// Remove from processing queue and verify it existed
	removed, err := q.client.LRem(ctx, processingQueueKey, 1, data).Result()
	if err != nil {
		return err
	}
	if removed == 0 {
		// Message was already recovered or acked by another process
		return domain.ErrMessageNotFound
	}

	// Re-enqueue with delay if specified
	if delay > 0 {
		return q.EnqueueDelayed(ctx, msg.EventID, delay)
	}

	// Otherwise, add back to main queue
	return q.client.RPush(ctx, mainQueueKey, data).Err()
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

	_, err := moveDelayedScript.Run(ctx, q.client,
		[]string{delayedQueueKey, mainQueueKey},
		now,
		delayedBatchLimit,
	).Result()

	// Ignore NOSCRIPT error on first run (script will be loaded automatically)
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

// RecoverStaleMessages moves messages that have been processing too long back to the main queue.
// This should be called periodically to handle worker crashes.
func (q *Queue) RecoverStaleMessages(ctx context.Context, maxProcessingTime time.Duration) (int, error) {
	// Get all messages in the processing queue
	messages, err := q.client.LRange(ctx, processingQueueKey, 0, -1).Result()
	if err != nil {
		return 0, err
	}

	recovered := 0
	cutoff := time.Now().UTC().Add(-maxProcessingTime)

	for _, msgData := range messages {
		var msg Message
		if err := json.Unmarshal([]byte(msgData), &msg); err != nil {
			continue
		}

		// If the message has been processing too long, move it back
		if msg.EnqueueAt.Before(cutoff) {
			pipe := q.client.Pipeline()
			pipe.LRem(ctx, processingQueueKey, 1, msgData)
			pipe.RPush(ctx, mainQueueKey, msgData)
			if _, err := pipe.Exec(ctx); err == nil {
				recovered++
			}
		}
	}

	return recovered, nil
}

// Stats returns queue statistics.
func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	pipe := q.client.Pipeline()
	mainLen := pipe.LLen(ctx, mainQueueKey)
	processingLen := pipe.LLen(ctx, processingQueueKey)
	delayedLen := pipe.ZCard(ctx, delayedQueueKey)

	if _, err := pipe.Exec(ctx); err != nil {
		return Stats{}, err
	}

	return Stats{
		Pending:    mainLen.Val(),
		Processing: processingLen.Val(),
		Delayed:    delayedLen.Val(),
	}, nil
}

// Stats holds queue statistics.
type Stats struct {
	Pending    int64
	Processing int64
	Delayed    int64
}

func formatFloat(f float64) string {
	return strconv.FormatFloat(f, 'f', 0, 64)
}
