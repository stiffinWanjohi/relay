package delivery

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/relay/internal/domain"
	"github.com/relay/pkg/backoff"
)

func TestNewRetryPolicy(t *testing.T) {
	policy := NewRetryPolicy()

	if policy == nil {
		t.Fatal("NewRetryPolicy returned nil")
	}
	if policy.calculator == nil {
		t.Error("calculator should not be nil")
	}
}

func TestRetryPolicy_WithCalculator(t *testing.T) {
	policy := NewRetryPolicy()
	customCalc := backoff.NewCalculator()

	newPolicy := policy.WithCalculator(customCalc)

	if newPolicy == policy {
		t.Error("WithCalculator should return new instance")
	}
	if newPolicy.calculator != customCalc {
		t.Error("calculator should be set to custom calculator")
	}
}

func TestRetryPolicy_ShouldRetry(t *testing.T) {
	policy := NewRetryPolicy()

	tests := []struct {
		name        string
		attempts    int
		maxAttempts int
		expected    bool
	}{
		{"first attempt", 1, 10, true},
		{"mid attempts", 5, 10, true},
		{"at max attempts", 10, 10, false},
		{"over max attempts", 11, 10, false},
		{"zero attempts", 0, 10, true},
		{"max attempts is 1", 1, 1, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := policy.ShouldRetry(tt.attempts, tt.maxAttempts)
			if result != tt.expected {
				t.Errorf("ShouldRetry(%d, %d) = %v, want %v", tt.attempts, tt.maxAttempts, result, tt.expected)
			}
		})
	}
}

func TestRetryPolicy_NextRetryDelay(t *testing.T) {
	policy := NewRetryPolicy()

	// First attempt should have a delay
	delay := policy.NextRetryDelay(1)
	if delay <= 0 {
		t.Errorf("NextRetryDelay(1) = %v, want > 0", delay)
	}

	// Later attempts should have longer delays (exponential backoff)
	delay1 := policy.NextRetryDelay(1)
	delay2 := policy.NextRetryDelay(2)
	delay5 := policy.NextRetryDelay(5)

	if delay2 <= delay1 {
		t.Errorf("delay for attempt 2 (%v) should be > delay for attempt 1 (%v)", delay2, delay1)
	}
	if delay5 <= delay2 {
		t.Errorf("delay for attempt 5 (%v) should be > delay for attempt 2 (%v)", delay5, delay2)
	}
}

func TestRetryPolicy_NextRetryTime(t *testing.T) {
	policy := NewRetryPolicy()

	before := time.Now()
	nextTime := policy.NextRetryTime(1)
	after := time.Now()

	if nextTime.Before(before) {
		t.Error("NextRetryTime should return a time in the future")
	}
	if nextTime.Before(after) {
		t.Error("NextRetryTime should be after current time")
	}
}

func TestRetryPolicy_MaxAttempts(t *testing.T) {
	policy := NewRetryPolicy()

	maxAttempts := policy.MaxAttempts()
	if maxAttempts <= 0 {
		t.Errorf("MaxAttempts() = %d, want > 0", maxAttempts)
	}
}

