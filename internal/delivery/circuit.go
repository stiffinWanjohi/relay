package delivery

import (
	"context"
	"sync"
	"time"

	"github.com/relay/internal/domain"
	"github.com/relay/internal/observability"
)

// CircuitState represents the state of a circuit breaker.
type CircuitState int

const (
	CircuitClosed CircuitState = iota
	CircuitOpen
	CircuitHalfOpen
)

func (s CircuitState) String() string {
	switch s {
	case CircuitClosed:
		return "closed"
	case CircuitOpen:
		return "open"
	case CircuitHalfOpen:
		return "half-open"
	default:
		return "unknown"
	}
}

// CircuitConfig holds circuit breaker configuration.
type CircuitConfig struct {
	// FailureThreshold is the number of consecutive failures before opening.
	FailureThreshold int
	// SuccessThreshold is the number of successes in half-open before closing.
	SuccessThreshold int
	// OpenDuration is how long to stay open before transitioning to half-open.
	OpenDuration time.Duration
}

// DefaultCircuitConfig returns the default circuit breaker configuration.
func DefaultCircuitConfig() CircuitConfig {
	return CircuitConfig{
		FailureThreshold: 5,
		SuccessThreshold: 3,
		OpenDuration:     5 * time.Minute,
	}
}

// CircuitConfigFromEndpoint creates a circuit config from endpoint settings.
// Returns the default config if the endpoint's values are not set.
func CircuitConfigFromEndpoint(endpoint *domain.Endpoint, defaultConfig CircuitConfig) CircuitConfig {
	if endpoint == nil {
		return defaultConfig
	}

	config := defaultConfig

	if endpoint.CircuitThreshold > 0 {
		config.FailureThreshold = endpoint.CircuitThreshold
	}

	if endpoint.CircuitResetMs > 0 {
		config.OpenDuration = time.Duration(endpoint.CircuitResetMs) * time.Millisecond
	}

	return config
}

// circuit represents a single circuit breaker instance.
type circuit struct {
	state             CircuitState
	failures          int
	successes         int
	lastStateChange   time.Time
	lastAccess        time.Time
	consecutiveErrors int
}

// CircuitBreaker manages circuit breakers per destination.
type CircuitBreaker struct {
	config      CircuitConfig
	circuits    map[string]*circuit
	mu          sync.Mutex // Use single Mutex to avoid race condition between RLock and Lock
	ttl         time.Duration
	cleanupStop chan struct{}
	metrics     *observability.Metrics
}

const (
	// Default TTL for idle circuits
	defaultCircuitTTL = 1 * time.Hour
	// Cleanup interval
	circuitCleanupInterval = 10 * time.Minute
)

// NewCircuitBreaker creates a new circuit breaker manager.
func NewCircuitBreaker(config CircuitConfig) *CircuitBreaker {
	cb := &CircuitBreaker{
		config:      config,
		circuits:    make(map[string]*circuit),
		ttl:         defaultCircuitTTL,
		cleanupStop: make(chan struct{}),
	}
	go cb.cleanupLoop()
	return cb
}

// WithMetrics sets a metrics provider for the circuit breaker.
func (cb *CircuitBreaker) WithMetrics(metrics *observability.Metrics) *CircuitBreaker {
	cb.metrics = metrics
	return cb
}

// Stop stops the circuit breaker cleanup goroutine.
func (cb *CircuitBreaker) Stop() {
	close(cb.cleanupStop)
}

// cleanupLoop periodically removes idle circuits to prevent memory leaks.
func (cb *CircuitBreaker) cleanupLoop() {
	ticker := time.NewTicker(circuitCleanupInterval)
	defer ticker.Stop()

	for {
		select {
		case <-cb.cleanupStop:
			return
		case <-ticker.C:
			cb.cleanup()
		}
	}
}

// cleanup removes circuits that have been idle for longer than TTL.
func (cb *CircuitBreaker) cleanup() {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	cutoff := time.Now().Add(-cb.ttl)
	for dest, c := range cb.circuits {
		// Only remove closed circuits that have been idle
		if c.state == CircuitClosed && c.lastAccess.Before(cutoff) {
			delete(cb.circuits, dest)
		}
	}
}

