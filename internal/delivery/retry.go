package delivery

import (
	"math"
	"math/rand"
	"time"

	"github.com/relay/internal/domain"
	"github.com/relay/pkg/backoff"
)

// RetryPolicy defines retry behavior.
type RetryPolicy struct {
	calculator *backoff.Calculator
}

// NewRetryPolicy creates a new retry policy with the default backoff schedule.
func NewRetryPolicy() *RetryPolicy {
	return &RetryPolicy{
		calculator: backoff.NewCalculator(),
	}
}

// WithCalculator sets a custom backoff calculator.
func (p *RetryPolicy) WithCalculator(calc *backoff.Calculator) *RetryPolicy {
	return &RetryPolicy{
		calculator: calc,
	}
}

// ShouldRetry determines if an event should be retried.
func (p *RetryPolicy) ShouldRetry(attempts, maxAttempts int) bool {
	return attempts < maxAttempts && p.calculator.ShouldRetry(attempts)
}

// NextRetryDelay returns the delay before the next retry attempt.
func (p *RetryPolicy) NextRetryDelay(attempts int) time.Duration {
	return p.calculator.Duration(attempts)
}

// NextRetryTime returns the time of the next retry attempt.
func (p *RetryPolicy) NextRetryTime(attempts int) time.Time {
	return p.calculator.NextAttemptTime(attempts)
}

// MaxAttempts returns the maximum number of retry attempts.
func (p *RetryPolicy) MaxAttempts() int {
	return p.calculator.MaxAttempts()
}

// NextRetryDelayForEndpoint calculates the retry delay using endpoint-specific configuration.
// If endpoint is nil, falls back to the default global policy.
func (p *RetryPolicy) NextRetryDelayForEndpoint(attempts int, endpoint *domain.Endpoint) time.Duration {
	if endpoint == nil {
		return p.NextRetryDelay(attempts)
	}

	// Custom backoff: initial * (mult ^ (attempts-1))
	// attempts is 1-indexed after increment
	delay := float64(endpoint.RetryBackoffMs) * math.Pow(endpoint.RetryBackoffMult, float64(attempts-1))

	// Cap at max delay
	if delay > float64(endpoint.RetryBackoffMax) {
		delay = float64(endpoint.RetryBackoffMax)
	}

	// Add jitter (10%) to prevent thundering herd
	jitter := delay * 0.1 * rand.Float64()
	delayWithJitter := delay + jitter

	return time.Duration(delayWithJitter) * time.Millisecond
}

// ShouldRetryForEndpoint determines if an event should be retried using endpoint-specific config.
func (p *RetryPolicy) ShouldRetryForEndpoint(attempts int, endpoint *domain.Endpoint) bool {
	if endpoint == nil {
		return p.ShouldRetry(attempts, p.MaxAttempts())
	}
	return attempts < endpoint.MaxRetries
}

// IsRetryableStatusCode determines if an HTTP status code should trigger a retry.
func IsRetryableStatusCode(statusCode int) bool {
	// Retry on 5xx server errors
	if statusCode >= 500 && statusCode < 600 {
		return true
	}

	// Retry on specific 4xx errors
	switch statusCode {
	case 408: // Request Timeout
		return true
	case 429: // Too Many Requests
		return true
	}

	return false
}

// IsRetryableError determines if an error should trigger a retry.
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Network errors, timeouts, and connection errors are retryable
	// This is a simplified check; in production you might want to be more specific
	return true
}
