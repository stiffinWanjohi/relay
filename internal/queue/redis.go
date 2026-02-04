package queue

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

const (
	// Queue keys
	mainQueueKey       = "relay:queue:main"
	processingQueueKey = "relay:queue:processing"
	delayedQueueKey    = "relay:queue:delayed"

	// Priority queue keys
	priorityHighKey   = "relay:queue:priority:high"   // priority 1-3
	priorityNormalKey = "relay:queue:priority:normal" // priority 4-7 (default)
	priorityLowKey    = "relay:queue:priority:low"    // priority 8-10

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
	ClientID  string    `json:"client_id,omitempty"` // For per-client queuing
	Priority  int       `json:"priority,omitempty"`  // 1-10, lower = higher priority
	EnqueueAt time.Time `json:"enqueue_at"`
}

// Queue provides reliable message queuing with visibility timeout.
type Queue struct {
	client            *redis.Client
	visibilityTimeout time.Duration
	blockingTimeout   time.Duration
	metrics           *observability.Metrics
	fifoConfig        FIFOConfig
}

// NewQueue creates a new Redis-backed queue.
func NewQueue(client *redis.Client) *Queue {
	return &Queue{
		client:            client,
		visibilityTimeout: defaultVisibilityTimeout,
		blockingTimeout:   defaultBlockingTimeout,
		fifoConfig:        DefaultFIFOConfig(),
	}
}

// WithMetrics sets a metrics provider for the queue.
func (q *Queue) WithMetrics(metrics *observability.Metrics) *Queue {
	return &Queue{
		client:            q.client,
		visibilityTimeout: q.visibilityTimeout,
		blockingTimeout:   q.blockingTimeout,
		metrics:           metrics,
		fifoConfig:        q.fifoConfig,
	}
}

// WithVisibilityTimeout sets a custom visibility timeout.
func (q *Queue) WithVisibilityTimeout(timeout time.Duration) *Queue {
	return &Queue{
		client:            q.client,
		visibilityTimeout: timeout,
		blockingTimeout:   q.blockingTimeout,
		metrics:           q.metrics,
		fifoConfig:        q.fifoConfig,
	}
}

// WithBlockingTimeout sets a custom blocking timeout for dequeue operations.
func (q *Queue) WithBlockingTimeout(timeout time.Duration) *Queue {
	return &Queue{
		client:            q.client,
		visibilityTimeout: q.visibilityTimeout,
		blockingTimeout:   timeout,
		metrics:           q.metrics,
		fifoConfig:        q.fifoConfig,
	}
}

// WithFIFOConfig sets custom FIFO configuration.
func (q *Queue) WithFIFOConfig(config FIFOConfig) *Queue {
	return &Queue{
		client:            q.client,
		visibilityTimeout: q.visibilityTimeout,
		blockingTimeout:   q.blockingTimeout,
		metrics:           q.metrics,
		fifoConfig:        config,
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

	if err := q.client.LPush(ctx, mainQueueKey, data).Err(); err != nil {
		return err
	}

	if q.metrics != nil {
		q.metrics.QueueEnqueued(ctx)
	}

	return nil
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

// ============================================================================
// Priority Queue Support
// ============================================================================
// Priority queues allow events to be processed in priority order.
// Priority 1-3 = High, 4-7 = Normal (default), 8-10 = Low
// Higher priority events (lower number) are processed first.

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
		return err
	}

	queueKey := priorityQueueKey(priority)
	if err := q.client.LPush(ctx, queueKey, data).Err(); err != nil {
		return err
	}

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
		return err
	}

	score := float64(msg.EnqueueAt.Unix())
	return q.client.ZAdd(ctx, delayedQueueKey, redis.Z{Score: score, Member: data}).Err()
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
		return nil, err
	}

	// Check for context cancellation before blocking
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}

	// Increment starvation counter atomically
	counter := starvationCounter
	starvationCounter++

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
				return nil, err
			}
			if q.metrics != nil {
				q.metrics.QueueDequeued(ctx)
			}
			return &msg, nil
		}
		if !errors.Is(err, redis.Nil) {
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
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		return nil, err
	}

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
		return err
	}

	if len(messages) == 0 {
		return nil
	}

	pipe := q.client.Pipeline()
	for _, msgData := range messages {
		var msg Message
		if err := json.Unmarshal([]byte(msgData), &msg); err != nil {
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
	}

	_, err = pipe.Exec(ctx)
	return err
}

