package queue

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
)

func BenchmarkQueueEnqueueFIFO(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"

	ids := make([]uuid.UUID, b.N)
	for i := range b.N {
		ids[i] = uuid.New()
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = q.EnqueueFIFO(ctx, endpointID, "", ids[i])
		i++
	}
}

func BenchmarkQueueEnqueueFIFOParallel(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine uses different endpoint to avoid lock contention
		endpointID := fmt.Sprintf("bench-endpoint-%d", uuid.New().ID())
		for pb.Next() {
			_ = q.EnqueueFIFO(ctx, endpointID, "", uuid.New())
		}
	})
}

func BenchmarkQueueEnqueueFIFOWithPartition(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"
	partitions := []string{"p1", "p2", "p3", "p4", "p5"}

	ids := make([]uuid.UUID, b.N)
	for i := range b.N {
		ids[i] = uuid.New()
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		partition := partitions[i%len(partitions)]
		_ = q.EnqueueFIFO(ctx, endpointID, partition, ids[i])
		i++
	}
}

func BenchmarkQueueDequeueFIFO(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"

	// Pre-populate
	for b.Loop() {
		_ = q.EnqueueFIFO(ctx, endpointID, "", uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, err := q.DequeueFIFO(ctx, endpointID, "")
		if err == nil {
			_ = q.AckFIFO(ctx, endpointID, "")
		}
	}
}

func BenchmarkQueueFIFOCycle(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		eventID := uuid.New()
		_ = q.EnqueueFIFO(ctx, endpointID, "", eventID)
		_, err := q.DequeueFIFO(ctx, endpointID, "")
		if err == nil {
			_ = q.AckFIFO(ctx, endpointID, "")
		}
	}
}

func BenchmarkQueueAckFIFO(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"

	// Pre-populate and dequeue
	for b.Loop() {
		_ = q.EnqueueFIFO(ctx, endpointID, "", uuid.New())
		_, _ = q.DequeueFIFO(ctx, endpointID, "")
		_ = q.AckFIFO(ctx, endpointID, "") // Ack so we can dequeue again
		_ = q.EnqueueFIFO(ctx, endpointID, "", uuid.New())
	}

	// Dequeue all
	for b.Loop() {
		_, _ = q.DequeueFIFO(ctx, endpointID, "")
	}

	// Now we can't dequeue anymore (locked), so we only have 1 message being processed
	// This benchmark isn't ideal, let's use different endpoints
	client.FlushDB(ctx)

	for i := range b.N {
		ep := fmt.Sprintf("ep-%d", i)
		_ = q.EnqueueFIFO(ctx, ep, "", uuid.New())
		_, _ = q.DequeueFIFO(ctx, ep, "")
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		ep := fmt.Sprintf("ep-%d", i)
		_ = q.AckFIFO(ctx, ep, "")
		i++
	}
}

func BenchmarkQueueNackFIFO(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Setup: create endpoint with dequeued message for each iteration
	msgs := make([]*Message, b.N)
	for i := range b.N {
		ep := fmt.Sprintf("ep-%d", i)
		_ = q.EnqueueFIFO(ctx, ep, "", uuid.New())
		msgs[i], _ = q.DequeueFIFO(ctx, ep, "")
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		ep := fmt.Sprintf("ep-%d", i)
		if msgs[i] != nil {
			_ = q.NackFIFO(ctx, ep, "", msgs[i], 0)
		}
		i++
	}
}

func BenchmarkQueueGetFIFOQueueLength(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"

	// Pre-populate
	for range 100 {
		_ = q.EnqueueFIFO(ctx, endpointID, "", uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = q.GetFIFOQueueLength(ctx, endpointID, "")
	}
}

func BenchmarkQueueIsFIFOLocked(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"

	// Create a lock
	_ = q.EnqueueFIFO(ctx, endpointID, "", uuid.New())
	_, _ = q.DequeueFIFO(ctx, endpointID, "")

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = q.IsFIFOLocked(ctx, endpointID, "")
	}
}

func BenchmarkQueueGetFIFOQueueStats(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"

	// Pre-populate
	for range 100 {
		_ = q.EnqueueFIFO(ctx, endpointID, "", uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = q.GetFIFOQueueStats(ctx, endpointID, "")
	}
}

func BenchmarkQueueMultiplePartitions(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()
	endpointID := "bench-endpoint"
	numPartitions := 10

	// Pre-populate each partition
	for p := range numPartitions {
		partition := fmt.Sprintf("partition-%d", p)
		for range 10 {
			_ = q.EnqueueFIFO(ctx, endpointID, partition, uuid.New())
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		partition := fmt.Sprintf("partition-%d", i%numPartitions)
		msg, err := q.DequeueFIFO(ctx, endpointID, partition)
		if err == nil && msg != nil {
			_ = q.AckFIFO(ctx, endpointID, partition)
		}
		i++
	}
}
