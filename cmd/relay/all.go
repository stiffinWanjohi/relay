package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/api"
	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/config"
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/delivery"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/notification"
	"github.com/stiffinWanjohi/relay/internal/observability"
	_ "github.com/stiffinWanjohi/relay/internal/observability/otel" // Register OTel provider
	"github.com/stiffinWanjohi/relay/internal/outbox"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

// runAll starts both the API server and worker in the same process.
// This is intended for development and testing purposes.
func runAll() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	logger.Info("starting relay in combined mode (API + Worker)")

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
	defer func() { _ = redisClient.Close() }()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		logger.Error("failed to connect to redis", "error", err)
		os.Exit(1)
	}
	logger.Info("connected to redis", "pool_size", cfg.Redis.PoolSize)

	// Initialize metrics provider dynamically
	metricsProvider, err := observability.NewMetricsProviderByName(ctx, cfg.Metrics.Provider, observability.MetricsConfig{
		ServiceName:    cfg.Metrics.ServiceName,
		ServiceVersion: cfg.Metrics.ServiceVersion,
		Environment:    cfg.Metrics.Environment,
		Endpoint:       cfg.Metrics.Endpoint,
	})
	if err != nil {
		logger.Error("failed to initialize metrics provider", "provider", cfg.Metrics.Provider, "error", err)
		os.Exit(1)
	}
	defer func() { _ = metricsProvider.Close(ctx) }()

	if cfg.Metrics.Provider != "" {
		logger.Info("metrics enabled", "provider", cfg.Metrics.Provider, "endpoint", cfg.Metrics.Endpoint)
	} else {
		logger.Info("metrics disabled (set METRICS_PROVIDER to enable)")
	}
	metrics := observability.NewMetrics(metricsProvider, cfg.Metrics.ServiceName)

	// Initialize notification service
	notificationService := notification.NewService(notification.Config{
		Enabled:         cfg.Notification.Enabled,
		Async:           cfg.Notification.Async,
		SlackWebhookURL: cfg.Notification.SlackWebhookURL,
		SMTPHost:        cfg.Notification.SMTPHost,
		SMTPPort:        cfg.Notification.SMTPPort,
		SMTPUsername:    cfg.Notification.SMTPUsername,
		SMTPPassword:    cfg.Notification.SMTPPassword,
		EmailFrom:       cfg.Notification.EmailFrom,
		EmailTo:         cfg.Notification.EmailTo,
		NotifyOnTrip:    cfg.Notification.NotifyOnTrip,
		NotifyOnRecover: cfg.Notification.NotifyOnRecover,
	}, logger)
	defer func() { _ = notificationService.Close() }()

	if cfg.Notification.Enabled {
		logger.Info("notifications enabled",
			"slack", cfg.Notification.SlackWebhookURL != "",
			"email", cfg.Notification.SMTPHost != "",
		)
	}

	// Initialize shared components
	store := event.NewStore(pool)
	q := queue.NewQueue(redisClient).WithMetrics(metrics)
	dedupChecker := dedup.NewChecker(redisClient)
	authStore := auth.NewStore(pool)
	rateLimiter := delivery.NewRateLimiter(redisClient)

	// Create cancellable context
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var wg sync.WaitGroup

	// --- Start Outbox Processor ---
	outboxProcessor := outbox.NewProcessor(store, q, outbox.ProcessorConfig{
		PollInterval:    cfg.Outbox.PollInterval,
		BatchSize:       cfg.Outbox.BatchSize,
		CleanupInterval: cfg.Outbox.CleanupInterval,
		RetentionPeriod: cfg.Outbox.RetentionPeriod,
	}, logger).WithMetrics(metrics)
	outboxProcessor.Start(ctx)

	// --- Start API Server ---
	serverCfg := api.ServerConfig{
		EnableAuth:       cfg.Auth.Enabled,
		EnablePlayground: cfg.Auth.EnablePlayground,
	}
	server := api.NewServer(store, q, dedupChecker, authStore, serverCfg, logger)

	httpServer := &http.Server{
		Addr:         cfg.API.Addr,
		Handler:      server.Handler(),
		ReadTimeout:  cfg.API.ReadTimeout,
		WriteTimeout: cfg.API.WriteTimeout,
		IdleTimeout:  cfg.API.IdleTimeout,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		logger.Info("starting API server", "addr", cfg.API.Addr)
		if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Error("server error", "error", err)
		}
	}()

	// --- Start Worker ---
	workerConfig := delivery.WorkerConfig{
		Concurrency:         cfg.Worker.Concurrency,
		VisibilityTime:      cfg.Worker.VisibilityTimeout,
		SigningKey:          cfg.Worker.SigningKey,
		CircuitConfig:       delivery.DefaultCircuitConfig(),
		Metrics:             metrics,
		RateLimiter:         rateLimiter,
		NotificationService: notificationService,
		NotifyOnTrip:        cfg.Notification.NotifyOnTrip,
		NotifyOnRecover:     cfg.Notification.NotifyOnRecover,
	}

	worker := delivery.NewWorker(q, store, workerConfig, logger)
	worker.Start(ctx)
	logger.Info("worker started", "concurrency", cfg.Worker.Concurrency)

	// --- Start Stale Message Recovery ---
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

	// Wait for shutdown signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down relay")

	// Cancel context to signal all goroutines
	cancel()

	// Graceful shutdown with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), cfg.API.ShutdownTimeout)
	defer shutdownCancel()

	// Stop HTTP server
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}
	logger.Info("HTTP server stopped")

	// Stop worker
	if err := worker.StopAndWait(cfg.Worker.ShutdownTimeout); err != nil {
		logger.Warn("worker shutdown timed out", "error", err)
	} else {
		logger.Info("worker stopped gracefully")
	}

	// Stop outbox processor
	if err := outboxProcessor.StopAndWait(cfg.API.ShutdownTimeout); err != nil {
		logger.Error("outbox processor shutdown error", "error", err)
	}
	logger.Info("outbox processor stopped")

	// Wait for remaining goroutines
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

	logger.Info("relay stopped")
}