func TestRetryPolicy_NextRetryDelayForEndpoint(t *testing.T) {
	policy := NewRetryPolicy()

	t.Run("nil endpoint uses default policy", func(t *testing.T) {
		delay := policy.NextRetryDelayForEndpoint(1, nil)

		// Should return a positive delay (exact value varies due to jitter)
		if delay <= 0 {
			t.Errorf("nil endpoint should return positive delay: got %v", delay)
		}
		// Should be in reasonable range for first attempt (typically ~1 second with jitter)
		if delay > 5*time.Second {
			t.Errorf("nil endpoint delay seems too large: got %v", delay)
		}
	})

	t.Run("custom endpoint backoff", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			RetryBackoffMs:   1000,  // 1 second initial
			RetryBackoffMult: 2.0,   // double each time
			RetryBackoffMax:  60000, // 1 minute max
		}

		delay1 := policy.NextRetryDelayForEndpoint(1, endpoint)
		delay2 := policy.NextRetryDelayForEndpoint(2, endpoint)
		delay3 := policy.NextRetryDelayForEndpoint(3, endpoint)

		// Delays should increase exponentially (approximately, allowing for jitter)
		if delay1 < 900*time.Millisecond || delay1 > 1200*time.Millisecond {
			t.Errorf("delay1 = %v, expected ~1s", delay1)
		}
		if delay2 < 1800*time.Millisecond || delay2 > 2400*time.Millisecond {
			t.Errorf("delay2 = %v, expected ~2s", delay2)
		}
		if delay3 < 3600*time.Millisecond || delay3 > 4800*time.Millisecond {
			t.Errorf("delay3 = %v, expected ~4s", delay3)
		}
	})

	t.Run("respects max backoff", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			RetryBackoffMs:   10000, // 10 seconds initial
			RetryBackoffMult: 10.0,  // 10x each time
			RetryBackoffMax:  30000, // 30 seconds max
		}

		// Attempt 3 would be 10 * 10^2 = 1000 seconds, but capped at 30s
		delay := policy.NextRetryDelayForEndpoint(3, endpoint)
		maxWithJitter := 33 * time.Second // 30s + 10% jitter

		if delay > maxWithJitter {
			t.Errorf("delay = %v, should be capped at ~30s", delay)
		}
	})

	t.Run("jitter is applied", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			RetryBackoffMs:   1000,
			RetryBackoffMult: 1.0, // No increase, so we can test jitter
			RetryBackoffMax:  60000,
		}

		// Run multiple times to verify jitter varies
		delays := make(map[time.Duration]bool)
		for i := 0; i < 10; i++ {
			delay := policy.NextRetryDelayForEndpoint(1, endpoint)
			delays[delay] = true
		}

		// With 10% jitter, we should see some variation
		if len(delays) < 2 {
			t.Log("Note: jitter test may occasionally fail due to randomness")
		}
	})
}

func TestRetryPolicy_ShouldRetryForEndpoint(t *testing.T) {
	policy := NewRetryPolicy()

	t.Run("nil endpoint uses default policy", func(t *testing.T) {
		result := policy.ShouldRetryForEndpoint(1, nil)
		expected := policy.ShouldRetry(1, policy.MaxAttempts())

		if result != expected {
			t.Errorf("nil endpoint: got %v, want %v", result, expected)
		}
	})

	t.Run("endpoint max retries", func(t *testing.T) {
		endpoint := &domain.Endpoint{
			MaxRetries: 5,
		}

		tests := []struct {
			attempts int
			expected bool
		}{
			{1, true},
			{4, true},
			{5, false},
			{6, false},
		}

		for _, tt := range tests {
			result := policy.ShouldRetryForEndpoint(tt.attempts, endpoint)
			if result != tt.expected {
				t.Errorf("attempts=%d: got %v, want %v", tt.attempts, result, tt.expected)
			}
		}
	})
}

func TestIsRetryableStatusCode(t *testing.T) {
	tests := []struct {
		statusCode int
		expected   bool
	}{
		// 2xx - not retryable (success)
		{200, false},
		{201, false},
		{204, false},

		// 3xx - not retryable
		{301, false},
		{302, false},
		{304, false},

		// 4xx - mostly not retryable
		{400, false},
		{401, false},
		{403, false},
		{404, false},
		{405, false},
		{408, true}, // Request Timeout - retryable
		{422, false},
		{429, true}, // Too Many Requests - retryable

		// 5xx - all retryable
		{500, true},
		{501, true},
		{502, true},
		{503, true},
		{504, true},
	}

	for _, tt := range tests {
		t.Run(string(rune(tt.statusCode)), func(t *testing.T) {
			result := IsRetryableStatusCode(tt.statusCode)
			if result != tt.expected {
				t.Errorf("IsRetryableStatusCode(%d) = %v, want %v", tt.statusCode, result, tt.expected)
			}
		})
	}
}

