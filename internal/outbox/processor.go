package outbox

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

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
	logger   *slog.Logger
	workerID string
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

// NewProcessor creates a new outbox processor.
func NewProcessor(store *event.Store, q *queue.Queue, config ProcessorConfig, logger *slog.Logger) *Processor {
	return &Processor{
		store:    store,
		queue:    q,
		config:   config,
		logger:   logger,
		workerID: uuid.New().String(),
		stopCh:   make(chan struct{}),
	}
}

// Start begins processing outbox entries.
func (p *Processor) Start(ctx context.Context) {
	p.logger.Info("starting outbox processor",
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
	p.logger.Info("stopping outbox processor")
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
				p.logger.Error("failed to process outbox batch", "error", err)
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

	p.logger.Debug("processing outbox entries", "count", len(entries), "worker_id", p.workerID)

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
