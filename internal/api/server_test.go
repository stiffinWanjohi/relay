package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

// mockAuthValidator implements auth.APIKeyValidator for testing
type mockAuthValidator struct {
	clientID string
	err      error
}

func (m *mockAuthValidator) ValidateAPIKey(_ context.Context, _ string) (string, error) {
	return m.clientID, m.err
}

// skipIfNoDatabase skips the test if PostgreSQL is not available
func skipIfNoDatabase(t *testing.T) *pgxpool.Pool {
	t.Helper()

	dbURL := os.Getenv("TEST_DATABASE_URL")
	if dbURL == "" {
		dbURL = "postgres://relay:relay@localhost:5432/relay_test?sslmode=disable"
	}

	pool, err := pgxpool.New(context.Background(), dbURL)
	if err != nil {
		t.Skipf("Skipping integration test: database not available: %v", err)
	}

	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Skipf("Skipping integration test: database not reachable: %v", err)
	}

	return pool
}

func setupTestServer(t *testing.T, cfg ServerConfig, authValidator auth.APIKeyValidator) *Server {
	t.Helper()

	pool := skipIfNoDatabase(t)

	mr, err := miniredis.Run()
	if err != nil {
		pool.Close()
		t.Fatalf("failed to start miniredis: %v", err)
	}

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)
	d := dedup.NewChecker(redisClient)

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	t.Cleanup(func() {
		_ = redisClient.Close()
		mr.Close()
		pool.Close()
	})

	return NewServer(store, q, d, authValidator, cfg, logger)
}

func TestNewServer(t *testing.T) {
	cfg := ServerConfig{
		EnableAuth:       false,
		EnablePlayground: true,
	}
	server := setupTestServer(t, cfg, nil)

	if server == nil {
		t.Fatal("expected non-nil server")
	}
	if server.router == nil {
		t.Error("expected router to be set")
	}
	if server.logger == nil {
		t.Error("expected logger to be set")
	}
}

func TestServer_Handler(t *testing.T) {
	cfg := ServerConfig{EnableAuth: false, EnablePlayground: false}
	server := setupTestServer(t, cfg, nil)

	handler := server.Handler()
	if handler == nil {
		t.Fatal("expected non-nil handler")
	}
}

func TestServer_HealthHandler(t *testing.T) {
	cfg := ServerConfig{EnableAuth: false, EnablePlayground: false}
	server := setupTestServer(t, cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	var response map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&response); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if response["status"] != "ok" {
		t.Errorf("expected status 'ok', got %s", response["status"])
	}

	contentType := rec.Header().Get("Content-Type")
	if contentType != "application/json" {
		t.Errorf("expected Content-Type 'application/json', got %s", contentType)
	}
}

func TestServer_PlaygroundEnabled(t *testing.T) {
	cfg := ServerConfig{EnableAuth: false, EnablePlayground: true}
	server := setupTestServer(t, cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/playground", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	// Playground should return 200 with HTML
	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for playground, got %d", rec.Code)
	}
}

func TestServer_PlaygroundDisabled(t *testing.T) {
	cfg := ServerConfig{EnableAuth: false, EnablePlayground: false}
	server := setupTestServer(t, cfg, nil)

	req := httptest.NewRequest(http.MethodGet, "/playground", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	// Playground should return 404 when disabled
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected status 404 for disabled playground, got %d", rec.Code)
	}
}

func TestServer_GraphQLEndpoint(t *testing.T) {
	cfg := ServerConfig{EnableAuth: false, EnablePlayground: false}
	server := setupTestServer(t, cfg, nil)

	// GraphQL endpoint should accept POST
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	// Should return some response (might be error due to empty body, but endpoint exists)
	// Status should not be 404
	if rec.Code == http.StatusNotFound {
		t.Error("expected GraphQL endpoint to exist")
	}
}

func TestServer_GraphQLWithAuth(t *testing.T) {
	validator := &mockAuthValidator{clientID: "test-client", err: nil}
	cfg := ServerConfig{EnableAuth: true, EnablePlayground: false}
	server := setupTestServer(t, cfg, validator)

	// Request without API key should fail
	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 without API key, got %d", rec.Code)
	}

	// Request with API key should succeed (endpoint-wise)
	req = httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("X-API-Key", "valid-key")
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	// Should not be 401 - the request might fail for other reasons but auth passed
	if rec.Code == http.StatusUnauthorized {
		t.Error("expected auth to pass with valid API key")
	}
}

func TestServer_GraphQLWithInvalidAuth(t *testing.T) {
	validator := &mockAuthValidator{clientID: "", err: auth.ErrInvalidAPIKey}
	cfg := ServerConfig{EnableAuth: true, EnablePlayground: false}
	server := setupTestServer(t, cfg, validator)

	req := httptest.NewRequest(http.MethodPost, "/graphql", nil)
	req.Header.Set("X-API-Key", "invalid-key")
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401 with invalid API key, got %d", rec.Code)
	}
}

func TestServer_HealthNoAuth(t *testing.T) {
	validator := &mockAuthValidator{clientID: "test", err: nil}
	cfg := ServerConfig{EnableAuth: true, EnablePlayground: false}
	server := setupTestServer(t, cfg, validator)

	// Health endpoint should work without auth even when auth is enabled
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()

	server.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200 for health without auth, got %d", rec.Code)
	}
}

func TestLoggingMiddleware(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	mw := loggingMiddleware(logger)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("test"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestServerConfig(t *testing.T) {
	cfg := ServerConfig{
		EnableAuth:       true,
		EnablePlayground: true,
	}

	if !cfg.EnableAuth {
		t.Error("expected EnableAuth to be true")
	}
	if !cfg.EnablePlayground {
		t.Error("expected EnablePlayground to be true")
	}
}
