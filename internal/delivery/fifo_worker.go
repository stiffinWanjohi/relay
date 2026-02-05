package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/notification"
	"github.com/stiffinWanjohi/relay/internal/observability"
	"github.com/stiffinWanjohi/relay/internal/queue"
	"github.com/stiffinWanjohi/relay/internal/transform"
)

var fifoLog = logging.Component("delivery.fifo")

const (
	// How often to poll for active FIFO endpoints
	fifoEndpointPollInterval = 5 * time.Second

	// How often to poll each FIFO queue for messages
	fifoQueuePollInterval = 100 * time.Millisecond

	// Maximum concurrent FIFO queue processors
	maxFIFOProcessors = 100
)

// FIFOWorker processes events from FIFO queues with ordered delivery guarantees.
// Unlike the standard worker, FIFO queues process one event at a time per endpoint/partition.
type FIFOWorker struct {
	queue       *queue.Queue
	store       *event.Store
	sender      *Sender
	circuit     *CircuitBreaker
	retry       *RetryPolicy
	rateLimiter *RateLimiter
	transformer domain.TransformationExecutor
	metrics     *observability.Metrics
	stopCh      chan struct{}
	wg          sync.WaitGroup

	// Track active FIFO queue processors
	activeProcessors   map[string]context.CancelFunc // key: endpoint:partition
	activeProcessorsMu sync.Mutex

	// Track in-flight deliveries for graceful shutdown
	inFlightDeliveries   map[string]struct{} // key: endpoint:partition
	inFlightDeliveriesMu sync.Mutex
	shutdownGracePeriod  time.Duration
}

// FIFOWorkerConfig holds FIFO worker configuration.
type FIFOWorkerConfig struct {
	SigningKey          string
	CircuitConfig       CircuitConfig
	Metrics             *observability.Metrics
	RateLimiter         *RateLimiter
	NotificationService *notification.Service
	NotifyOnTrip        bool
	NotifyOnRecover     bool
	ShutdownGracePeriod time.Duration // Time to wait for in-flight deliveries during shutdown
}

// NewFIFOWorker creates a new FIFO delivery worker.
func NewFIFOWorker(q *queue.Queue, store *event.Store, config FIFOWorkerConfig) *FIFOWorker {
	circuit := NewCircuitBreaker(config.CircuitConfig)
	if config.NotificationService != nil {
		circuit.WithNotifier(config.NotificationService, config.NotifyOnTrip, config.NotifyOnRecover)
	}

	gracePeriod := config.ShutdownGracePeriod
	if gracePeriod == 0 {
		gracePeriod = 30 * time.Second // Default grace period
	}

	return &FIFOWorker{
		queue:               q,
		store:               store,
		sender:              NewSender(config.SigningKey),
		circuit:             circuit,
		retry:               NewRetryPolicy(),
		rateLimiter:         config.RateLimiter,
		transformer:         transform.NewDefaultV8Executor(),
		metrics:             config.Metrics,
		stopCh:              make(chan struct{}),
		activeProcessors:    make(map[string]context.CancelFunc),
		inFlightDeliveries:  make(map[string]struct{}),
		shutdownGracePeriod: gracePeriod,
	}
}

// Start begins the FIFO worker.
func (w *FIFOWorker) Start(ctx context.Context) {
	fifoLog.Info("FIFO worker started")

	// Recover any in-flight messages from previous worker crash
	w.recoverStaleFIFOMessages(ctx)

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.endpointDiscoveryLoop(ctx)
	}()
}

// recoverStaleFIFOMessages recovers any messages that were in-flight when worker crashed.
func (w *FIFOWorker) recoverStaleFIFOMessages(ctx context.Context) {
	recovered, err := w.queue.RecoverAllStaleFIFO(ctx)
	if err != nil {
		fifoLog.Error("failed to recover stale FIFO messages", "error", err)
		return
	}
	if recovered > 0 {
		fifoLog.Info("recovered stale FIFO messages", "count", recovered)
		if w.metrics != nil {
			w.metrics.FIFOMessagesRecovered(ctx, recovered)
		}
	}
}

// Stop signals the FIFO worker to stop.
// It waits for in-flight deliveries to complete before cancelling processors.
func (w *FIFOWorker) Stop() {
	fifoLog.Info("stopping FIFO worker, waiting for in-flight deliveries")

	// Signal stop to prevent new work
	close(w.stopCh)

	// Wait for in-flight deliveries to complete (with timeout)
	w.waitForInFlightDeliveries()

	// Cancel all active processors
	w.activeProcessorsMu.Lock()
	for key, cancel := range w.activeProcessors {
		fifoLog.Debug("cancelling processor", "key", key)
		cancel()
	}
	w.activeProcessorsMu.Unlock()
}

