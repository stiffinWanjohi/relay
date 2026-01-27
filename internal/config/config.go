package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	// MinSigningKeyLength is the minimum required length for the signing key.
	MinSigningKeyLength = 32

	// MaxPayloadSize is the maximum allowed payload size in bytes (1MB).
	MaxPayloadSize = 1 * 1024 * 1024

	// MaxURLLength is the maximum allowed URL length.
	MaxURLLength = 2048

	// DefaultMaxAttempts is the default maximum delivery attempts.
	DefaultMaxAttempts = 10

	// DefaultWorkerConcurrency is the default number of concurrent workers.
	DefaultWorkerConcurrency = 10

	// DefaultVisibilityTimeout is the default queue visibility timeout.
	DefaultVisibilityTimeout = 30 * time.Second
)

// Config holds all application configuration.
type Config struct {
	Database DatabaseConfig
	Redis    RedisConfig
	API      APIConfig
	Worker   WorkerConfig
	Outbox   OutboxConfig
	Auth     AuthConfig
}

// DatabaseConfig holds database configuration.
type DatabaseConfig struct {
	URL             string
	MaxConns        int32
	MinConns        int32
	MaxConnLifetime time.Duration
	MaxConnIdleTime time.Duration
}

// RedisConfig holds Redis configuration.
type RedisConfig struct {
	URL          string
	PoolSize     int
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
}

// APIConfig holds API server configuration.
type APIConfig struct {
	Addr            string
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	ShutdownTimeout time.Duration
}

// WorkerConfig holds worker configuration.
type WorkerConfig struct {
	Concurrency       int
	VisibilityTimeout time.Duration
	SigningKey        string
	ShutdownTimeout   time.Duration
}

// OutboxConfig holds outbox processor configuration.
type OutboxConfig struct {
	PollInterval    time.Duration
	BatchSize       int
	CleanupInterval time.Duration
	RetentionPeriod time.Duration
}

// AuthConfig holds authentication configuration.
type AuthConfig struct {
	Enabled          bool
	EnablePlayground bool
}

// Validation errors.
var (
	ErrSigningKeyRequired   = errors.New("SIGNING_KEY environment variable is required")
	ErrSigningKeyTooShort   = fmt.Errorf("SIGNING_KEY must be at least %d characters", MinSigningKeyLength)
	ErrDatabaseURLRequired  = errors.New("DATABASE_URL environment variable is required")
	ErrRedisURLRequired     = errors.New("REDIS_URL environment variable is required")
	ErrInvalidURL           = errors.New("invalid URL format")
	ErrInvalidURLScheme     = errors.New("URL scheme must be http or https")
	ErrURLTooLong           = fmt.Errorf("URL exceeds maximum length of %d characters", MaxURLLength)
	ErrPayloadTooLarge      = fmt.Errorf("payload exceeds maximum size of %d bytes", MaxPayloadSize)
	ErrInternalURLBlocked   = errors.New("internal/private URLs are not allowed")
	ErrInvalidIdempotencyKey = errors.New("idempotency key is required and must be non-empty")
)

// LoadConfig loads configuration from environment variables.
func LoadConfig() (*Config, error) {
	cfg := &Config{}

	// Database config
	cfg.Database.URL = os.Getenv("DATABASE_URL")
	if cfg.Database.URL == "" {
		return nil, ErrDatabaseURLRequired
	}
	cfg.Database.MaxConns = int32(getEnvInt("DATABASE_MAX_CONNS", 100))
	cfg.Database.MinConns = int32(getEnvInt("DATABASE_MIN_CONNS", 10))
	cfg.Database.MaxConnLifetime = getEnvDuration("DATABASE_MAX_CONN_LIFETIME", 1*time.Hour)
	cfg.Database.MaxConnIdleTime = getEnvDuration("DATABASE_MAX_CONN_IDLE_TIME", 30*time.Minute)

	// Redis config
	cfg.Redis.URL = getEnv("REDIS_URL", "localhost:6379")
	cfg.Redis.PoolSize = getEnvInt("REDIS_POOL_SIZE", 100)
	cfg.Redis.ReadTimeout = getEnvDuration("REDIS_READ_TIMEOUT", 3*time.Second)
	cfg.Redis.WriteTimeout = getEnvDuration("REDIS_WRITE_TIMEOUT", 3*time.Second)

	// API config
	cfg.API.Addr = getEnv("API_ADDR", ":8080")
	cfg.API.ReadTimeout = getEnvDuration("API_READ_TIMEOUT", 15*time.Second)
	cfg.API.WriteTimeout = getEnvDuration("API_WRITE_TIMEOUT", 15*time.Second)
	cfg.API.IdleTimeout = getEnvDuration("API_IDLE_TIMEOUT", 60*time.Second)
	cfg.API.ShutdownTimeout = getEnvDuration("API_SHUTDOWN_TIMEOUT", 30*time.Second)

	// Worker config
	cfg.Worker.Concurrency = getEnvInt("WORKER_CONCURRENCY", DefaultWorkerConcurrency)
	cfg.Worker.VisibilityTimeout = getEnvDuration("WORKER_VISIBILITY_TIMEOUT", DefaultVisibilityTimeout)
	cfg.Worker.ShutdownTimeout = getEnvDuration("WORKER_SHUTDOWN_TIMEOUT", 30*time.Second)

	// Signing key - REQUIRED, no default
	cfg.Worker.SigningKey = os.Getenv("SIGNING_KEY")
	if cfg.Worker.SigningKey == "" {
		return nil, ErrSigningKeyRequired
	}
	if len(cfg.Worker.SigningKey) < MinSigningKeyLength {
		return nil, ErrSigningKeyTooShort
	}

	// Outbox config
	cfg.Outbox.PollInterval = getEnvDuration("OUTBOX_POLL_INTERVAL", 1*time.Second)
	cfg.Outbox.BatchSize = getEnvInt("OUTBOX_BATCH_SIZE", 100)
	cfg.Outbox.CleanupInterval = getEnvDuration("OUTBOX_CLEANUP_INTERVAL", 1*time.Hour)
	cfg.Outbox.RetentionPeriod = getEnvDuration("OUTBOX_RETENTION_PERIOD", 24*time.Hour)

	// Auth config
	cfg.Auth.Enabled = getEnvBool("AUTH_ENABLED", true)
	cfg.Auth.EnablePlayground = getEnvBool("ENABLE_PLAYGROUND", false)

	return cfg, nil
}

