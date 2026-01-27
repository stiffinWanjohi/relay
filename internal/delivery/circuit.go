package delivery

import (
	"sync"
	"time"
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

// circuit represents a single circuit breaker instance.
type circuit struct {
	state             CircuitState
	failures          int
	successes         int
	lastStateChange   time.Time
	consecutiveErrors int
}

// CircuitBreaker manages circuit breakers per destination.
type CircuitBreaker struct {
	config   CircuitConfig
	circuits map[string]*circuit
	mu       sync.Mutex // Use single Mutex to avoid race condition between RLock and Lock
}

// NewCircuitBreaker creates a new circuit breaker manager.
func NewCircuitBreaker(config CircuitConfig) *CircuitBreaker {
	return &CircuitBreaker{
		config:   config,
		circuits: make(map[string]*circuit),
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

	c.consecutiveErrors = 0

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
}

// RecordFailure records a failed delivery.
func (cb *CircuitBreaker) RecordFailure(destination string) {
	cb.mu.Lock()
	defer cb.mu.Unlock()

	c, exists := cb.circuits[destination]
	if !exists {
		c = &circuit{
			state:           CircuitClosed,
			lastStateChange: time.Now(),
		}
		cb.circuits[destination] = c
	}

	c.consecutiveErrors++

	switch c.state {
	case CircuitClosed:
		c.failures++
		if c.failures >= cb.config.FailureThreshold {
			c.state = CircuitOpen
			c.lastStateChange = time.Now()
		}
	case CircuitHalfOpen:
		// Any failure in half-open returns to open
		c.state = CircuitOpen
		c.successes = 0
		c.lastStateChange = time.Now()
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
