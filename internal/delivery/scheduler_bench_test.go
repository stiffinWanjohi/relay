package delivery

import (
	"context"
	"fmt"
	"testing"

	"github.com/redis/go-redis/v9"
)

func setupBenchRedisForScheduler(b *testing.B) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		PoolSize: 100,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		b.Skip("Redis not available, skipping benchmark")
	}

	b.Cleanup(func() {
		client.Del(ctx, drrDeficitKey, drrActiveKey, drrLastServed)
		client.Close()
	})

	return client
}

func BenchmarkDRRSchedulerSelectNextClient(b *testing.B) {
	client := setupBenchRedisForScheduler(b)
	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	// Register some clients
	clients := []string{"client-1", "client-2", "client-3", "client-4", "client-5"}
	for _, c := range clients {
		_ = scheduler.RegisterClient(ctx, c)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = scheduler.SelectNextClient(ctx)
	}
}

func BenchmarkDRRSchedulerRecordDelivery(b *testing.B) {
	client := setupBenchRedisForScheduler(b)
	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	clients := []string{"client-1", "client-2", "client-3"}
	for _, c := range clients {
		_ = scheduler.RegisterClient(ctx, c)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		clientID := clients[i%len(clients)]
		_ = scheduler.RecordDelivery(ctx, clientID, 1024) // 1KB payload
	}
}

func BenchmarkDRRSchedulerFullCycle(b *testing.B) {
	client := setupBenchRedisForScheduler(b)
	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	clients := []string{"client-1", "client-2", "client-3", "client-4", "client-5"}
	for _, c := range clients {
		_ = scheduler.RegisterClient(ctx, c)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		clientID, _ := scheduler.SelectNextClient(ctx)
		if clientID != "" {
			_ = scheduler.RecordDelivery(ctx, clientID, 2048)
		}
	}
}

func BenchmarkDRRSchedulerManyClients(b *testing.B) {
	client := setupBenchRedisForScheduler(b)
	ctx := context.Background()

	for _, numClients := range []int{10, 50, 100, 500} {
		b.Run(fmt.Sprintf("clients-%d", numClients), func(b *testing.B) {
			scheduler := NewDRRScheduler(client, DefaultDRRConfig())

			// Register clients
			for i := 0; i < numClients; i++ {
				_ = scheduler.RegisterClient(ctx, fmt.Sprintf("client-%d", i))
			}

			b.ResetTimer()
			b.ReportAllocs()

			for i := 0; i < b.N; i++ {
				clientID, _ := scheduler.SelectNextClient(ctx)
				if clientID != "" {
					_ = scheduler.RecordDelivery(ctx, clientID, 1024)
				}
			}

			b.StopTimer()
			_ = scheduler.Reset(ctx)
		})
	}
}

func BenchmarkDRRSchedulerGetDeficit(b *testing.B) {
	client := setupBenchRedisForScheduler(b)
	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	clients := []string{"client-1", "client-2", "client-3"}
	for _, c := range clients {
		_ = scheduler.RegisterClient(ctx, c)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = scheduler.GetDeficit(ctx, clients[i%len(clients)])
	}
}

func BenchmarkDRRSchedulerStats(b *testing.B) {
	client := setupBenchRedisForScheduler(b)
	scheduler := NewDRRScheduler(client, DefaultDRRConfig())
	ctx := context.Background()

	// Register 50 clients
	for i := range 50 {
		_ = scheduler.RegisterClient(ctx, fmt.Sprintf("client-%d", i))
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = scheduler.Stats(ctx)
	}
}