// waitForInFlightDeliveries waits for all in-flight deliveries to complete.
func (w *FIFOWorker) waitForInFlightDeliveries() {
	deadline := time.Now().Add(w.shutdownGracePeriod)
	checkInterval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		w.inFlightDeliveriesMu.Lock()
		count := len(w.inFlightDeliveries)
		w.inFlightDeliveriesMu.Unlock()

		if count == 0 {
			fifoLog.Info("all in-flight deliveries completed")
			return
		}

		fifoLog.Debug("waiting for in-flight deliveries", "count", count)
		time.Sleep(checkInterval)
	}

	w.inFlightDeliveriesMu.Lock()
	remaining := len(w.inFlightDeliveries)
	w.inFlightDeliveriesMu.Unlock()

	if remaining > 0 {
		fifoLog.Warn("shutdown grace period exceeded, abandoning in-flight deliveries", "count", remaining)
	}
}

// markDeliveryInFlight marks a delivery as in-flight.
func (w *FIFOWorker) markDeliveryInFlight(key string) {
	w.inFlightDeliveriesMu.Lock()
	w.inFlightDeliveries[key] = struct{}{}
	w.inFlightDeliveriesMu.Unlock()
}

// markDeliveryComplete marks a delivery as complete.
func (w *FIFOWorker) markDeliveryComplete(key string) {
	w.inFlightDeliveriesMu.Lock()
	delete(w.inFlightDeliveries, key)
	w.inFlightDeliveriesMu.Unlock()
}

// InFlightDeliveryCount returns the number of in-flight deliveries.
func (w *FIFOWorker) InFlightDeliveryCount() int {
	w.inFlightDeliveriesMu.Lock()
	defer w.inFlightDeliveriesMu.Unlock()
	return len(w.inFlightDeliveries)
}

// Wait blocks until the worker has stopped.
func (w *FIFOWorker) Wait() {
	w.wg.Wait()
}

// StopAndWait stops the worker and waits for completion.
func (w *FIFOWorker) StopAndWait(timeout time.Duration) error {
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
		return errors.New("FIFO worker shutdown timed out")
	}
}

// endpointDiscoveryLoop periodically discovers FIFO endpoints and starts processors.
func (w *FIFOWorker) endpointDiscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(fifoEndpointPollInterval)
	defer ticker.Stop()

	// Run immediately on start
	w.discoverAndProcessFIFOEndpoints(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			w.discoverAndProcessFIFOEndpoints(ctx)
		}
	}
}

// discoverAndProcessFIFOEndpoints finds all FIFO endpoints and ensures processors are running.
func (w *FIFOWorker) discoverAndProcessFIFOEndpoints(ctx context.Context) {
	if w.store == nil {
		return
	}

	// Get all active FIFO endpoints
	endpoints, err := w.store.ListFIFOEndpoints(ctx)
	if err != nil {
		fifoLog.Error("failed to list FIFO endpoints", "error", err)
		return
	}

	// Build a map of endpoint IDs for quick lookup
	endpointMap := make(map[string]domain.Endpoint)
	for _, ep := range endpoints {
		endpointMap[ep.ID.String()] = ep
	}

	// Track which processors should be active
	activeKeys := make(map[string]bool)

	// First, start processors for endpoints without partition keys (default queue)
	for _, endpoint := range endpoints {
		if !endpoint.HasPartitionKey() {
			key := w.processorKey(endpoint.ID.String(), "")
			activeKeys[key] = true
			w.ensureProcessorRunning(ctx, endpoint, "")
		}
	}

	// Discover active partitions from Redis queues
	activeQueues, err := w.queue.GetActiveFIFOQueues(ctx)
	if err != nil {
		fifoLog.Warn("failed to get active FIFO queues", "error", err)
	} else {
		for _, queueKey := range activeQueues {
			endpointID, partitionKey, ok := queue.ParseFIFOQueueKey(queueKey)
			if !ok {
				continue
			}

			// Check if this endpoint is still active and FIFO
			endpoint, exists := endpointMap[endpointID]
			if !exists {
				// Endpoint no longer exists or is not FIFO, skip
				continue
			}

			key := w.processorKey(endpointID, partitionKey)
			activeKeys[key] = true
			w.ensureProcessorRunning(ctx, endpoint, partitionKey)
		}
	}

	// Clean up processors for endpoints that are no longer FIFO or active
	w.activeProcessorsMu.Lock()
	for key, cancel := range w.activeProcessors {
		if !activeKeys[key] {
			fifoLog.Debug("stopping processor for inactive endpoint", "key", key)
			cancel()
			delete(w.activeProcessors, key)
		}
	}
	w.activeProcessorsMu.Unlock()
}

