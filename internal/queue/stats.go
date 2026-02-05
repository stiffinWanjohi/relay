package queue

import (
	"context"
	"encoding/json"
	"time"
)

// Stats returns queue statistics.
func (q *Queue) Stats(ctx context.Context) (Stats, error) {
	pipe := q.client.Pipeline()
	mainLen := pipe.LLen(ctx, mainQueueKey)
	processingLen := pipe.LLen(ctx, processingQueueKey)
	delayedLen := pipe.ZCard(ctx, delayedQueueKey)

	if _, err := pipe.Exec(ctx); err != nil {
		log.Error("failed to get queue stats", "error", err)
		return Stats{}, err
	}

	return Stats{
		Pending:    mainLen.Val(),
		Processing: processingLen.Val(),
		Delayed:    delayedLen.Val(),
	}, nil
}

// RecoverStaleMessages moves messages that have been processing too long back to the main queue.
// This should be called periodically to handle worker crashes.
func (q *Queue) RecoverStaleMessages(ctx context.Context, maxProcessingTime time.Duration) (int, error) {
	// Get all messages in the processing queue
	messages, err := q.client.LRange(ctx, processingQueueKey, 0, -1).Result()
	if err != nil {
		log.Error("failed to get processing queue messages", "error", err)
		return 0, err
	}

	recovered := 0
	cutoff := time.Now().UTC().Add(-maxProcessingTime)

	for _, msgData := range messages {
		var msg Message
		if err := json.Unmarshal([]byte(msgData), &msg); err != nil {
			log.Warn("failed to unmarshal stale message, skipping", "error", err)
			continue
		}

		// If the message has been processing too long, move it back
		if msg.EnqueueAt.Before(cutoff) {
			pipe := q.client.Pipeline()
			pipe.LRem(ctx, processingQueueKey, 1, msgData)
			pipe.RPush(ctx, mainQueueKey, msgData)
			if _, err := pipe.Exec(ctx); err == nil {
				recovered++
				log.Debug("recovered stale message",
					"event_id", msg.EventID,
					"message_id", msg.ID,
					"age", time.Since(msg.EnqueueAt),
				)
			} else {
				log.Warn("failed to recover stale message", "message_id", msg.ID, "error", err)
			}
		}
	}

	if recovered > 0 {
		log.Info("recovered stale messages", "count", recovered, "max_processing_time", maxProcessingTime)
	}

	return recovered, nil
}
