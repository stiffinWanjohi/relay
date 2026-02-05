package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/url"
	"sync"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/logstream"
	"github.com/stiffinWanjohi/relay/internal/metrics"
	"github.com/stiffinWanjohi/relay/internal/notification"
	"github.com/stiffinWanjohi/relay/internal/observability"
	"github.com/stiffinWanjohi/relay/internal/queue"
	"github.com/stiffinWanjohi/relay/internal/transform"
)

var workerLog = logging.Component("delivery.worker")

const (
	// Default delay for circuit-open nack
	circuitOpenDelay = 5 * time.Minute

	// Backoff settings for empty queue
	minEmptyQueueBackoff = 50 * time.Millisecond
	maxEmptyQueueBackoff = 2 * time.Second
)

// Worker processes events from the queue and delivers them.
type Worker struct {
	queue               *queue.Queue
	store               *event.Store
	sender              *Sender
	circuit             *CircuitBreaker
	retry               *RetryPolicy
	rateLimiter         *RateLimiter
	transformer         domain.TransformationExecutor
	metrics             *observability.Metrics
	metricsStore        *metrics.Store
	deliveryLogger      *logstream.DeliveryLogger
	stopCh              chan struct{}
	wg                  sync.WaitGroup
	concurrency         int
	visibilityTime      time.Duration
	enablePriorityQueue bool
}

// WorkerConfig holds worker configuration.
type WorkerConfig struct {
	Concurrency         int
	VisibilityTime      time.Duration
	SigningKey          string
	CircuitConfig       CircuitConfig
	Metrics             *observability.Metrics
	MetricsStore        *metrics.Store
	LogStreamHub        *logstream.Hub
	RateLimiter         *RateLimiter
	NotificationService *notification.Service
	NotifyOnTrip        bool
	NotifyOnRecover     bool
	EnablePriorityQueue bool // Enable priority queue processing
}

// DefaultWorkerConfig returns the default worker configuration.
func DefaultWorkerConfig() WorkerConfig {
	return WorkerConfig{
		Concurrency:         10,
		VisibilityTime:      30 * time.Second,
		CircuitConfig:       DefaultCircuitConfig(),
		EnablePriorityQueue: true, // Enable by default
	}
}

// NewWorker creates a new delivery worker.
func NewWorker(q *queue.Queue, store *event.Store, config WorkerConfig) *Worker {
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

	return &Worker{
		queue:               q,
		store:               store,
		sender:              NewSender(config.SigningKey),
		circuit:             circuit,
		retry:               NewRetryPolicy(),
		rateLimiter:         config.RateLimiter,
		transformer:         transform.NewDefaultV8Executor(),
		metrics:             config.Metrics,
		metricsStore:        config.MetricsStore,
		deliveryLogger:      deliveryLogger,
		stopCh:              make(chan struct{}),
		concurrency:         config.Concurrency,
		visibilityTime:      config.VisibilityTime,
		enablePriorityQueue: config.EnablePriorityQueue,
	}
}

