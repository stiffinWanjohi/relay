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

	// Content-based routing filter (optional)
	// If set, events are only delivered if they match the filter conditions
	Filter []byte // JSON-encoded Filter struct

	// Payload transformation (optional)
	// JavaScript code to transform the webhook before delivery
	Transformation string

	// FIFO (ordered delivery) configuration
	// When enabled, events are delivered sequentially - one at a time
	FIFO bool
	// FIFOPartitionKey is a JSONPath expression to extract a partition key from the payload
	// Events with the same partition key are delivered in order
	// Different partition keys can be processed in parallel
	// Example: "$.customer_id" or "$.data.order_id"
	FIFOPartitionKey string

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

	// Signing secret for webhook signatures (per-endpoint)
	SigningSecret   string     // Current signing secret (if empty, use global)
	PreviousSecret  string     // Previous secret for rotation grace period
	SecretRotatedAt *time.Time // When the secret was last rotated

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
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
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
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
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
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
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
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       maxRetries,
		RetryBackoffMs:   backoffMs,
		RetryBackoffMax:  backoffMax,
		RetryBackoffMult: backoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
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
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        timeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
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
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  rps,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
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

// RotateSecret rotates the signing secret, moving current to previous.
func (e Endpoint) RotateSecret(newSecret string) Endpoint {
	now := time.Now().UTC()
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    newSecret,
		PreviousSecret:   e.SigningSecret, // Move current to previous
		SecretRotatedAt:  &now,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        now,
	}
}

// ClearPreviousSecret removes the previous secret after rotation grace period.
func (e Endpoint) ClearPreviousSecret() Endpoint {
	now := time.Now().UTC()
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   "", // Clear previous
		SecretRotatedAt:  e.SecretRotatedAt,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        now,
	}
}

// HasCustomSecret returns true if the endpoint has a custom signing secret.
func (e Endpoint) HasCustomSecret() bool {
	return e.SigningSecret != ""
}

// GetSigningSecrets returns all active signing secrets for this endpoint.
// During rotation, both current and previous secrets are valid.
func (e Endpoint) GetSigningSecrets() []string {
	secrets := make([]string, 0, 2)
	if e.SigningSecret != "" {
		secrets = append(secrets, e.SigningSecret)
	}
	if e.PreviousSecret != "" {
		secrets = append(secrets, e.PreviousSecret)
	}
	return secrets
}

// HasFilter returns true if the endpoint has a content-based filter configured.
func (e Endpoint) HasFilter() bool {
	return len(e.Filter) > 0
}

// GetFilter parses and returns the endpoint's filter.
// Returns an empty filter if no filter is configured.
func (e Endpoint) GetFilter() (Filter, error) {
	if !e.HasFilter() {
		return Filter{}, nil
	}
	return ParseFilter(e.Filter)
}

// MatchesFilter evaluates whether the given payload matches the endpoint's filter.
// Returns true if no filter is configured or if the payload matches.
func (e Endpoint) MatchesFilter(payload []byte) (bool, error) {
	if !e.HasFilter() {
		return true, nil // No filter means match everything
	}

	filter, err := e.GetFilter()
	if err != nil {
		return false, err
	}

	return filter.Evaluate(payload)
}

// WithFilter returns a new endpoint with the specified filter.
func (e Endpoint) WithFilter(filter []byte) Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		Filter:           filter,
		Transformation:   e.Transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// HasTransformation returns true if the endpoint has a payload transformation configured.
func (e Endpoint) HasTransformation() bool {
	return e.Transformation != ""
}

// WithTransformation returns a new endpoint with the specified transformation.
func (e Endpoint) WithTransformation(transformation string) Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		Filter:           e.Filter,
		Transformation:   transformation,
		FIFO:             e.FIFO,
		FIFOPartitionKey: e.FIFOPartitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}

// IsFIFO returns true if the endpoint requires ordered delivery.
func (e Endpoint) IsFIFO() bool {
	return e.FIFO
}

// HasPartitionKey returns true if the endpoint has a partition key configured.
func (e Endpoint) HasPartitionKey() bool {
	return e.FIFOPartitionKey != ""
}

// WithFIFO returns a new endpoint with FIFO configuration.
func (e Endpoint) WithFIFO(fifo bool, partitionKey string) Endpoint {
	return Endpoint{
		ID:               e.ID,
		ClientID:         e.ClientID,
		URL:              e.URL,
		Description:      e.Description,
		EventTypes:       e.EventTypes,
		Status:           e.Status,
		Filter:           e.Filter,
		Transformation:   e.Transformation,
		FIFO:             fifo,
		FIFOPartitionKey: partitionKey,
		MaxRetries:       e.MaxRetries,
		RetryBackoffMs:   e.RetryBackoffMs,
		RetryBackoffMax:  e.RetryBackoffMax,
		RetryBackoffMult: e.RetryBackoffMult,
		TimeoutMs:        e.TimeoutMs,
		RateLimitPerSec:  e.RateLimitPerSec,
		CircuitThreshold: e.CircuitThreshold,
		CircuitResetMs:   e.CircuitResetMs,
		CustomHeaders:    e.CustomHeaders,
		SigningSecret:    e.SigningSecret,
		PreviousSecret:   e.PreviousSecret,
		SecretRotatedAt:  e.SecretRotatedAt,
		CreatedAt:        e.CreatedAt,
		UpdatedAt:        time.Now().UTC(),
	}
}
