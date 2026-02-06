package delivery

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/internal/queue"
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

// FIFOProcessor handles ordered delivery processing for FIFO endpoints.
// It maintains one processor per endpoint/partition combination.
type FIFOProcessor struct {
	worker *Worker

	// Track active queue processors
	activeProcessors   map[string]context.CancelFunc
	activeProcessorsMu sync.Mutex

	// Track in-flight deliveries for graceful shutdown
	inFlightDeliveries   map[string]struct{}
	inFlightDeliveriesMu sync.Mutex
	shutdownGracePeriod  time.Duration

	stopCh chan struct{}
	wg     sync.WaitGroup
}

// NewFIFOProcessor creates a new FIFOProcessor.
func NewFIFOProcessor(w *Worker, config Config) *FIFOProcessor {
	gracePeriod := config.FIFOGracePeriod
	if gracePeriod == 0 {
		gracePeriod = 30 * time.Second
	}

	return &FIFOProcessor{
		worker:              w,
		activeProcessors:    make(map[string]context.CancelFunc),
		inFlightDeliveries:  make(map[string]struct{}),
		shutdownGracePeriod: gracePeriod,
		stopCh:              make(chan struct{}),
	}
}

// Start begins the FIFO processor.
func (p *FIFOProcessor) Start(ctx context.Context) {
	fifoLog.Info("starting FIFO processor")

	// Recover any in-flight messages from previous crash
	p.recoverStaleFIFOMessages(ctx)

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.endpointDiscoveryLoop(ctx)
	}()
}

// Stop signals the processor to stop and waits for in-flight deliveries.
func (p *FIFOProcessor) Stop() {
	fifoLog.Info("stopping FIFO processor")
	close(p.stopCh)

	// Wait for in-flight deliveries
	p.waitForInFlightDeliveries()

	// Cancel all active processors
	p.activeProcessorsMu.Lock()
	for key, cancel := range p.activeProcessors {
		fifoLog.Debug("cancelling processor", "key", key)
		cancel()
	}
	p.activeProcessorsMu.Unlock()

	p.wg.Wait()
}

func (p *FIFOProcessor) recoverStaleFIFOMessages(ctx context.Context) {
	recovered, err := p.worker.Queue().RecoverAllStaleFIFO(ctx)
	if err != nil {
		fifoLog.Error("failed to recover stale FIFO messages", "error", err)
		return
	}
	if recovered > 0 {
		fifoLog.Info("recovered stale FIFO messages", "count", recovered)
	}
}

func (p *FIFOProcessor) waitForInFlightDeliveries() {
	deadline := time.Now().Add(p.shutdownGracePeriod)
	checkInterval := 100 * time.Millisecond

	for time.Now().Before(deadline) {
		p.inFlightDeliveriesMu.Lock()
		count := len(p.inFlightDeliveries)
		p.inFlightDeliveriesMu.Unlock()

		if count == 0 {
			fifoLog.Info("all in-flight deliveries completed")
			return
		}

		fifoLog.Debug("waiting for in-flight deliveries", "count", count)
		time.Sleep(checkInterval)
	}

	p.inFlightDeliveriesMu.Lock()
	remaining := len(p.inFlightDeliveries)
	p.inFlightDeliveriesMu.Unlock()

	if remaining > 0 {
		fifoLog.Warn("shutdown grace period exceeded", "remaining", remaining)
	}
}

func (p *FIFOProcessor) markDeliveryInFlight(key string) {
	p.inFlightDeliveriesMu.Lock()
	p.inFlightDeliveries[key] = struct{}{}
	p.inFlightDeliveriesMu.Unlock()
}

func (p *FIFOProcessor) markDeliveryComplete(key string) {
	p.inFlightDeliveriesMu.Lock()
	delete(p.inFlightDeliveries, key)
	p.inFlightDeliveriesMu.Unlock()
}

func (p *FIFOProcessor) endpointDiscoveryLoop(ctx context.Context) {
	ticker := time.NewTicker(fifoEndpointPollInterval)
	defer ticker.Stop()

	// Run immediately on start
	p.discoverAndProcessFIFOEndpoints(ctx)

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			p.discoverAndProcessFIFOEndpoints(ctx)
		}
	}
}

