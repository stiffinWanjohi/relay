package delivery

import (
	"time"

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
