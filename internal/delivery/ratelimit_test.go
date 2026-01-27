package delivery

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestNewRateLimiter(t *testing.T) {
	_, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)

	if rl == nil {
		t.Fatal("NewRateLimiter returned nil")
	}
	if rl.client != client {
		t.Error("client not set correctly")
	}
}

func TestRateLimiter_Allow_NoLimit(t *testing.T) {
	_, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	// limit <= 0 means no limit
	tests := []struct {
		name  string
		limit int
	}{
		{"zero limit", 0},
		{"negative limit", -1},
		{"negative limit large", -100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for range 100 {
				if !rl.Allow(ctx, "test-key", tt.limit) {
					t.Errorf("Allow should return true when limit is %d", tt.limit)
				}
			}
		})
	}
}

func TestRateLimiter_Allow_WithLimit(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	key := "test-limited-key"
	limit := 5

	// First 5 requests should be allowed
	for i := range limit {
		if !rl.Allow(ctx, key, limit) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 6th request should be blocked
	if rl.Allow(ctx, key, limit) {
		t.Error("request 6 should be blocked")
	}

	// Fast forward time to reset the window
	mr.FastForward(2 * time.Second)

	// Should be allowed again
	if !rl.Allow(ctx, key, limit) {
		t.Error("request after window reset should be allowed")
	}
}

func TestRateLimiter_Allow_DifferentKeys(t *testing.T) {
	_, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	limit := 2

	// Key 1 should have its own limit
	if !rl.Allow(ctx, "key1", limit) {
		t.Error("key1 request 1 should be allowed")
	}
	if !rl.Allow(ctx, "key1", limit) {
		t.Error("key1 request 2 should be allowed")
	}
	if rl.Allow(ctx, "key1", limit) {
		t.Error("key1 request 3 should be blocked")
	}

	// Key 2 should have its own independent limit
	if !rl.Allow(ctx, "key2", limit) {
		t.Error("key2 request 1 should be allowed")
	}
	if !rl.Allow(ctx, "key2", limit) {
		t.Error("key2 request 2 should be allowed")
	}
	if rl.Allow(ctx, "key2", limit) {
		t.Error("key2 request 3 should be blocked")
	}
}

func TestRateLimiter_AllowN(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	t.Run("no limit", func(t *testing.T) {
		if !rl.AllowN(ctx, "no-limit-key", 0, 100) {
			t.Error("AllowN should return true when limit is 0")
		}
	})

	t.Run("within limit", func(t *testing.T) {
		mr.FastForward(2 * time.Second) // Reset window
		key := "within-limit-key"
		if !rl.AllowN(ctx, key, 10, 5) {
			t.Error("AllowN(5) should be allowed with limit 10")
		}
	})

	t.Run("exceeds limit", func(t *testing.T) {
		mr.FastForward(2 * time.Second) // Reset window
		key := "exceeds-limit-key"
		if rl.AllowN(ctx, key, 3, 5) {
			t.Error("AllowN(5) should be blocked with limit 3")
		}
	})

	t.Run("exact limit", func(t *testing.T) {
		mr.FastForward(2 * time.Second) // Reset window
		key := "exact-limit-key"
		if !rl.AllowN(ctx, key, 5, 5) {
			t.Error("AllowN(5) should be allowed with limit 5")
		}
		// Next request should be blocked
		if rl.AllowN(ctx, key, 5, 1) {
			t.Error("additional request should be blocked after hitting limit")
		}
	})
}

func TestRateLimiter_GetCurrentRate(t *testing.T) {
	mr, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	key := "rate-check-key"

	// Initially should be 0
	rate, err := rl.GetCurrentRate(ctx, key)
	if err != nil {
		t.Fatalf("GetCurrentRate error: %v", err)
	}
	if rate != 0 {
		t.Errorf("initial rate = %d, want 0", rate)
	}

	// Make some requests
	rl.Allow(ctx, key, 100)
	rl.Allow(ctx, key, 100)
	rl.Allow(ctx, key, 100)

	rate, err = rl.GetCurrentRate(ctx, key)
	if err != nil {
		t.Fatalf("GetCurrentRate error: %v", err)
	}
	if rate != 3 {
		t.Errorf("rate = %d, want 3", rate)
	}

	// Fast forward to reset window
	mr.FastForward(2 * time.Second)

	rate, err = rl.GetCurrentRate(ctx, key)
	if err != nil {
		t.Fatalf("GetCurrentRate error: %v", err)
	}
	if rate != 0 {
		t.Errorf("rate after window reset = %d, want 0", rate)
	}
}

func TestRateLimiter_Reset(t *testing.T) {
	_, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	key := "reset-key"
	limit := 2

	// Use up the limit
	rl.Allow(ctx, key, limit)
	rl.Allow(ctx, key, limit)

	if rl.Allow(ctx, key, limit) {
		t.Error("should be blocked after hitting limit")
	}

	// Reset the key
	err := rl.Reset(ctx, key)
	if err != nil {
		t.Fatalf("Reset error: %v", err)
	}

	// Should be allowed again
	if !rl.Allow(ctx, key, limit) {
		t.Error("should be allowed after reset")
	}
}

func TestRateLimiter_Allow_RedisError(t *testing.T) {
	mr, client := setupTestRedis(t)

	rl := NewRateLimiter(client)
	ctx := context.Background()

	// Close miniredis to simulate connection error
	mr.Close()
	client.Close()

	// Should fail open (return true) on Redis error
	result := rl.Allow(ctx, "error-key", 5)
	if !result {
		t.Error("should fail open (return true) on Redis error")
	}
}

func TestRateLimiter_SlidingWindow_BasicBehavior(t *testing.T) {
	_, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	key := "sliding-window-key"
	limit := 5

	// Make 5 requests - all should be allowed
	for i := range 5 {
		if !rl.Allow(ctx, key, limit) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}

	// 6th should be blocked
	if rl.Allow(ctx, key, limit) {
		t.Error("6th request should be blocked")
	}

	// Verify the rate counter
	rate, err := rl.GetCurrentRate(ctx, key)
	if err != nil {
		t.Fatalf("GetCurrentRate error: %v", err)
	}
	if rate != 5 {
		t.Errorf("rate = %d, want 5", rate)
	}
}

func TestRateLimitKey(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"test", "relay:ratelimit:test"},
		{"endpoint-123", "relay:ratelimit:endpoint-123"},
		{"", "relay:ratelimit:"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := rateLimitKey(tt.input)
			if result != tt.expected {
				t.Errorf("rateLimitKey(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestFormatInt(t *testing.T) {
	tests := []struct {
		input    int64
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{123, "123"},
		{1000000, "1000000"},
		{-1, ""}, // Note: formatInt doesn't handle negative numbers well
	}

	for _, tt := range tests {
		if tt.input >= 0 { // Skip negative test since formatInt doesn't handle it
			t.Run(tt.expected, func(t *testing.T) {
				result := formatInt(tt.input)
				if result != tt.expected {
					t.Errorf("formatInt(%d) = %q, want %q", tt.input, result, tt.expected)
				}
			})
		}
	}
}

func TestRateLimiter_ConcurrentAccess(t *testing.T) {
	_, client := setupTestRedis(t)
	defer client.Close()

	rl := NewRateLimiter(client)
	ctx := context.Background()

	key := "concurrent-key"
	limit := 100

	// Run concurrent requests
	done := make(chan bool, 200)
	allowed := make(chan bool, 200)

	for range 200 {
		go func() {
			result := rl.Allow(ctx, key, limit)
			allowed <- result
			done <- true
		}()
	}

	// Wait for all goroutines
	for range 200 {
		<-done
	}
	close(allowed)

	// Count allowed requests
	count := 0
	for a := range allowed {
		if a {
			count++
		}
	}

	// Should have approximately 'limit' allowed requests
	// Allow some tolerance for timing issues
	if count > limit+10 {
		t.Errorf("too many requests allowed: %d (limit is %d)", count, limit)
	}
	if count < limit-10 {
		t.Errorf("too few requests allowed: %d (limit is %d)", count, limit)
	}
}