func (p *FIFOProcessor) discoverAndProcessFIFOEndpoints(ctx context.Context) {
	store := p.worker.Store()
	if store == nil {
		return
	}

	// Get all active FIFO endpoints
	endpoints, err := store.ListFIFOEndpoints(ctx)
	if err != nil {
		fifoLog.Error("failed to list FIFO endpoints", "error", err)
		return
	}

	// Build map for quick lookup
	endpointMap := make(map[string]domain.Endpoint)
	for _, ep := range endpoints {
		endpointMap[ep.ID.String()] = ep
	}

	// Track which processors should be active
	activeKeys := make(map[string]bool)

	// Start processors for endpoints without partition keys
	for _, endpoint := range endpoints {
		if !endpoint.HasPartitionKey() {
			key := processorKey(endpoint.ID.String(), "")
			activeKeys[key] = true
			p.ensureProcessorRunning(ctx, endpoint, "")
		}
	}

	// Discover active partitions from Redis
	activeQueues, err := p.worker.Queue().GetActiveFIFOQueues(ctx)
	if err != nil {
		fifoLog.Warn("failed to get active FIFO queues", "error", err)
	} else {
		for _, queueKey := range activeQueues {
			endpointID, partitionKey, ok := queue.ParseFIFOQueueKey(queueKey)
			if !ok {
				continue
			}

			endpoint, exists := endpointMap[endpointID]
			if !exists {
				continue
			}

			key := processorKey(endpointID, partitionKey)
			activeKeys[key] = true
			p.ensureProcessorRunning(ctx, endpoint, partitionKey)
		}
	}

	// Clean up inactive processors
	p.activeProcessorsMu.Lock()
	for key, cancel := range p.activeProcessors {
		if !activeKeys[key] {
			fifoLog.Debug("stopping inactive processor", "key", key)
			cancel()
			delete(p.activeProcessors, key)
		}
	}
	p.activeProcessorsMu.Unlock()
}

func (p *FIFOProcessor) ensureProcessorRunning(ctx context.Context, endpoint domain.Endpoint, partitionKey string) {
	key := processorKey(endpoint.ID.String(), partitionKey)

	p.activeProcessorsMu.Lock()
	defer p.activeProcessorsMu.Unlock()

	if _, exists := p.activeProcessors[key]; exists {
		return
	}

	if len(p.activeProcessors) >= maxFIFOProcessors {
		fifoLog.Warn("max FIFO processors reached", "max", maxFIFOProcessors)
		return
	}

	procCtx, cancel := context.WithCancel(ctx)
	p.activeProcessors[key] = cancel

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		defer func() {
			p.activeProcessorsMu.Lock()
			delete(p.activeProcessors, key)
			p.activeProcessorsMu.Unlock()
		}()

		p.processFIFOQueue(procCtx, endpoint, partitionKey)
	}()

	fifoLog.Debug("started FIFO processor", "endpoint_id", endpoint.ID, "partition", partitionKey)
}

func (p *FIFOProcessor) processFIFOQueue(ctx context.Context, endpoint domain.Endpoint, partitionKey string) {
	logger := fifoLog.With("endpoint_id", endpoint.ID, "partition", partitionKey)

	ticker := time.NewTicker(fifoQueuePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-p.stopCh:
			return
		case <-ticker.C:
			// Refresh endpoint config
			store := p.worker.Store()
			if store == nil {
				logger.Debug("store not configured, stopping processor")
				return
			}
			refreshedEndpoint, err := store.GetEndpointByID(ctx, endpoint.ID)
			if err != nil {
				if errors.Is(err, domain.ErrEndpointNotFound) {
					logger.Debug("endpoint no longer exists")
					return
				}
				logger.Warn("failed to refresh endpoint", "error", err)
				continue
			}

			if !refreshedEndpoint.IsFIFO() || refreshedEndpoint.Status != domain.EndpointStatusActive {
				logger.Debug("endpoint no longer FIFO or active")
				p.handleOrphanedFIFOMessages(ctx, endpoint.ID.String(), partitionKey, refreshedEndpoint, logger)
				return
			}

			endpoint = refreshedEndpoint

			if err := p.processOneFIFO(ctx, endpoint, partitionKey, logger); err != nil {
				if !errors.Is(err, domain.ErrQueueEmpty) {
					logger.Error("error processing FIFO message", "error", err)
				}
			}
		}
	}
}

