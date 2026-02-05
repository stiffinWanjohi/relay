package api

import (
	"net/http"
	"strconv"
	"time"

	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/delivery"
)

const (
	// globalRateLimitKey is the Redis key for global rate limiting.
	globalRateLimitKey = "global"

	// rateLimitClientKeyPrefix is the prefix for per-client rate limit keys.
	rateLimitClientKeyPrefix = "client:"
)

// RateLimitConfig holds rate limiting configuration.
type RateLimitConfig struct {
	GlobalLimit int // Global requests per second (0 = unlimited)
	ClientLimit int // Per-client requests per second (0 = unlimited)
}

// RateLimitMiddleware creates HTTP middleware for rate limiting.
// It applies both global and per-client rate limits using Redis.
func RateLimitMiddleware(limiter *delivery.RateLimiter, cfg RateLimitConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()

			// Check global rate limit
			if cfg.GlobalLimit > 0 {
				if !limiter.Allow(ctx, globalRateLimitKey, cfg.GlobalLimit) {
					apiLog.Warn("global rate limit exceeded",
						"path", r.URL.Path,
						"method", r.Method,
						"remote_addr", r.RemoteAddr,
					)
					writeRateLimitResponse(w, cfg.GlobalLimit)
					return
				}
			}

			// Check per-client rate limit
			if cfg.ClientLimit > 0 {
				clientID, ok := auth.ClientIDFromContext(ctx)
				if ok && clientID != "" {
					clientKey := rateLimitClientKeyPrefix + clientID
					if !limiter.Allow(ctx, clientKey, cfg.ClientLimit) {
						apiLog.Warn("client rate limit exceeded",
							"client_id", clientID,
							"path", r.URL.Path,
							"method", r.Method,
						)
						writeRateLimitResponse(w, cfg.ClientLimit)
						return
					}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// writeRateLimitResponse writes a 429 Too Many Requests response.
func writeRateLimitResponse(w http.ResponseWriter, limit int) {
	// Calculate Retry-After based on rate limit window (1 second)
	retryAfter := 1
	if limit > 0 {
		// Suggest retry after the window resets
		retryAfter = 1
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Retry-After", strconv.Itoa(retryAfter))
	w.Header().Set("X-RateLimit-Limit", strconv.Itoa(limit))
	w.Header().Set("X-RateLimit-Reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
	w.WriteHeader(http.StatusTooManyRequests)
	_, _ = w.Write([]byte(`{"error":"rate limit exceeded","retry_after":` + strconv.Itoa(retryAfter) + `}`))
}
