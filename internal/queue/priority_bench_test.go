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
	for i := range b.N {
		ids[i] = uuid.New()
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		priority := (i % 10) + 1 // Cycle through priorities 1-10
		_ = q.EnqueueWithPriority(ctx, ids[i], priority)
		i++
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
	for i := range b.N {
		priority := (i % 10) + 1
		_ = q.EnqueueWithPriority(ctx, uuid.New(), priority)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
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
	for i := range b.N {
		ids[i] = uuid.New()
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		priority := (i % 10) + 1
		_ = q.EnqueueDelayedWithPriority(ctx, ids[i], priority, 1*time.Second)
		i++
	}
}

func BenchmarkQueueGetPriorityStats(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate with various priorities
	for i := range 100 {
		_ = q.EnqueueWithPriority(ctx, uuid.New(), (i%10)+1)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = q.GetPriorityStats(ctx)
	}
}
