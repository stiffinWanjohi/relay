package prometheus

import (
	"context"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/relay/internal/observability"
)

func init() {
	observability.RegisterMetricsProvider("prometheus", NewPrometheusProvider)
}

// PrometheusProvider implements MetricsProvider using Prometheus client.
type PrometheusProvider struct {
	registry   *prometheus.Registry
	counters   map[string]*prometheus.CounterVec
	gauges     map[string]*prometheus.GaugeVec
	histograms map[string]*prometheus.HistogramVec
	mu         sync.RWMutex
	namespace  string
}

// NewPrometheusProvider creates a new Prometheus metrics provider.
func NewPrometheusProvider(ctx context.Context, cfg observability.MetricsConfig) (observability.MetricsProvider, error) {
	registry := prometheus.NewRegistry()

	// Register default Go and process metrics
	registry.MustRegister(collectors.NewGoCollector())
	registry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	return &PrometheusProvider{
		registry:   registry,
		counters:   make(map[string]*prometheus.CounterVec),
		gauges:     make(map[string]*prometheus.GaugeVec),
		histograms: make(map[string]*prometheus.HistogramVec),
		namespace:  sanitizeName(cfg.ServiceName),
	}, nil
}

// Handler returns an HTTP handler for the /metrics endpoint.
func (p *PrometheusProvider) Handler() http.Handler {
	return promhttp.HandlerFor(p.registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Registry returns the Prometheus registry for custom registration.
func (p *PrometheusProvider) Registry() *prometheus.Registry {
	return p.registry
}

func (p *PrometheusProvider) Counter(ctx context.Context, name string, value int64, tags map[string]string) {
	counter := p.getOrCreateCounter(name, tags)
	counter.With(tagsToLabels(tags)).Add(float64(value))
}

func (p *PrometheusProvider) Gauge(ctx context.Context, name string, value float64, tags map[string]string) {
	gauge := p.getOrCreateGauge(name, tags)
	gauge.With(tagsToLabels(tags)).Set(value)
}

func (p *PrometheusProvider) Histogram(ctx context.Context, name string, value float64, tags map[string]string) {
	histogram := p.getOrCreateHistogram(name, tags)
	histogram.With(tagsToLabels(tags)).Observe(value)
}

func (p *PrometheusProvider) Timing(ctx context.Context, name string, duration time.Duration, tags map[string]string) {
	// Convert to seconds for Prometheus conventions
	histogram := p.getOrCreateHistogram(name+"_seconds", tags)
	histogram.With(tagsToLabels(tags)).Observe(duration.Seconds())
}

func (p *PrometheusProvider) Flush(ctx context.Context) error {
	// Prometheus uses pull model, no flush needed
	return nil
}

func (p *PrometheusProvider) Close(ctx context.Context) error {
	return nil
}

func (p *PrometheusProvider) getOrCreateCounter(name string, tags map[string]string) *prometheus.CounterVec {
	fullName := p.fullName(name)

	p.mu.RLock()
	counter, ok := p.counters[fullName]
	p.mu.RUnlock()

	if ok {
		return counter
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	// Double-check after acquiring write lock
	if counter, ok = p.counters[fullName]; ok {
		return counter
	}

	labelNames := getLabelNames(tags)
	counter = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: p.namespace,
		Name:      sanitizeName(name),
		Help:      "Counter for " + name,
	}, labelNames)

	p.registry.MustRegister(counter)
	p.counters[fullName] = counter
	return counter
}

func (p *PrometheusProvider) getOrCreateGauge(name string, tags map[string]string) *prometheus.GaugeVec {
	fullName := p.fullName(name)

	p.mu.RLock()
	gauge, ok := p.gauges[fullName]
	p.mu.RUnlock()

	if ok {
		return gauge
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if gauge, ok = p.gauges[fullName]; ok {
		return gauge
	}

	labelNames := getLabelNames(tags)
	gauge = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: p.namespace,
		Name:      sanitizeName(name),
		Help:      "Gauge for " + name,
	}, labelNames)

	p.registry.MustRegister(gauge)
	p.gauges[fullName] = gauge
	return gauge
}

func (p *PrometheusProvider) getOrCreateHistogram(name string, tags map[string]string) *prometheus.HistogramVec {
	fullName := p.fullName(name)

	p.mu.RLock()
	histogram, ok := p.histograms[fullName]
	p.mu.RUnlock()

	if ok {
		return histogram
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if histogram, ok = p.histograms[fullName]; ok {
		return histogram
	}

	labelNames := getLabelNames(tags)
	histogram = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Namespace: p.namespace,
		Name:      sanitizeName(name),
		Help:      "Histogram for " + name,
		Buckets:   prometheus.DefBuckets,
	}, labelNames)

	p.registry.MustRegister(histogram)
	p.histograms[fullName] = histogram
	return histogram
}

func (p *PrometheusProvider) fullName(name string) string {
	return p.namespace + "_" + sanitizeName(name)
}

func sanitizeName(name string) string {
	// Prometheus metric names must match [a-zA-Z_:][a-zA-Z0-9_:]*
	result := strings.Builder{}
	for i, r := range name {
		switch {
		case r >= 'a' && r <= 'z':
			result.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			result.WriteRune(r)
		case r >= '0' && r <= '9':
			if i == 0 {
				result.WriteRune('_')
			}
			result.WriteRune(r)
		case r == '_' || r == ':':
			result.WriteRune(r)
		case r == '.' || r == '-' || r == '/':
			result.WriteRune('_')
		default:
			result.WriteRune('_')
		}
	}
	return result.String()
}

func getLabelNames(tags map[string]string) []string {
	if tags == nil {
		return nil
	}
	names := make([]string, 0, len(tags))
	for k := range tags {
		names = append(names, sanitizeName(k))
	}
	return names
}

func tagsToLabels(tags map[string]string) prometheus.Labels {
	if tags == nil {
		return prometheus.Labels{}
	}
	labels := make(prometheus.Labels, len(tags))
	for k, v := range tags {
		labels[sanitizeName(k)] = v
	}
	return labels
}
