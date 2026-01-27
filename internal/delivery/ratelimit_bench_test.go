package delivery

import (
	"context"
	"fmt"
	"sync"
	"testing"

	"github.com/redis/go-redis/v9"
)

func setupBenchRedisForRateLimit(b *testing.B) *redis.Client {
	client := redis.NewClient(&redis.Options{
		Addr:     "localhost:6379",
		PoolSize: 100,
	})

	ctx := context.Background()
	if err := client.Ping(ctx).Err(); err != nil {
		b.Skip("Redis not available, skipping benchmark")
	}

	b.Cleanup(func() {
		// Clean up rate limit keys
		keys, _ := client.Keys(ctx, "relay:ratelimit:*").Result()
		if len(keys) > 0 {
			client.Del(ctx, keys...)
		}
		client.Close()
	})

	return client
}

func BenchmarkRateLimiterAllow(b *testing.B) {
	client := setupBenchRedisForRateLimit(b)
	rl := NewRateLimiter(client)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_ = rl.Allow(ctx, "bench-key", 10000) // High limit to avoid blocking
	}
}

func BenchmarkRateLimiterAllowParallel(b *testing.B) {
	client := setupBenchRedisForRateLimit(b)
	rl := NewRateLimiter(client)
	ctx := context.Background()

	b.ResetTimer()
	b.ReportAllocs()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			_ = rl.Allow(ctx, "bench-key-parallel", 100000)
		}
	})
}

func BenchmarkRateLimiterMultipleKeys(b *testing.B) {
	client := setupBenchRedisForRateLimit(b)
	rl := NewRateLimiter(client)
	ctx := context.Background()

	keys := make([]string, 100)
	for i := 0; i < 100; i++ {
		keys[i] = fmt.Sprintf("endpoint-%d", i)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		key := keys[i%len(keys)]
		_ = rl.Allow(ctx, key, 1000)
	}
}

func BenchmarkRateLimiterUnderLoad(b *testing.B) {
	client := setupBenchRedisForRateLimit(b)
	rl := NewRateLimiter(client)
	ctx := context.Background()

	// Simulate realistic scenario: 10 endpoints, each with different limits
	endpoints := []struct {
		key   string
		limit int
	}{
		{"endpoint-high-volume", 10000},
		{"endpoint-medium-1", 1000},
		{"endpoint-medium-2", 1000},
		{"endpoint-medium-3", 1000},
		{"endpoint-low-1", 100},
		{"endpoint-low-2", 100},
		{"endpoint-low-3", 100},
		{"endpoint-low-4", 100},
		{"endpoint-low-5", 100},
		{"endpoint-low-6", 100},
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		ep := endpoints[i%len(endpoints)]
		_ = rl.Allow(ctx, ep.key, ep.limit)
	}
}

func BenchmarkRateLimiterContention(b *testing.B) {
	// Benchmark with high contention on single key
	client := setupBenchRedisForRateLimit(b)
	rl := NewRateLimiter(client)
	ctx := context.Background()

	for _, goroutines := range []int{1, 4, 8, 16, 32} {
		b.Run(fmt.Sprintf("goroutines-%d", goroutines), func(b *testing.B) {
			var wg sync.WaitGroup
			ops := b.N / goroutines

			b.ResetTimer()

			for g := 0; g < goroutines; g++ {
				wg.Add(1)
				go func() {
					defer wg.Done()
					for i := 0; i < ops; i++ {
						_ = rl.Allow(ctx, "contention-key", 100000)
					}
				}()
			}
			wg.Wait()
		})
	}
}

func BenchmarkRateLimiterGetCurrentRate(b *testing.B) {
	client := setupBenchRedisForRateLimit(b)
	rl := NewRateLimiter(client)
	ctx := context.Background()

	// Warm up with some requests
	for i := 0; i < 50; i++ {
		_ = rl.Allow(ctx, "rate-check-key", 1000)
	}

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		_, _ = rl.GetCurrentRate(ctx, "rate-check-key")
	}
}
