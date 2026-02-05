package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/delivery"
)

func setupTestRateLimiter(t *testing.T) (*delivery.RateLimiter, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	limiter := delivery.NewRateLimiter(client)
	cleanup := func() {
		client.Close()
		mr.Close()
	}
	return limiter, cleanup
}

func TestRateLimitMiddleware_NoLimits(t *testing.T) {
	limiter, cleanup := setupTestRateLimiter(t)
	defer cleanup()

	cfg := RateLimitConfig{
		GlobalLimit: 0,
		ClientLimit: 0,
	}

	handler := RateLimitMiddleware(limiter, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Should always pass through with no limits
	for i := 0; i < 100; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request %d: expected status 200, got %d", i, rr.Code)
		}
	}
}

func TestRateLimitMiddleware_GlobalLimit(t *testing.T) {
	limiter, cleanup := setupTestRateLimiter(t)
	defer cleanup()

	cfg := RateLimitConfig{
		GlobalLimit: 5,
		ClientLimit: 0,
	}

	successCount := 0
	rateLimitedCount := 0

	handler := RateLimitMiddleware(limiter, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Send 10 requests, only 5 should succeed
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusOK {
			successCount++
		} else if rr.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	if successCount != 5 {
		t.Errorf("expected 5 successful requests, got %d", successCount)
	}
	if rateLimitedCount != 5 {
		t.Errorf("expected 5 rate-limited requests, got %d", rateLimitedCount)
	}
}

func TestRateLimitMiddleware_ClientLimit(t *testing.T) {
	limiter, cleanup := setupTestRateLimiter(t)
	defer cleanup()

	cfg := RateLimitConfig{
		GlobalLimit: 0,
		ClientLimit: 3,
	}

	handler := RateLimitMiddleware(limiter, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Requests without client ID should pass through (no client limit applied)
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Errorf("request without client ID %d: expected status 200, got %d", i, rr.Code)
		}
	}

	// Requests with client ID should be limited
	successCount := 0
	rateLimitedCount := 0

	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx := auth.WithClientID(req.Context(), "test-client")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusOK {
			successCount++
		} else if rr.Code == http.StatusTooManyRequests {
			rateLimitedCount++
		}
	}

	if successCount != 3 {
		t.Errorf("expected 3 successful requests, got %d", successCount)
	}
	if rateLimitedCount != 7 {
		t.Errorf("expected 7 rate-limited requests, got %d", rateLimitedCount)
	}
}

func TestRateLimitMiddleware_DifferentClients(t *testing.T) {
	limiter, cleanup := setupTestRateLimiter(t)
	defer cleanup()

	cfg := RateLimitConfig{
		GlobalLimit: 0,
		ClientLimit: 2,
	}

	handler := RateLimitMiddleware(limiter, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// Each client should have independent limits
	clients := []string{"client-a", "client-b", "client-c"}
	for _, clientID := range clients {
		successCount := 0
		for i := 0; i < 5; i++ {
			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			ctx := auth.WithClientID(req.Context(), clientID)
			req = req.WithContext(ctx)
			rr := httptest.NewRecorder()
			handler.ServeHTTP(rr, req)
			if rr.Code == http.StatusOK {
				successCount++
			}
		}
		if successCount != 2 {
			t.Errorf("client %s: expected 2 successful requests, got %d", clientID, successCount)
		}
	}
}

func TestRateLimitMiddleware_BothLimits(t *testing.T) {
	limiter, cleanup := setupTestRateLimiter(t)
	defer cleanup()

	// Global limit is tighter than client limit
	cfg := RateLimitConfig{
		GlobalLimit: 3,
		ClientLimit: 10,
	}

	handler := RateLimitMiddleware(limiter, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	successCount := 0
	for i := 0; i < 10; i++ {
		req := httptest.NewRequest(http.MethodGet, "/test", nil)
		ctx := auth.WithClientID(req.Context(), "test-client")
		req = req.WithContext(ctx)
		rr := httptest.NewRecorder()
		handler.ServeHTTP(rr, req)
		if rr.Code == http.StatusOK {
			successCount++
		}
	}

	// Global limit (3) should kick in before client limit (10)
	if successCount != 3 {
		t.Errorf("expected 3 successful requests (global limit), got %d", successCount)
	}
}

func TestRateLimitMiddleware_ResponseHeaders(t *testing.T) {
	limiter, cleanup := setupTestRateLimiter(t)
	defer cleanup()

	cfg := RateLimitConfig{
		GlobalLimit: 1,
		ClientLimit: 0,
	}

	handler := RateLimitMiddleware(limiter, cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	// First request should pass
	req1 := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr1 := httptest.NewRecorder()
	handler.ServeHTTP(rr1, req1)
	if rr1.Code != http.StatusOK {
		t.Errorf("first request: expected status 200, got %d", rr1.Code)
	}

	// Second request should be rate limited with proper headers
	req2 := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr2 := httptest.NewRecorder()
	handler.ServeHTTP(rr2, req2)

	if rr2.Code != http.StatusTooManyRequests {
		t.Errorf("second request: expected status 429, got %d", rr2.Code)
	}

	retryAfter := rr2.Header().Get("Retry-After")
	if retryAfter == "" {
		t.Error("expected Retry-After header")
	}

	rateLimitLimit := rr2.Header().Get("X-RateLimit-Limit")
	if rateLimitLimit != "1" {
		t.Errorf("expected X-RateLimit-Limit to be 1, got %s", rateLimitLimit)
	}

	rateLimitReset := rr2.Header().Get("X-RateLimit-Reset")
	if rateLimitReset == "" {
		t.Error("expected X-RateLimit-Reset header")
	}

	contentType := rr2.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %s", contentType)
	}
}

// Helper function to add client ID to context (need to check if auth package exports this)
func init() {
	// Verify auth.WithClientID exists
	_ = auth.WithClientID(context.Background(), "test")
}
