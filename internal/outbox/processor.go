package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/observability"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

var log = logging.Component("outbox")

const (
	// DefaultBatchSize is the default number of outbox entries to process per batch.
	DefaultBatchSize = 100

	// DefaultPollInterval is the default interval between polling for new entries.
	DefaultPollInterval = 1 * time.Second

	// DefaultCleanupInterval is the default interval between cleanup runs.
	DefaultCleanupInterval = 1 * time.Hour

	// DefaultRetentionPeriod is the default retention period for processed entries.
	DefaultRetentionPeriod = 24 * time.Hour

	// DefaultClaimTimeout is how long a claim is valid before another worker can take it.
	DefaultClaimTimeout = 5 * time.Minute
)

// ProcessorConfig holds outbox processor configuration.
type ProcessorConfig struct {
	BatchSize       int
	PollInterval    time.Duration
	CleanupInterval time.Duration
	RetentionPeriod time.Duration
}

// DefaultProcessorConfig returns the default processor configuration.
func DefaultProcessorConfig() ProcessorConfig {
	return ProcessorConfig{
		BatchSize:       DefaultBatchSize,
		PollInterval:    DefaultPollInterval,
		CleanupInterval: DefaultCleanupInterval,
		RetentionPeriod: DefaultRetentionPeriod,
	}
}

// Processor processes outbox entries and publishes them to the queue.
type Processor struct {
	store    *event.Store
	queue    *queue.Queue
	config   ProcessorConfig
	metrics  *observability.Metrics
	workerID string
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewProcessor creates a new outbox processor.
func NewProcessor(store *event.Store, q *queue.Queue, config ProcessorConfig) *Processor {
	return &Processor{
		store:    store,
		queue:    q,
		config:   config,
		workerID: uuid.New().String(),
		stopCh:   make(chan struct{}),
	}
}

// WithMetrics sets a metrics provider for the processor.
func (p *Processor) WithMetrics(metrics *observability.Metrics) *Processor {
	p.metrics = metrics
	return p
}

// Start begins processing outbox entries.
func (p *Processor) Start(ctx context.Context) {
	log.Info("starting outbox processor",
		"batch_size", p.config.BatchSize,
		"poll_interval", p.config.PollInterval,
		"worker_id", p.workerID,
	)

	// Main processing loop
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.processLoop(ctx)
	}()

	// Cleanup loop
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.cleanupLoop(ctx)
	}()
}

// Stop signals the processor to stop.
func (p *Processor) Stop() {
	log.Info("stopping outbox processor")
	close(p.stopCh)
}

// Wait blocks until all processor goroutines have exited.
func (p *Processor) Wait() {
	p.wg.Wait()
}

// StopAndWait stops the processor and waits for all goroutines to exit.
func (p *Processor) StopAndWait(timeout time.Duration) error {
	p.Stop()

	done := make(chan struct{})
	go func() {
		p.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return context.DeadlineExceeded
	}
}

func (p *Processor) processLoop(ctx context.Context) {
	ticker := time.NewTicker(p.config.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			if err := p.processBatch(ctx); err != nil {
				log.Error("failed to process outbox batch", "error", err)
			}
		}
	}
}

func (p *Processor) processBatch(ctx context.Context) error {
	// Use atomic claim to prevent race conditions
	entries, err := p.store.ClaimAndGetOutbox(ctx, p.workerID, p.config.BatchSize, DefaultClaimTimeout)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	// Record pending count metric
	if p.metrics != nil {
		p.metrics.OutboxPending(ctx, int64(len(entries)))
	}

	log.Debug("processing outbox entries", "count", len(entries), "worker_id", p.workerID)

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.stopCh:
			return nil
		default:
		}

		start := time.Now()
		if err := p.processEntry(ctx, entry); err != nil {
			log.Error("failed to process outbox entry",
				"outbox_id", entry.ID,
				"event_id", entry.EventID,
				"error", err,
			)
			// Mark as failed but continue processing other entries
			if markErr := p.store.MarkOutboxFailed(ctx, entry.ID, err.Error()); markErr != nil {
				log.Error("failed to mark outbox entry as failed",
					"outbox_id", entry.ID,
					"error", markErr,
				)
			}
			// Record failure metric
			if p.metrics != nil {
				p.metrics.OutboxFailed(ctx)
			}
			continue
		}

		// Mark as processed
		if err := p.store.MarkOutboxProcessed(ctx, entry.ID); err != nil {
			log.Error("failed to mark outbox entry as processed",
				"outbox_id", entry.ID,
				"error", err,
			)
		}

		// Record success metric
		if p.metrics != nil {
			p.metrics.OutboxProcessed(ctx, time.Since(start))
		}
	}

	return nil
}

