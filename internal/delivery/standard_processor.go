package delivery

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

var standardLog = logging.Component("delivery.standard")

const (
	// Backoff settings for empty queue
	minEmptyQueueBackoff = 50 * time.Millisecond
	maxEmptyQueueBackoff = 2 * time.Second
)

// StandardProcessor handles parallel delivery processing from priority queues.
type StandardProcessor struct {
	worker              *Worker
	concurrency         int
	visibilityTime      time.Duration
	enablePriorityQueue bool
	stopCh              chan struct{}
	wg                  sync.WaitGroup
}

// NewStandardProcessor creates a new StandardProcessor.
func NewStandardProcessor(w *Worker, config Config) *StandardProcessor {
	return &StandardProcessor{
		worker:              w,
		concurrency:         config.Concurrency,
		visibilityTime:      config.VisibilityTime,
		enablePriorityQueue: config.EnablePriorityQueue,
		stopCh:              make(chan struct{}),
	}
}

// Start begins the processing loop with multiple concurrent workers.
func (p *StandardProcessor) Start(ctx context.Context) {
	standardLog.Info("starting standard processor", "concurrency", p.concurrency)

	for i := range p.concurrency {
		p.wg.Add(1)
		go func(workerID int) {
			defer p.wg.Done()
			p.processLoop(ctx, workerID)
		}(i)
	}
}

// Stop signals the processor to stop and waits for all workers.
func (p *StandardProcessor) Stop() {
	standardLog.Info("stopping standard processor")
	close(p.stopCh)
	p.wg.Wait()
}

func (p *StandardProcessor) processLoop(ctx context.Context, workerID int) {
	logger := standardLog.With("worker_id", workerID)
	backoff := minEmptyQueueBackoff

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		default:
			err := p.processOne(ctx, logger)
			if err != nil {
				if errors.Is(err, domain.ErrQueueEmpty) {
					select {
					case <-ctx.Done():
						return
					case <-p.stopCh:
						return
					case <-time.After(backoff):
						backoff = min(backoff*2, maxEmptyQueueBackoff)
					}
					continue
				}
				logger.Error("error processing message", "error", err)
			}
			backoff = minEmptyQueueBackoff
		}
	}
}

func (p *StandardProcessor) processOne(ctx context.Context, logger *slog.Logger) error {
	// Dequeue a message
	var msg *queue.Message
	var err error
	if p.enablePriorityQueue {
		msg, err = p.worker.Queue().DequeueWithPriority(ctx)
	} else {
		msg, err = p.worker.Queue().Dequeue(ctx)
	}
	if err != nil {
		return err
	}

	logger = logger.With("event_id", msg.EventID, "message_id", msg.ID)
	logger.Debug("processing message")

	// Get the event
	evt, err := p.worker.GetEvent(ctx, msg.EventID)
	if err != nil {
		logger.Error("failed to get event", "error", err)
		return p.worker.Queue().Ack(ctx, msg)
	}

	// Load endpoint if available
	var endpoint *domain.Endpoint
	if evt.EndpointID != nil {
		ep, err := p.worker.GetEndpoint(ctx, evt.EndpointID.String())
		if err == nil {
			endpoint = ep
			logger = logger.With("endpoint_id", endpoint.ID)
		} else if !errors.Is(err, domain.ErrEndpointNotFound) {
			logger.Warn("failed to load endpoint config", "error", err)
		}
	}

	// Execute delivery
	_, err = p.worker.Deliver(ctx, evt, endpoint, logger)

	// Handle delivery result
	if err != nil {
		if errors.Is(err, domain.ErrRateLimited) {
			return p.worker.Queue().Nack(ctx, msg, 100*time.Millisecond)
		}
		if errors.Is(err, domain.ErrCircuitOpen) {
			return p.worker.Queue().Nack(ctx, msg, circuitOpenDelay)
		}
		var retryErr *RetryError
		if errors.As(err, &retryErr) {
			return p.worker.Queue().Nack(ctx, msg, retryErr.Delay)
		}
	}

	// Success or permanent failure - ack the message
	return p.worker.Queue().Ack(ctx, msg)
}