// IsOpen checks if the circuit is open for a destination.
// Returns true if the circuit is open and requests should be blocked.
func (cb *CircuitBreaker) IsOpen(destination string) bool {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.circuits[destination]
	if !exists {
		return false
	}

	c.lastAccess = time.Now()

	// Check if we should transition from open to half-open
	if c.state == CircuitOpen {
		if time.Since(c.lastStateChange) >= cb.config.OpenDuration {
			c.state = CircuitHalfOpen
			c.successes = 0
			c.lastStateChange = time.Now()
			return false // Allow request through in half-open state
		}
		return true // Circuit is open, block request
	}

	return false
}

// RecordSuccess records a successful delivery.
func (cb *CircuitBreaker) RecordSuccess(destination string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.circuits[destination]
	if !exists {
		return
	}

	c.lastAccess = time.Now()
	c.consecutiveErrors = 0
	previousState := c.state

	switch c.state {
	case CircuitHalfOpen:
		c.successes++
		if c.successes >= cb.config.SuccessThreshold {
			c.state = CircuitClosed
			c.failures = 0
			c.successes = 0
			c.lastStateChange = time.Now()
		}
	case CircuitClosed:
		c.failures = 0
	}

	// Record metrics if state changed (recovered from half-open to closed)
	if cb.metrics != nil && previousState != c.state {
		cb.metrics.CircuitBreakerStateChange(context.Background(), destination, c.state.String())
	}
}

// RecordFailure records a failed delivery.
func (cb *CircuitBreaker) RecordFailure(destination string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	now := time.Now()
	c, exists := cb.circuits[destination]
	if !exists {
		c = &circuit{
			state:           CircuitClosed,
			lastStateChange: now,
			lastAccess:      now,
		}
		cb.circuits[destination] = c
	}

	c.lastAccess = now
	c.consecutiveErrors++
	previousState := c.state

	switch c.state {
	case CircuitClosed:
		c.failures++
		if c.failures >= cb.config.FailureThreshold {
			c.state = CircuitOpen
			c.lastStateChange = now
		}
	case CircuitHalfOpen:
		// Any failure in half-open returns to open
		c.state = CircuitOpen
		c.successes = 0
		c.lastStateChange = now
	}

	// Record metrics if state changed to open (circuit tripped)
	if cb.metrics != nil && previousState != CircuitOpen && c.state == CircuitOpen {
		cb.metrics.CircuitBreakerTrip(context.Background(), destination)
		cb.metrics.CircuitBreakerStateChange(context.Background(), destination, c.state.String())
	}
}

// GetState returns the current state of a circuit.
func (cb *CircuitBreaker) GetState(destination string) CircuitState {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.circuits[destination]
	if !exists {
		return CircuitClosed
	}

	// Also check for state transition here for consistency
	if c.state == CircuitOpen && time.Since(c.lastStateChange) >= cb.config.OpenDuration {
		c.state = CircuitHalfOpen
		c.successes = 0
		c.lastStateChange = time.Now()
	}

	return c.state
}

// Reset resets the circuit breaker for a destination.
func (cb *CircuitBreaker) Reset(destination string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	delete(cb.circuits, destination)
}

// Stats returns statistics about circuit breakers.
func (cb *CircuitBreaker) Stats() CircuitStats {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	stats := CircuitStats{
		Circuits: make(map[string]CircuitInfo),
	}

	for dest, c := range cb.circuits {
		stats.Circuits[dest] = CircuitInfo{
			State:             c.state.String(),
			Failures:          c.failures,
			Successes:         c.successes,
			ConsecutiveErrors: c.consecutiveErrors,
			LastStateChange:   c.lastStateChange,
		}
	}

	return stats
}

// CircuitStats holds circuit breaker statistics.
type CircuitStats struct {
	Circuits map[string]CircuitInfo
}

// CircuitInfo holds information about a single circuit.
type CircuitInfo struct {
	State             string
	Failures          int
	Successes         int
	ConsecutiveErrors int
	LastStateChange   time.Time
}