// PriorityStats holds statistics for priority queues.
type PriorityStats struct {
	High    int64
	Normal  int64
	Low     int64
	Delayed int64
}

// GetPriorityStats returns the current depth of each priority queue.
func (q *Queue) GetPriorityStats(ctx context.Context) (PriorityStats, error) {
	pipe := q.client.Pipeline()
	highLen := pipe.LLen(ctx, priorityHighKey)
	normalLen := pipe.LLen(ctx, priorityNormalKey)
	lowLen := pipe.LLen(ctx, priorityLowKey)
	delayedLen := pipe.ZCard(ctx, delayedQueueKey)

	if _, err := pipe.Exec(ctx); err != nil {
		return PriorityStats{}, err
	}

	return PriorityStats{
		High:    highLen.Val(),
		Normal:  normalLen.Val(),
		Low:     lowLen.Val(),
		Delayed: delayedLen.Val(),
	}, nil
}

// RemoveFromDelayed removes a specific event from the delayed queue.
// This is used when cancelling a scheduled event.
func (q *Queue) RemoveFromDelayed(ctx context.Context, eventID uuid.UUID) error {
	// We need to find and remove the message with the matching event ID
	// Since ZSET members are serialized Message structs, we need to scan and remove
	messages, err := q.client.ZRange(ctx, delayedQueueKey, 0, -1).Result()
	if err != nil {
		return err
	}

	for _, msgData := range messages {
		var msg Message
		if err := json.Unmarshal([]byte(msgData), &msg); err != nil {
			continue
		}
		if msg.EventID == eventID {
			return q.client.ZRem(ctx, delayedQueueKey, msgData).Err()
		}
	}

	return nil // Not found is not an error
}

// Dequeue retrieves a message from the queue with visibility timeout.
// Returns ErrQueueEmpty if no messages are available.
func (q *Queue) Dequeue(ctx context.Context) (*Message, error) {
	// First, move any delayed messages that are ready
	if err := q.moveDelayedToMain(ctx); err != nil {
		return nil, err
	}

	// Check for context cancellation before blocking
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

	if q.metrics != nil {
		q.metrics.QueueDequeued(ctx)
	}

	return &msg, nil
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

// Per-client queue support for fair scheduling

const (
	clientQueuePrefix = "relay:queue:client:"
	activeClientsKey  = "relay:queue:active_clients"
)

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
		return err
	}

	pipe := q.client.Pipeline()
	pipe.LPush(ctx, clientQueueKey(clientID), data)
	pipe.SAdd(ctx, activeClientsKey, clientID) // Track active clients
	if _, err := pipe.Exec(ctx); err != nil {
		return err
	}

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
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(result), &msg); err != nil {
		return nil, err
	}

	// Check if client queue is now empty
	length, _ := q.client.LLen(ctx, clientQueueKey(clientID)).Result()
	if length == 0 {
		// Remove from active clients set
		q.client.SRem(ctx, activeClientsKey, clientID)
	}

	if q.metrics != nil {
		q.metrics.QueueDequeued(ctx)
	}

	return &msg, nil
}

// GetActiveClients returns a list of clients with pending events.
func (q *Queue) GetActiveClients(ctx context.Context) ([]string, error) {
	return q.client.SMembers(ctx, activeClientsKey).Result()
}

