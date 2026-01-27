package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// setupBenchRedis creates a Redis client for benchmarks.
// Requires Redis running on localhost:6379
func setupBenchRedis(b *testing.B) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		PoolSize: 100,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		b.Skip("Redis not available, skipping benchmark")
	}

	// Clean up test keys
	b.Cleanup(func() {
		client.Del(ctx, mainQueueKey, processingQueueKey, delayedQueueKey)
		client.Close()
	})

	return client
}

func BenchmarkQueueEnqueue(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-generate UUIDs to avoid measuring UUID generation
	ids := make([]uuid.UUID, b.N)
	for i := 0; i < b.N; i++ {
		ids[i] = uuid.New()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = q.Enqueue(ctx, ids[i])
	}
}

func BenchmarkQueueEnqueueParallel(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = q.Enqueue(ctx, uuid.New())
		}
	})
}

func BenchmarkQueueDequeue(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()

	// Pre-populate queue
	for i := 0; i < b.N; i++ {
		_ = q.Enqueue(ctx, uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg, err := q.Dequeue(ctx)
		if err == nil && msg != nil {
			_ = q.Ack(ctx, msg)
		}
	}
}

func BenchmarkQueueEnqueueDequeue(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		id := uuid.New()
		_ = q.Enqueue(ctx, id)
		msg, err := q.Dequeue(ctx)
		if err == nil && msg != nil {
			_ = q.Ack(ctx, msg)
		}
	}
}

func BenchmarkQueueEnqueueDelayed(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	ids := make([]uuid.UUID, b.N)
	for i := 0; i < b.N; i++ {
		ids[i] = uuid.New()
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = q.EnqueueDelayed(ctx, ids[i], 1*time.Second)
	}
}

func BenchmarkQueueStats(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Add some items
	for i := 0; i < 100; i++ {
		_ = q.Enqueue(ctx, uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = q.Stats(ctx)
	}
}

func BenchmarkQueueNack(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()

	// Pre-populate and dequeue
	msgs := make([]*Message, b.N)
	for i := 0; i < b.N; i++ {
		_ = q.Enqueue(ctx, uuid.New())
		msgs[i], _ = q.Dequeue(ctx)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if msgs[i] != nil {
			_ = q.Nack(ctx, msgs[i], 100*time.Millisecond)
		}
	}
}

// Benchmark per-client queue operations
func BenchmarkQueueEnqueueForClient(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	clients := []string{"client-1", "client-2", "client-3", "client-4", "client-5"}

	b.Cleanup(func() {
		for _, c := range clients {
			client.Del(ctx, clientQueuePrefix+c)
		}
		client.Del(ctx, activeClientsKey)
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		clientID := clients[i%len(clients)]
		_ = q.EnqueueForClient(ctx, clientID, uuid.New())
	}
}

func BenchmarkQueueThroughput(b *testing.B) {
	// This benchmark measures sustained throughput
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(1 * time.Millisecond)
	ctx := context.Background()

	// Use multiple goroutines to simulate real load
	for _, workers := range []int{1, 2, 4, 8} {
		b.Run(fmt.Sprintf("workers-%d", workers), func(b *testing.B) {
			// Pre-populate
			for i := 0; i < b.N; i++ {
				_ = q.Enqueue(ctx, uuid.New())
			}

			b.ResetTimer()

			done := make(chan struct{})
			count := make(chan int, workers)

			for w := 0; w < workers; w++ {
				go func() {
					processed := 0
					for {
						select {
						case <-done:
							count <- processed
							return
						default:
							msg, err := q.Dequeue(ctx)
							if err == nil && msg != nil {
								_ = q.Ack(ctx, msg)
								processed++
							}
						}
					}
				}()
			}

			time.Sleep(time.Duration(b.N) * time.Microsecond * 10)
			close(done)

			total := 0
			for w := 0; w < workers; w++ {
				total += <-count
			}
		})
	}
}
