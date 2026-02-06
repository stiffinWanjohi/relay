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
	for i := range b.N {
		ids[i] = uuid.New()
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = q.EnqueueDelayed(ctx, ids[i], 1*time.Second)
		i++
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
	for range 100 {
		_ = q.EnqueueDelayed(ctx, uuid.New(), -1*time.Second) // Already ready
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_ = q.moveDelayedToMain(ctx)
	}
}

func BenchmarkQueueRemoveFromDelayed(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate
	ids := make([]uuid.UUID, b.N)
	for i := range b.N {
		ids[i] = uuid.New()
		_ = q.EnqueueDelayed(ctx, ids[i], 1*time.Hour)
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = q.RemoveFromDelayed(ctx, ids[i])
		i++
	}
}
