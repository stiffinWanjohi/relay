package delivery

import (
	"context"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// RateLimiter provides distributed rate limiting using Redis.
// Uses a sliding window algorithm for accurate rate limiting.
type RateLimiter struct {
	client *redis.Client
}

// NewRateLimiter creates a new Redis-backed rate limiter.
func NewRateLimiter(client *redis.Client) *RateLimiter {
	return &RateLimiter{client: client}
}

// Allow checks if a request is allowed under the rate limit.
// Returns true if allowed, false if rate limited.
// limit is requests per second, 0 means unlimited.
func (rl *RateLimiter) Allow(ctx context.Context, key string, limit int) bool {
	if limit <= 0 {
		return true // No limit
	}

	allowed, err := rl.allowSlidingWindow(ctx, key, limit)
	if err != nil {
		// On error, allow the request (fail open)
		return true
	}
	return allowed
}

// AllowN checks if n requests are allowed under the rate limit.
func (rl *RateLimiter) AllowN(ctx context.Context, key string, limit int, n int) bool {
	if limit <= 0 {
		return true
	}

	for range n {
		if !rl.Allow(ctx, key, limit) {
			return false
		}
	}
	return true
}

// Lua script for sliding window rate limiting.
// This provides more accurate rate limiting than fixed windows.
var slidingWindowScript = redis.NewScript(`
	local key = KEYS[1]
	local now = tonumber(ARGV[1])
	local window = tonumber(ARGV[2])
	local limit = tonumber(ARGV[3])
	
	-- Remove old entries outside the window
	redis.call('ZREMRANGEBYSCORE', key, '-inf', now - window)
	
	-- Count current requests in window
	local count = redis.call('ZCARD', key)
	
	if count < limit then
		-- Add this request
		redis.call('ZADD', key, now, now .. '-' .. math.random())
		-- Set expiry on the key
		redis.call('EXPIRE', key, window / 1000 + 1)
		return 1
	end
	
	return 0
`)

func (rl *RateLimiter) allowSlidingWindow(ctx context.Context, key string, limit int) (bool, error) {
	now := time.Now().UnixMilli()
	window := int64(1000) // 1 second window in milliseconds

	result, err := slidingWindowScript.Run(ctx, rl.client,
		[]string{rateLimitKey(key)},
		now,
		window,
		limit,
	).Int()

	if err != nil {
		return false, err
	}

	return result == 1, nil
}

// GetCurrentRate returns the current request count in the window.
func (rl *RateLimiter) GetCurrentRate(ctx context.Context, key string) (int64, error) {
	now := time.Now().UnixMilli()
	window := int64(1000) // 1 second window

	// Remove old entries and count
	pipe := rl.client.Pipeline()
	pipe.ZRemRangeByScore(ctx, rateLimitKey(key), "-inf", formatInt(now-window))
	countCmd := pipe.ZCard(ctx, rateLimitKey(key))

	if _, err := pipe.Exec(ctx); err != nil {
		return 0, err
	}

	return countCmd.Val(), nil
}

// Reset clears the rate limit counter for a key.
func (rl *RateLimiter) Reset(ctx context.Context, key string) error {
	return rl.client.Del(ctx, rateLimitKey(key)).Err()
}

func rateLimitKey(key string) string {
	return "relay:ratelimit:" + key
}

func formatInt(i int64) string {
	return strconv.FormatInt(i, 10)
}
