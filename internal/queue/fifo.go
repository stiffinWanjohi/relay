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

// FIFOQueueStats holds statistics for a FIFO queue.
type FIFOQueueStats struct {
	EndpointID   string
	PartitionKey string
	QueueLength  int64
	IsLocked     bool
	HasInFlight  bool
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
		log.Error("failed to marshal FIFO message",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"event_id", eventID,
			"error", err,
		)
		return err
	}

	queueKey := FIFOQueueKey(endpointID, partitionKey)
	if err := q.client.RPush(ctx, queueKey, data).Err(); err != nil {
		log.Error("failed to enqueue FIFO message",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"event_id", eventID,
			"error", err,
		)
		return err
	}

	log.Debug("message enqueued to FIFO queue",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
		"event_id", eventID,
		"message_id", msg.ID,
	)

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
		log.Error("failed to dequeue from FIFO queue",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return nil, err
	}

	// Parse result: [message_data, error_code]
	resultSlice, ok := result.([]any)
	if !ok || len(resultSlice) != 2 {
		return nil, domain.ErrQueueEmpty
	}

	errorCode, _ := resultSlice[1].(int64)
	switch errorCode {
	case 1: // Locked
		log.Debug("FIFO queue locked, skipping",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
		)
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
		log.Error("failed to unmarshal FIFO message, releasing lock",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		// Parse error, release the lock atomically
		ackFIFOScript.Run(ctx, q.client, []string{lockKey, inflightKey})
		return nil, err
	}

	log.Debug("message dequeued from FIFO queue",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
		"event_id", msg.EventID,
		"message_id", msg.ID,
	)

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
		log.Error("failed to ack FIFO message",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return err
	}

	log.Debug("FIFO message acked",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
	)

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
		log.Error("failed to marshal FIFO message for nack",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return err
	}

	delayMs := int64(delay / time.Millisecond)

	_, err = nackFIFOScript.Run(ctx, q.client,
		[]string{queueKey, lockKey, inflightKey},
		string(data), delayMs,
	).Result()

	if err != nil && !errors.Is(err, redis.Nil) {
		log.Error("failed to nack FIFO message",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return err
	}

	log.Debug("FIFO message nacked",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
		"event_id", msg.EventID,
		"delay", delay,
	)

	return nil
}

// GetFIFOQueueLength returns the number of pending events in a FIFO queue.
func (q *Queue) GetFIFOQueueLength(ctx context.Context, endpointID string, partitionKey string) (int64, error) {
	queueKey := FIFOQueueKey(endpointID, partitionKey)
	length, err := q.client.LLen(ctx, queueKey).Result()
	if err != nil {
		log.Error("failed to get FIFO queue length",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return 0, err
	}
	return length, nil
}

// IsFIFOLocked returns true if a delivery is currently in progress for the FIFO queue.
func (q *Queue) IsFIFOLocked(ctx context.Context, endpointID string, partitionKey string) (bool, error) {
	lockKey := FIFOLockKey(endpointID, partitionKey)
	exists, err := q.client.Exists(ctx, lockKey).Result()
	if err != nil {
		log.Error("failed to check FIFO lock",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return false, err
	}
	return exists > 0, nil
}

// ReleaseFIFOLock forcibly releases a FIFO lock (used for recovery).
func (q *Queue) ReleaseFIFOLock(ctx context.Context, endpointID string, partitionKey string) error {
	lockKey := FIFOLockKey(endpointID, partitionKey)
	inflightKey := FIFOInFlightKey(endpointID, partitionKey)

	pipe := q.client.Pipeline()
	pipe.Del(ctx, lockKey)
	pipe.Del(ctx, inflightKey)
	_, err := pipe.Exec(ctx)

	if err != nil {
		log.Error("failed to release FIFO lock",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return err
	}

	log.Info("FIFO lock released",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
	)

	return nil
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
		log.Error("failed to get in-flight message",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return nil, err
	}

	var msg Message
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		log.Warn("failed to unmarshal in-flight message, cleaning up",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
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
		log.Error("failed to recover in-flight message",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return nil, err
	}

	log.Info("recovered in-flight FIFO message",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
		"event_id", msg.EventID,
	)

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
			log.Error("failed to scan stale FIFO locks", "error", err)
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
			log.Error("failed to scan stale FIFO in-flight messages", "error", err)
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

	if recovered > 0 {
		log.Info("recovered stale FIFO messages", "count", recovered)
	}

	return recovered, nil
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
		log.Error("failed to get FIFO queue stats",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
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
		log.Error("failed to get FIFO queue messages for drain",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
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
		log.Error("failed to delete FIFO queue keys",
			"endpoint_id", endpointID,
			"partition_key", partitionKey,
			"error", err,
		)
		return messages, err
	}

	log.Info("drained FIFO queue",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
		"message_count", len(messages),
	)

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
			log.Warn("failed to drain FIFO partition",
				"endpoint_id", endpointID,
				"partition_key", partition,
				"error", err,
			)
			continue
		}
		totalDrained += len(messages)
	}

	log.Info("drained all FIFO queues for endpoint",
		"endpoint_id", endpointID,
		"total_drained", totalDrained,
	)

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
			log.Warn("failed to move FIFO message to standard queue",
				"event_id", msg.EventID,
				"error", err,
			)
			continue
		}
		moved++
	}

	log.Info("moved FIFO messages to standard queue",
		"endpoint_id", endpointID,
		"partition_key", partitionKey,
		"moved", moved,
	)

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
			log.Error("failed to scan FIFO partitions",
				"endpoint_id", endpointID,
				"error", err,
			)
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
			log.Error("failed to scan active FIFO queues", "error", err)
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
