package delivery

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/logstream"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

var workerLog = logging.Component("delivery.worker")

const (
	// Default delay for circuit-open nack
	circuitOpenDelay = 5 * time.Minute
)

// Worker is the unified delivery worker that manages all processing strategies.
// It owns the shared delivery logic and delegates processing loops to Processors.
type Worker struct {
	// Infrastructure
	queue *queue.Queue
	store *event.Store

	// Shared components
	sender         *Sender
	circuit        *CircuitBreaker
	retry          *RetryPolicy
	rateLimiter    *RateLimiter
	transformer    *Transformer
	recorder       *Recorder
	handler        *ResultHandler
	deliveryLogger *logstream.DeliveryLogger

	// Processors
	processors []Processor
	config     Config

	// Lifecycle
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewWorker creates a new unified delivery worker.
func NewWorker(q *queue.Queue, store *event.Store, config Config) *Worker {
	config = config.Validate()

	var deliveryLogger *logstream.DeliveryLogger
	if config.LogStreamHub != nil {
		deliveryLogger = logstream.NewDeliveryLogger(config.LogStreamHub)
	}

	circuit := NewCircuitBreaker(config.CircuitConfig)
	if config.NotificationService != nil {
		circuit.WithNotifier(config.NotificationService, config.NotifyOnTrip, config.NotifyOnRecover)
	}
	if deliveryLogger != nil {
		circuit.WithDeliveryLogger(deliveryLogger)
	}

	retry := NewRetryPolicy()
	recorder := NewRecorder(config.Metrics, config.MetricsStore, workerLog)
	transformer := NewTransformer()

	handler := NewResultHandler(
		store,
		circuit,
		retry,
		config.Metrics,
		deliveryLogger,
		workerLog,
	)

	w := &Worker{
		queue:          q,
		store:          store,
		sender:         NewSender(config.SigningKey),
		circuit:        circuit,
		retry:          retry,
		rateLimiter:    config.RateLimiter,
		transformer:    transformer,
		recorder:       recorder,
		handler:        handler,
		deliveryLogger: deliveryLogger,
		config:         config,
		stopCh:         make(chan struct{}),
	}

	// Create processors based on configuration
	if config.EnableStandard {
		w.processors = append(w.processors, NewStandardProcessor(w, config))
	}
	if config.EnableFIFO {
		w.processors = append(w.processors, NewFIFOProcessor(w, config))
	}

	return w
}

// Start begins all processors.
func (w *Worker) Start(ctx context.Context) {
	workerLog.Info("starting delivery worker",
		"processors", len(w.processors),
		"standard", w.config.EnableStandard,
		"fifo", w.config.EnableFIFO,
	)

	for _, p := range w.processors {
		p.Start(ctx)
	}
}

// Stop signals all processors to stop.
func (w *Worker) Stop() {
	workerLog.Info("stopping delivery worker")
	close(w.stopCh)

	for _, p := range w.processors {
		p.Stop()
	}
}

// Wait blocks until all processors have stopped.
func (w *Worker) Wait() {
	w.wg.Wait()
}

// StopAndWait stops the worker and waits for completion with a timeout.
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

// Deliver executes the core delivery logic for a single event.
// This is the shared logic used by all processors.
func (w *Worker) Deliver(ctx context.Context, evt domain.Event, endpoint *domain.Endpoint, logger *slog.Logger) (domain.DeliveryResult, error) {
	// Determine circuit breaker key
	circuitKey := evt.Destination
	if endpoint != nil {
		circuitKey = endpoint.ID.String()
	}

	// Check rate limit
	if w.rateLimiter != nil && endpoint != nil && endpoint.RateLimitPerSec > 0 {
		if !w.rateLimiter.Allow(ctx, endpoint.ID.String(), endpoint.RateLimitPerSec) {
			logger.Debug("rate limited", "limit", endpoint.RateLimitPerSec)
			w.recorder.RecordRateLimit(ctx, endpoint, evt)
			return domain.DeliveryResult{}, domain.ErrRateLimited
		}
	}

	// Check circuit breaker
	if w.circuit.IsOpen(circuitKey) {
		logger.Debug("circuit open")
		return domain.DeliveryResult{}, domain.ErrCircuitOpen
	}

	// Mark as delivering and increment attempts
	evt = evt.MarkDelivering().IncrementAttempts()
	if w.store != nil {
		if _, err := w.store.Update(ctx, evt); err != nil {
			logger.Error("failed to update event status", "error", err)
		}
	}

	// Apply transformation if configured
	if endpoint != nil && endpoint.HasTransformation() {
		transformedEvt, err := w.transformer.Apply(ctx, evt, endpoint, logger)
		if err != nil {
			if errors.Is(err, domain.ErrTransformationCancelled) {
				logger.Info("delivery cancelled by transformation")
				evt = evt.MarkDelivered()
				if w.store != nil {
					if _, err := w.store.Update(ctx, evt); err != nil {
						logger.Error("failed to update event status", "error", err)
					}
				}
				return domain.DeliveryResult{Success: true}, nil
			}
			logger.Warn("transformation failed, using original payload", "error", err)
		} else {
			evt = transformedEvt
		}
	}

	// Attempt delivery
	result := w.sender.SendWithEndpoint(ctx, evt, endpoint)

	// Create delivery attempt record
	w.handler.CreateDeliveryAttempt(ctx, evt, result)

	// Record delivery metrics
	w.recorder.RecordDelivery(ctx, evt, endpoint, result)

	// Handle result
	if result.Success {
		w.handler.HandleSuccess(ctx, evt, endpoint, circuitKey, result)
		return result, nil
	}

	updatedEvt, retryDelay := w.handler.HandleFailure(ctx, evt, endpoint, circuitKey, result)
	_ = updatedEvt

	if retryDelay > 0 {
		return result, &RetryError{Delay: retryDelay}
	}

	return result, nil
}

// GetEvent retrieves an event from the store.
func (w *Worker) GetEvent(ctx context.Context, eventID uuid.UUID) (domain.Event, error) {
	if w.store == nil {
		return domain.Event{}, errors.New("store is nil")
	}
	return w.store.GetByID(ctx, eventID)
}

// GetEndpoint retrieves an endpoint from the store.
func (w *Worker) GetEndpoint(ctx context.Context, endpointID string) (*domain.Endpoint, error) {
	if w.store == nil {
		return nil, errors.New("store is nil")
	}
	id, err := uuid.Parse(endpointID)
	if err != nil {
		return nil, err
	}
	ep, err := w.store.GetEndpointByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return &ep, nil
}

// Queue returns the queue for processor access.
func (w *Worker) Queue() *queue.Queue {
	return w.queue
}

// Store returns the event store for processor access.
func (w *Worker) Store() *event.Store {
	return w.store
}

// CircuitStats returns the current circuit breaker statistics.
func (w *Worker) CircuitStats() CircuitStats {
	return w.circuit.Stats()
}

// RetryError indicates that a delivery should be retried after a delay.
type RetryError struct {
	Delay time.Duration
}

func (e *RetryError) Error() string {
	return "delivery failed, retry scheduled"
}