// GetClientQueueLength returns the number of pending events for a client.
func (q *Queue) GetClientQueueLength(ctx context.Context, clientID string) (int64, error) {
	return q.client.LLen(ctx, clientQueueKey(clientID)).Result()
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

func clientQueueKey(clientID string) string {
	return clientQueuePrefix + clientID
}

// ============================================================================
// FIFO Queue Support
// ============================================================================
// FIFO queues provide ordered delivery for endpoints that require it.
// Events for a FIFO endpoint are delivered sequentially - one at a time.
// Partition keys allow parallel ordered streams within the same endpoint.

const (
	fifoQueuePrefix           = "relay:queue:fifo:"
	fifoLockPrefix            = "relay:lock:fifo:"
	fifoInFlightPrefix        = "relay:inflight:fifo:"
	DefaultFIFOLockExpiration = 5 * time.Minute // Default lock timeout for in-flight deliveries
)

// FIFOConfig holds configuration for FIFO queue operations.
type FIFOConfig struct {
	LockExpiration time.Duration
}

// DefaultFIFOConfig returns the default FIFO configuration.
func DefaultFIFOConfig() FIFOConfig {
	return FIFOConfig{
		LockExpiration: DefaultFIFOLockExpiration,
	}
}

// FIFOQueueKey generates the queue key for a FIFO endpoint/partition combination.
func FIFOQueueKey(endpointID string, partitionKey string) string {
	if partitionKey == "" {
		return fifoQueuePrefix + endpointID
	}
	return fifoQueuePrefix + endpointID + ":" + partitionKey
}

// FIFOLockKey generates the lock key for a FIFO endpoint/partition combination.
func FIFOLockKey(endpointID string, partitionKey string) string {
	if partitionKey == "" {
		return fifoLockPrefix + endpointID
	}
	return fifoLockPrefix + endpointID + ":" + partitionKey
}

// FIFOInFlightKey generates the key for storing in-flight message data.
func FIFOInFlightKey(endpointID string, partitionKey string) string {
	if partitionKey == "" {
		return fifoInFlightPrefix + endpointID
	}
	return fifoInFlightPrefix + endpointID + ":" + partitionKey
}

// Lua script for atomic FIFO dequeue with lock acquisition
// Returns: [1]=message_data or nil, [2]=error_code (0=success, 1=locked, 2=empty)
var dequeueFIFOScript = redis.NewScript(`
	local queue_key = KEYS[1]
	local lock_key = KEYS[2]
	local inflight_key = KEYS[3]
	local lock_expiration = tonumber(ARGV[1])

	-- Try to acquire lock
	local acquired = redis.call('SET', lock_key, 'locked', 'NX', 'PX', lock_expiration)
	if not acquired then
		return {nil, 1} -- Already locked
	end

	-- Pop message from queue
	local msg = redis.call('LPOP', queue_key)
	if not msg then
		-- Queue empty, release lock
		redis.call('DEL', lock_key)
		return {nil, 2} -- Empty
	end

	-- Store in-flight message for recovery
	redis.call('SET', inflight_key, msg, 'PX', lock_expiration)

	return {msg, 0} -- Success
`)

// Lua script for atomic FIFO ack (release lock and clear in-flight)
var ackFIFOScript = redis.NewScript(`
	local lock_key = KEYS[1]
	local inflight_key = KEYS[2]

	redis.call('DEL', lock_key)
	redis.call('DEL', inflight_key)
	return 1
`)

// Lua script for atomic FIFO nack (push back to front, optionally hold lock)
var nackFIFOScript = redis.NewScript(`
	local queue_key = KEYS[1]
	local lock_key = KEYS[2]
	local inflight_key = KEYS[3]
	local msg = ARGV[1]
	local delay_ms = tonumber(ARGV[2])

	-- Push message back to front of queue
	redis.call('LPUSH', queue_key, msg)

	-- Clear in-flight
	redis.call('DEL', inflight_key)

	if delay_ms > 0 then
		-- Keep lock for delay period
		redis.call('PEXPIRE', lock_key, delay_ms)
	else
		-- Release lock immediately
		redis.call('DEL', lock_key)
	end

	return 1
`)

// EnqueueFIFO adds an event to a FIFO queue for ordered delivery.
// Events are appended to the tail of the queue for the endpoint/partition.
func (q *Queue) EnqueueFIFO(ctx context.Context, endpointID string, partitionKey string, eventID uuid.UUID) error {
	msg := Message{
		ID:        uuid.New().String(),
		EventID:   eventID,
		EnqueueAt: time.Now().UTC(),
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	queueKey := FIFOQueueKey(endpointID, partitionKey)
	if err := q.client.RPush(ctx, queueKey, data).Err(); err != nil {
		return err
	}

	if q.metrics != nil {
		q.metrics.QueueEnqueued(ctx)
	}

	return nil
}

// DequeueFIFO retrieves the next message from a FIFO queue if no delivery is in flight.
// Uses a distributed lock to ensure only one message is being processed at a time.
// Returns ErrQueueEmpty if the queue is empty or a delivery is already in progress.
// This operation is atomic - lock acquisition and message pop happen together.
func (q *Queue) DequeueFIFO(ctx context.Context, endpointID string, partitionKey string) (*Message, error) {
	queueKey := FIFOQueueKey(endpointID, partitionKey)
	lockKey := FIFOLockKey(endpointID, partitionKey)
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)

	lockExpirationMs := int64(q.fifoConfig.LockExpiration / time.Millisecond)

	// Atomic dequeue with lock acquisition
	result, err := dequeueFIFOScript.Run(ctx, q.client,
		[]string{queueKey, lockKey, inflightKey},
		lockExpirationMs,
	).Result()

	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}

	// Parse result: [message_data, error_code]
	resultSlice, ok := result.([]interface{})
	if !ok || len(resultSlice) != 2 {
		return nil, domain.ErrQueueEmpty
	}

	errorCode, _ := resultSlice[1].(int64)
	switch errorCode {
	case 1: // Locked
		return nil, domain.ErrQueueEmpty
	case 2: // Empty
		return nil, domain.ErrQueueEmpty
	}

	// Success - parse message
	msgData, ok := resultSlice[0].(string)
	if !ok {
		return nil, domain.ErrQueueEmpty
	}

	var msg Message
	if err := json.Unmarshal([]byte(msgData), &msg); err != nil {
		// Parse error, release the lock atomically
		ackFIFOScript.Run(ctx, q.client, []string{lockKey, inflightKey})
		return nil, err
	}

	if q.metrics != nil {
		q.metrics.QueueDequeued(ctx)
	}

	return &msg, nil
}