// Start begins processing events.
func (w *Worker) Start(ctx context.Context) {
	workerLog.Info("workers started", "count", w.concurrency)

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
	workerLog.Info("stopping worker")
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
	logger := workerLog.With("worker_id", workerID)
	// Individual worker start logs removed for cleaner output

	backoff := minEmptyQueueBackoff

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
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
	// Dequeue a message (using priority queue if enabled)
	var msg *queue.Message
	var err error
	if w.enablePriorityQueue {
		msg, err = w.queue.DequeueWithPriority(ctx)
	} else {
		msg, err = w.queue.Dequeue(ctx)
	}
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

	// Load endpoint configuration if available
	var endpoint *domain.Endpoint
	if evt.EndpointID != nil {
		ep, err := w.store.GetEndpointByID(ctx, *evt.EndpointID)
		if err == nil {
			endpoint = &ep
			logger = logger.With("endpoint_id", endpoint.ID)
		} else if !errors.Is(err, domain.ErrEndpointNotFound) {
			logger.Warn("failed to load endpoint config, using defaults", "error", err)
		}
	}

	// Determine circuit breaker key (use endpoint ID if available, otherwise destination URL)
	circuitKey := evt.Destination
	if endpoint != nil {
		circuitKey = endpoint.ID.String()
	}

	// Check rate limit (per-endpoint)
	if w.rateLimiter != nil && endpoint != nil && endpoint.RateLimitPerSec > 0 {
		if !w.rateLimiter.Allow(ctx, endpoint.ID.String(), endpoint.RateLimitPerSec) {
			logger.Debug("rate limited, delaying", "limit", endpoint.RateLimitPerSec)

			// Record rate limit metric
			if w.metricsStore != nil {
				_ = w.metricsStore.RecordRateLimit(ctx, metrics.RateLimitEvent{
					EndpointID: endpoint.ID.String(),
					EventID:    evt.ID.String(),
					Limit:      endpoint.RateLimitPerSec,
				})
			}

			// Short delay for rate limiting - try again soon
			return w.queue.Nack(ctx, msg, 100*time.Millisecond)
		}
	}

	// Check circuit breaker
	if w.circuit.IsOpen(circuitKey) {
		logger.Debug("circuit open, delaying")
		return w.queue.Nack(ctx, msg, circuitOpenDelay)
	}

	// Mark as delivering and increment attempts
	evt = evt.MarkDelivering().IncrementAttempts()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	// Apply transformation if configured
	if endpoint != nil && endpoint.HasTransformation() {
		transformedEvt, err := w.applyTransformation(ctx, evt, endpoint, logger)
		if err != nil {
			if errors.Is(err, domain.ErrTransformationCancelled) {
				// Transformation requested cancellation - mark as delivered (no-op delivery)
				logger.Info("delivery cancelled by transformation")
				evt = evt.MarkDelivered()
				if _, err := w.store.Update(ctx, evt); err != nil {
					logger.Error("failed to update event status", "error", err)
				}
				return w.queue.Ack(ctx, msg)
			}
			// Transformation failed - log error but continue with original payload
			logger.Warn("transformation failed, using original payload",
				slog.String("error", err.Error()),
			)
		} else {
			evt = transformedEvt
		}
	}

	// Attempt delivery with endpoint-specific configuration (timeout, signing secret)
	result := w.sender.SendWithEndpoint(ctx, evt, endpoint)

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

	// Record delivery metrics
	w.recordDeliveryMetrics(ctx, evt, endpoint, result)

	// Handle result with endpoint config for retry decisions
	deliveryDuration := time.Duration(result.DurationMs) * time.Millisecond
	if result.Success {
		return w.handleSuccess(ctx, msg, evt, endpoint, circuitKey, deliveryDuration, result.StatusCode, logger)
	}
	return w.handleFailure(ctx, msg, evt, endpoint, circuitKey, result, logger)
}

func (w *Worker) handleSuccess(ctx context.Context, msg *queue.Message, evt domain.Event, endpoint *domain.Endpoint, circuitKey string, duration time.Duration, statusCode int, logger *slog.Logger) error {
	logger.Info("delivery successful", "attempts", evt.Attempts)

	// Mark as delivered
	evt = evt.MarkDelivered()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	// Record success for circuit breaker (using endpoint-based key)
	w.circuit.RecordSuccess(circuitKey)

	// Record metrics
	if w.metrics != nil {
		destHost := extractHost(evt.Destination)
		w.metrics.EventDelivered(ctx, evt.ClientID, destHost, duration)
	}

	// Stream log entry
	if w.deliveryLogger != nil {
		endpointID := ""
		if endpoint != nil {
			endpointID = endpoint.ID.String()
		}
		w.deliveryLogger.LogDeliverySuccess(ctx, evt.ID.String(), evt.EventType, endpointID, evt.Destination, evt.ClientID, statusCode, duration.Milliseconds(), evt.Attempts, evt.MaxAttempts)
	}

	// Ack the message
	return w.queue.Ack(ctx, msg)
}