// ensureProcessorRunning starts a processor for the endpoint/partition if not already running.
func (w *FIFOWorker) ensureProcessorRunning(ctx context.Context, endpoint domain.Endpoint, partitionKey string) {
	key := w.processorKey(endpoint.ID.String(), partitionKey)

	w.activeProcessorsMu.Lock()
	defer w.activeProcessorsMu.Unlock()

	// Already running
	if _, exists := w.activeProcessors[key]; exists {
		return
	}

	// Check max processors limit
	if len(w.activeProcessors) >= maxFIFOProcessors {
		fifoLog.Warn("max FIFO processors reached", "max", maxFIFOProcessors)
		return
	}

	// Start new processor
	procCtx, cancel := context.WithCancel(ctx)
	w.activeProcessors[key] = cancel

	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		defer func() {
			w.activeProcessorsMu.Lock()
			delete(w.activeProcessors, key)
			w.activeProcessorsMu.Unlock()
		}()

		w.processFIFOQueue(procCtx, endpoint, partitionKey)
	}()

	fifoLog.Debug("started FIFO processor", "endpoint_id", endpoint.ID, "partition", partitionKey)
}

// processFIFOQueue processes events from a single FIFO queue.
func (w *FIFOWorker) processFIFOQueue(ctx context.Context, endpoint domain.Endpoint, partitionKey string) {
	logger := fifoLog.With(
		"endpoint_id", endpoint.ID,
		"partition", partitionKey,
	)

	ticker := time.NewTicker(fifoQueuePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-w.stopCh:
			return
		case <-ticker.C:
			// Refresh endpoint config periodically
			refreshedEndpoint, err := w.store.GetEndpointByID(ctx, endpoint.ID)
			if err != nil {
				if errors.Is(err, domain.ErrEndpointNotFound) {
					logger.Debug("endpoint no longer exists, stopping processor")
					return
				}
				logger.Warn("failed to refresh endpoint", "error", err)
				continue
			}

			// Stop if endpoint is no longer FIFO or active
			if !refreshedEndpoint.IsFIFO() || refreshedEndpoint.Status != domain.EndpointStatusActive {
				logger.Debug("endpoint no longer FIFO or active, stopping processor")
				// Handle orphaned messages
				w.handleOrphanedFIFOMessages(ctx, endpoint.ID.String(), partitionKey, refreshedEndpoint, logger)
				return
			}

			endpoint = refreshedEndpoint

			// Try to process one message
			if err := w.processOneFIFO(ctx, endpoint, partitionKey, logger); err != nil {
				if !errors.Is(err, domain.ErrQueueEmpty) {
					logger.Error("error processing FIFO message", "error", err)
				}
			}
		}
	}
}

