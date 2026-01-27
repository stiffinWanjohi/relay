package domain

import (
	"time"

	"github.com/google/uuid"
)

// EndpointStatus represents the operational state of an endpoint.
type EndpointStatus string

const (
	EndpointStatusActive   EndpointStatus = "active"
	EndpointStatusPaused   EndpointStatus = "paused"
	EndpointStatusDisabled EndpointStatus = "disabled"
)

// Default endpoint configuration values.
const (
	DefaultMaxRetries       = 10
	DefaultRetryBackoffMs   = 1000
	DefaultRetryBackoffMax  = 86400000 // 24 hours in ms
	DefaultRetryBackoffMult = 2.0
	DefaultTimeoutMs        = 30000 // 30 seconds
	DefaultCircuitThreshold = 5
	DefaultCircuitResetMs   = 300000 // 5 minutes
)

// Endpoint represents a webhook destination that subscribes to event types.
// Each endpoint has its own delivery configuration including retry policy,
// timeout, rate limiting, and circuit breaker settings.
type Endpoint struct {
	ID          uuid.UUID
	ClientID    string
	URL         string
	Description string
	EventTypes  []string // Event types this endpoint subscribes to
	Status      EndpointStatus

	// Retry configuration
	MaxRetries       int     // Maximum delivery attempts
	RetryBackoffMs   int     // Initial backoff delay in milliseconds
	RetryBackoffMax  int     // Maximum backoff delay in milliseconds
	RetryBackoffMult float64 // Backoff multiplier

	// Delivery configuration
	TimeoutMs       int // HTTP request timeout in milliseconds
	RateLimitPerSec int // Requests per second (0 = unlimited)

	// Circuit breaker configuration
	CircuitThreshold int // Failures before opening circuit
	CircuitResetMs   int // Time before attempting recovery

	// Custom headers to include with every delivery
	CustomHeaders map[string]string

	// Metadata
	CreatedAt time.Time
	UpdatedAt time.Time
}

// NewEndpoint creates a new endpoint with default configuration.
func NewEndpoint(clientID, url string, eventTypes []string) Endpoint {
	now := time.Now().UTC()
	return Endpoint{
		ID:               uuid.New(),
		ClientID:         clientID,
		URL:              url,
		EventTypes:       eventTypes,
		Status:           EndpointStatusActive,
		MaxRetries:       DefaultMaxRetries,
		RetryBackoffMs:   DefaultRetryBackoffMs,
		RetryBackoffMax:  DefaultRetryBackoffMax,
		RetryBackoffMult: DefaultRetryBackoffMult,
		TimeoutMs:        DefaultTimeoutMs,
		CircuitThreshold: DefaultCircuitThreshold,
		CircuitResetMs:   DefaultCircuitResetMs,
		CustomHeaders:    make(map[string]string),
		CreatedAt:        now,
		UpdatedAt:        now,
	}
}

// IsActive returns true if the endpoint can receive events.
func (e Endpoint) IsActive() bool {
	return e.Status == EndpointStatusActive
}

// SubscribesTo returns true if the endpoint subscribes to the given event type.
// An empty EventTypes slice means the endpoint subscribes to all event types.
func (e Endpoint) SubscribesTo(eventType string) bool {
	if len(e.EventTypes) == 0 {
		return true // Wildcard subscription
	}
	for _, et := range e.EventTypes {
		if et == eventType || et == "*" {
			return true
		}
	}
	return false
}

// Pause returns a new endpoint with paused status.
func (e Endpoint) Pause() Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           EndpointStatusPaused,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// Resume returns a new endpoint with active status.
func (e Endpoint) Resume() Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           EndpointStatusActive,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// Disable returns a new endpoint with disabled status.
func (e Endpoint) Disable() Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           EndpointStatusDisabled,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// WithRetryConfig returns a new endpoint with updated retry configuration.
func (e Endpoint) WithRetryConfig(maxRetries, backoffMs, backoffMax int, backoffMult float64) Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		MaxRetries:       maxRetries,
		RetryBackoffMs:   backoffMs,
		RetryBackoffMax:  backoffMax,
		RetryBackoffMult: backoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// WithTimeout returns a new endpoint with updated timeout.
func (e Endpoint) WithTimeout(timeoutMs int) Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        timeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// WithRateLimit returns a new endpoint with updated rate limit.
func (e Endpoint) WithRateLimit(rps int) Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  rps,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// GetTimeoutDuration returns the timeout as a time.Duration.
func (e Endpoint) GetTimeoutDuration() time.Duration {
	return time.Duration(e.TimeoutMs) * time.Millisecond
}

// GetCircuitResetDuration returns the circuit reset time as a time.Duration.
func (e Endpoint) GetCircuitResetDuration() time.Duration {
	return time.Duration(e.CircuitResetMs) * time.Millisecond
}