func (p *Processor) processEntry(ctx context.Context, entry event.OutboxEntry) error {
	// Get the event to check if it's for a FIFO endpoint and for priority/scheduling info
	evt, err := p.store.GetByID(ctx, entry.EventID)
	if err != nil {
		return err
	}

	// Check if this event is for a FIFO endpoint
	if evt.EndpointID != nil {
		endpoint, err := p.store.GetEndpointByID(ctx, *evt.EndpointID)
		if err == nil && endpoint.IsFIFO() {
			// Extract partition key if configured
			partitionKey := ""
			if endpoint.HasPartitionKey() {
				var extractErr error
				partitionKey, extractErr = extractPartitionKey(evt.Payload, endpoint.FIFOPartitionKey)
				if extractErr != nil {
					// Log the error but continue with empty partition key (default queue)
					log.Warn("failed to extract FIFO partition key, using default partition",
						"event_id", entry.EventID,
						"endpoint_id", endpoint.ID,
						"partition_key_path", endpoint.FIFOPartitionKey,
						"error", extractErr,
					)
					// Record metric for monitoring
					if p.metrics != nil {
						p.metrics.FIFOPartitionKeyExtractionFailed(ctx, endpoint.ID.String())
					}
				} else if partitionKey == "" && endpoint.FIFOPartitionKey != "" {
					// Path was valid but value not found in payload
					log.Warn("FIFO partition key path not found in payload, using default partition",
						"event_id", entry.EventID,
						"endpoint_id", endpoint.ID,
						"partition_key_path", endpoint.FIFOPartitionKey,
					)
					if p.metrics != nil {
						p.metrics.FIFOPartitionKeyExtractionFailed(ctx, endpoint.ID.String())
					}
				}
			}

			// Enqueue to FIFO queue (FIFO doesn't support priority/scheduled delivery)
			if err := p.queue.EnqueueFIFO(ctx, endpoint.ID.String(), partitionKey, entry.EventID); err != nil {
				return err
			}

			log.Debug("enqueued event to FIFO queue",
				"outbox_id", entry.ID,
				"event_id", entry.EventID,
				"endpoint_id", endpoint.ID,
				"partition_key", partitionKey,
			)
			return nil
		}
	}

	// Check if event is scheduled for future delivery
	if evt.IsScheduled() {
		delay := evt.ScheduledAt.Sub(time.Now())
		if delay > 0 {
			// Use delayed queue with priority info
			if err := p.queue.EnqueueDelayedWithPriority(ctx, entry.EventID, evt.Priority, delay); err != nil {
				return err
			}
			log.Debug("enqueued scheduled event to delayed queue",
				"outbox_id", entry.ID,
				"event_id", entry.EventID,
				"scheduled_at", evt.ScheduledAt,
				"delay", delay,
				"priority", evt.Priority,
			)
			return nil
		}
		// Scheduled time has passed, deliver immediately with priority
	}

	// Check if event has non-default priority
	if evt.Priority != 5 { // Default priority is 5
		if err := p.queue.EnqueueWithPriority(ctx, entry.EventID, evt.Priority); err != nil {
			return err
		}
		log.Debug("enqueued event to priority queue",
			"outbox_id", entry.ID,
			"event_id", entry.EventID,
			"priority", evt.Priority,
		)
		return nil
	}

	// Enqueue to standard queue
	if err := p.queue.Enqueue(ctx, entry.EventID); err != nil {
		return err
	}

	log.Debug("enqueued event from outbox",
		"outbox_id", entry.ID,
		"event_id", entry.EventID,
	)

	return nil
}

// extractPartitionKey extracts a partition key from the payload using a JSONPath expression.
func extractPartitionKey(payload []byte, path string) (string, error) {
	if len(payload) == 0 || path == "" {
		return "", nil
	}

	// Use the domain helper
	return extractJSONPathValue(payload, path)
}

// extractJSONPathValue is a local wrapper for domain.ExtractJSONPathValue
// This avoids circular imports
func extractJSONPathValue(payload []byte, path string) (string, error) {
	if len(payload) == 0 || path == "" {
		return "", nil
	}

	var data interface{}
	if err := json.Unmarshal(payload, &data); err != nil {
		return "", err
	}

	// Simple JSONPath extraction (supports $.field.subfield format)
	// For complex paths, the full jsonpath library is used in domain
	parts := parseJSONPath(path)
	if len(parts) == 0 {
		return "", nil
	}

	current := data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return "", nil
		}
		current, ok = m[part]
		if !ok {
			return "", nil
		}
	}

	// Convert to string
	switch v := current.(type) {
	case string:
		return v, nil
	case float64:
		if v == float64(int64(v)) {
			return fmt.Sprintf("%d", int64(v)), nil
		}
		return fmt.Sprintf("%v", v), nil
	case bool:
		return fmt.Sprintf("%v", v), nil
	default:
		return fmt.Sprintf("%v", v), nil
	}
}

// parseJSONPath parses a simple JSONPath like "$.field.subfield" into ["field", "subfield"]
func parseJSONPath(path string) []string {
	// Remove leading "$." or "$"
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")

	if path == "" {
		return nil
	}

	return strings.Split(path, ".")
}

func (p *Processor) cleanupLoop(ctx context.Context) {
	ticker := time.NewTicker(p.config.CleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			if err := p.cleanup(ctx); err != nil {
				log.Error("failed to cleanup outbox", "error", err)
			}
		}
	}
}

func (p *Processor) cleanup(ctx context.Context) error {
	deleted, err := p.store.CleanupProcessedOutbox(ctx, p.config.RetentionPeriod)
	if err != nil {
		return err
	}

	if deleted > 0 {
		log.Info("cleaned up processed outbox entries", "count", deleted)
	}

	return nil
}
