package otel

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/stiffinWanjohi/relay/internal/observability"
)

// TracingConfig holds configuration for the OTel tracing provider.
type TracingConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	SampleRate     float64
}

// TracingProvider implements observability.TracingProvider using OpenTelemetry.
type TracingProvider struct {
	provider *sdktrace.TracerProvider
	tracer   trace.Tracer
	prop     propagation.TextMapPropagator
}

var _ observability.TracingProvider = (*TracingProvider)(nil)

// NewTracingProvider creates a new OTel tracing provider.
func NewTracingProvider(ctx context.Context, cfg TracingConfig) (*TracingProvider, error) {
	var opts []sdktrace.TracerProviderOption

	res, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
			attribute.String("environment", cfg.Environment),
		),
	)
	if err != nil {
		return nil, err
	}
	opts = append(opts, sdktrace.WithResource(res))

	if cfg.OTLPEndpoint != "" {
		exporter, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}
		opts = append(opts, sdktrace.WithBatcher(exporter))
	}

	sampler := sdktrace.TraceIDRatioBased(cfg.SampleRate)
	if cfg.SampleRate >= 1.0 {
		sampler = sdktrace.AlwaysSample()
	} else if cfg.SampleRate <= 0 {
		sampler = sdktrace.NeverSample()
	}
	opts = append(opts, sdktrace.WithSampler(sampler))

	provider := sdktrace.NewTracerProvider(opts...)
	otel.SetTracerProvider(provider)

	prop := propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	)
	otel.SetTextMapPropagator(prop)

	return &TracingProvider{
		provider: provider,
		tracer:   provider.Tracer(cfg.ServiceName),
		prop:     prop,
	}, nil
}

func (p *TracingProvider) StartSpan(ctx context.Context, name string, opts ...observability.SpanOption) (context.Context, observability.Span) {
	options := observability.ApplyOptions(opts...)

	traceOpts := []trace.SpanStartOption{
		trace.WithSpanKind(toOTelSpanKind(options.Kind)),
	}

	if len(options.Attributes) > 0 {
		attrs := make([]attribute.KeyValue, 0, len(options.Attributes))
		for k, v := range options.Attributes {
			attrs = append(attrs, attributeFromAny(k, v))
		}
		traceOpts = append(traceOpts, trace.WithAttributes(attrs...))
	}

	ctx, span := p.tracer.Start(ctx, name, traceOpts...)
	return ctx, &otelSpan{span: span}
}

func (p *TracingProvider) SpanFromContext(ctx context.Context) observability.Span {
	span := trace.SpanFromContext(ctx)
	return &otelSpan{span: span}
}

func (p *TracingProvider) Inject(ctx context.Context, carrier observability.TextMapCarrier) {
	p.prop.Inject(ctx, &textMapCarrierAdapter{carrier: carrier})
}

func (p *TracingProvider) Extract(ctx context.Context, carrier observability.TextMapCarrier) context.Context {
	return p.prop.Extract(ctx, &textMapCarrierAdapter{carrier: carrier})
}

func (p *TracingProvider) Shutdown(ctx context.Context) error {
	return p.provider.Shutdown(ctx)
}

// otelSpan wraps an OTel span to implement observability.Span.
type otelSpan struct {
	span trace.Span
}

var _ observability.Span = (*otelSpan)(nil)

func (s *otelSpan) End() {
	s.span.End()
}

func (s *otelSpan) SetAttribute(key string, value any) {
	s.span.SetAttributes(attributeFromAny(key, value))
}

func (s *otelSpan) SetStatus(status observability.SpanStatus, description string) {
	switch status {
	case observability.SpanStatusOK:
		s.span.SetStatus(codes.Ok, description)
	case observability.SpanStatusError:
		s.span.SetStatus(codes.Error, description)
	default:
		s.span.SetStatus(codes.Unset, description)
	}
}

func (s *otelSpan) RecordError(err error) {
	s.span.RecordError(err)
}

func (s *otelSpan) AddEvent(name string, attributes map[string]any) {
	attrs := make([]attribute.KeyValue, 0, len(attributes))
	for k, v := range attributes {
		attrs = append(attrs, attributeFromAny(k, v))
	}
	s.span.AddEvent(name, trace.WithAttributes(attrs...))
}

func (s *otelSpan) SpanContext() observability.SpanContext {
	sc := s.span.SpanContext()
	return observability.SpanContext{
		TraceID:    sc.TraceID().String(),
		SpanID:     sc.SpanID().String(),
		TraceFlags: byte(sc.TraceFlags()),
		TraceState: sc.TraceState().String(),
		Remote:     sc.IsRemote(),
	}
}

// textMapCarrierAdapter adapts observability.TextMapCarrier to OTel's propagation.TextMapCarrier.
type textMapCarrierAdapter struct {
	carrier observability.TextMapCarrier
}

func (a *textMapCarrierAdapter) Get(key string) string {
	return a.carrier.Get(key)
}

func (a *textMapCarrierAdapter) Set(key, value string) {
	a.carrier.Set(key, value)
}

func (a *textMapCarrierAdapter) Keys() []string {
	return a.carrier.Keys()
}

func toOTelSpanKind(kind observability.SpanKind) trace.SpanKind {
	switch kind {
	case observability.SpanKindServer:
		return trace.SpanKindServer
	case observability.SpanKindClient:
		return trace.SpanKindClient
	case observability.SpanKindProducer:
		return trace.SpanKindProducer
	case observability.SpanKindConsumer:
		return trace.SpanKindConsumer
	default:
		return trace.SpanKindInternal
	}
}

func attributeFromAny(key string, value any) attribute.KeyValue {
	switch v := value.(type) {
	case string:
		return attribute.String(key, v)
	case int:
		return attribute.Int(key, v)
	case int64:
		return attribute.Int64(key, v)
	case float64:
		return attribute.Float64(key, v)
	case bool:
		return attribute.Bool(key, v)
	case []string:
		return attribute.StringSlice(key, v)
	case []int:
		return attribute.IntSlice(key, v)
	case []int64:
		return attribute.Int64Slice(key, v)
	case []float64:
		return attribute.Float64Slice(key, v)
	case []bool:
		return attribute.BoolSlice(key, v)
	default:
		return attribute.String(key, "")
	}
}
