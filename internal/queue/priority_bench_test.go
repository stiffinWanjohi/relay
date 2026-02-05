package queue

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

func BenchmarkQueueEnqueueWithPriority(b *testing.B) {
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
		priority := (i % 10) + 1 // Cycle through priorities 1-10
		_ = q.EnqueueWithPriority(ctx, ids[i], priority)
	}
}

func BenchmarkQueueEnqueueWithPriorityParallel(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			priority := (i % 10) + 1
			_ = q.EnqueueWithPriority(ctx, uuid.New(), priority)
			i++
		}
	})
}

func BenchmarkQueueDequeueWithPriority(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()

	// Pre-populate with mixed priorities
	for i := 0; i < b.N; i++ {
		priority := (i % 10) + 1
		_ = q.EnqueueWithPriority(ctx, uuid.New(), priority)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		msg, err := q.DequeueWithPriority(ctx)
		if err == nil && msg != nil {
			_ = q.Ack(ctx, msg)
		}
	}
}

func BenchmarkQueueEnqueueDelayedWithPriority(b *testing.B) {
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
		priority := (i % 10) + 1
		_ = q.EnqueueDelayedWithPriority(ctx, ids[i], priority, 1*time.Second)
	}
}

func BenchmarkQueueGetPriorityStats(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate with various priorities
	for i := 0; i < 100; i++ {
		_ = q.EnqueueWithPriority(ctx, uuid.New(), (i%10)+1)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = q.GetPriorityStats(ctx)
	}
}
