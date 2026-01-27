package otel

import (
	"context"
	"sync"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"

	"github.com/stiffinWanjohi/relay/internal/observability"
)

func init() {
	// Register the OTel metrics provider
	observability.RegisterMetricsProvider("otel", newMetricsProviderFromConfig)
	observability.RegisterMetricsProvider("otlp", newMetricsProviderFromConfig)
}

// newMetricsProviderFromConfig is the factory function for the registry.
func newMetricsProviderFromConfig(ctx context.Context, cfg observability.MetricsConfig) (observability.MetricsProvider, error) {
	exportInterval := 15 * time.Second
	if interval, ok := cfg.Options["export_interval"]; ok {
		if parsed, err := time.ParseDuration(interval); err == nil {
			exportInterval = parsed
		}
	}

	return NewMetricsProvider(ctx, MetricsConfig{
		ServiceName:    cfg.ServiceName,
		ServiceVersion: cfg.ServiceVersion,
		Environment:    cfg.Environment,
		OTLPEndpoint:   cfg.Endpoint,
		ExportInterval: exportInterval,
	})
}

// MetricsConfig holds configuration for the OTel metrics provider.
type MetricsConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	OTLPEndpoint   string
	ExportInterval time.Duration
}

// MetricsProvider implements observability.MetricsProvider using OpenTelemetry.
type MetricsProvider struct {
	provider *sdkmetric.MeterProvider
	meter    metric.Meter

	mu         sync.RWMutex
	counters   map[string]metric.Int64Counter
	gauges     map[string]metric.Float64Gauge
	histograms map[string]metric.Float64Histogram
}

var _ observability.MetricsProvider = (*MetricsProvider)(nil)

// NewMetricsProvider creates a new OTel metrics provider.
func NewMetricsProvider(ctx context.Context, cfg MetricsConfig) (*MetricsProvider, error) {
	var opts []sdkmetric.Option

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
	opts = append(opts, sdkmetric.WithResource(res))

	if cfg.OTLPEndpoint != "" {
		exporter, err := otlpmetricgrpc.New(ctx,
			otlpmetricgrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlpmetricgrpc.WithInsecure(),
		)
		if err != nil {
			return nil, err
		}

		interval := cfg.ExportInterval
		if interval == 0 {
			interval = 15 * time.Second
		}

		opts = append(opts, sdkmetric.WithReader(
			sdkmetric.NewPeriodicReader(exporter, sdkmetric.WithInterval(interval)),
		))
	}

	provider := sdkmetric.NewMeterProvider(opts...)
	otel.SetMeterProvider(provider)

	return &MetricsProvider{
		provider:   provider,
		meter:      provider.Meter(cfg.ServiceName),
		counters:   make(map[string]metric.Int64Counter),
		gauges:     make(map[string]metric.Float64Gauge),
		histograms: make(map[string]metric.Float64Histogram),
	}, nil
}

func (p *MetricsProvider) Counter(ctx context.Context, name string, value int64, tags map[string]string) {
	counter := p.getOrCreateCounter(name)
	counter.Add(ctx, value, metric.WithAttributes(tagsToAttributes(tags)...))
}

func (p *MetricsProvider) Gauge(ctx context.Context, name string, value float64, tags map[string]string) {
	gauge := p.getOrCreateGauge(name)
	gauge.Record(ctx, value, metric.WithAttributes(tagsToAttributes(tags)...))
}

func (p *MetricsProvider) Histogram(ctx context.Context, name string, value float64, tags map[string]string) {
	histogram := p.getOrCreateHistogram(name)
	histogram.Record(ctx, value, metric.WithAttributes(tagsToAttributes(tags)...))
}

func (p *MetricsProvider) Timing(ctx context.Context, name string, duration time.Duration, tags map[string]string) {
	histogram := p.getOrCreateHistogram(name)
	histogram.Record(ctx, duration.Seconds(), metric.WithAttributes(tagsToAttributes(tags)...))
}

func (p *MetricsProvider) Flush(ctx context.Context) error {
	return p.provider.ForceFlush(ctx)
}

func (p *MetricsProvider) Close(ctx context.Context) error {
	return p.provider.Shutdown(ctx)
}

func (p *MetricsProvider) getOrCreateCounter(name string) metric.Int64Counter {
	p.mu.RLock()
	counter, ok := p.counters[name]
	p.mu.RUnlock()
	if ok {
		return counter
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if counter, ok = p.counters[name]; ok {
		return counter
	}

	counter, _ = p.meter.Int64Counter(name)
	p.counters[name] = counter
	return counter
}

func (p *MetricsProvider) getOrCreateGauge(name string) metric.Float64Gauge {
	p.mu.RLock()
	gauge, ok := p.gauges[name]
	p.mu.RUnlock()
	if ok {
		return gauge
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if gauge, ok = p.gauges[name]; ok {
		return gauge
	}

	gauge, _ = p.meter.Float64Gauge(name)
	p.gauges[name] = gauge
	return gauge
}

func (p *MetricsProvider) getOrCreateHistogram(name string) metric.Float64Histogram {
	p.mu.RLock()
	histogram, ok := p.histograms[name]
	p.mu.RUnlock()
	if ok {
		return histogram
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if histogram, ok = p.histograms[name]; ok {
		return histogram
	}

	histogram, _ = p.meter.Float64Histogram(name)
	p.histograms[name] = histogram
	return histogram
}

func tagsToAttributes(tags map[string]string) []attribute.KeyValue {
	if len(tags) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(tags))
	for k, v := range tags {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}
