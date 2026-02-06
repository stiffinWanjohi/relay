package queue

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/uuid"
)

func BenchmarkQueueEnqueueForClient(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	clients := []string{"client-1", "client-2", "client-3", "client-4", "client-5"}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		clientID := clients[i%len(clients)]
		_ = q.EnqueueForClient(ctx, clientID, uuid.New())
		i++
	}
}

func BenchmarkQueueEnqueueForClientParallel(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	clients := []string{"client-1", "client-2", "client-3", "client-4", "client-5"}

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			clientID := clients[i%len(clients)]
			_ = q.EnqueueForClient(ctx, clientID, uuid.New())
			i++
		}
	})
}

func BenchmarkQueueDequeueFromClient(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()
	clientID := "bench-client"

	// Pre-populate
	for b.Loop() {
		_ = q.EnqueueForClient(ctx, clientID, uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		msg, err := q.DequeueFromClient(ctx, clientID)
		if err == nil && msg != nil {
			_ = q.Ack(ctx, msg)
		}
	}
}

func BenchmarkQueueGetActiveClients(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate with clients
	for i := range 100 {
		_ = q.EnqueueForClient(ctx, fmt.Sprintf("client-%d", i), uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = q.GetActiveClients(ctx)
	}
}

func BenchmarkQueueClientStats(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate with clients
	for i := range 10 {
		clientID := fmt.Sprintf("client-%d", i)
		for range 10 {
			_ = q.EnqueueForClient(ctx, clientID, uuid.New())
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		_, _ = q.ClientStats(ctx)
	}
}

func BenchmarkQueueRoundRobinClients(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client).WithBlockingTimeout(10 * time.Millisecond)
	ctx := context.Background()

	// Setup clients with messages
	numClients := 5
	clients := make([]string, numClients)
	for i := range numClients {
		clients[i] = fmt.Sprintf("client-%d", i)
		for range b.N/numClients + 1 {
			_ = q.EnqueueForClient(ctx, clients[i], uuid.New())
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		clientID := clients[i%numClients]
		msg, err := q.DequeueFromClient(ctx, clientID)
		if err == nil && msg != nil {
			_ = q.Ack(ctx, msg)
		}
		i++
	}
}
