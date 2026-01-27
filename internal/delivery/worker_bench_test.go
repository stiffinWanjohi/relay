package delivery

import (
	"net/url"
	"testing"

	"github.com/relay/internal/domain"
)

// ============================================================================
// URL Parsing Benchmarks (used in extractHost)
// ============================================================================

func BenchmarkExtractHost_Valid(b *testing.B) {
	destination := "https://webhook.example.com:8080/events"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractHost(destination)
	}
}

func BenchmarkExtractHost_Invalid(b *testing.B) {
	destination := "not-a-valid-url"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = extractHost(destination)
	}
}

func BenchmarkURLParse(b *testing.B) {
	destination := "https://webhook.example.com:8080/events/webhook?key=value"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = url.Parse(destination)
	}
}

// ============================================================================
// Failure Classification Benchmarks
// ============================================================================

func BenchmarkClassifyFailureReason_Timeout(b *testing.B) {
	result := domain.DeliveryResult{
		Error: &timeoutError{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyFailureReason(result)
	}
}

func BenchmarkClassifyFailureReason_ServerError(b *testing.B) {
	result := domain.DeliveryResult{
		StatusCode: 503,
		Error:      nil,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyFailureReason(result)
	}
}

func BenchmarkClassifyFailureReason_ClientError(b *testing.B) {
	result := domain.DeliveryResult{
		StatusCode: 404,
		Error:      nil,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyFailureReason(result)
	}
}

func BenchmarkClassifyFailureReason_ConnectionRefused(b *testing.B) {
	result := domain.DeliveryResult{
		Error: &connRefusedError{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyFailureReason(result)
	}
}

func BenchmarkClassifyFailureReason_DNSError(b *testing.B) {
	result := domain.DeliveryResult{
		Error: &dnsError{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyFailureReason(result)
	}
}

func BenchmarkClassifyFailureReason_TLSError(b *testing.B) {
	result := domain.DeliveryResult{
		Error: &tlsError{},
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = classifyFailureReason(result)
	}
}

// Error types for benchmarking
type timeoutError struct{}

func (e *timeoutError) Error() string { return "context deadline exceeded (timeout)" }

type connRefusedError struct{}

func (e *connRefusedError) Error() string { return "dial tcp: connection refused" }

type dnsError struct{}

func (e *dnsError) Error() string { return "lookup webhook.example.com: no such host" }

type tlsError struct{}

func (e *tlsError) Error() string { return "TLS handshake failed: certificate verify failed" }

// ============================================================================
// String Contains Benchmarks (used in classification)
// ============================================================================

func BenchmarkContains_Short(b *testing.B) {
	s := "connection refused"
	substr := "refused"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = contains(s, substr)
	}
}

func BenchmarkContains_Long(b *testing.B) {
	s := "dial tcp 192.168.1.1:443: connect: connection refused by remote host after timeout"
	substr := "connection refused"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = contains(s, substr)
	}
}

func BenchmarkContains_NotFound(b *testing.B) {
	s := "dial tcp 192.168.1.1:443: connect: network unreachable"
	substr := "connection refused"

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = contains(s, substr)
	}
}

// ============================================================================
// Worker Configuration Benchmarks
// ============================================================================

func BenchmarkDefaultWorkerConfig(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = DefaultWorkerConfig()
	}
}

func BenchmarkDefaultCircuitConfig(b *testing.B) {
	for i := 0; i < b.N; i++ {
		_ = DefaultCircuitConfig()
	}
}

// ============================================================================
// Event Status Transition Benchmarks
// ============================================================================

func BenchmarkEventMarkDelivering(b *testing.B) {
	evt := domain.NewEvent(
		"key-123",
		"https://webhook.example.com",
		[]byte(`{"test": true}`),
		map[string]string{"Content-Type": "application/json"},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evt.MarkDelivering()
	}
}

func BenchmarkEventMarkDelivered(b *testing.B) {
	evt := domain.NewEvent(
		"key-123",
		"https://webhook.example.com",
		[]byte(`{"test": true}`),
		map[string]string{"Content-Type": "application/json"},
	).MarkDelivering()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evt.MarkDelivered()
	}
}

func BenchmarkEventIncrementAttempts(b *testing.B) {
	evt := domain.NewEvent(
		"key-123",
		"https://webhook.example.com",
		[]byte(`{"test": true}`),
		map[string]string{"Content-Type": "application/json"},
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evt.IncrementAttempts()
	}
}

func BenchmarkEventShouldRetry_True(b *testing.B) {
	evt := domain.NewEvent(
		"key-123",
		"https://webhook.example.com",
		[]byte(`{"test": true}`),
		map[string]string{"Content-Type": "application/json"},
	)
	evt.Attempts = 3

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evt.ShouldRetry()
	}
}

func BenchmarkEventShouldRetry_False(b *testing.B) {
	evt := domain.NewEvent(
		"key-123",
		"https://webhook.example.com",
		[]byte(`{"test": true}`),
		map[string]string{"Content-Type": "application/json"},
	)
	evt.Attempts = 11

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = evt.ShouldRetry()
	}
}

// ============================================================================
// Parallel Benchmarks
// ============================================================================

func BenchmarkExtractHost_Parallel(b *testing.B) {
	destinations := []string{
		"https://api.example.com/webhooks",
		"https://hooks.stripe.com/events",
		"http://localhost:8080/callback",
		"https://webhook.site/abc-123",
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = extractHost(destinations[i%len(destinations)])
			i++
		}
	})
}

func BenchmarkClassifyFailureReason_Parallel(b *testing.B) {
	results := []domain.DeliveryResult{
		{Error: &timeoutError{}},
		{StatusCode: 503},
		{StatusCode: 404},
		{Error: &connRefusedError{}},
		{Error: &dnsError{}},
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = classifyFailureReason(results[i%len(results)])
			i++
		}
	})
}

// ============================================================================
// Delivery Attempt Creation Benchmarks
// ============================================================================

func BenchmarkNewDeliveryAttemptSuccess(b *testing.B) {
	evt := domain.NewEvent(
		"key-123",
		"https://webhook.example.com",
		[]byte(`{"test": true}`),
		nil,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		attempt := domain.NewDeliveryAttempt(evt.ID, 1)
		_ = attempt.WithSuccess(200, "OK", 150)
	}
}

func BenchmarkNewDeliveryAttemptFailure(b *testing.B) {
	evt := domain.NewEvent(
		"key-123",
		"https://webhook.example.com",
		[]byte(`{"test": true}`),
		nil,
	)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		attempt := domain.NewDeliveryAttempt(evt.ID, 1)
		_ = attempt.WithFailure(500, "Internal Server Error", "server error", 250)
	}
}

// ============================================================================
// Min Function Benchmark (used for backoff capping)
// ============================================================================

func BenchmarkMinFunction(b *testing.B) {
	a := 500
	c := 2000

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = min(a*2, c)
	}
}
