package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/relay/internal/api"
	"github.com/relay/internal/auth"
	"github.com/relay/internal/config"
	"github.com/relay/internal/dedup"
	"github.com/relay/internal/event"
	"github.com/relay/internal/outbox"
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
	dedupChecker := dedup.NewChecker(redisClient)

	// Initialize auth store
	authStore := auth.NewStore(pool)

	// Create and start outbox processor
	outboxProcessor := outbox.NewProcessor(store, q, outbox.ProcessorConfig{
		PollInterval:    cfg.Outbox.PollInterval,
		BatchSize:       cfg.Outbox.BatchSize,
		CleanupInterval: cfg.Outbox.CleanupInterval,
		RetentionPeriod: cfg.Outbox.RetentionPeriod,
	}, logger)
	outboxProcessor.Start(ctx)

	// Create server
	serverCfg := api.ServerConfig{
		EnableAuth:       cfg.Auth.Enabled,
		EnablePlayground: cfg.Auth.EnablePlayground,
	}
	server := api.NewServer(store, q, dedupChecker, authStore, serverCfg, logger)

	// Create HTTP server with proper timeouts
	httpServer := &http.Server{
		Addr:         cfg.API.Addr,
		Handler:      server.Handler(),
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
		IdleTimeout:  cfg.API.IdleTimeout,
	}

	// Start server in goroutine
	go func() {
		logger.Info("starting API server", "addr", cfg.API.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server")

	// Graceful shutdown with timeout
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.API.ShutdownTimeout)
	defer cancel()

	// Stop HTTP server first to stop accepting new requests
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}
	logger.Info("HTTP server stopped")

	// Then stop outbox processor to ensure all pending events are processed
	if err := outboxProcessor.StopAndWait(cfg.API.ShutdownTimeout); err != nil {
		logger.Error("outbox processor shutdown error", "error", err)
	}
	logger.Info("outbox processor stopped")

	logger.Info("server stopped")
}
