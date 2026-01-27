package observability

import (
	"context"
	"time"
)

// NoopMetricsProvider is a metrics provider that does nothing.
// Use this as a default when no metrics backend is configured.
type NoopMetricsProvider struct{}

var _ MetricsProvider = (*NoopMetricsProvider)(nil)

func (n *NoopMetricsProvider) Counter(ctx context.Context, name string, value int64, tags map[string]string) {
}
func (n *NoopMetricsProvider) Gauge(ctx context.Context, name string, value float64, tags map[string]string) {
}
func (n *NoopMetricsProvider) Histogram(ctx context.Context, name string, value float64, tags map[string]string) {
}
func (n *NoopMetricsProvider) Timing(ctx context.Context, name string, duration time.Duration, tags map[string]string) {
}
func (n *NoopMetricsProvider) Flush(ctx context.Context) error { return nil }
func (n *NoopMetricsProvider) Close(ctx context.Context) error { return nil }

// NoopTracingProvider is a tracing provider that does nothing.
// Use this as a default when no tracing backend is configured.
type NoopTracingProvider struct{}

var _ TracingProvider = (*NoopTracingProvider)(nil)

func (n *NoopTracingProvider) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span) {
	return ctx, &noopSpan{}
}
func (n *NoopTracingProvider) SpanFromContext(ctx context.Context) Span {
	return &noopSpan{}
}
func (n *NoopTracingProvider) Inject(ctx context.Context, carrier TextMapCarrier) {}
func (n *NoopTracingProvider) Extract(ctx context.Context, carrier TextMapCarrier) context.Context {
	return ctx
}
func (n *NoopTracingProvider) Shutdown(ctx context.Context) error { return nil }

// noopSpan is a span that does nothing.
type noopSpan struct{}

var _ Span = (*noopSpan)(nil)

func (n *noopSpan) End()                                         {}
func (n *noopSpan) SetAttribute(key string, value any)           {}
func (n *noopSpan) SetStatus(status SpanStatus, description string) {}
func (n *noopSpan) RecordError(err error)                        {}
func (n *noopSpan) AddEvent(name string, attributes map[string]any) {}
func (n *noopSpan) SpanContext() SpanContext                     { return SpanContext{} }