func (w *Worker) handleFailure(ctx context.Context, msg *queue.Message, evt domain.Event, endpoint *domain.Endpoint, circuitKey string, result domain.DeliveryResult, logger *slog.Logger) error {
	logger.Warn("delivery failed",
		"attempts", evt.Attempts,
		"status_code", result.StatusCode,
		"error", result.Error,
	)

	// Record failure for circuit breaker (using endpoint-based key)
	w.circuit.RecordFailure(circuitKey)

	// Record failure metric
	if w.metrics != nil {
		reason := classifyFailureReason(result)
		w.metrics.EventFailed(ctx, evt.ClientID, reason)
	}

	// Check if we should retry using endpoint-specific configuration
	shouldRetry := evt.ShouldRetry() && w.retry.ShouldRetryForEndpoint(evt.Attempts, endpoint)

	// Stream log entry
	if w.deliveryLogger != nil {
		endpointID := ""
		if endpoint != nil {
			endpointID = endpoint.ID.String()
		}
		errMsg := ""
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		w.deliveryLogger.LogDeliveryFailure(ctx, evt.ID.String(), evt.EventType, endpointID, evt.Destination, evt.ClientID, result.StatusCode, result.DurationMs, errMsg, evt.Attempts, evt.MaxAttempts, shouldRetry)
	}

	if shouldRetry {
		// Calculate delay using endpoint-specific backoff
		delay := w.retry.NextRetryDelayForEndpoint(evt.Attempts, endpoint)
		nextAttempt := time.Now().UTC().Add(delay)

		logger.Info("scheduling retry", "delay", delay, "next_attempt_at", nextAttempt)

		// Record retry metric
		if w.metrics != nil {
			w.metrics.EventRetry(ctx, evt.ClientID, evt.Attempts)
		}

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

// applyTransformation applies the endpoint's transformation to the event.
// Returns the transformed event or an error if transformation fails.
func (w *Worker) applyTransformation(ctx context.Context, evt domain.Event, endpoint *domain.Endpoint, logger *slog.Logger) (domain.Event, error) {
	if endpoint == nil || !endpoint.HasTransformation() {
		return evt, nil
	}

	// Build transformation input
	input := domain.NewTransformationInput(
		"POST",
		evt.Destination,
		evt.Headers,
		evt.Payload,
	)

	// Execute transformation
	result, err := w.transformer.Execute(ctx, endpoint.Transformation, input)
	if err != nil {
		return evt, err
	}

	// Apply transformation result to event
	transformedEvt := evt

	// Update destination URL if changed
	if result.URL != evt.Destination {
		transformedEvt.Destination = result.URL
	}

	// Update headers
	if result.Headers != nil {
		transformedEvt.Headers = result.Headers
	}

	// Update payload
	if result.Payload != nil {
		transformedEvt.Payload = json.RawMessage(result.Payload)
	}

	logger.Debug("transformation applied",
		slog.String("original_url", evt.Destination),
		slog.String("transformed_url", transformedEvt.Destination),
	)

	return transformedEvt, nil
}

// extractHost extracts the host from a URL for metrics tagging.
func extractHost(destination string) string {
	u, err := url.Parse(destination)
	if err != nil {
		return "unknown"
	}
	return u.Host
}

// classifyFailureReason classifies the delivery failure for metrics.
func classifyFailureReason(result domain.DeliveryResult) string {
	if result.Error != nil {
		errStr := result.Error.Error()
		switch {
		case contains(errStr, "timeout"):
			return "timeout"
		case contains(errStr, "connection refused"):
			return "connection_refused"
		case contains(errStr, "no such host"):
			return "dns_error"
		case contains(errStr, "TLS"):
			return "tls_error"
		default:
			return "network_error"
		}
	}

	switch {
	case result.StatusCode >= 500:
		return "server_error"
	case result.StatusCode >= 400:
		return "client_error"
	default:
		return "unknown"
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// recordDeliveryMetrics records delivery metrics to the metrics store.
func (w *Worker) recordDeliveryMetrics(_ context.Context, evt domain.Event, endpoint *domain.Endpoint, result domain.DeliveryResult) {
	if w.metricsStore == nil {
		return
	}

	// Determine outcome
	var outcome metrics.DeliveryOutcome
	if result.Success {
		outcome = metrics.OutcomeSuccess
	} else if result.Error != nil && contains(result.Error.Error(), "timeout") {
		outcome = metrics.OutcomeTimeout
	} else {
		outcome = metrics.OutcomeFailure
	}

	// Build record
	record := metrics.DeliveryRecord{
		EventID:    evt.ID.String(),
		Outcome:    outcome,
		StatusCode: result.StatusCode,
		LatencyMs:  result.DurationMs,
		AttemptNum: evt.Attempts,
		ClientID:   evt.ClientID,
	}

	// Add endpoint ID if available
	if endpoint != nil {
		record.EndpointID = endpoint.ID.String()
	}

	// Add event type if available
	if evt.EventType != "" {
		record.EventType = evt.EventType
	}

	// Add error message if present
	if result.Error != nil {
		record.Error = result.Error.Error()
	}

	// Record the delivery (fire and forget - don't block on metrics)
	go func() {
		if err := w.metricsStore.RecordDelivery(context.Background(), record); err != nil {
			workerLog.Warn("failed to record delivery metrics", "error", err)
		}
	}()
}
