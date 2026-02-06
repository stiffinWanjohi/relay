package delivery

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

func fifoSetupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestNewFIFOProcessor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		config        Config
		expectedGrace time.Duration
	}{
		{
			name:          "default grace period",
			config:        Config{},
			expectedGrace: 30 * time.Second,
		},
		{
			name: "custom grace period",
			config: Config{
				FIFOGracePeriod: 60 * time.Second,
			},
			expectedGrace: 60 * time.Second,
		},
		{
			name: "short grace period",
			config: Config{
				FIFOGracePeriod: 5 * time.Second,
			},
			expectedGrace: 5 * time.Second,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Use nil worker - we're just testing processor construction
			processor := NewFIFOProcessor(nil, tt.config)

			require.NotNil(t, processor)
			assert.Equal(t, tt.expectedGrace, processor.shutdownGracePeriod)
			assert.NotNil(t, processor.activeProcessors)
			assert.NotNil(t, processor.inFlightDeliveries)
			assert.NotNil(t, processor.stopCh)
		})
	}
}

func TestFIFOProcessor_InFlightTracking(t *testing.T) {
	t.Parallel()

	t.Run("tracks in-flight deliveries", func(t *testing.T) {
		t.Parallel()

		processor := NewFIFOProcessor(nil, Config{})

		processor.markDeliveryInFlight("endpoint-1:partition-a")
		processor.markDeliveryInFlight("endpoint-1:partition-b")
		processor.markDeliveryInFlight("endpoint-2:partition-a")

		assert.Equal(t, 3, processor.InFlightDeliveryCount())

		processor.markDeliveryComplete("endpoint-1:partition-a")
		assert.Equal(t, 2, processor.InFlightDeliveryCount())

		processor.markDeliveryComplete("endpoint-1:partition-b")
		processor.markDeliveryComplete("endpoint-2:partition-a")
		assert.Equal(t, 0, processor.InFlightDeliveryCount())
	})

	t.Run("handles duplicate marks gracefully", func(t *testing.T) {
		t.Parallel()

		processor := NewFIFOProcessor(nil, Config{})

		processor.markDeliveryInFlight("key-1")
		processor.markDeliveryInFlight("key-1") // duplicate
		assert.Equal(t, 1, processor.InFlightDeliveryCount())

		processor.markDeliveryComplete("key-1")
		processor.markDeliveryComplete("key-1") // already removed
		assert.Equal(t, 0, processor.InFlightDeliveryCount())
	})

	t.Run("concurrent in-flight tracking", func(t *testing.T) {
		t.Parallel()

		processor := NewFIFOProcessor(nil, Config{})

		var wg sync.WaitGroup
		numGoroutines := 100

		for i := 0; i < numGoroutines; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				key := processorKey("endpoint", string(rune('a'+i%26)))
				processor.markDeliveryInFlight(key)
				time.Sleep(time.Millisecond)
				processor.markDeliveryComplete(key)
			}(i)
		}

		wg.Wait()
		assert.Equal(t, 0, processor.InFlightDeliveryCount())
	})
}

func TestFIFOProcessor_ActiveProcessorCount(t *testing.T) {
	t.Parallel()

	processor := NewFIFOProcessor(nil, Config{})

	// Initially zero
	assert.Equal(t, 0, processor.ActiveProcessorCount())
}

func TestFIFOProcessor_ProcessorKey(t *testing.T) {
	t.Parallel()

	// Test the processorKey utility function
	tests := []struct {
		endpointID   string
		partitionKey string
		expected     string
	}{
		{
			endpointID:   "endpoint-1",
			partitionKey: "",
			expected:     "endpoint-1",
		},
		{
			endpointID:   "endpoint-1",
			partitionKey: "partition-a",
			expected:     "endpoint-1:partition-a",
		},
		{
			endpointID:   "abc-123",
			partitionKey: "user-456",
			expected:     "abc-123:user-456",
		},
	}

	for _, tt := range tests {
		result := processorKey(tt.endpointID, tt.partitionKey)
		assert.Equal(t, tt.expected, result)
	}
}

func TestFIFOProcessor_StartStop(t *testing.T) {
	t.Parallel()

	t.Run("starts and stops cleanly", func(t *testing.T) {
		t.Parallel()

		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.FIFOGracePeriod = 100 * time.Millisecond

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)

		ctx, cancel := context.WithCancel(context.Background())

		processor.Start(ctx)

		// Give it a moment
		time.Sleep(50 * time.Millisecond)

		// Cancel context and stop
		cancel()

		done := make(chan struct{})
		go func() {
			processor.Stop()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(2 * time.Second):
			t.Fatal("Stop timed out")
		}
	})
}

