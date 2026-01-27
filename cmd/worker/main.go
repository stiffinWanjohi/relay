package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/relay/internal/config"
	"github.com/relay/internal/delivery"
	"github.com/relay/internal/event"
	"github.com/relay/internal/queue"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load configuration
	cfg, err := config.LoadConfig()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	// Connect to PostgreSQL with connection pool settings
	ctx := context.Background()
	poolConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		logger.Error("failed to parse database URL", "error", err)
		os.Exit(1)
	}
	poolConfig.MaxConns = cfg.Database.MaxConns
	poolConfig.MinConns = cfg.Database.MinConns
	poolConfig.MaxConnLifetime = cfg.Database.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.Database.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database",
		"max_conns", cfg.Database.MaxConns,
		"min_conns", cfg.Database.MinConns,
	)

	// Connect to Redis with proper configuration
	redisClient := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.URL,
		PoolSize:     cfg.Redis.PoolSize,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis", "pool_size", cfg.Redis.PoolSize)

	// Initialize components
	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	// Create worker configuration
	workerConfig := delivery.WorkerConfig{
		Concurrency:    cfg.Worker.Concurrency,
		VisibilityTime: cfg.Worker.VisibilityTimeout,
		SigningKey:     cfg.Worker.SigningKey,
		CircuitConfig:  delivery.DefaultCircuitConfig(),
	}

	// Create and start worker
	worker := delivery.NewWorker(q, store, workerConfig, logger)

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// WaitGroup for graceful shutdown
	var wg sync.WaitGroup

	// Start worker
	worker.Start(ctx)

	// Start stale message recovery goroutine
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(1 * time.Minute)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				recovered, err := q.RecoverStaleMessages(ctx, 5*time.Minute)
				if err != nil {
					logger.Error("failed to recover stale messages", "error", err)
				} else if recovered > 0 {
					logger.Info("recovered stale messages", "count", recovered)
				}
			}
		}
	}()

	logger.Info("worker started", "concurrency", cfg.Worker.Concurrency)

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down worker")

	// Cancel context first to signal all goroutines
	cancel()

	// Stop worker and wait for completion
	if err := worker.StopAndWait(cfg.Worker.ShutdownTimeout); err != nil {
		logger.Warn("worker shutdown timed out", "error", err)
	} else {
		logger.Info("worker stopped gracefully")
	}

	// Wait for recovery goroutine
	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		logger.Info("all goroutines stopped")
	case <-time.After(5 * time.Second):
		logger.Warn("some goroutines did not stop in time")
	}
}
