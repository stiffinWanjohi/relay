package delivery

import (
	"errors"
	"testing"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

func TestExtractHost(t *testing.T) {
	tests := []struct {
		name        string
		destination string
		expected    string
	}{
		{
			name:        "simple https URL",
			destination: "https://example.com/webhook",
			expected:    "example.com",
		},
		{
			name:        "https with port",
			destination: "https://api.example.com:8080/v1/hook",
			expected:    "api.example.com:8080",
		},
		{
			name:        "http localhost with port",
			destination: "http://localhost:3000/callback",
			expected:    "localhost:3000",
		},
		{
			name:        "URL with credentials",
			destination: "https://user:pass@secure.example.com/hook",
			expected:    "secure.example.com",
		},
		{
			name:        "URL with query params",
			destination: "https://api.example.com/webhook?token=abc",
			expected:    "api.example.com",
		},
		{
			name:        "URL with fragment",
			destination: "https://api.example.com/webhook#section",
			expected:    "api.example.com",
		},
		{
			name:        "URL with path",
			destination: "https://api.example.com/v1/v2/v3/webhook",
			expected:    "api.example.com",
		},
		{
			name:        "IP address",
			destination: "http://192.168.1.1:8080/webhook",
			expected:    "192.168.1.1:8080",
		},
		{
			name:        "IPv6 address",
			destination: "http://[::1]:8080/webhook",
			expected:    "[::1]:8080",
		},
		{
			name:        "missing scheme",
			destination: "://missing-scheme",
			expected:    "unknown",
		},
		{
			name:        "empty string",
			destination: "",
			expected:    "",
		},
		{
			name:        "subdomain",
			destination: "https://api.v1.example.com/webhook",
			expected:    "api.v1.example.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractHost(tt.destination)
			if result != tt.expected {
				t.Errorf("extractHost(%q) = %q, want %q", tt.destination, result, tt.expected)
			}
		})
	}
}

