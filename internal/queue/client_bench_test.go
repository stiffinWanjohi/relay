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

	for i := 0; i < b.N; i++ {
		clientID := clients[i%len(clients)]
		_ = q.EnqueueForClient(ctx, clientID, uuid.New())
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
	for i := 0; i < b.N; i++ {
		_ = q.EnqueueForClient(ctx, clientID, uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
	for i := 0; i < 100; i++ {
		_ = q.EnqueueForClient(ctx, fmt.Sprintf("client-%d", i), uuid.New())
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = q.GetActiveClients(ctx)
	}
}

func BenchmarkQueueClientStats(b *testing.B) {
	client := setupBenchRedis(b)
	q := NewQueue(client)
	ctx := context.Background()

	// Pre-populate with clients
	for i := 0; i < 10; i++ {
		clientID := fmt.Sprintf("client-%d", i)
		for j := 0; j < 10; j++ {
			_ = q.EnqueueForClient(ctx, clientID, uuid.New())
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
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
	for i := 0; i < numClients; i++ {
		clients[i] = fmt.Sprintf("client-%d", i)
		for j := 0; j < b.N/numClients+1; j++ {
			_ = q.EnqueueForClient(ctx, clients[i], uuid.New())
		}
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		clientID := clients[i%numClients]
		msg, err := q.DequeueFromClient(ctx, clientID)
		if err == nil && msg != nil {
			_ = q.Ack(ctx, msg)
		}
	}
}
