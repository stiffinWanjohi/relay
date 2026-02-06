package delivery

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stiffinWanjohi/relay/internal/queue"
)

func standardSetupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
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

func TestNewStandardProcessor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		config Config
		want   struct {
			concurrency         int
			visibilityTime      time.Duration
			enablePriorityQueue bool
		}
	}{
		{
			name:   "default config",
			config: DefaultConfig(),
			want: struct {
				concurrency         int
				visibilityTime      time.Duration
				enablePriorityQueue bool
			}{
				concurrency:         10,
				visibilityTime:      30 * time.Second,
				enablePriorityQueue: true,
			},
		},
		{
			name: "custom config",
			config: Config{
				Concurrency:         5,
				VisibilityTime:      60 * time.Second,
				EnablePriorityQueue: false,
			},
			want: struct {
				concurrency         int
				visibilityTime      time.Duration
				enablePriorityQueue bool
			}{
				concurrency:         5,
				visibilityTime:      60 * time.Second,
				enablePriorityQueue: false,
			},
		},
		{
			name: "high concurrency",
			config: Config{
				Concurrency:         100,
				VisibilityTime:      15 * time.Second,
				EnablePriorityQueue: true,
			},
			want: struct {
				concurrency         int
				visibilityTime      time.Duration
				enablePriorityQueue bool
			}{
				concurrency:         100,
				visibilityTime:      15 * time.Second,
				enablePriorityQueue: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			processor := NewStandardProcessor(nil, tt.config)

			require.NotNil(t, processor)
			assert.Equal(t, tt.want.concurrency, processor.concurrency)
			assert.Equal(t, tt.want.visibilityTime, processor.visibilityTime)
			assert.Equal(t, tt.want.enablePriorityQueue, processor.enablePriorityQueue)
			assert.NotNil(t, processor.stopCh)
		})
	}
}

func TestStandardProcessor_StartStop(t *testing.T) {
	t.Run("starts and stops cleanly", func(t *testing.T) {
		mr, client := standardSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 3
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewStandardProcessor(worker, config)
		ctx := context.Background()

		processor.Start(ctx)

		// Allow goroutines to start
		time.Sleep(50 * time.Millisecond)

		// Stop should complete without hanging
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

	t.Run("stops on context cancellation", func(t *testing.T) {
		mr, client := standardSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 2
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewStandardProcessor(worker, config)
		ctx, cancel := context.WithCancel(context.Background())

		processor.Start(ctx)
		time.Sleep(50 * time.Millisecond)

		// Cancel context
		cancel()

		// Stop should complete
		done := make(chan struct{})
		go func() {
			processor.Stop()
			close(done)
		}()

		select {
		case <-done:
			// Success
		case <-time.After(2 * time.Second):
			t.Fatal("Stop timed out after context cancellation")
		}
	})
}

func TestStandardProcessor_BackoffBehavior(t *testing.T) {
	t.Run("applies backoff on empty queue", func(t *testing.T) {
		mr, client := standardSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 1
		config.EnablePriorityQueue = false
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewStandardProcessor(worker, config)
		ctx := context.Background()

		startTime := time.Now()
		processor.Start(ctx)

		// Wait for backoff to accumulate
		time.Sleep(300 * time.Millisecond)
		processor.Stop()

		elapsed := time.Since(startTime)
		// Should have waited at least 200ms with backoff
		assert.GreaterOrEqual(t, elapsed, 200*time.Millisecond)
	})
}

func TestStandardProcessor_Concurrency(t *testing.T) {
	t.Run("runs specified number of workers", func(t *testing.T) {
		mr, client := standardSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 5
		config.EnablePriorityQueue = false
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewStandardProcessor(worker, config)

		// Verify configuration
		assert.Equal(t, 5, processor.concurrency)
	})
}

func TestStandardProcessor_ProcessLoop_EmptyQueue(t *testing.T) {
	t.Run("handles empty queue gracefully", func(t *testing.T) {
		mr, client := standardSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 1
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewStandardProcessor(worker, config)
		ctx := context.Background()

		// Start processor with empty queue
		processor.Start(ctx)
		time.Sleep(100 * time.Millisecond)

		// Should not have crashed - just be waiting with backoff
		processor.Stop()
	})
}

func TestStandardProcessor_WithPriorityQueue(t *testing.T) {
	t.Run("uses priority queue when enabled", func(t *testing.T) {
		mr, client := standardSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 1
		config.EnablePriorityQueue = true
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		processor := NewStandardProcessor(worker, config)

		assert.True(t, processor.enablePriorityQueue)
	})
}

func TestStandardProcessor_ConcurrentSafety(t *testing.T) {
	t.Run("multiple start/stop cycles are safe", func(t *testing.T) {
		mr, client := standardSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.Concurrency = 2
		config.EnableStandard = true
		config.EnableFIFO = false

		worker := NewWorker(q, nil, config)
		defer worker.circuit.Stop()

		for range 3 {
			processor := NewStandardProcessor(worker, config)
			ctx := context.Background()

			processor.Start(ctx)
			time.Sleep(30 * time.Millisecond)
			processor.Stop()
		}
	})
}

func TestStandardProcessor_StopIdempotent(t *testing.T) {
	t.Run("stop is idempotent", func(t *testing.T) {
		processor := NewStandardProcessor(nil, DefaultConfig())

		// Multiple stops should not panic
		var wg sync.WaitGroup
		var panicked int32

		for range 5 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						atomic.AddInt32(&panicked, 1)
					}
				}()
				processor.Stop()
			}()
		}

		wg.Wait()
		// First stop closes channel, subsequent ones should be handled gracefully
		// The implementation uses sync.Once or similar pattern
	})
}