func TestFIFOProcessor_WithRealQueue(t *testing.T) {
	mr, client := fifoSetupTestRedis(t)
	defer mr.Close()
	defer client.Close()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = true

	worker := NewWorker(q, nil, config)
	defer worker.circuit.Stop()

	processor := NewFIFOProcessor(worker, config)

	t.Run("processor starts with real queue", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		processor.Start(ctx)
		time.Sleep(50 * time.Millisecond)

		// Should have zero active processors (no FIFO endpoints)
		assert.Equal(t, 0, processor.ActiveProcessorCount())

		processor.Stop()
	})
}

func TestFIFOProcessor_ProcessPartition_Errors(t *testing.T) {
	t.Parallel()

	t.Run("returns error when store not configured", func(t *testing.T) {
		t.Parallel()

		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		worker := NewWorker(q, nil, config) // nil store
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)
		ctx := context.Background()

		err := processor.ProcessPartition(ctx, uuid.New(), "partition-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "store not configured")
	})
}

func TestFIFOProcessor_GracefulShutdown(t *testing.T) {
	t.Parallel()

	t.Run("waits for in-flight deliveries", func(t *testing.T) {
		t.Parallel()

		processor := NewFIFOProcessor(nil, Config{
			FIFOGracePeriod: 500 * time.Millisecond,
		})

		// Simulate in-flight delivery
		processor.markDeliveryInFlight("test-delivery")

		// Start stop in background
		stopDone := make(chan struct{})
		go func() {
			processor.Stop()
			close(stopDone)
		}()

		// Complete delivery after small delay
		time.Sleep(50 * time.Millisecond)
		processor.markDeliveryComplete("test-delivery")

		select {
		case <-stopDone:
			// Success - stopped after delivery completed
		case <-time.After(2 * time.Second):
			t.Fatal("Stop did not complete after in-flight delivery finished")
		}
	})

	t.Run("respects shutdown grace period", func(t *testing.T) {
		t.Parallel()

		shortGrace := 200 * time.Millisecond
		processor := NewFIFOProcessor(nil, Config{
			FIFOGracePeriod: shortGrace,
		})

		// Simulate in-flight delivery that won't complete
		processor.markDeliveryInFlight("stuck-delivery")

		start := time.Now()
		processor.Stop()
		elapsed := time.Since(start)

		// Should have waited approximately the grace period
		assert.GreaterOrEqual(t, elapsed, shortGrace, "should wait for grace period")
		assert.LessOrEqual(t, elapsed, shortGrace+500*time.Millisecond, "should not wait too long beyond grace period")
	})
}

func TestFIFOProcessor_EndpointHelpers(t *testing.T) {
	t.Parallel()

	t.Run("IsFIFO checks FIFO field", func(t *testing.T) {
		t.Parallel()

		fifoEndpoint := domain.Endpoint{
			ID:   uuid.New(),
			FIFO: true,
		}
		assert.True(t, fifoEndpoint.IsFIFO())

		standardEndpoint := domain.Endpoint{
			ID:   uuid.New(),
			FIFO: false,
		}
		assert.False(t, standardEndpoint.IsFIFO())
	})

	t.Run("HasPartitionKey checks FIFOPartitionKey", func(t *testing.T) {
		t.Parallel()

		withPartition := domain.Endpoint{
			ID:               uuid.New(),
			FIFO:             true,
			FIFOPartitionKey: "$.customer_id",
		}
		assert.True(t, withPartition.HasPartitionKey())

		withoutPartition := domain.Endpoint{
			ID:               uuid.New(),
			FIFO:             true,
			FIFOPartitionKey: "",
		}
		assert.False(t, withoutPartition.HasPartitionKey())
	})
}

func TestFIFOProcessor_DiscoverAndProcessFIFOEndpoints(t *testing.T) {
	t.Run("handles nil store gracefully", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()

		worker := NewWorker(q, nil, config) // nil store
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)

		ctx := context.Background()

		// Should not panic with nil store
		processor.discoverAndProcessFIFOEndpoints(ctx)

		assert.Equal(t, 0, processor.ActiveProcessorCount())
	})
}

