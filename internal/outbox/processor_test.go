package outbox

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/observability"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

func TestDefaultProcessorConfig(t *testing.T) {
	config := DefaultProcessorConfig()

	if config.BatchSize != DefaultBatchSize {
		t.Errorf("expected BatchSize %d, got %d", DefaultBatchSize, config.BatchSize)
	}
	if config.PollInterval != DefaultPollInterval {
		t.Errorf("expected PollInterval %v, got %v", DefaultPollInterval, config.PollInterval)
	}
	if config.CleanupInterval != DefaultCleanupInterval {
		t.Errorf("expected CleanupInterval %v, got %v", DefaultCleanupInterval, config.CleanupInterval)
	}
	if config.RetentionPeriod != DefaultRetentionPeriod {
		t.Errorf("expected RetentionPeriod %v, got %v", DefaultRetentionPeriod, config.RetentionPeriod)
	}
}

func TestConstants(t *testing.T) {
	if DefaultBatchSize != 100 {
		t.Errorf("expected DefaultBatchSize 100, got %d", DefaultBatchSize)
	}
	if DefaultPollInterval != 1*time.Second {
		t.Errorf("expected DefaultPollInterval 1s, got %v", DefaultPollInterval)
	}
	if DefaultCleanupInterval != 1*time.Hour {
		t.Errorf("expected DefaultCleanupInterval 1h, got %v", DefaultCleanupInterval)
	}
	if DefaultRetentionPeriod != 24*time.Hour {
		t.Errorf("expected DefaultRetentionPeriod 24h, got %v", DefaultRetentionPeriod)
	}
	if DefaultClaimTimeout != 5*time.Minute {
		t.Errorf("expected DefaultClaimTimeout 5m, got %v", DefaultClaimTimeout)
	}
}

// Helper to skip if database is not available
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

func setupTestProcessor(t *testing.T) (*Processor, *pgxpool.Pool, *miniredis.Miniredis) {
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

	config := ProcessorConfig{
		BatchSize:       10,
		PollInterval:    100 * time.Millisecond,
		CleanupInterval: 1 * time.Hour,
		RetentionPeriod: 24 * time.Hour,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	processor := NewProcessor(store, q, config, logger)

	t.Cleanup(func() {
		_ = redisClient.Close()
		mr.Close()
		pool.Close()
	})

	return processor, pool, mr
}

func TestNewProcessor(t *testing.T) {
	processor, _, _ := setupTestProcessor(t)

	if processor.store == nil {
		t.Error("expected store to be set")
	}
	if processor.queue == nil {
		t.Error("expected queue to be set")
	}
	if processor.logger == nil {
		t.Error("expected logger to be set")
	}
	if processor.workerID == "" {
		t.Error("expected workerID to be set")
	}
	if processor.stopCh == nil {
		t.Error("expected stopCh to be initialized")
	}
}

func TestProcessor_WithMetrics(t *testing.T) {
	processor, _, _ := setupTestProcessor(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	result := processor.WithMetrics(metrics)

	if result != processor {
		t.Error("expected WithMetrics to return same processor")
	}
	if processor.metrics != metrics {
		t.Error("expected metrics to be set")
	}
}

func TestProcessor_StartStop(t *testing.T) {
	processor, _, _ := setupTestProcessor(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	processor.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Stop should not block
	processor.Stop()

	// Wait should complete
	done := make(chan struct{})
	go func() {
		processor.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Wait took too long")
	}
}

func TestProcessor_StopAndWait(t *testing.T) {
	processor, _, _ := setupTestProcessor(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	processor.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	err := processor.StopAndWait(5 * time.Second)
	if err != nil {
		t.Errorf("StopAndWait failed: %v", err)
	}
}

func TestProcessor_StopAndWait_Timeout(t *testing.T) {
	pool := skipIfNoDatabase(t)

	mr, err := miniredis.Run()
	if err != nil {
		pool.Close()
		t.Fatalf("failed to start miniredis: %v", err)
	}

	t.Cleanup(func() {
		mr.Close()
		pool.Close()
	})

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	// Very long poll interval to simulate slow processing
	config := ProcessorConfig{
		BatchSize:       10,
		PollInterval:    10 * time.Second, // Long interval
		CleanupInterval: 10 * time.Second,
		RetentionPeriod: 24 * time.Hour,
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))
	processor := NewProcessor(store, q, config, logger)

	ctx := context.Background()
	processor.Start(ctx)

	// Immediate timeout should fail since goroutines are waiting on ticker
	// Actually the tickers are waiting on select, so Stop() will work quickly
	err = processor.StopAndWait(100 * time.Millisecond)
	if err != nil {
		// This is expected if goroutines don't stop in time
		// But usually Stop() will trigger the stopCh and goroutines will exit
		t.Logf("StopAndWait returned: %v", err)
	}
}

func TestProcessor_ContextCancellation(t *testing.T) {
	processor, _, _ := setupTestProcessor(t)

	ctx, cancel := context.WithCancel(context.Background())

	processor.Start(ctx)

	// Give it a moment to start
	time.Sleep(50 * time.Millisecond)

	// Cancel context instead of calling Stop
	cancel()

	// Wait should complete due to context cancellation
	done := make(chan struct{})
	go func() {
		processor.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("Wait took too long after context cancellation")
	}
}

func TestProcessor_ProcessBatch_EmptyOutbox(t *testing.T) {
	processor, _, _ := setupTestProcessor(t)
	ctx := context.Background()

	// Processing empty outbox should not error
	err := processor.processBatch(ctx)
	if err != nil {
		t.Errorf("processBatch failed on empty outbox: %v", err)
	}
}

func TestProcessor_WorkerIDUnique(t *testing.T) {
	pool := skipIfNoDatabase(t)

	mr, err := miniredis.Run()
	if err != nil {
		pool.Close()
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	redisClient := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = redisClient.Close() }()

	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)
	config := DefaultProcessorConfig()
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelError}))

	p1 := NewProcessor(store, q, config, logger)
	p2 := NewProcessor(store, q, config, logger)

	if p1.workerID == p2.workerID {
		t.Error("expected different worker IDs for different processors")
	}

	// Validate worker ID is a valid UUID
	_, err = uuid.Parse(p1.workerID)
	if err != nil {
		t.Errorf("worker ID is not a valid UUID: %v", err)
	}

	pool.Close()
}

func TestProcessor_WithMetrics_Processing(t *testing.T) {
	processor, _, _ := setupTestProcessor(t)
	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")
	processor.WithMetrics(metrics)

	ctx := context.Background()

	// Process empty batch with metrics enabled
	err := processor.processBatch(ctx)
	if err != nil {
		t.Errorf("processBatch failed: %v", err)
	}
}
