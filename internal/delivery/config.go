package delivery

import (
	"time"

	"github.com/stiffinWanjohi/relay/internal/logstream"
	"github.com/stiffinWanjohi/relay/internal/metrics"
	"github.com/stiffinWanjohi/relay/internal/notification"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

// Config holds unified configuration for the delivery worker.
// This replaces both WorkerConfig and FIFOWorkerConfig.
type Config struct {
	// Shared configuration
	SigningKey          string
	CircuitConfig       CircuitConfig
	Metrics             *observability.Metrics
	MetricsStore        *metrics.Store
	LogStreamHub        *logstream.Hub
	RateLimiter         *RateLimiter
	NotificationService *notification.Service
	NotifyOnTrip        bool
	NotifyOnRecover     bool

	// Standard processor configuration
	Concurrency         int
	VisibilityTime      time.Duration
	EnablePriorityQueue bool

	// FIFO processor configuration
	FIFOGracePeriod time.Duration

	// Feature flags
	EnableStandard bool // Enable standard parallel processor (default: true)
	EnableFIFO     bool // Enable FIFO ordered processor (default: true)
}

// DefaultConfig returns the default worker configuration.
func DefaultConfig() Config {
	return Config{
		CircuitConfig:       DefaultCircuitConfig(),
		Concurrency:         10,
		VisibilityTime:      30 * time.Second,
		EnablePriorityQueue: true,
		FIFOGracePeriod:     30 * time.Second,
		EnableStandard:      true,
		EnableFIFO:          true,
	}
}

// Validate checks if the configuration is valid and returns a corrected copy.
func (c Config) Validate() Config {
	if c.Concurrency < 1 {
		c.Concurrency = 10
	}
	if c.VisibilityTime < time.Second {
		c.VisibilityTime = 30 * time.Second
	}
	if c.FIFOGracePeriod < time.Second {
		c.FIFOGracePeriod = 30 * time.Second
	}
	return c
}

// WithConcurrency returns a copy of the config with the specified concurrency.
func (c Config) WithConcurrency(n int) Config {
	c.Concurrency = n
	return c
}

// WithFIFOOnly returns a copy of the config with only FIFO processing enabled.
func (c Config) WithFIFOOnly() Config {
	c.EnableStandard = false
	c.EnableFIFO = true
	return c
}

// WithStandardOnly returns a copy of the config with only standard processing enabled.
func (c Config) WithStandardOnly() Config {
	c.EnableStandard = true
	c.EnableFIFO = false
	return c
}