// processOneFIFO processes a single message from a FIFO queue.
func (w *FIFOWorker) processOneFIFO(ctx context.Context, endpoint domain.Endpoint, partitionKey string, logger *slog.Logger) error {
	endpointID := endpoint.ID.String()
	deliveryKey := w.processorKey(endpointID, partitionKey)

	// Try to dequeue (this acquires the FIFO lock)
	msg, err := w.queue.DequeueFIFO(ctx, endpointID, partitionKey)
	if err != nil {
		return err
	}

	// Track this delivery as in-flight for graceful shutdown
	w.markDeliveryInFlight(deliveryKey)
	defer w.markDeliveryComplete(deliveryKey)

	logger = logger.With("event_id", msg.EventID, "message_id", msg.ID)
	logger.Debug("processing FIFO message")

	// Get the event from the store
	evt, err := w.store.GetByID(ctx, msg.EventID)
	if err != nil {
		logger.Error("failed to get event", "error", err)
		// Ack to remove from queue (event doesn't exist)
		return w.queue.AckFIFO(ctx, endpointID, partitionKey)
	}

	// Circuit breaker key
	circuitKey := endpoint.ID.String()

	// Check rate limit
	if w.rateLimiter != nil && endpoint.RateLimitPerSec > 0 {
		if !w.rateLimiter.Allow(ctx, circuitKey, endpoint.RateLimitPerSec) {
			logger.Debug("rate limited, delaying")
			return w.queue.NackFIFO(ctx, endpointID, partitionKey, msg, 100*time.Millisecond)
		}
	}

	// Check circuit breaker
	if w.circuit.IsOpen(circuitKey) {
		logger.Debug("circuit open, delaying")
		return w.queue.NackFIFO(ctx, endpointID, partitionKey, msg, circuitOpenDelay)
	}

	// Mark as delivering
	evt = evt.MarkDelivering().IncrementAttempts()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	// Apply transformation if configured
	if endpoint.HasTransformation() {
		transformedEvt, err := w.applyTransformation(ctx, evt, &endpoint, logger)
		if err != nil {
			if errors.Is(err, domain.ErrTransformationCancelled) {
				logger.Info("delivery cancelled by transformation")
				evt = evt.MarkDelivered()
				if _, err := w.store.Update(ctx, evt); err != nil {
					logger.Error("failed to update event status", "error", err)
				}
				return w.queue.AckFIFO(ctx, endpointID, partitionKey)
			}
			logger.Warn("transformation failed, using original payload", "error", err)
		} else {
			evt = transformedEvt
		}
	}

	// Attempt delivery
	result := w.sender.SendWithEndpoint(ctx, evt, &endpoint)

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

	if _, err := w.store.CreateDeliveryAttempt(ctx, attempt); err != nil {
		logger.Error("failed to create delivery attempt", "error", err)
	}

	// Handle result
	if result.Success {
		return w.handleFIFOSuccess(ctx, endpointID, partitionKey, evt, circuitKey, result, logger)
	}
	return w.handleFIFOFailure(ctx, endpointID, partitionKey, msg, evt, &endpoint, circuitKey, result, logger)
}

func (w *FIFOWorker) handleFIFOSuccess(ctx context.Context, endpointID, partitionKey string, evt domain.Event, circuitKey string, result domain.DeliveryResult, logger *slog.Logger) error {
	logger.Info("FIFO delivery successful", "attempts", evt.Attempts)

	evt = evt.MarkDelivered()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	w.circuit.RecordSuccess(circuitKey)

	if w.metrics != nil {
		duration := time.Duration(result.DurationMs) * time.Millisecond
		destHost := extractHost(evt.Destination)
		w.metrics.EventDelivered(ctx, evt.ClientID, destHost, duration)
	}

	// Release the FIFO lock to allow next message
	return w.queue.AckFIFO(ctx, endpointID, partitionKey)
}

func (w *FIFOWorker) handleFIFOFailure(ctx context.Context, endpointID, partitionKey string, msg *queue.Message, evt domain.Event, endpoint *domain.Endpoint, circuitKey string, result domain.DeliveryResult, logger *slog.Logger) error {
	logger.Warn("FIFO delivery failed",
		"attempts", evt.Attempts,
		"status_code", result.StatusCode,
		"error", result.Error,
	)

	w.circuit.RecordFailure(circuitKey)

	if w.metrics != nil {
		reason := classifyFailureReason(result)
		w.metrics.EventFailed(ctx, evt.ClientID, reason)
	}

	// Check if we should retry
	shouldRetry := evt.ShouldRetry() && w.retry.ShouldRetryForEndpoint(evt.Attempts, endpoint)

	if shouldRetry {
		delay := w.retry.NextRetryDelayForEndpoint(evt.Attempts, endpoint)
		nextAttempt := time.Now().UTC().Add(delay)

		logger.Info("scheduling FIFO retry", "delay", delay, "next_attempt_at", nextAttempt)

		if w.metrics != nil {
			w.metrics.EventRetry(ctx, evt.ClientID, evt.Attempts)
		}

		evt = evt.MarkFailed(nextAttempt)
		if _, err := w.store.Update(ctx, evt); err != nil {
			logger.Error("failed to update event status", "error", err)
		}

		// Nack with delay - message goes back to front of queue, lock held until delay expires
		return w.queue.NackFIFO(ctx, endpointID, partitionKey, msg, delay)
	}

	// No more retries, mark as dead
	logger.Warn("max retries exceeded, marking as dead")

	evt = evt.MarkDead()
	if _, err := w.store.Update(ctx, evt); err != nil {
		logger.Error("failed to update event status", "error", err)
	}

	// Release the lock to allow processing next message
	return w.queue.AckFIFO(ctx, endpointID, partitionKey)
}

