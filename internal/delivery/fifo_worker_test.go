package delivery

import (
	"context"
	"log/slog"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

func TestFIFOWorker_processorKey(t *testing.T) {
	w := &FIFOWorker{}

	tests := []struct {
		endpointID   string
		partitionKey string
		expected     string
	}{
		{"endpoint-1", "", "endpoint-1"},
		{"endpoint-1", "partition-a", "endpoint-1:partition-a"},
		{"ep-abc", "customer-123", "ep-abc:customer-123"},
	}

	for _, tc := range tests {
		result := w.processorKey(tc.endpointID, tc.partitionKey)
		if result != tc.expected {
			t.Errorf("processorKey(%q, %q) = %q, expected %q",
				tc.endpointID, tc.partitionKey, result, tc.expected)
		}
	}
}

func TestFIFOWorker_ActiveProcessorCount(t *testing.T) {
	w := &FIFOWorker{
		activeProcessors: make(map[string]context.CancelFunc),
	}

	if count := w.ActiveProcessorCount(); count != 0 {
		t.Errorf("expected 0 active processors, got %d", count)
	}

	// Add some mock processors (cancel functions)
	_, cancel1 := context.WithCancel(context.Background())
	_, cancel2 := context.WithCancel(context.Background())
	w.activeProcessors["ep1"] = cancel1
	w.activeProcessors["ep2:part1"] = cancel2

	if count := w.ActiveProcessorCount(); count != 2 {
		t.Errorf("expected 2 active processors, got %d", count)
	}
}

func TestFIFOWorker_NewFIFOWorker(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	config := FIFOWorkerConfig{
		SigningKey:    "test-key",
		CircuitConfig: DefaultCircuitConfig(),
	}

	worker := NewFIFOWorker(q, nil, config, logger)

	if worker == nil {
		t.Fatal("expected worker to be created")
	}
	if worker.queue != q {
		t.Error("expected queue to be set")
	}
	if worker.circuit == nil {
		t.Error("expected circuit breaker to be set")
	}
	if worker.retry == nil {
		t.Error("expected retry policy to be set")
	}
	if worker.activeProcessors == nil {
		t.Error("expected activeProcessors map to be initialized")
	}
}

func TestFIFOWorker_StartStop(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	config := FIFOWorkerConfig{
		SigningKey:    "test-key",
		CircuitConfig: DefaultCircuitConfig(),
	}

	// Use NewFIFOWorker which properly initializes all fields
	worker := NewFIFOWorker(q, nil, config, logger)

	ctx, cancel := context.WithCancel(context.Background())

	// Start should not panic
	worker.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context first to stop the discovery loop
	cancel()

	// Then stop and wait
	err = worker.StopAndWait(2 * time.Second)
	if err != nil {
		t.Errorf("StopAndWait failed: %v", err)
	}
}

func TestFIFOWorker_ensureProcessorRunning(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	worker := &FIFOWorker{
		queue:            q,
		store:            nil,
		circuit:          NewCircuitBreaker(DefaultCircuitConfig()),
		retry:            NewRetryPolicy(),
		logger:           logger,
		stopCh:           make(chan struct{}),
		activeProcessors: make(map[string]context.CancelFunc),
		wg:               sync.WaitGroup{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	endpoint := domain.Endpoint{
		ID:       uuid.New(),
		ClientID: "client-1",
		URL:      "https://example.com/webhook",
		Status:   domain.EndpointStatusActive,
		FIFO:     true,
	}

	// First call should start a processor
	worker.ensureProcessorRunning(ctx, endpoint, "")

	if count := worker.ActiveProcessorCount(); count != 1 {
		t.Errorf("expected 1 active processor, got %d", count)
	}

	// Second call with same key should not start another
	worker.ensureProcessorRunning(ctx, endpoint, "")

	if count := worker.ActiveProcessorCount(); count != 1 {
		t.Errorf("expected still 1 active processor, got %d", count)
	}

	// Call with different partition should start another
	worker.ensureProcessorRunning(ctx, endpoint, "partition-1")

	if count := worker.ActiveProcessorCount(); count != 2 {
		t.Errorf("expected 2 active processors, got %d", count)
	}

	// Cleanup
	worker.Stop()
	worker.Wait()
}

func TestFIFOWorker_maxProcessorsLimit(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	worker := &FIFOWorker{
		queue:            q,
		store:            nil,
		circuit:          NewCircuitBreaker(DefaultCircuitConfig()),
		retry:            NewRetryPolicy(),
		logger:           logger,
		stopCh:           make(chan struct{}),
		activeProcessors: make(map[string]context.CancelFunc),
		wg:               sync.WaitGroup{},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Fill up to max processors
	for i := 0; i < maxFIFOProcessors; i++ {
		endpoint := domain.Endpoint{
			ID:       uuid.New(),
			ClientID: "client-1",
			URL:      "https://example.com/webhook",
			Status:   domain.EndpointStatusActive,
			FIFO:     true,
		}
		worker.ensureProcessorRunning(ctx, endpoint, "")
	}

	if count := worker.ActiveProcessorCount(); count != maxFIFOProcessors {
		t.Errorf("expected %d active processors, got %d", maxFIFOProcessors, count)
	}

	// Try to add one more - should not increase
	endpoint := domain.Endpoint{
		ID:       uuid.New(),
		ClientID: "client-1",
		URL:      "https://example.com/webhook",
		Status:   domain.EndpointStatusActive,
		FIFO:     true,
	}
	worker.ensureProcessorRunning(ctx, endpoint, "")

	if count := worker.ActiveProcessorCount(); count != maxFIFOProcessors {
		t.Errorf("expected still %d active processors (max limit), got %d", maxFIFOProcessors, count)
	}

	// Cleanup
	worker.Stop()
	worker.Wait()
}

func TestFIFOWorker_CircuitStats(t *testing.T) {
	circuit := NewCircuitBreaker(DefaultCircuitConfig())
	worker := &FIFOWorker{
		circuit:          circuit,
		activeProcessors: make(map[string]context.CancelFunc),
	}

	stats := worker.CircuitStats()

	// Initial stats should have empty circuits map
	if stats.Circuits == nil {
		t.Error("expected Circuits map to be initialized")
	}
	if len(stats.Circuits) != 0 {
		t.Errorf("expected 0 circuits, got %d", len(stats.Circuits))
	}

	// Record failure first (this creates the circuit)
	circuit.RecordFailure("test-endpoint")

	stats = worker.CircuitStats()
	if len(stats.Circuits) != 1 {
		t.Errorf("expected 1 circuit, got %d", len(stats.Circuits))
	}

	info, exists := stats.Circuits["test-endpoint"]
	if !exists {
		t.Error("expected test-endpoint circuit to exist")
	}
	if info.Failures != 1 {
		t.Errorf("expected 1 failure, got %d", info.Failures)
	}
}

func TestFIFOWorker_StopCancelsProcessors(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	worker := &FIFOWorker{
		queue:            q,
		store:            nil,
		circuit:          NewCircuitBreaker(DefaultCircuitConfig()),
		retry:            NewRetryPolicy(),
		logger:           logger,
		stopCh:           make(chan struct{}),
		activeProcessors: make(map[string]context.CancelFunc),
		wg:               sync.WaitGroup{},
	}

	ctx := context.Background()

	// Start a few processors
	for i := 0; i < 5; i++ {
		endpoint := domain.Endpoint{
			ID:       uuid.New(),
			ClientID: "client-1",
			URL:      "https://example.com/webhook",
			Status:   domain.EndpointStatusActive,
			FIFO:     true,
		}
		worker.ensureProcessorRunning(ctx, endpoint, "")
	}

	if count := worker.ActiveProcessorCount(); count != 5 {
		t.Errorf("expected 5 active processors, got %d", count)
	}

	// Stop should cancel all processors
	worker.Stop()

	// Wait a bit for processors to clean up
	time.Sleep(100 * time.Millisecond)

	// After stop, activeProcessors should be cleared by the goroutines
	// (they remove themselves on context cancellation)
}

// Additional queue tests for new FIFO methods

func TestQueue_ListFIFOPartitions(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	ctx := context.Background()
	endpointID := "endpoint-1"

	// Enqueue to default partition
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Enqueue to partition A
	if err := q.EnqueueFIFO(ctx, endpointID, "partition-a", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Enqueue to partition B
	if err := q.EnqueueFIFO(ctx, endpointID, "partition-b", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// List partitions
	partitions, err := q.ListFIFOPartitions(ctx, endpointID)
	if err != nil {
		t.Fatalf("ListFIFOPartitions failed: %v", err)
	}

	if len(partitions) != 3 {
		t.Errorf("expected 3 partitions, got %d: %v", len(partitions), partitions)
	}

	// Check that all partitions are present
	found := make(map[string]bool)
	for _, p := range partitions {
		found[p] = true
	}

	if !found[""] {
		t.Error("expected default partition (empty string)")
	}
	if !found["partition-a"] {
		t.Error("expected partition-a")
	}
	if !found["partition-b"] {
		t.Error("expected partition-b")
	}
}

func TestQueue_ListFIFOPartitions_Empty(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	ctx := context.Background()

	// List partitions for non-existent endpoint
	partitions, err := q.ListFIFOPartitions(ctx, "non-existent")
	if err != nil {
		t.Fatalf("ListFIFOPartitions failed: %v", err)
	}

	if len(partitions) != 0 {
		t.Errorf("expected 0 partitions, got %d", len(partitions))
	}
}

func TestQueue_GetActiveFIFOQueues(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	ctx := context.Background()

	// Initially no active queues
	queues, err := q.GetActiveFIFOQueues(ctx)
	if err != nil {
		t.Fatalf("GetActiveFIFOQueues failed: %v", err)
	}
	if len(queues) != 0 {
		t.Errorf("expected 0 active queues, got %d", len(queues))
	}

	// Enqueue some messages
	if err := q.EnqueueFIFO(ctx, "ep1", "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}
	if err := q.EnqueueFIFO(ctx, "ep2", "part1", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Now should have 2 active queues
	queues, err = q.GetActiveFIFOQueues(ctx)
	if err != nil {
		t.Fatalf("GetActiveFIFOQueues failed: %v", err)
	}
	if len(queues) != 2 {
		t.Errorf("expected 2 active queues, got %d", len(queues))
	}

	// Dequeue from one queue (empties it)
	if _, err := q.DequeueFIFO(ctx, "ep1", ""); err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}
	if err := q.AckFIFO(ctx, "ep1", ""); err != nil {
		t.Fatalf("AckFIFO failed: %v", err)
	}

	// Now should have 1 active queue
	queues, err = q.GetActiveFIFOQueues(ctx)
	if err != nil {
		t.Fatalf("GetActiveFIFOQueues failed: %v", err)
	}
	if len(queues) != 1 {
		t.Errorf("expected 1 active queue, got %d", len(queues))
	}
}

func TestParseFIFOQueueKey(t *testing.T) {
	tests := []struct {
		queueKey          string
		expectedEndpoint  string
		expectedPartition string
		expectedOk        bool
	}{
		{"relay:queue:fifo:endpoint-1", "endpoint-1", "", true},
		{"relay:queue:fifo:endpoint-1:partition-a", "endpoint-1", "partition-a", true},
		{"relay:queue:fifo:ep-abc:customer-123", "ep-abc", "customer-123", true},
		{"relay:queue:fifo:", "", "", false}, // Empty endpoint should fail
		{"invalid", "", "", false},
		{"relay:queue:fifo", "", "", false},
	}

	for _, tc := range tests {
		endpointID, partitionKey, ok := queue.ParseFIFOQueueKey(tc.queueKey)
		if ok != tc.expectedOk {
			t.Errorf("ParseFIFOQueueKey(%q): ok = %v, expected %v", tc.queueKey, ok, tc.expectedOk)
			continue
		}
		if ok {
			if endpointID != tc.expectedEndpoint {
				t.Errorf("ParseFIFOQueueKey(%q): endpointID = %q, expected %q", tc.queueKey, endpointID, tc.expectedEndpoint)
			}
			if partitionKey != tc.expectedPartition {
				t.Errorf("ParseFIFOQueueKey(%q): partitionKey = %q, expected %q", tc.queueKey, partitionKey, tc.expectedPartition)
			}
		}
	}
}

func TestFIFOWorker_StopAndWait_Timeout(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	worker := &FIFOWorker{
		stopCh:           make(chan struct{}),
		activeProcessors: make(map[string]context.CancelFunc),
		wg:               sync.WaitGroup{},
		logger:           logger,
	}

	// Channel to allow the stuck goroutine to exit after test
	exitCh := make(chan struct{})
	t.Cleanup(func() {
		close(exitCh)
	})

	// Add a goroutine that simulates being stuck during timeout
	worker.wg.Add(1)
	go func() {
		defer worker.wg.Done()
		select {
		case <-exitCh:
			// Allow cleanup after test
		case <-time.After(10 * time.Second):
			// Safety timeout - should never reach this
		}
	}()

	// StopAndWait should timeout (100ms) because goroutine is still "stuck"
	err := worker.StopAndWait(100 * time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
	if err.Error() != "FIFO worker shutdown timed out" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestFIFOWorker_InFlightDeliveryTracking(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	worker := &FIFOWorker{
		queue:               q,
		store:               nil,
		circuit:             NewCircuitBreaker(DefaultCircuitConfig()),
		retry:               NewRetryPolicy(),
		logger:              logger,
		stopCh:              make(chan struct{}),
		activeProcessors:    make(map[string]context.CancelFunc),
		inFlightDeliveries:  make(map[string]struct{}),
		shutdownGracePeriod: 30 * time.Second,
		wg:                  sync.WaitGroup{},
	}

	// Initially no in-flight deliveries
	if count := worker.InFlightDeliveryCount(); count != 0 {
		t.Errorf("expected 0 in-flight deliveries, got %d", count)
	}

	// Mark some deliveries as in-flight
	worker.markDeliveryInFlight("endpoint1")
	worker.markDeliveryInFlight("endpoint2:partition1")

	if count := worker.InFlightDeliveryCount(); count != 2 {
		t.Errorf("expected 2 in-flight deliveries, got %d", count)
	}

	// Complete one delivery
	worker.markDeliveryComplete("endpoint1")

	if count := worker.InFlightDeliveryCount(); count != 1 {
		t.Errorf("expected 1 in-flight delivery, got %d", count)
	}

	// Complete remaining
	worker.markDeliveryComplete("endpoint2:partition1")

	if count := worker.InFlightDeliveryCount(); count != 0 {
		t.Errorf("expected 0 in-flight deliveries, got %d", count)
	}
}

func TestQueue_RecoverFIFOInFlight(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	ctx := context.Background()
	endpointID := "endpoint-recovery-test"

	// Enqueue a message
	eventID := uuid.New()
	if err := q.EnqueueFIFO(ctx, endpointID, "", eventID); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Dequeue it (simulating it being in-flight)
	msg, err := q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	if msg.EventID != eventID {
		t.Errorf("expected event ID %s, got %s", eventID, msg.EventID)
	}

	// Verify queue is now empty
	length, _ := q.GetFIFOQueueLength(ctx, endpointID, "")
	if length != 0 {
		t.Errorf("expected queue length 0, got %d", length)
	}

	// Recover the in-flight message
	recovered, err := q.RecoverFIFOInFlight(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("RecoverFIFOInFlight failed: %v", err)
	}

	if recovered == nil {
		t.Fatal("expected recovered message, got nil")
	}

	if recovered.EventID != eventID {
		t.Errorf("expected recovered event ID %s, got %s", eventID, recovered.EventID)
	}

	// Verify message is back in queue
	length, _ = q.GetFIFOQueueLength(ctx, endpointID, "")
	if length != 1 {
		t.Errorf("expected queue length 1 after recovery, got %d", length)
	}

	// Verify lock is released
	locked, _ := q.IsFIFOLocked(ctx, endpointID, "")
	if locked {
		t.Error("expected lock to be released after recovery")
	}
}

func TestQueue_DrainFIFOQueue(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	ctx := context.Background()
	endpointID := "endpoint-drain-test"

	// Enqueue multiple messages
	eventIDs := []uuid.UUID{uuid.New(), uuid.New(), uuid.New()}
	for _, eventID := range eventIDs {
		if err := q.EnqueueFIFO(ctx, endpointID, "", eventID); err != nil {
			t.Fatalf("EnqueueFIFO failed: %v", err)
		}
	}

	// Verify queue has messages
	length, _ := q.GetFIFOQueueLength(ctx, endpointID, "")
	if length != 3 {
		t.Errorf("expected queue length 3, got %d", length)
	}

	// Drain the queue
	messages, err := q.DrainFIFOQueue(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DrainFIFOQueue failed: %v", err)
	}

	if len(messages) != 3 {
		t.Errorf("expected 3 drained messages, got %d", len(messages))
	}

	// Verify queue is now empty
	length, _ = q.GetFIFOQueueLength(ctx, endpointID, "")
	if length != 0 {
		t.Errorf("expected queue length 0 after drain, got %d", length)
	}
}

func TestQueue_MoveFIFOToStandardQueue(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	ctx := context.Background()
	endpointID := "endpoint-move-test"

	// Enqueue messages to FIFO queue
	eventIDs := []uuid.UUID{uuid.New(), uuid.New()}
	for _, eventID := range eventIDs {
		if err := q.EnqueueFIFO(ctx, endpointID, "", eventID); err != nil {
			t.Fatalf("EnqueueFIFO failed: %v", err)
		}
	}

	// Move to standard queue
	moved, err := q.MoveFIFOToStandardQueue(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("MoveFIFOToStandardQueue failed: %v", err)
	}

	if moved != 2 {
		t.Errorf("expected 2 moved messages, got %d", moved)
	}

	// Verify FIFO queue is empty
	length, _ := q.GetFIFOQueueLength(ctx, endpointID, "")
	if length != 0 {
		t.Errorf("expected FIFO queue length 0, got %d", length)
	}

	// Verify messages are in standard queue (by checking stats)
	stats, _ := q.Stats(ctx)
	if stats.Pending != 2 {
		t.Errorf("expected 2 pending in standard queue, got %d", stats.Pending)
	}
}

func TestQueue_GetFIFOQueueStats(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer client.Close()

	q := queue.NewQueue(client)
	ctx := context.Background()
	endpointID := "endpoint-stats-test"

	// Initially empty
	stats, err := q.GetFIFOQueueStats(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("GetFIFOQueueStats failed: %v", err)
	}

	if stats.QueueLength != 0 {
		t.Errorf("expected queue length 0, got %d", stats.QueueLength)
	}
	if stats.IsLocked {
		t.Error("expected not locked")
	}
	if stats.HasInFlight {
		t.Error("expected no in-flight")
	}

	// Add messages and dequeue one
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}
	if err := q.EnqueueFIFO(ctx, endpointID, "", uuid.New()); err != nil {
		t.Fatalf("EnqueueFIFO failed: %v", err)
	}

	// Dequeue one (creates lock and in-flight)
	_, err = q.DequeueFIFO(ctx, endpointID, "")
	if err != nil {
		t.Fatalf("DequeueFIFO failed: %v", err)
	}

	stats, _ = q.GetFIFOQueueStats(ctx, endpointID, "")
	if stats.QueueLength != 1 {
		t.Errorf("expected queue length 1, got %d", stats.QueueLength)
	}
	if !stats.IsLocked {
		t.Error("expected locked")
	}
	if !stats.HasInFlight {
		t.Error("expected in-flight")
	}
}
