package delivery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"math"
	"math/rand"
	"net"
	"net/url"
	"os"
	"strings"
	"syscall"
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
// Returns true for transient errors (network issues, timeouts, temporary failures).
// Returns false for permanent errors (invalid URLs, certificate errors, etc.).
func IsRetryableError(err error) bool {
	if err == nil {
		return false
	}

	// Context cancellation/deadline - not retryable (caller requested stop)
	if errors.Is(err, context.Canceled) {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true // Timeout is retryable
	}

	// URL parsing errors - not retryable (bad configuration)
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		// Check the underlying error
		return IsRetryableError(urlErr.Err)
	}

	// Network errors
	var netErr net.Error
	if errors.As(err, &netErr) {
		// Timeouts are retryable
		if netErr.Timeout() {
			return true
		}
	}

	// DNS errors
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		// Temporary DNS failures are retryable
		if dnsErr.Temporary() {
			return true
		}
		// "no such host" is usually permanent but could be transient DNS propagation
		// Be conservative and retry a few times
		return true
	}

	// Connection errors
	var opErr *net.OpError
	if errors.As(err, &opErr) {
		// Check for specific syscall errors
		if sysErr, ok := opErr.Err.(*os.SyscallError); ok {
			return isRetryableSyscallError(sysErr.Err)
		}
		// Connection refused, reset, etc. are often transient
		return true
	}

	// TLS/Certificate errors - generally NOT retryable (security issue)
	var certErr *tls.CertificateVerificationError
	if errors.As(err, &certErr) {
		return false
	}
	var unknownAuthErr x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthErr) {
		return false
	}
	var certInvalidErr x509.CertificateInvalidError
	if errors.As(err, &certInvalidErr) {
		return false
	}
	var hostErr x509.HostnameError
	if errors.As(err, &hostErr) {
		return false
	}

	// Check error message for common patterns
	errMsg := strings.ToLower(err.Error())

	// Retryable patterns
	retryablePatterns := []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"no route to host",
		"network is unreachable",
		"i/o timeout",
		"timeout",
		"temporary failure",
		"try again",
		"eof",
		"connection closed",
	}
	for _, pattern := range retryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return true
		}
	}

	// Non-retryable patterns
	nonRetryablePatterns := []string{
		"certificate",
		"x509",
		"tls",
		"invalid url",
		"unsupported protocol",
		"no such host", // After DNS retry exhaustion
	}
	for _, pattern := range nonRetryablePatterns {
		if strings.Contains(errMsg, pattern) {
			return false
		}
	}

	// Default: retry on unknown errors (fail-safe for transient issues)
	return true
}

// isRetryableSyscallError checks if a syscall error is retryable.
func isRetryableSyscallError(err error) bool {
	if err == nil {
		return false
	}

	// Check for specific syscall errors
	switch {
	case errors.Is(err, syscall.ECONNREFUSED): // Connection refused
		return true
	case errors.Is(err, syscall.ECONNRESET): // Connection reset by peer
		return true
	case errors.Is(err, syscall.ECONNABORTED): // Connection aborted
		return true
	case errors.Is(err, syscall.ETIMEDOUT): // Connection timed out
		return true
	case errors.Is(err, syscall.ENETUNREACH): // Network unreachable
		return true
	case errors.Is(err, syscall.EHOSTUNREACH): // Host unreachable
		return true
	case errors.Is(err, syscall.EPIPE): // Broken pipe
		return true
	case errors.Is(err, syscall.ENOBUFS): // No buffer space available
		return true
	}

	return true // Default to retryable for unknown syscall errors
}