// applyTransformation applies the endpoint's transformation to the event.
func (w *FIFOWorker) applyTransformation(ctx context.Context, evt domain.Event, endpoint *domain.Endpoint, logger *slog.Logger) (domain.Event, error) {
	if endpoint == nil || !endpoint.HasTransformation() {
		return evt, nil
	}

	input := domain.NewTransformationInput(
		"POST",
		evt.Destination,
		evt.Headers,
		evt.Payload,
	)

	result, err := w.transformer.Execute(ctx, endpoint.Transformation, input)
	if err != nil {
		return evt, err
	}

	transformedEvt := evt

	if result.URL != evt.Destination {
		transformedEvt.Destination = result.URL
	}

	if result.Headers != nil {
		transformedEvt.Headers = result.Headers
	}

	if result.Payload != nil {
		transformedEvt.Payload = json.RawMessage(result.Payload)
	}

	logger.Debug("transformation applied",
		slog.String("original_url", evt.Destination),
		slog.String("transformed_url", transformedEvt.Destination),
	)

	return transformedEvt, nil
}

// processorKey generates a unique key for an endpoint/partition processor.
func (w *FIFOWorker) processorKey(endpointID, partitionKey string) string {
	if partitionKey == "" {
		return endpointID
	}
	return endpointID + ":" + partitionKey
}

// ActiveProcessorCount returns the number of active FIFO queue processors.
func (w *FIFOWorker) ActiveProcessorCount() int {
	w.activeProcessorsMu.Lock()
	defer w.activeProcessorsMu.Unlock()
	return len(w.activeProcessors)
}

// CircuitStats returns the circuit breaker statistics.
func (w *FIFOWorker) CircuitStats() CircuitStats {
	return w.circuit.Stats()
}

// ProcessPartition starts processing a specific partition for an endpoint.
// This is useful when a new partition key is discovered in incoming events.
func (w *FIFOWorker) ProcessPartition(ctx context.Context, endpointID uuid.UUID, partitionKey string) error {
	if w.store == nil {
		return errors.New("store not configured")
	}

	endpoint, err := w.store.GetEndpointByID(ctx, endpointID)
	if err != nil {
		return err
	}

	if !endpoint.IsFIFO() {
		return errors.New("endpoint is not configured for FIFO delivery")
	}

	w.ensureProcessorRunning(ctx, endpoint, partitionKey)
	return nil
}

// handleOrphanedFIFOMessages handles messages left in a FIFO queue when the endpoint
// is disabled or switches from FIFO to standard delivery.
func (w *FIFOWorker) handleOrphanedFIFOMessages(ctx context.Context, endpointID, partitionKey string, endpoint domain.Endpoint, logger *slog.Logger) {
	// Check what happened to the endpoint
	if endpoint.Status != domain.EndpointStatusActive {
		// Endpoint was disabled - mark events as failed
		logger.Info("endpoint disabled, marking queued events as failed", "endpoint_id", endpointID)
		w.markQueuedEventsAsFailed(ctx, endpointID, partitionKey, logger)
		return
	}

	if !endpoint.IsFIFO() {
		// Endpoint switched from FIFO to standard - move messages to standard queue
		logger.Info("endpoint switched to standard delivery, moving messages to standard queue", "endpoint_id", endpointID)
		moved, err := w.queue.MoveFIFOToStandardQueue(ctx, endpointID, partitionKey)
		if err != nil {
			logger.Error("failed to move FIFO messages to standard queue", "error", err)
			return
		}
		if moved > 0 {
			logger.Info("moved FIFO messages to standard queue", "count", moved)
		}
	}
}

// markQueuedEventsAsFailed marks all events in a FIFO queue as failed.
func (w *FIFOWorker) markQueuedEventsAsFailed(ctx context.Context, endpointID, partitionKey string, logger *slog.Logger) {
	messages, err := w.queue.DrainFIFOQueue(ctx, endpointID, partitionKey)
	if err != nil {
		logger.Error("failed to drain FIFO queue", "error", err)
		return
	}

	for _, msg := range messages {
		evt, err := w.store.GetByID(ctx, msg.EventID)
		if err != nil {
			logger.Warn("failed to get event for marking as failed", "event_id", msg.EventID, "error", err)
			continue
		}

		// Mark as dead (endpoint disabled)
		evt = evt.MarkDead()
		if _, err := w.store.Update(ctx, evt); err != nil {
			logger.Error("failed to mark event as dead", "event_id", msg.EventID, "error", err)
		}
	}

	if len(messages) > 0 {
		logger.Info("marked orphaned events as dead", "count", len(messages))
	}
}
