package delivery

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/url"
	"syscall"
	"testing"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

func BenchmarkRetryPolicyNextDelay(b *testing.B) {
	rp := NewRetryPolicy()

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = rp.NextRetryDelay(i%10 + 1)
		i++
	}
}

func BenchmarkRetryPolicyShouldRetry(b *testing.B) {
	rp := NewRetryPolicy()

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = rp.ShouldRetry(i%15+1, 10)
		i++
	}
}

func BenchmarkRetryPolicyForEndpoint(b *testing.B) {
	rp := NewRetryPolicy()
	endpoint := &domain.Endpoint{
		MaxRetries:       15,
		RetryBackoffMs:   500,
		RetryBackoffMax:  3600000,
		RetryBackoffMult: 1.5,
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = rp.NextRetryDelayForEndpoint(i%15+1, endpoint)
		i++
	}
}

func BenchmarkRetryPolicyShouldRetryForEndpoint(b *testing.B) {
	rp := NewRetryPolicy()
	endpoint := &domain.Endpoint{
		MaxRetries: 20,
	}

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = rp.ShouldRetryForEndpoint(i%25+1, endpoint)
		i++
	}
}

func BenchmarkRetryPolicyNilEndpoint(b *testing.B) {
	rp := NewRetryPolicy()

	b.ResetTimer()
	b.ReportAllocs()

	i := 0
	for b.Loop() {
		_ = rp.NextRetryDelayForEndpoint(i%10+1, nil)
		_ = rp.ShouldRetryForEndpoint(i%10+1, nil)
		i++
	}
}

// Benchmarks for IsRetryableError

func BenchmarkIsRetryableError_Nil(b *testing.B) {
	b.ReportAllocs()
	for b.Loop() {
		_ = IsRetryableError(nil)
	}
}

func BenchmarkIsRetryableError_ContextCanceled(b *testing.B) {
	err := context.Canceled
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = IsRetryableError(err)
	}
}

func BenchmarkIsRetryableError_Timeout(b *testing.B) {
	err := context.DeadlineExceeded
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = IsRetryableError(err)
	}
}

func BenchmarkIsRetryableError_NetError(b *testing.B) {
	err := &net.OpError{
		Op:  "dial",
		Net: "tcp",
		Err: &net.DNSError{Err: "no such host", Name: "example.com", IsTemporary: true},
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = IsRetryableError(err)
	}
}

func BenchmarkIsRetryableError_URLError(b *testing.B) {
	err := &url.Error{
		Op:  "Get",
		URL: "https://example.com",
		Err: syscall.ECONNREFUSED,
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = IsRetryableError(err)
	}
}

func BenchmarkIsRetryableError_TLSError(b *testing.B) {
	err := &tls.CertificateVerificationError{
		Err: errors.New("certificate signed by unknown authority"),
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = IsRetryableError(err)
	}
}

func BenchmarkIsRetryableError_StringMatch(b *testing.B) {
	err := errors.New("connection reset by peer")
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		_ = IsRetryableError(err)
	}
}

func BenchmarkIsRetryableError_MixedErrors(b *testing.B) {
	errs := []error{
		context.Canceled,
		context.DeadlineExceeded,
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		&url.Error{Op: "Get", URL: "https://example.com", Err: syscall.ETIMEDOUT},
		errors.New("connection reset by peer"),
		errors.New("broken pipe"),
		errors.New("certificate verification failed"),
		nil,
	}

	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		_ = IsRetryableError(errs[i%len(errs)])
		i++
	}
}

func BenchmarkIsRetryableStatusCode(b *testing.B) {
	codes := []int{200, 201, 400, 404, 408, 429, 500, 502, 503, 504}

	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		_ = IsRetryableStatusCode(codes[i%len(codes)])
		i++
	}
}

func BenchmarkIsRetryableSyscallError(b *testing.B) {
	errs := []error{
		syscall.ECONNREFUSED,
		syscall.ECONNRESET,
		syscall.ETIMEDOUT,
		syscall.EPIPE,
		syscall.ENETUNREACH,
	}

	b.ResetTimer()
	b.ReportAllocs()
	i := 0
	for b.Loop() {
		_ = isRetryableSyscallError(errs[i%len(errs)])
		i++
	}
}

// Benchmark parallel access
func BenchmarkIsRetryableError_Parallel(b *testing.B) {
	errs := []error{
		context.DeadlineExceeded,
		&net.OpError{Op: "dial", Net: "tcp", Err: syscall.ECONNREFUSED},
		errors.New("connection reset"),
		fmt.Errorf("wrapped: %w", syscall.ETIMEDOUT),
	}

	b.ResetTimer()
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = IsRetryableError(errs[i%len(errs)])
			i++
		}
	})
}
