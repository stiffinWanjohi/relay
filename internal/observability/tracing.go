package observability

import (
	"context"
)

// SpanKind represents the type of span.
type SpanKind int

const (
	SpanKindInternal SpanKind = iota
	SpanKindServer
	SpanKindClient
	SpanKindProducer
	SpanKindConsumer
)

// SpanStatus represents the status of a span.
type SpanStatus int

const (
	SpanStatusUnset SpanStatus = iota
	SpanStatusOK
	SpanStatusError
)

// Span represents an active trace span.
type Span interface {
	// End completes the span
	End()

	// SetAttribute sets an attribute on the span
	SetAttribute(key string, value any)

	// SetStatus sets the span status
	SetStatus(status SpanStatus, description string)

	// RecordError records an error on the span
	RecordError(err error)

	// AddEvent adds an event to the span
	AddEvent(name string, attributes map[string]any)

	// SpanContext returns the span context for propagation
	SpanContext() SpanContext
}

// SpanContext contains the trace context for propagation.
type SpanContext struct {
	TraceID    string
	SpanID     string
	TraceFlags byte
	TraceState string
	Remote     bool
}

// IsValid returns true if the span context has valid trace and span IDs.
func (sc SpanContext) IsValid() bool {
	return sc.TraceID != "" && sc.SpanID != ""
}

// TracingProvider defines the interface for distributed tracing.
// Implement this interface to integrate with any tracing backend
// (OpenTelemetry, Jaeger, Zipkin, DataDog, AWS X-Ray, etc.)
type TracingProvider interface {
	// StartSpan starts a new span
	StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span)

	// SpanFromContext returns the current span from context
	SpanFromContext(ctx context.Context) Span

	// Inject injects trace context into a carrier (for propagation)
	Inject(ctx context.Context, carrier TextMapCarrier)

	// Extract extracts trace context from a carrier
	Extract(ctx context.Context, carrier TextMapCarrier) context.Context

	// Shutdown gracefully shuts down the tracing provider
	Shutdown(ctx context.Context) error
}

// SpanOption configures a span.
type SpanOption func(*SpanOptions)

// SpanOptions holds span configuration options.
type SpanOptions struct {
	Kind       SpanKind
	Attributes map[string]any
}

// ApplyOptions applies all options and returns the resulting SpanOptions.
func ApplyOptions(opts ...SpanOption) SpanOptions {
	o := SpanOptions{
		Attributes: make(map[string]any),
	}
	for _, opt := range opts {
		opt(&o)
	}
	return o
}

// WithSpanKind sets the span kind.
func WithSpanKind(kind SpanKind) SpanOption {
	return func(o *SpanOptions) {
		o.Kind = kind
	}
}

// WithAttributes sets initial span attributes.
func WithAttributes(attrs map[string]any) SpanOption {
	return func(o *SpanOptions) {
		o.Attributes = attrs
	}
}

// TextMapCarrier is a carrier for trace context propagation.
type TextMapCarrier interface {
	Get(key string) string
	Set(key, value string)
	Keys() []string
}

// HTTPHeaderCarrier adapts http.Header to TextMapCarrier.
type HTTPHeaderCarrier map[string][]string

func (c HTTPHeaderCarrier) Get(key string) string {
	vals := c[key]
	if len(vals) == 0 {
		return ""
	}
	return vals[0]
}

func (c HTTPHeaderCarrier) Set(key, value string) {
	c[key] = []string{value}
}

func (c HTTPHeaderCarrier) Keys() []string {
	keys := make([]string, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	return keys
}

// Tracer provides a convenient wrapper for tracing operations.
type Tracer struct {
	provider    TracingProvider
	serviceName string
}

// NewTracer creates a new Tracer with the given provider.
func NewTracer(provider TracingProvider, serviceName string) *Tracer {
	return &Tracer{
		provider:    provider,
		serviceName: serviceName,
	}
}

// StartSpan starts a new span with common attributes.
func (t *Tracer) StartSpan(ctx context.Context, name string, opts ...SpanOption) (context.Context, Span) {
	return t.provider.StartSpan(ctx, name, opts...)
}

// SpanFromContext returns the current span.
func (t *Tracer) SpanFromContext(ctx context.Context) Span {
	return t.provider.SpanFromContext(ctx)
}

// Inject injects trace context into a carrier.
func (t *Tracer) Inject(ctx context.Context, carrier TextMapCarrier) {
	t.provider.Inject(ctx, carrier)
}

// Extract extracts trace context from a carrier.
func (t *Tracer) Extract(ctx context.Context, carrier TextMapCarrier) context.Context {
	return t.provider.Extract(ctx, carrier)
}

// Shutdown shuts down the tracer.
func (t *Tracer) Shutdown(ctx context.Context) error {
	return t.provider.Shutdown(ctx)
}

// Common span names for webhook delivery operations.
const (
	SpanHTTPRequest      = "http.request"
	SpanEventCreate      = "event.create"
	SpanEventDelivery    = "event.delivery"
	SpanQueueEnqueue     = "queue.enqueue"
	SpanQueueDequeue     = "queue.dequeue"
	SpanDBQuery          = "db.query"
	SpanRedisCommand     = "redis.command"
	SpanOutboxProcess    = "outbox.process"
	SpanCircuitBreaker   = "circuit_breaker.check"
)

// Common attribute keys.
const (
	AttrEventID        = "relay.event.id"
	AttrClientID       = "relay.client.id"
	AttrDestination    = "relay.destination"
	AttrAttempt        = "relay.attempt"
	AttrDeliveryStatus = "relay.delivery.status"
	AttrHTTPMethod     = "http.method"
	AttrHTTPURL        = "http.url"
	AttrHTTPStatusCode = "http.status_code"
	AttrDBOperation    = "db.operation"
	AttrDBTable        = "db.table"
	AttrRedisCommand   = "redis.command"
)