func TestClassifyFailureReason(t *testing.T) {
	tests := []struct {
		name     string
		result   domain.DeliveryResult
		expected string
	}{
		// Error-based classifications
		{
			name:     "timeout error",
			result:   domain.NewFailureResult(0, "", errors.New("connection timeout"), 100),
			expected: "timeout",
		},
		{
			name:     "timeout in middle of message",
			result:   domain.NewFailureResult(0, "", errors.New("request failed: timeout exceeded"), 100),
			expected: "timeout",
		},
		{
			name:     "connection refused",
			result:   domain.NewFailureResult(0, "", errors.New("dial tcp: connection refused"), 100),
			expected: "connection_refused",
		},
		{
			name:     "connection refused variant",
			result:   domain.NewFailureResult(0, "", errors.New("connect: connection refused"), 100),
			expected: "connection_refused",
		},
		{
			name:     "DNS error - no such host",
			result:   domain.NewFailureResult(0, "", errors.New("lookup example.com: no such host"), 100),
			expected: "dns_error",
		},
		{
			name:     "TLS handshake error",
			result:   domain.NewFailureResult(0, "", errors.New("TLS handshake error"), 100),
			expected: "tls_error",
		},
		{
			name:     "TLS certificate error",
			result:   domain.NewFailureResult(0, "", errors.New("x509: certificate signed by unknown authority (TLS)"), 100),
			expected: "tls_error",
		},
		{
			name:     "generic network error",
			result:   domain.NewFailureResult(0, "", errors.New("network unreachable"), 100),
			expected: "network_error",
		},
		{
			name:     "EOF error",
			result:   domain.NewFailureResult(0, "", errors.New("unexpected EOF"), 100),
			expected: "network_error",
		},

		// Status code based classifications
		{
			name:     "server error 500",
			result:   domain.NewFailureResult(500, "Internal Server Error", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 501",
			result:   domain.NewFailureResult(501, "Not Implemented", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 502",
			result:   domain.NewFailureResult(502, "Bad Gateway", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 503",
			result:   domain.NewFailureResult(503, "Service Unavailable", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 504",
			result:   domain.NewFailureResult(504, "Gateway Timeout", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 599",
			result:   domain.NewFailureResult(599, "", nil, 100),
			expected: "server_error",
		},
		{
			name:     "client error 400",
			result:   domain.NewFailureResult(400, "Bad Request", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 401",
			result:   domain.NewFailureResult(401, "Unauthorized", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 403",
			result:   domain.NewFailureResult(403, "Forbidden", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 404",
			result:   domain.NewFailureResult(404, "Not Found", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 429",
			result:   domain.NewFailureResult(429, "Too Many Requests", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 499",
			result:   domain.NewFailureResult(499, "", nil, 100),
			expected: "client_error",
		},

		// Unknown status codes
		{
			name:     "unknown status 0",
			result:   domain.NewFailureResult(0, "", nil, 100),
			expected: "unknown",
		},
		{
			name:     "redirect 301",
			result:   domain.NewFailureResult(301, "", nil, 100),
			expected: "unknown",
		},
		{
			name:     "redirect 302",
			result:   domain.NewFailureResult(302, "", nil, 100),
			expected: "unknown",
		},
		{
			name:     "informational 100",
			result:   domain.NewFailureResult(100, "", nil, 100),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyFailureReason(tt.result)
			if result != tt.expected {
				t.Errorf("classifyFailureReason() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestContainsSubstr(t *testing.T) {
	tests := []struct {
		name     string
		s        string
		substr   string
		expected bool
	}{
		// Positive cases
		{
			name:     "contains at end",
			s:        "hello world",
			substr:   "world",
			expected: true,
		},
		{
			name:     "contains at start",
			s:        "hello world",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "contains in middle",
			s:        "hello world",
			substr:   "lo wo",
			expected: true,
		},
		{
			name:     "exact match",
			s:        "hello",
			substr:   "hello",
			expected: true,
		},
		{
			name:     "empty substr",
			s:        "hello",
			substr:   "",
			expected: true,
		},
		{
			name:     "both empty",
			s:        "",
			substr:   "",
			expected: true,
		},
		{
			name:     "real world timeout",
			s:        "connection timeout occurred",
			substr:   "timeout",
			expected: true,
		},
		{
			name:     "single char match",
			s:        "abc",
			substr:   "b",
			expected: true,
		},

		// Negative cases
		{
			name:     "not contains",
			s:        "hello",
			substr:   "world",
			expected: false,
		},
		{
			name:     "empty string, non-empty substr",
			s:        "",
			substr:   "a",
			expected: false,
		},
		{
			name:     "case sensitive - uppercase",
			s:        "TIMEOUT",
			substr:   "timeout",
			expected: false,
		},
		{
			name:     "case sensitive - lowercase",
			s:        "timeout",
			substr:   "TIMEOUT",
			expected: false,
		},
		{
			name:     "partial match not enough",
			s:        "time",
			substr:   "timeout",
			expected: false,
		},
		{
			name:     "substr longer than string",
			s:        "hi",
			substr:   "hello",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := containsSubstr(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("containsSubstr(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestProcessorKey(t *testing.T) {
	tests := []struct {
		name         string
		endpointID   string
		partitionKey string
		expected     string
	}{
		{
			name:         "empty partition key",
			endpointID:   "endpoint-1",
			partitionKey: "",
			expected:     "endpoint-1",
		},
		{
			name:         "with partition key",
			endpointID:   "endpoint-1",
			partitionKey: "partition-a",
			expected:     "endpoint-1:partition-a",
		},
		{
			name:         "short keys",
			endpointID:   "ep",
			partitionKey: "pk",
			expected:     "ep:pk",
		},
		{
			name:         "uuid format",
			endpointID:   "550e8400-e29b-41d4-a716-446655440000",
			partitionKey: "user-123",
			expected:     "550e8400-e29b-41d4-a716-446655440000:user-123",
		},
		{
			name:         "partition with colons",
			endpointID:   "endpoint",
			partitionKey: "a:b:c",
			expected:     "endpoint:a:b:c",
		},
		{
			name:         "empty endpoint, with partition",
			endpointID:   "",
			partitionKey: "partition",
			expected:     ":partition",
		},
		{
			name:         "both empty",
			endpointID:   "",
			partitionKey: "",
			expected:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := processorKey(tt.endpointID, tt.partitionKey)
			if result != tt.expected {
				t.Errorf("processorKey(%q, %q) = %q, want %q", tt.endpointID, tt.partitionKey, result, tt.expected)
			}
		})
	}
}

// Benchmark tests for utils

func BenchmarkExtractHost(b *testing.B) {
	urls := []string{
		"https://example.com/webhook",
		"https://api.example.com:8080/v1/hook",
		"http://localhost:3000/callback",
		"https://user:pass@secure.example.com/hook",
	}

	b.ResetTimer()
	for b.Loop() {
		for _, url := range urls {
			_ = extractHost(url)
		}
	}
}

func BenchmarkClassifyFailureReason(b *testing.B) {
	results := []domain.DeliveryResult{
		domain.NewFailureResult(500, "Internal Server Error", nil, 100),
		domain.NewFailureResult(0, "", errors.New("connection timeout"), 100),
		domain.NewFailureResult(404, "Not Found", nil, 100),
	}

	b.ResetTimer()
	for b.Loop() {
		for _, result := range results {
			_ = classifyFailureReason(result)
		}
	}
}

func BenchmarkContainsSubstr(b *testing.B) {
	testCases := []struct {
		s      string
		substr string
	}{
		{"connection timeout occurred", "timeout"},
		{"dial tcp: connection refused", "connection refused"},
		{"lookup example.com: no such host", "no such host"},
	}

	b.ResetTimer()
	for b.Loop() {
		for _, tc := range testCases {
			_ = containsSubstr(tc.s, tc.substr)
		}
	}
}

func BenchmarkProcessorKey(b *testing.B) {
	for b.Loop() {
		_ = processorKey("550e8400-e29b-41d4-a716-446655440000", "user-123")
	}
}

func BenchmarkProcessorKey_NoPartition(b *testing.B) {
	for b.Loop() {
		_ = processorKey("550e8400-e29b-41d4-a716-446655440000", "")
	}
}
