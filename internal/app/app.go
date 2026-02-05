// Package app provides shared application setup and lifecycle management.
package app

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/config"
	"github.com/stiffinWanjohi/relay/internal/notification"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

// Services holds all initialized application services.
type Services struct {
	Config       *config.Config
	Pool         *pgxpool.Pool
	Redis        *redis.Client
	Metrics      *observability.Metrics
	Notification *notification.Service
	Logger       *slog.Logger

	metricsProvider observability.MetricsProvider
}

// Close closes all services gracefully.
func (s *Services) Close(ctx context.Context) {
	if s.Notification != nil {
		_ = s.Notification.Close()
	}
	if s.metricsProvider != nil {
		_ = s.metricsProvider.Close(ctx)
	}
	if s.Redis != nil {
		_ = s.Redis.Close()
	}
	if s.Pool != nil {
		s.Pool.Close()
	}
}

// NewLogger creates a new structured logger with colors.
func NewLogger() *slog.Logger {
	// Use colored text handler for terminal
	opts := &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}

	// Check if we should use colors
	if os.Getenv("NO_COLOR") == "" {
		return slog.New(NewColorHandler(os.Stdout, opts))
	}

	return slog.New(slog.NewJSONHandler(os.Stdout, opts))
}

// ColorHandler is a colored slog handler for terminal output.
type ColorHandler struct {
	opts   *slog.HandlerOptions
	out    *os.File
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

// NewColorHandler creates a new colored log handler.
func NewColorHandler(out *os.File, opts *slog.HandlerOptions) *ColorHandler {
	if opts == nil {
		opts = &slog.HandlerOptions{}
	}
	return &ColorHandler{opts: opts, out: out, mu: &sync.Mutex{}}
}

const (
	colorReset  = "\033[0m"
	colorDim    = "\033[2m"
	colorGreen  = "\033[32m"
	colorYellow = "\033[33m"
	colorRed    = "\033[31m"
	colorCyan   = "\033[36m"
)

func (h *ColorHandler) Enabled(_ context.Context, level slog.Level) bool {
	minLevel := slog.LevelInfo
	if h.opts.Level != nil {
		minLevel = h.opts.Level.Level()
	}
	return level >= minLevel
}

func (h *ColorHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	// Color based on level
	levelColor := colorGreen
	switch r.Level {
	case slog.LevelWarn:
		levelColor = colorYellow
	case slog.LevelError:
		levelColor = colorRed
	}

	// Format: [time] level msg key=value...
	timeStr := r.Time.Format("15:04:05")

	_, _ = fmt.Fprintf(h.out, "%s%s%s %s%-5s%s %s",
		colorDim, timeStr, colorReset,
		levelColor, r.Level.String(), colorReset,
		r.Message)

	// Print pre-existing attrs (from WithAttrs)
	for _, a := range h.attrs {
		_, _ = fmt.Fprintf(h.out, " %s%s=%s%v%s", colorDim, a.Key, colorCyan, a.Value, colorReset)
	}

	// Print record attributes
	r.Attrs(func(a slog.Attr) bool {
		_, _ = fmt.Fprintf(h.out, " %s%s=%s%v%s", colorDim, a.Key, colorCyan, a.Value, colorReset)
		return true
	})

	_, _ = fmt.Fprintln(h.out)
	return nil
}

func (h *ColorHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &ColorHandler{
		opts:   h.opts,
		out:    h.out,
		attrs:  append(h.attrs, attrs...),
		groups: h.groups,
		mu:     h.mu, // Share mutex
	}
}

func (h *ColorHandler) WithGroup(name string) slog.Handler {
	return &ColorHandler{
		opts:   h.opts,
		out:    h.out,
		attrs:  h.attrs,
		groups: append(h.groups, name),
		mu:     h.mu, // Share mutex
	}
}

// ConnectPostgres connects to PostgreSQL with the provided configuration.
func ConnectPostgres(ctx context.Context, cfg *config.Config) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.Database.URL)
	if err != nil {
		return nil, fmt.Errorf("invalid database URL: %w", err)
	}

	poolConfig.MaxConns = cfg.Database.MaxConns
	poolConfig.MinConns = cfg.Database.MinConns
	poolConfig.MaxConnLifetime = cfg.Database.MaxConnLifetime
	poolConfig.MaxConnIdleTime = cfg.Database.MaxConnIdleTime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("database ping failed: %w", err)
	}

	return pool, nil
}

// ConnectRedis connects to Redis with the provided configuration.
func ConnectRedis(ctx context.Context, cfg *config.Config) (*redis.Client, error) {
	client := redis.NewClient(&redis.Options{
		Addr:         cfg.Redis.URL,
		PoolSize:     cfg.Redis.PoolSize,
		ReadTimeout:  cfg.Redis.ReadTimeout,
		WriteTimeout: cfg.Redis.WriteTimeout,
	})

	if err := client.Ping(ctx).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	return client, nil
}

// NewMetricsProvider creates a metrics provider based on configuration.
func NewMetricsProvider(ctx context.Context, cfg *config.Config) (observability.MetricsProvider, *observability.Metrics, error) {
	provider, err := observability.NewMetricsProviderByName(ctx, cfg.Metrics.Provider, observability.MetricsConfig{
		ServiceName:    cfg.Metrics.ServiceName,
		ServiceVersion: cfg.Metrics.ServiceVersion,
		Environment:    cfg.Metrics.Environment,
		Endpoint:       cfg.Metrics.Endpoint,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("failed to initialize metrics: %w", err)
	}

	metrics := observability.NewMetrics(provider, cfg.Metrics.ServiceName)
	return provider, metrics, nil
}

// NewNotificationService creates a notification service based on configuration.
func NewNotificationService(cfg *config.Config) *notification.Service {
	return notification.NewService(notification.Config{
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
	})
}

// InitAll initializes all services needed for the full application.
// Returns Services which should be closed with Close() when done.
func InitAll(ctx context.Context, cfg *config.Config) (*Services, error) {
	logger := NewLogger()

	pool, err := ConnectPostgres(ctx, cfg)
	if err != nil {
		return nil, err
	}

	redisClient, err := ConnectRedis(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}

	metricsProvider, metrics, err := NewMetricsProvider(ctx, cfg)
	if err != nil {
		_ = redisClient.Close()
		pool.Close()
		return nil, err
	}

	notificationService := NewNotificationService(cfg)

	return &Services{
		Config:          cfg,
		Pool:            pool,
		Redis:           redisClient,
		Metrics:         metrics,
		Notification:    notificationService,
		Logger:          logger,
		metricsProvider: metricsProvider,
	}, nil
}

// InitAPI initializes services needed for the API server only (no notifications).
func InitAPI(ctx context.Context, cfg *config.Config) (*Services, error) {
	logger := NewLogger()

	pool, err := ConnectPostgres(ctx, cfg)
	if err != nil {
		return nil, err
	}

	redisClient, err := ConnectRedis(ctx, cfg)
	if err != nil {
		pool.Close()
		return nil, err
	}

	metricsProvider, metrics, err := NewMetricsProvider(ctx, cfg)
	if err != nil {
		_ = redisClient.Close()
		pool.Close()
		return nil, err
	}

	return &Services{
		Config:          cfg,
		Pool:            pool,
		Redis:           redisClient,
		Metrics:         metrics,
		Logger:          logger,
		metricsProvider: metricsProvider,
	}, nil
}

// WaitForShutdown blocks until SIGINT or SIGTERM is received.
func WaitForShutdown() {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit
}

// ShutdownChannel returns a channel that receives shutdown signals.
func ShutdownChannel() <-chan os.Signal {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	return quit
}
