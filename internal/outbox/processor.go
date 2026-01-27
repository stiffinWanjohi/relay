package outbox

import (
	"context"
	"log/slog"
	"time"

	"github.com/relay/internal/event"
	"github.com/relay/internal/queue"
)

const (
	// DefaultBatchSize is the default number of outbox entries to process per batch.
	DefaultBatchSize = 100

	// DefaultPollInterval is the default interval between polling for new entries.
	DefaultPollInterval = 1 * time.Second

	// DefaultCleanupInterval is the default interval between cleanup runs.
	DefaultCleanupInterval = 1 * time.Hour

	// DefaultRetentionPeriod is the default retention period for processed entries.
	DefaultRetentionPeriod = 24 * time.Hour
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
	store  *event.Store
	queue  *queue.Queue
	config ProcessorConfig
	logger *slog.Logger
	stopCh chan struct{}
}

// NewProcessor creates a new outbox processor.
func NewProcessor(store *event.Store, q *queue.Queue, config ProcessorConfig, logger *slog.Logger) *Processor {
	return &Processor{
		store:  store,
		queue:  q,
		config: config,
		logger: logger,
		stopCh: make(chan struct{}),
	}
}

// Start begins processing outbox entries.
func (p *Processor) Start(ctx context.Context) {
	p.logger.Info("starting outbox processor",
		"batch_size", p.config.BatchSize,
		"poll_interval", p.config.PollInterval,
	)

	// Main processing loop
	go p.processLoop(ctx)

	// Cleanup loop
	go p.cleanupLoop(ctx)
}

// Stop signals the processor to stop.
func (p *Processor) Stop() {
	p.logger.Info("stopping outbox processor")
	close(p.stopCh)
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
				p.logger.Error("failed to process outbox batch", "error", err)
			}
		}
	}
}

func (p *Processor) processBatch(ctx context.Context) error {
	entries, err := p.store.GetUnprocessedOutbox(ctx, p.config.BatchSize)
	if err != nil {
		return err
	}

	if len(entries) == 0 {
		return nil
	}

	p.logger.Debug("processing outbox entries", "count", len(entries))

	for _, entry := range entries {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-p.stopCh:
			return nil
		default:
		}

		if err := p.processEntry(ctx, entry); err != nil {
			p.logger.Error("failed to process outbox entry",
				"outbox_id", entry.ID,
				"event_id", entry.EventID,
				"error", err,
			)
			// Mark as failed but continue processing other entries
			if markErr := p.store.MarkOutboxFailed(ctx, entry.ID, err.Error()); markErr != nil {
				p.logger.Error("failed to mark outbox entry as failed",
					"outbox_id", entry.ID,
					"error", markErr,
				)
			}
			continue
		}

		// Mark as processed
		if err := p.store.MarkOutboxProcessed(ctx, entry.ID); err != nil {
			p.logger.Error("failed to mark outbox entry as processed",
				"outbox_id", entry.ID,
				"error", err,
			)
		}
	}

	return nil
}

func (p *Processor) processEntry(ctx context.Context, entry event.OutboxEntry) error {
	// Enqueue the event to Redis
	if err := p.queue.Enqueue(ctx, entry.EventID); err != nil {
		return err
	}

	p.logger.Debug("enqueued event from outbox",
		"outbox_id", entry.ID,
		"event_id", entry.EventID,
	)

	return nil
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
				p.logger.Error("failed to cleanup outbox", "error", err)
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
		p.logger.Info("cleaned up processed outbox entries", "count", deleted)
	}

	return nil
}