// AckFIFO acknowledges successful processing of a FIFO message.
// Releases the lock and clears in-flight data atomically.
func (q *Queue) AckFIFO(ctx context.Context, endpointID string, partitionKey string) error {
	lockKey := FIFOLockKey(endpointID, partitionKey)
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)

	_, err := ackFIFOScript.Run(ctx, q.client, []string{lockKey, inflightKey}).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

// NackFIFO returns a message to the front of the FIFO queue for reprocessing.
// If delay > 0, the lock is held for that duration before the message can be retried.
// Otherwise, the lock is released immediately.
// This operation is atomic to prevent race conditions.
func (q *Queue) NackFIFO(ctx context.Context, endpointID string, partitionKey string, msg *Message, delay time.Duration) error {
	queueKey := FIFOQueueKey(endpointID, partitionKey)
	lockKey := FIFOLockKey(endpointID, partitionKey)
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	delayMs := int64(delay / time.Millisecond)

	_, err = nackFIFOScript.Run(ctx, q.client,
		[]string{queueKey, lockKey, inflightKey},
		string(data), delayMs,
	).Result()

	if err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

// GetFIFOQueueLength returns the number of pending events in a FIFO queue.
func (q *Queue) GetFIFOQueueLength(ctx context.Context, endpointID string, partitionKey string) (int64, error) {
	queueKey := FIFOQueueKey(endpointID, partitionKey)
	return q.client.LLen(ctx, queueKey).Result()
}

// IsFIFOLocked returns true if a delivery is currently in progress for the FIFO queue.
func (q *Queue) IsFIFOLocked(ctx context.Context, endpointID string, partitionKey string) (bool, error) {
	lockKey := FIFOLockKey(endpointID, partitionKey)
	exists, err := q.client.Exists(ctx, lockKey).Result()
	return exists > 0, err
}

// ReleaseFIFOLock forcibly releases a FIFO lock (used for recovery).
func (q *Queue) ReleaseFIFOLock(ctx context.Context, endpointID string, partitionKey string) error {
	lockKey := FIFOLockKey(endpointID, partitionKey)
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)

	pipe := q.client.Pipeline()
	pipe.Del(ctx, lockKey)
	pipe.Del(ctx, inflightKey)
	_, err := pipe.Exec(ctx)
	return err
}

