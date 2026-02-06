package delivery

import (
	"net/url"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

// extractHost extracts the host from a URL for metrics tagging and circuit breaker keying.
func extractHost(destination string) string {
	u, err := url.Parse(destination)
	if err != nil {
		return "unknown"
	}
	return u.Host
}

// classifyFailureReason classifies the delivery failure for metrics.
func classifyFailureReason(result domain.DeliveryResult) string {
	if result.Error != nil {
		errStr := result.Error.Error()
		switch {
		case containsSubstr(errStr, "timeout"):
			return "timeout"
		case containsSubstr(errStr, "connection refused"):
			return "connection_refused"
		case containsSubstr(errStr, "no such host"):
			return "dns_error"
		case containsSubstr(errStr, "TLS"):
			return "tls_error"
		default:
			return "network_error"
		}
	}

	switch {
	case result.StatusCode >= 500:
		return "server_error"
	case result.StatusCode >= 400:
		return "client_error"
	default:
		return "unknown"
	}
}

// containsSubstr checks if s contains substr.
func containsSubstr(s, substr string) bool {
	if len(substr) == 0 {
		return true
	}
	if len(s) < len(substr) {
		return false
	}
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// processorKey generates a unique key for an endpoint/partition processor.
func processorKey(endpointID, partitionKey string) string {
	if partitionKey == "" {
		return endpointID
	}
	return endpointID + ":" + partitionKey
}