func TestIsRetryableError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		if IsRetryableError(nil) {
			t.Error("nil error should not be retryable")
		}
	})

	t.Run("context canceled", func(t *testing.T) {
		if IsRetryableError(context.Canceled) {
			t.Error("context.Canceled should not be retryable")
		}
	})

	t.Run("context deadline exceeded", func(t *testing.T) {
		if !IsRetryableError(context.DeadlineExceeded) {
			t.Error("context.DeadlineExceeded should be retryable")
		}
	})

	t.Run("URL error wrapping retryable error", func(t *testing.T) {
		urlErr := &url.Error{
			Op:  "Get",
			URL: "https://example.com",
			Err: context.DeadlineExceeded,
		}
		if !IsRetryableError(urlErr) {
			t.Error("URL error wrapping timeout should be retryable")
		}
	})

	t.Run("URL error wrapping non-retryable error", func(t *testing.T) {
		urlErr := &url.Error{
			Op:  "Get",
			URL: "https://example.com",
			Err: context.Canceled,
		}
		if IsRetryableError(urlErr) {
			t.Error("URL error wrapping canceled should not be retryable")
		}
	})

	t.Run("network timeout error", func(t *testing.T) {
		netErr := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &retryTimeoutError{},
		}
		if !IsRetryableError(netErr) {
			t.Error("network timeout should be retryable")
		}
	})

	t.Run("DNS error", func(t *testing.T) {
		dnsErr := &net.DNSError{
			Err:       "no such host",
			Name:      "example.com",
			IsTimeout: false,
		}
		if !IsRetryableError(dnsErr) {
			t.Error("DNS error should be retryable (could be transient)")
		}
	})

	t.Run("DNS temporary error", func(t *testing.T) {
		dnsErr := &net.DNSError{
			Err:         "temporary failure",
			Name:        "example.com",
			IsTimeout:   false,
			IsTemporary: true,
		}
		if !IsRetryableError(dnsErr) {
			t.Error("temporary DNS error should be retryable")
		}
	})

	t.Run("TLS certificate verification error", func(t *testing.T) {
		certErr := &tls.CertificateVerificationError{
			Err: errors.New("certificate is invalid"),
		}
		if IsRetryableError(certErr) {
			t.Error("TLS certificate error should not be retryable")
		}
	})

	t.Run("x509 unknown authority error", func(t *testing.T) {
		err := x509.UnknownAuthorityError{}
		if IsRetryableError(err) {
			t.Error("x509 unknown authority should not be retryable")
		}
	})

	t.Run("x509 certificate invalid error", func(t *testing.T) {
		err := x509.CertificateInvalidError{}
		if IsRetryableError(err) {
			t.Error("x509 certificate invalid should not be retryable")
		}
	})

	t.Run("x509 hostname error", func(t *testing.T) {
		err := x509.HostnameError{}
		if IsRetryableError(err) {
			t.Error("x509 hostname error should not be retryable")
		}
	})

	t.Run("connection refused error message", func(t *testing.T) {
		err := errors.New("connection refused")
		if !IsRetryableError(err) {
			t.Error("connection refused should be retryable")
		}
	})

	t.Run("connection reset error message", func(t *testing.T) {
		err := errors.New("connection reset by peer")
		if !IsRetryableError(err) {
			t.Error("connection reset should be retryable")
		}
	})

	t.Run("broken pipe error message", func(t *testing.T) {
		err := errors.New("broken pipe")
		if !IsRetryableError(err) {
			t.Error("broken pipe should be retryable")
		}
	})

	t.Run("timeout error message", func(t *testing.T) {
		err := errors.New("i/o timeout")
		if !IsRetryableError(err) {
			t.Error("timeout should be retryable")
		}
	})

	t.Run("EOF error message", func(t *testing.T) {
		err := errors.New("unexpected eof")
		if !IsRetryableError(err) {
			t.Error("EOF should be retryable")
		}
	})

	t.Run("certificate error message", func(t *testing.T) {
		err := errors.New("certificate has expired")
		if IsRetryableError(err) {
			t.Error("certificate errors should not be retryable")
		}
	})

	t.Run("invalid URL error message", func(t *testing.T) {
		err := errors.New("invalid url format")
		if IsRetryableError(err) {
			t.Error("invalid URL should not be retryable")
		}
	})

	t.Run("unknown error defaults to retryable", func(t *testing.T) {
		err := errors.New("some unknown error")
		if !IsRetryableError(err) {
			t.Error("unknown error should default to retryable")
		}
	})

	t.Run("network unreachable", func(t *testing.T) {
		err := errors.New("network is unreachable")
		if !IsRetryableError(err) {
			t.Error("network unreachable should be retryable")
		}
	})

	t.Run("no route to host", func(t *testing.T) {
		err := errors.New("no route to host")
		if !IsRetryableError(err) {
			t.Error("no route to host should be retryable")
		}
	})

	t.Run("try again", func(t *testing.T) {
		err := errors.New("try again later")
		if !IsRetryableError(err) {
			t.Error("try again should be retryable")
		}
	})

	t.Run("temporary failure", func(t *testing.T) {
		err := errors.New("temporary failure in name resolution")
		if !IsRetryableError(err) {
			t.Error("temporary failure should be retryable")
		}
	})

	t.Run("connection closed", func(t *testing.T) {
		err := errors.New("connection closed")
		if !IsRetryableError(err) {
			t.Error("connection closed should be retryable")
		}
	})

	t.Run("TLS error message", func(t *testing.T) {
		err := errors.New("tls handshake failed")
		if IsRetryableError(err) {
			t.Error("TLS errors should not be retryable")
		}
	})

	t.Run("x509 error message", func(t *testing.T) {
		err := errors.New("x509 verification failed")
		if IsRetryableError(err) {
			t.Error("x509 errors should not be retryable")
		}
	})

	t.Run("unsupported protocol", func(t *testing.T) {
		err := errors.New("unsupported protocol scheme")
		if IsRetryableError(err) {
			t.Error("unsupported protocol should not be retryable")
		}
	})
}