func TestFIFOProcessor_EnsureProcessorRunning(t *testing.T) {
	t.Run("starts processor for endpoint", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.FIFOGracePeriod = 100 * time.Millisecond

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		fifoEndpoint := domain.Endpoint{
			ID:     uuid.New(),
			URL:    "https://example.com/webhook",
			FIFO:   true,
			Status: domain.EndpointStatusActive,
		}

		processor := NewFIFOProcessor(worker, config)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		processor.ensureProcessorRunning(ctx, fifoEndpoint, "")

		time.Sleep(50 * time.Millisecond)
		assert.Equal(t, 1, processor.ActiveProcessorCount())

		// Calling again should not create duplicate
		processor.ensureProcessorRunning(ctx, fifoEndpoint, "")
		assert.Equal(t, 1, processor.ActiveProcessorCount())

		processor.Stop()
	})

	t.Run("does not create duplicate for same partition key", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.FIFOGracePeriod = 100 * time.Millisecond

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		fifoEndpoint := domain.Endpoint{
			ID:               uuid.New(),
			URL:              "https://example.com/webhook",
			FIFO:             true,
			FIFOPartitionKey: "$.customer_id",
			Status:           domain.EndpointStatusActive,
		}

		processor := NewFIFOProcessor(worker, config)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Manually add a processor to the map to simulate an existing processor
		key := processorKey(fifoEndpoint.ID.String(), "customer-123")
		processor.activeProcessorsMu.Lock()
		processor.activeProcessors[key] = func() {} // Fake cancel function
		processor.activeProcessorsMu.Unlock()

		assert.Equal(t, 1, processor.ActiveProcessorCount())

		// Calling ensureProcessorRunning with same key should not create duplicate
		processor.ensureProcessorRunning(ctx, fifoEndpoint, "customer-123")

		assert.Equal(t, 1, processor.ActiveProcessorCount())

		processor.Stop()
	})

	t.Run("respects max processors limit", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.FIFOGracePeriod = 100 * time.Millisecond

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Fill up to max (simulate by adding to activeProcessors directly)
		processor.activeProcessorsMu.Lock()
		for i := range maxFIFOProcessors {
			processor.activeProcessors[processorKey("fake", string(rune(i)))] = func() {}
		}
		processor.activeProcessorsMu.Unlock()

		// Try to add one more
		fifoEndpoint := domain.Endpoint{
			ID:     uuid.New(),
			FIFO:   true,
			Status: domain.EndpointStatusActive,
		}
		processor.ensureProcessorRunning(ctx, fifoEndpoint, "new-partition")

		// Should still be at max
		assert.Equal(t, maxFIFOProcessors, processor.ActiveProcessorCount())

		processor.Stop()
	})
}

func TestFIFOProcessor_ProcessPartition(t *testing.T) {
	t.Run("returns error when store not configured", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()

		worker := NewWorker(q, nil, config) // nil store
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)
		ctx := context.Background()

		err := processor.ProcessPartition(ctx, uuid.New(), "partition-1")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "store not configured")
	})
}

func TestFIFOProcessor_RecoverStaleFIFOMessages(t *testing.T) {
	t.Run("recovers stale messages on start", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.FIFOGracePeriod = 100 * time.Millisecond

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)

		ctx, cancel := context.WithCancel(context.Background())

		// Start should call recoverStaleFIFOMessages
		processor.Start(ctx)
		time.Sleep(50 * time.Millisecond)

		cancel()
		processor.Stop()
	})
}

func TestFIFOProcessor_WaitForInFlightDeliveries(t *testing.T) {
	t.Run("completes immediately when no in-flight", func(t *testing.T) {
		processor := NewFIFOProcessor(nil, Config{
			FIFOGracePeriod: time.Second,
		})

		start := time.Now()
		processor.waitForInFlightDeliveries()
		elapsed := time.Since(start)

		assert.Less(t, elapsed, 100*time.Millisecond)
	})

	t.Run("waits for in-flight to complete", func(t *testing.T) {
		processor := NewFIFOProcessor(nil, Config{
			FIFOGracePeriod: time.Second,
		})

		processor.markDeliveryInFlight("test-1")

		done := make(chan struct{})
		go func() {
			processor.waitForInFlightDeliveries()
			close(done)
		}()

		// Complete delivery after short delay
		time.Sleep(50 * time.Millisecond)
		processor.markDeliveryComplete("test-1")

		select {
		case <-done:
			// Success
		case <-time.After(time.Second):
			t.Fatal("waitForInFlightDeliveries did not complete")
		}
	})
}

func TestFIFOProcessor_EndpointDiscoveryLoop(t *testing.T) {
	t.Run("stops on context cancellation", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.FIFOGracePeriod = 100 * time.Millisecond

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)

		ctx, cancel := context.WithCancel(context.Background())

		done := make(chan struct{})
		go func() {
			processor.endpointDiscoveryLoop(ctx)
			close(done)
		}()

		// Give it time to start
		time.Sleep(50 * time.Millisecond)

		cancel()

		select {
		case <-done:
			// Success
		case <-time.After(time.Second):
			t.Fatal("endpointDiscoveryLoop did not stop on context cancellation")
		}
	})

	t.Run("stops on stopCh signal", func(t *testing.T) {
		mr, client := fifoSetupTestRedis(t)
		defer mr.Close()
		defer client.Close()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.FIFOGracePeriod = 100 * time.Millisecond

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewFIFOProcessor(worker, config)

		ctx := context.Background()

		done := make(chan struct{})
		go func() {
			processor.endpointDiscoveryLoop(ctx)
			close(done)
		}()

		// Give it time to start
		time.Sleep(50 * time.Millisecond)

		close(processor.stopCh)

		select {
		case <-done:
			// Success
		case <-time.After(time.Second):
			t.Fatal("endpointDiscoveryLoop did not stop on stopCh signal")
		}
	})
}