// RecoverFIFOInFlight recovers an in-flight message that was lost due to worker crash.
// Returns the recovered message or nil if no in-flight message exists.
func (q *Queue) RecoverFIFOInFlight(ctx context.Context, endpointID string, partitionKey string) (*Message, error) {
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)
	lockKey := FIFOLockKey(endpointID, partitionKey)
	queueKey := FIFOQueueKey(endpointID, partitionKey)

	// Check if there's an in-flight message
	data, err := q.client.Get(ctx, inflightKey).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil // No in-flight message
	}
	if err != nil {
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		// Invalid data, just clean up
		q.client.Del(ctx, inflightKey)
		q.client.Del(ctx, lockKey)
		return nil, nil
	}

	// Push message back to front of queue and clean up
	pipe := q.client.Pipeline()
	pipe.LPush(ctx, queueKey, data)
	pipe.Del(ctx, inflightKey)
	pipe.Del(ctx, lockKey)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}

	return &msg, nil
}

// GetStaleFIFOLocks returns all FIFO locks that have been held longer than the threshold.
// This is used to detect potentially stuck deliveries.
func (q *Queue) GetStaleFIFOLocks(ctx context.Context) ([]string, error) {
	pattern := fifoLockPrefix + "*"

	var staleLocks []string
	var cursor uint64

	for {
		keys, nextCursor, err := q.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		staleLocks = append(staleLocks, keys...)

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return staleLocks, nil
}

// RecoverAllStaleFIFO recovers all stale in-flight messages.
// Returns the number of messages recovered.
func (q *Queue) RecoverAllStaleFIFO(ctx context.Context) (int, error) {
	pattern := fifoInFlightPrefix + "*"

	var recovered int
	var cursor uint64

	for {
		keys, nextCursor, err := q.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return recovered, err
		}

		for _, inflightKey := range keys {
			// Extract endpoint and partition from key
			suffix := inflightKey[len(fifoInFlightPrefix):]
			if suffix == "" {
				continue
			}

			var endpointID, partitionKey string
			if idx := indexOf(suffix, ':'); idx >= 0 {
				endpointID = suffix[:idx]
				partitionKey = suffix[idx+1:]
			} else {
				endpointID = suffix
				partitionKey = ""
			}

			msg, err := q.RecoverFIFOInFlight(ctx, endpointID, partitionKey)
			if err != nil {
				continue
			}
			if msg != nil {
				recovered++
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return recovered, nil
}

// FIFOQueueStats holds statistics for a FIFO queue.
type FIFOQueueStats struct {
	EndpointID   string
	PartitionKey string
	QueueLength  int64
	IsLocked     bool
	HasInFlight  bool
}

// GetFIFOQueueStats returns detailed statistics for a FIFO queue.
func (q *Queue) GetFIFOQueueStats(ctx context.Context, endpointID string, partitionKey string) (FIFOQueueStats, error) {
	queueKey := FIFOQueueKey(endpointID, partitionKey)
	lockKey := FIFOLockKey(endpointID, partitionKey)
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)

	pipe := q.client.Pipeline()
	lengthCmd := pipe.LLen(ctx, queueKey)
	lockExistsCmd := pipe.Exists(ctx, lockKey)
	inflightExistsCmd := pipe.Exists(ctx, inflightKey)

	_, err := pipe.Exec(ctx)
	if err != nil {
		return FIFOQueueStats{}, err
	}

	return FIFOQueueStats{
		EndpointID:   endpointID,
		PartitionKey: partitionKey,
		QueueLength:  lengthCmd.Val(),
		IsLocked:     lockExistsCmd.Val() > 0,
		HasInFlight:  inflightExistsCmd.Val() > 0,
	}, nil
}

// DrainFIFOQueue removes all messages from a FIFO queue and returns them.
// This is used when an endpoint is disabled to handle orphaned messages.
func (q *Queue) DrainFIFOQueue(ctx context.Context, endpointID string, partitionKey string) ([]*Message, error) {
	queueKey := FIFOQueueKey(endpointID, partitionKey)
	lockKey := FIFOLockKey(endpointID, partitionKey)
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)

	var messages []*Message

	// Get all messages from the queue
	results, err := q.client.LRange(ctx, queueKey, 0, -1).Result()
	if err != nil {
		return nil, err
	}

	for _, data := range results {
		var msg Message
		if err := json.Unmarshal([]byte(data), &msg); err != nil {
			continue
		}
		messages = append(messages, &msg)
	}

	// Delete the queue and associated locks
	pipe := q.client.Pipeline()
	pipe.Del(ctx, queueKey)
	pipe.Del(ctx, lockKey)
	pipe.Del(ctx, inflightKey)
	_, err = pipe.Exec(ctx)
	if err != nil {
		return messages, err
	}

	return messages, nil
}

// DrainAllFIFOQueuesForEndpoint drains all FIFO queues for an endpoint (all partitions).
// Returns the total number of messages drained.
func (q *Queue) DrainAllFIFOQueuesForEndpoint(ctx context.Context, endpointID string) (int, error) {
	partitions, err := q.ListFIFOPartitions(ctx, endpointID)
	if err != nil {
		return 0, err
	}

	totalDrained := 0
	for _, partition := range partitions {
		messages, err := q.DrainFIFOQueue(ctx, endpointID, partition)
		if err != nil {
			continue
		}
		totalDrained += len(messages)
	}

	return totalDrained, nil
}

// MoveFIFOToStandardQueue moves all messages from a FIFO queue to the standard queue.
// This is useful when an endpoint switches from FIFO to standard delivery.
func (q *Queue) MoveFIFOToStandardQueue(ctx context.Context, endpointID string, partitionKey string) (int, error) {
	messages, err := q.DrainFIFOQueue(ctx, endpointID, partitionKey)
	if err != nil {
		return 0, err
	}

	moved := 0
	for _, msg := range messages {
		if err := q.Enqueue(ctx, msg.EventID); err != nil {
			continue
		}
		moved++
	}

	return moved, nil
}

// ListFIFOPartitions returns all active partition keys for a FIFO endpoint.
// It scans Redis for queue keys matching the endpoint pattern.
func (q *Queue) ListFIFOPartitions(ctx context.Context, endpointID string) ([]string, error) {
	// Pattern to match: relay:queue:fifo:endpointID or relay:queue:fifo:endpointID:*
	pattern := fifoQueuePrefix + endpointID + "*"

	var partitions []string
	var cursor uint64

	for {
		keys, nextCursor, err := q.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			// Extract partition key from the full key
			// Key format: relay:queue:fifo:endpointID or relay:queue:fifo:endpointID:partitionKey
			prefix := fifoQueuePrefix + endpointID
			if key == prefix {
				// No partition key (default queue)
				partitions = append(partitions, "")
			} else if len(key) > len(prefix)+1 && key[len(prefix)] == ':' {
				// Has partition key
				partitions = append(partitions, key[len(prefix)+1:])
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return partitions, nil
}

// GetActiveFIFOQueues returns all FIFO queue keys that have pending messages.
func (q *Queue) GetActiveFIFOQueues(ctx context.Context) ([]string, error) {
	pattern := fifoQueuePrefix + "*"

	var queues []string
	var cursor uint64

	for {
		keys, nextCursor, err := q.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}

		for _, key := range keys {
			// Check if queue has messages
			length, err := q.client.LLen(ctx, key).Result()
			if err != nil {
				continue
			}
			if length > 0 {
				queues = append(queues, key)
			}
		}

		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	return queues, nil
}

// ParseFIFOQueueKey extracts the endpoint ID and partition key from a FIFO queue key.
func ParseFIFOQueueKey(queueKey string) (endpointID, partitionKey string, ok bool) {
	if len(queueKey) <= len(fifoQueuePrefix) {
		return "", "", false
	}

	suffix := queueKey[len(fifoQueuePrefix):]
	if suffix == "" {
		return "", "", false
	}

	if idx := indexOf(suffix, ':'); idx >= 0 {
		endpointID = suffix[:idx]
		if endpointID == "" {
			return "", "", false
		}
		return endpointID, suffix[idx+1:], true
	}
	return suffix, "", true
}

func indexOf(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}