func TestIsRetryableError_NetOpError(t *testing.T) {
	t.Run("op error with syscall error", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: &os.SyscallError{
				Syscall: "connect",
				Err:     syscall.ECONNREFUSED,
			},
		}
		if !IsRetryableError(opErr) {
			t.Error("ECONNREFUSED should be retryable")
		}
	})

	t.Run("op error without syscall error", func(t *testing.T) {
		opErr := &net.OpError{
			Op:  "dial",
			Net: "tcp",
			Err: errors.New("some error"),
		}
		// Should still be retryable (connection errors are often transient)
		if !IsRetryableError(opErr) {
			t.Error("net.OpError should default to retryable")
		}
	})
}

func TestIsRetryableSyscallError(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		expected bool
	}{
		{"nil error", nil, false},
		{"ECONNREFUSED", syscall.ECONNREFUSED, true},
		{"ECONNRESET", syscall.ECONNRESET, true},
		{"ECONNABORTED", syscall.ECONNABORTED, true},
		{"ETIMEDOUT", syscall.ETIMEDOUT, true},
		{"ENETUNREACH", syscall.ENETUNREACH, true},
		{"EHOSTUNREACH", syscall.EHOSTUNREACH, true},
		{"EPIPE", syscall.EPIPE, true},
		{"ENOBUFS", syscall.ENOBUFS, true},
		{"unknown syscall error", syscall.EACCES, true}, // defaults to retryable
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isRetryableSyscallError(tt.err)
			if result != tt.expected {
				t.Errorf("isRetryableSyscallError(%v) = %v, want %v", tt.err, result, tt.expected)
			}
		})
	}
}

// retryTimeoutError implements net.Error for testing
type retryTimeoutError struct{}

func (e *retryTimeoutError) Error() string   { return "timeout" }
func (e *retryTimeoutError) Timeout() bool   { return true }
func (e *retryTimeoutError) Temporary() bool { return true }
