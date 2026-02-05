// Package queue provides Redis-backed message queuing with multiple delivery modes.
package queue

import (
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

var log = logging.Component("queue")

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

// Stats holds queue statistics.
type Stats struct {
	Pending    int64
	Processing int64
	Delayed    int64
}
