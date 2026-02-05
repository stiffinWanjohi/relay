package queue

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

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

func BenchmarkQueueEnqueueDelayedParallel(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = q.EnqueueDelayed(ctx, uuid.New(), 1*time.Second)
		}
	})
}

func BenchmarkQueueMoveDelayedToMain(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate with ready delayed messages
	for i := 0; i < 100; i++ {
		_ = q.EnqueueDelayed(ctx, uuid.New(), -1*time.Second) // Already ready
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = q.moveDelayedToMain(ctx)
	}
}

func BenchmarkQueueRemoveFromDelayed(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate
	ids := make([]uuid.UUID, b.N)
	for i := 0; i < b.N; i++ {
		ids[i] = uuid.New()
		_ = q.EnqueueDelayed(ctx, ids[i], 1*time.Hour)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = q.RemoveFromDelayed(ctx, ids[i])
	}
}
