package observability

import (
	"context"
	"time"
)

// MetricsProvider defines the interface for recording metrics.
// Implement this interface to integrate with any metrics backend
// (Prometheus, DataDog, CloudWatch, StatsD, etc.)
type MetricsProvider interface {
	// Counter increments a counter metric
	Counter(ctx context.Context, name string, value int64, tags map[string]string)

	// Gauge sets a gauge metric value
	Gauge(ctx context.Context, name string, value float64, tags map[string]string)

	// Histogram records a value in a histogram/distribution
	Histogram(ctx context.Context, name string, value float64, tags map[string]string)

	// Timing records a duration
	Timing(ctx context.Context, name string, duration time.Duration, tags map[string]string)

	// Flush ensures all metrics are sent (for buffered providers)
	Flush(ctx context.Context) error

	// Close shuts down the metrics provider
	Close(ctx context.Context) error
}

// Metrics provides a convenient wrapper for recording application metrics.
type Metrics struct {
	provider  MetricsProvider
	namespace string
}

// NewMetrics creates a new Metrics instance with the given provider.
func NewMetrics(provider MetricsProvider, namespace string) *Metrics {
	return &Metrics{
		provider:  provider,
		namespace: namespace,
	}
}

func (m *Metrics) prefixName(name string) string {
	if m.namespace == "" {
		return name
	}
	return m.namespace + "." + name
}

// HTTP metrics

func (m *Metrics) HTTPRequestTotal(ctx context.Context, method, path, status string) {
	m.provider.Counter(ctx, m.prefixName("http.requests.total"), 1, map[string]string{
		"method": method,
		"path":   path,
		"status": status,
	})
}

func (m *Metrics) HTTPRequestDuration(ctx context.Context, method, path string, duration time.Duration) {
	m.provider.Timing(ctx, m.prefixName("http.request.duration"), duration, map[string]string{
		"method": method,
		"path":   path,
	})
}

// Event metrics

func (m *Metrics) EventCreated(ctx context.Context, clientID string) {
	m.provider.Counter(ctx, m.prefixName("events.created"), 1, map[string]string{
		"client_id": clientID,
	})
}

func (m *Metrics) EventDelivered(ctx context.Context, clientID, destinationHost string, duration time.Duration) {
	m.provider.Counter(ctx, m.prefixName("events.delivered"), 1, map[string]string{
		"client_id":        clientID,
		"destination_host": destinationHost,
	})
	m.provider.Timing(ctx, m.prefixName("events.delivery.duration"), duration, map[string]string{
		"destination_host": destinationHost,
	})
}

func (m *Metrics) EventFailed(ctx context.Context, clientID, reason string) {
	m.provider.Counter(ctx, m.prefixName("events.failed"), 1, map[string]string{
		"client_id": clientID,
		"reason":    reason,
	})
}

func (m *Metrics) EventRetry(ctx context.Context, clientID string, attempt int) {
	m.provider.Counter(ctx, m.prefixName("events.retries"), 1, map[string]string{
		"client_id": clientID,
		"attempt":   intToString(attempt),
	})
}

// Queue metrics

func (m *Metrics) QueueSize(ctx context.Context, size int64) {
	m.provider.Gauge(ctx, m.prefixName("queue.size"), float64(size), nil)
}

func (m *Metrics) QueueEnqueued(ctx context.Context) {
	m.provider.Counter(ctx, m.prefixName("queue.enqueued"), 1, nil)
}

func (m *Metrics) QueueDequeued(ctx context.Context) {
	m.provider.Counter(ctx, m.prefixName("queue.dequeued"), 1, nil)
}

// Circuit breaker metrics

func (m *Metrics) CircuitBreakerStateChange(ctx context.Context, destinationHost, state string) {
	stateValue := 0.0
	switch state {
	case "closed":
		stateValue = 0
	case "open":
		stateValue = 1
	case "half-open":
		stateValue = 0.5
	}
	m.provider.Gauge(ctx, m.prefixName("circuit_breaker.state"), stateValue, map[string]string{
		"destination_host": destinationHost,
	})
}

func (m *Metrics) CircuitBreakerTrip(ctx context.Context, destinationHost string) {
	m.provider.Counter(ctx, m.prefixName("circuit_breaker.trips"), 1, map[string]string{
		"destination_host": destinationHost,
	})
}

// Outbox metrics

func (m *Metrics) OutboxProcessed(ctx context.Context, duration time.Duration) {
	m.provider.Counter(ctx, m.prefixName("outbox.processed"), 1, nil)
	m.provider.Timing(ctx, m.prefixName("outbox.processing.duration"), duration, map[string]string{
		"status": "success",
	})
}

func (m *Metrics) OutboxFailed(ctx context.Context) {
	m.provider.Counter(ctx, m.prefixName("outbox.failed"), 1, nil)
}

func (m *Metrics) OutboxPending(ctx context.Context, count int64) {
	m.provider.Gauge(ctx, m.prefixName("outbox.pending"), float64(count), nil)
}

// Database metrics

func (m *Metrics) DBConnections(ctx context.Context, active, idle int) {
	m.provider.Gauge(ctx, m.prefixName("db.connections.active"), float64(active), nil)
	m.provider.Gauge(ctx, m.prefixName("db.connections.idle"), float64(idle), nil)
}

func (m *Metrics) DBQueryDuration(ctx context.Context, operation string, duration time.Duration) {
	m.provider.Timing(ctx, m.prefixName("db.query.duration"), duration, map[string]string{
		"operation": operation,
	})
}

// Redis metrics

func (m *Metrics) RedisConnections(ctx context.Context, active int) {
	m.provider.Gauge(ctx, m.prefixName("redis.connections.active"), float64(active), nil)
}

func (m *Metrics) RedisCommandDuration(ctx context.Context, command string, duration time.Duration) {
	m.provider.Timing(ctx, m.prefixName("redis.command.duration"), duration, map[string]string{
		"command": command,
	})
}

// Flush flushes all pending metrics.
func (m *Metrics) Flush(ctx context.Context) error {
	return m.provider.Flush(ctx)
}

// Close shuts down the metrics provider.
func (m *Metrics) Close(ctx context.Context) error {
	return m.provider.Close(ctx)
}

func intToString(i int) string {
	const digits = "0123456789"
	if i == 0 {
		return "0"
	}
	var b [20]byte
	idx := len(b)
	for i > 0 {
		idx--
		b[idx] = digits[i%10]
		i /= 10
	}
	return string(b[idx:])
}
