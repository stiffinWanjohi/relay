package delivery

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/relay/internal/domain"
	"github.com/relay/internal/event"
	"github.com/relay/internal/queue"
)

const (
	// Default delay for circuit-open nack
	circuitOpenDelay = 5 * time.Minute

	// Backoff settings for empty queue
	minEmptyQueueBackoff = 50 * time.Millisecond
	maxEmptyQueueBackoff = 2 * time.Second
)

// Worker processes events from the queue and delivers them.
type Worker struct {
	queue          *queue.Queue
	store          *event.Store
	sender         *Sender
	circuit        *CircuitBreaker
	retry          *RetryPolicy
	logger         *slog.Logger
	stopCh         chan struct{}
	wg             sync.WaitGroup
	concurrency    int
	visibilityTime time.Duration
}

// WorkerConfig holds worker configuration.
type WorkerConfig struct {
	Concurrency    int
	VisibilityTime time.Duration
	SigningKey     string
	CircuitConfig  CircuitConfig
}

// DefaultWorkerConfig returns the default worker configuration.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Concurrency:    10,
		VisibilityTime: 30 * time.Second,
		CircuitConfig:  DefaultCircuitConfig(),
	}
}

// NewWorker creates a new delivery worker.
func NewWorker(q *queue.Queue, store *event.Store, config WorkerConfig, logger *slog.Logger) *Worker {
	return &Worker{
		queue:          q,
		store:          store,
		sender:         NewSender(config.SigningKey),
		circuit:        NewCircuitBreaker(config.CircuitConfig),
		retry:          NewRetryPolicy(),
		logger:         logger,
		stopCh:         make(chan struct{}),
		concurrency:    config.Concurrency,
		visibilityTime: config.VisibilityTime,
	}
}

// Start begins processing events.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("starting worker", "concurrency", w.concurrency)

	for i := 0; i < w.concurrency; i++ {
		w.wg.Add(1)
		go func(workerID int) {
			defer w.wg.Done()
			w.processLoop(ctx, workerID)
		}(i)
	}
}

// Stop signals the worker to stop processing.
func (w *Worker) Stop() {
	w.logger.Info("stopping worker")
	close(w.stopCh)
}

// Wait blocks until all worker goroutines have exited.
func (w *Worker) Wait() {
	w.wg.Wait()
}

// StopAndWait stops the worker and waits for all goroutines to exit.
func (w *Worker) StopAndWait(timeout time.Duration) error {
	w.Stop()

	done := make(chan struct{})
	go func() {
		w.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		return nil
	case <-time.After(timeout):
		return errors.New("worker shutdown timed out")
	}
}

func (w *Worker) processLoop(ctx context.Context, workerID int) {
	logger := w.logger.With("worker_id", workerID)
	logger.Info("worker started")

	backoff := minEmptyQueueBackoff

	for {
		select {
		case <-ctx.Done():
			logger.Info("worker stopped (context cancelled)")
			return
		case <-w.stopCh:
			logger.Info("worker stopped (stop signal)")
			return
		default:
			err := w.processOne(ctx, logger)
			if err != nil {
				if errors.Is(err, domain.ErrQueueEmpty) {
					// Exponential backoff when queue is empty
					select {
					case <-ctx.Done():
						return
					case <-w.stopCh:
						return
					case <-time.After(backoff):
						backoff = min(backoff*2, maxEmptyQueueBackoff)
					}
					continue
				}
				logger.Error("error processing message", "error", err)
			}
			// Reset backoff on successful processing
			backoff = minEmptyQueueBackoff
		}
	}
}

func (w *Worker) processOne(ctx context.Context, logger *slog.Logger) error {
	// Dequeue a message
	msg, err := w.queue.Dequeue(ctx)
	if err != nil {
		return err
	}

	logger = logger.With("event_id", msg.EventID, "message_id", msg.ID)
	logger.Debug("processing message")

	// Get the event from the store
	evt, err := w.store.GetByID(ctx, msg.EventID)
	if err != nil {
		logger.Error("failed to get event", "error", err)
		// Ack the message to remove it from the queue
		return w.queue.Ack(ctx, msg)
	}

	// Check circuit breaker
	if w.circuit.IsOpen(evt.Destination) {
		logger.Debug("circuit open, delaying")
		return w.queue.Nack(ctx, msg, circuitOpenDelay)
	}

	// Mark as delivering and increment attempts
	evt = evt.MarkDelivering().IncrementAttempts()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	// Attempt delivery
	result := w.sender.Send(ctx, evt)

	// Create delivery attempt record
	attempt := domain.NewDeliveryAttempt(evt.ID, evt.Attempts)
	if result.Success {
		attempt = attempt.WithSuccess(result.StatusCode, result.ResponseBody, result.DurationMs)
	} else {
		errMsg := ""
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		attempt = attempt.WithFailure(result.StatusCode, result.ResponseBody, errMsg, result.DurationMs)
	}

	// Save the attempt
	if _, err := w.store.CreateDeliveryAttempt(ctx, attempt); err != nil {
		logger.Error("failed to create delivery attempt", "error", err)
	}

	// Handle result
	if result.Success {
		return w.handleSuccess(ctx, msg, evt, logger)
	}
	return w.handleFailure(ctx, msg, evt, result, logger)
}

func (w *Worker) handleSuccess(ctx context.Context, msg *queue.Message, evt domain.Event, logger *slog.Logger) error {
	logger.Info("delivery successful", "attempts", evt.Attempts)

	// Mark as delivered
	evt = evt.MarkDelivered()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	// Record success for circuit breaker
	w.circuit.RecordSuccess(evt.Destination)

	// Ack the message
	return w.queue.Ack(ctx, msg)
}

func (w *Worker) handleFailure(ctx context.Context, msg *queue.Message, evt domain.Event, result domain.DeliveryResult, logger *slog.Logger) error {
	logger.Warn("delivery failed",
		"attempts", evt.Attempts,
		"status_code", result.StatusCode,
		"error", result.Error,
	)

	// Record failure for circuit breaker
	w.circuit.RecordFailure(evt.Destination)

	// Check if we should retry
	if evt.ShouldRetry() && w.retry.ShouldRetry(evt.Attempts, evt.MaxAttempts) {
		delay := w.retry.NextRetryDelay(evt.Attempts)
		nextAttempt := time.Now().UTC().Add(delay)

		logger.Info("scheduling retry", "delay", delay, "next_attempt_at", nextAttempt)

		evt = evt.MarkFailed(nextAttempt)
		if _, err := w.store.Update(ctx, evt); err != nil {
			logger.Error("failed to update event status", "error", err)
		}

		return w.queue.Nack(ctx, msg, delay)
	}

	// No more retries, mark as dead
	logger.Warn("max retries exceeded, marking as dead")

	evt = evt.MarkDead()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	// Ack the message to remove from queue
	return w.queue.Ack(ctx, msg)
}

// CircuitStats returns the current circuit breaker statistics.
func (w *Worker) CircuitStats() CircuitStats {
	return w.circuit.Stats()
}