func (p *FIFOProcessor) processOneFIFO(ctx context.Context, endpoint domain.Endpoint, partitionKey string, logger *slog.Logger) error {
	endpointID := endpoint.ID.String()
	deliveryKey := processorKey(endpointID, partitionKey)

	// Dequeue (acquires FIFO lock)
	msg, err := p.worker.Queue().DequeueFIFO(ctx, endpointID, partitionKey)
	if err != nil {
		return err
	}

	// Track in-flight for graceful shutdown
	p.markDeliveryInFlight(deliveryKey)
	defer p.markDeliveryComplete(deliveryKey)

	eventID := msg.EventID
	logger = logger.With("event_id", eventID, "message_id", msg.ID)
	logger.Debug("processing FIFO message")

	// Get event
	evt, err := p.worker.GetEvent(ctx, eventID)
	if err != nil {
		logger.Error("failed to get event", "error", err)
		return p.worker.Queue().AckFIFO(ctx, endpointID, partitionKey)
	}

	// Execute delivery
	_, err = p.worker.Deliver(ctx, evt, &endpoint, logger)

	// Handle delivery result
	if err != nil {
		if errors.Is(err, domain.ErrRateLimited) {
			return p.worker.Queue().NackFIFO(ctx, endpointID, partitionKey, msg, 100*time.Millisecond)
		}
		if errors.Is(err, domain.ErrCircuitOpen) {
			return p.worker.Queue().NackFIFO(ctx, endpointID, partitionKey, msg, circuitOpenDelay)
		}
		var retryErr *RetryError
		if errors.As(err, &retryErr) {
			return p.worker.Queue().NackFIFO(ctx, endpointID, partitionKey, msg, retryErr.Delay)
		}
	}

	// Success or permanent failure - ack the message
	return p.worker.Queue().AckFIFO(ctx, endpointID, partitionKey)
}

func (p *FIFOProcessor) handleOrphanedFIFOMessages(ctx context.Context, endpointID, partitionKey string, endpoint domain.Endpoint, logger *slog.Logger) {
	if endpoint.Status != domain.EndpointStatusActive {
		logger.Info("endpoint disabled, marking queued events as failed")
		p.markQueuedEventsAsFailed(ctx, endpointID, partitionKey, logger)
		return
	}

	if !endpoint.IsFIFO() {
		logger.Info("endpoint switched to standard, moving messages")
		moved, err := p.worker.Queue().MoveFIFOToStandardQueue(ctx, endpointID, partitionKey)
		if err != nil {
			logger.Error("failed to move FIFO messages", "error", err)
			return
		}
		if moved > 0 {
			logger.Info("moved FIFO messages to standard queue", "count", moved)
		}
	}
}

func (p *FIFOProcessor) markQueuedEventsAsFailed(ctx context.Context, endpointID, partitionKey string, logger *slog.Logger) {
	messages, err := p.worker.Queue().DrainFIFOQueue(ctx, endpointID, partitionKey)
	if err != nil {
		logger.Error("failed to drain FIFO queue", "error", err)
		return
	}

	for _, msg := range messages {
		eventID := msg.EventID
		evt, err := p.worker.GetEvent(ctx, eventID)
		if err != nil {
			logger.Warn("failed to get event", "event_id", eventID, "error", err)
			continue
		}

		evt = evt.MarkDead()
		if _, err := p.worker.Store().Update(ctx, evt); err != nil {
			logger.Error("failed to mark event as dead", "event_id", eventID, "error", err)
		}
	}

	if len(messages) > 0 {
		logger.Info("marked orphaned events as dead", "count", len(messages))
	}
}

// ActiveProcessorCount returns the number of active FIFO queue processors.
func (p *FIFOProcessor) ActiveProcessorCount() int {
	p.activeProcessorsMu.Lock()
	defer p.activeProcessorsMu.Unlock()
	return len(p.activeProcessors)
}

// InFlightDeliveryCount returns the number of in-flight deliveries.
func (p *FIFOProcessor) InFlightDeliveryCount() int {
	p.inFlightDeliveriesMu.Lock()
	defer p.inFlightDeliveriesMu.Unlock()
	return len(p.inFlightDeliveries)
}

// ProcessPartition starts processing a specific partition.
func (p *FIFOProcessor) ProcessPartition(ctx context.Context, endpointID uuid.UUID, partitionKey string) error {
	store := p.worker.Store()
	if store == nil {
		return errors.New("store not configured")
	}

	endpoint, err := store.GetEndpointByID(ctx, endpointID)
	if err != nil {
		return err
	}

	if !endpoint.IsFIFO() {
		return errors.New("endpoint is not configured for FIFO delivery")
	}

	p.ensureProcessorRunning(ctx, endpoint, partitionKey)
	return nil
}
