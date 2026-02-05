package queue

import (
	"context"
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
		client.Del(ctx, priorityHighKey, priorityNormalKey, priorityLowKey)
		client.Del(ctx, activeClientsKey)
		// Clean up client queues
		keys, _ := client.Keys(ctx, clientQueuePrefix+"*").Result()
		if len(keys) > 0 {
			client.Del(ctx, keys...)
		}
		// Clean up FIFO queues
		fifoKeys, _ := client.Keys(ctx, "relay:queue:fifo:*").Result()
		if len(fifoKeys) > 0 {
			client.Del(ctx, fifoKeys...)
		}
		fifoLocks, _ := client.Keys(ctx, "relay:lock:fifo:*").Result()
		if len(fifoLocks) > 0 {
			client.Del(ctx, fifoLocks...)
		}
		fifoInflight, _ := client.Keys(ctx, "relay:inflight:fifo:*").Result()
		if len(fifoInflight) > 0 {
			client.Del(ctx, fifoInflight...)
		}
		_ = client.Close()
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

func BenchmarkQueueAck(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()

	// Pre-populate and dequeue messages
	msgs := make([]*Message, b.N)
	for i := 0; i < b.N; i++ {
		_ = q.Enqueue(ctx, uuid.New())
		msgs[i], _ = q.Dequeue(ctx)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		if msgs[i] != nil {
			_ = q.Ack(ctx, msgs[i])
		}
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

func BenchmarkQueueNackNoDelay(b *testing.B) {
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
			_ = q.Nack(ctx, msgs[i], 0)
		}
	}
}