// ValidateDestinationURL validates a webhook destination URL.
func ValidateDestinationURL(rawURL string) error {
	if rawURL == "" {
		return ErrInvalidURL
	}

	if len(rawURL) > MaxURLLength {
		return ErrURLTooLong
	}

	parsed, err := url.Parse(rawURL)
	if err != nil {
		return ErrInvalidURL
	}

	// Only allow http and https schemes
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return ErrInvalidURLScheme
	}

	// Block internal/private addresses
	if isInternalHost(parsed.Host) {
		return ErrInternalURLBlocked
	}

	return nil
}

// ValidatePayloadSize checks if the payload is within allowed limits.
func ValidatePayloadSize(payload []byte) error {
	if len(payload) > MaxPayloadSize {
		return ErrPayloadTooLarge
	}
	return nil
}

// ValidateIdempotencyKey validates the idempotency key.
func ValidateIdempotencyKey(key string) error {
	if strings.TrimSpace(key) == "" {
		return ErrInvalidIdempotencyKey
	}
	return nil
}

// isInternalHost checks if the host is an internal/private address.
func isInternalHost(host string) bool {
	// Remove port if present
	hostname := host
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		hostname = host[:colonIdx]
	}

	// Handle IPv6 addresses in brackets
	hostname = strings.TrimPrefix(hostname, "[")
	hostname = strings.TrimSuffix(hostname, "]")

	lower := strings.ToLower(hostname)

	// Block localhost
	if lower == "localhost" {
		return true
	}

	// Block common internal hostnames
	internalSuffixes := []string{
		".local",
		".internal",
		".localhost",
		".localdomain",
	}
	for _, suffix := range internalSuffixes {
		if strings.HasSuffix(lower, suffix) {
			return true
		}
	}

	// Block private IP ranges
	// 10.0.0.0/8
	if strings.HasPrefix(hostname, "10.") {
		return true
	}

	// 172.16.0.0/12
	if strings.HasPrefix(hostname, "172.") {
		parts := strings.Split(hostname, ".")
		if len(parts) >= 2 {
			if second, err := strconv.Atoi(parts[1]); err == nil {
				if second >= 16 && second <= 31 {
					return true
				}
			}
		}
	}

	// 192.168.0.0/16
	if strings.HasPrefix(hostname, "192.168.") {
		return true
	}

	// 127.0.0.0/8 (loopback)
	if strings.HasPrefix(hostname, "127.") {
		return true
	}

	// 169.254.0.0/16 (link-local)
	if strings.HasPrefix(hostname, "169.254.") {
		return true
	}

	// IPv6 loopback and link-local
	if hostname == "::1" || strings.HasPrefix(lower, "fe80:") || strings.HasPrefix(lower, "fc") || strings.HasPrefix(lower, "fd") {
		return true
	}

	// Block metadata endpoints (cloud providers)
	metadataHosts := []string{
		"169.254.169.254", // AWS, GCP, Azure metadata
		"metadata.google.internal",
		"metadata.goog",
	}
	for _, meta := range metadataHosts {
		if lower == meta {
			return true
		}
	}

	return false
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func getEnvInt(key string, defaultValue int) int {
	if value := os.Getenv(key); value != "" {
		if i, err := strconv.Atoi(value); err == nil {
			return i
		}
	}
	return defaultValue
}

func getEnvDuration(key string, defaultValue time.Duration) time.Duration {
	if value := os.Getenv(key); value != "" {
		if d, err := time.ParseDuration(value); err == nil {
			return d
		}
	}
	return defaultValue
}

func getEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	return value == "true" || value == "1" || value == "yes"
}
