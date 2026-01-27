package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/relay/internal/delivery"
	"github.com/relay/internal/event"
	"github.com/relay/internal/queue"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	// Load configuration from environment
	databaseURL := getEnv("DATABASE_URL", "postgres://relay:relay@localhost:5432/relay?sslmode=disable")
	redisURL := getEnv("REDIS_URL", "localhost:6379")
	signingKey := getEnv("SIGNING_KEY", "default-signing-key-change-me-in-production")
	concurrency := getEnvInt("CONCURRENCY", 10)

	// Connect to PostgreSQL
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		logger.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		logger.Error("failed to ping database", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to database")

	// Connect to Redis
	redisClient := redis.NewClient(&redis.Options{
		Addr: redisURL,
	})
	defer redisClient.Close()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis")

	// Initialize components
	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient)

	// Create worker configuration
	config := delivery.WorkerConfig{
		Concurrency:    concurrency,
		VisibilityTime: 30 * time.Second,
		SigningKey:     signingKey,
		CircuitConfig:  delivery.DefaultCircuitConfig(),
	}

	// Create and start worker
	worker := delivery.NewWorker(q, store, config, logger)

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Start worker
	worker.Start(ctx)

	// Start stale message recovery goroutine
	go func() {
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

	logger.Info("worker started", "concurrency", concurrency)

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down worker")

	// Stop worker
	worker.Stop()
	cancel()

	// Give workers time to finish current tasks
	time.Sleep(2 * time.Second)

	logger.Info("worker stopped")
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		var result int
		for _, c := range value {
			if c >= '0' && c <= '9' {
				result = result*10 + int(c-'0')
			}
		}
		if result > 0 {
			return result
		}
	}
	return defaultValue
}
