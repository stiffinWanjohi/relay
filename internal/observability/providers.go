package observability

import (
	"context"
	"fmt"
	"sync"
)

// MetricsConfig is a generic configuration passed to metrics provider factories.
type MetricsConfig struct {
	ServiceName    string
	ServiceVersion string
	Environment    string
	Endpoint       string            // Provider-specific endpoint (OTLP, StatsD, etc.)
	Options        map[string]string // Additional provider-specific options
}

// MetricsProviderFactory creates a MetricsProvider from configuration.
type MetricsProviderFactory func(ctx context.Context, cfg MetricsConfig) (MetricsProvider, error)

// TracingProviderFactory creates a TracingProvider from configuration.
type TracingProviderFactory func(ctx context.Context, cfg MetricsConfig) (TracingProvider, error)

// Global registry for provider factories
var (
	registryMu       sync.RWMutex
	metricsFactories = make(map[string]MetricsProviderFactory)
	tracingFactories = make(map[string]TracingProviderFactory)
)

// RegisterMetricsProvider registers a metrics provider factory globally.
// Call this in init() functions of provider packages.
// Example:
//
//	func init() {
//	    observability.RegisterMetricsProvider("prometheus", NewPrometheusProvider)
//	    observability.RegisterMetricsProvider("datadog", NewDatadogProvider)
//	}
func RegisterMetricsProvider(name string, factory MetricsProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	metricsFactories[name] = factory
}

// RegisterTracingProvider registers a tracing provider factory globally.
func RegisterTracingProvider(name string, factory TracingProviderFactory) {
	registryMu.Lock()
	defer registryMu.Unlock()
	tracingFactories[name] = factory
}

// NewMetricsProvider creates a metrics provider by name.
// Returns NoopMetricsProvider if the provider is not found or name is empty.
func NewMetricsProviderByName(ctx context.Context, name string, cfg MetricsConfig) (MetricsProvider, error) {
	if name == "" || name == "noop" {
		return &NoopMetricsProvider{}, nil
	}

	registryMu.RLock()
	factory, ok := metricsFactories[name]
	registryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown metrics provider: %s (available: %v)", name, ListMetricsProviders())
	}

	return factory(ctx, cfg)
}

// NewTracingProviderByName creates a tracing provider by name.
func NewTracingProviderByName(ctx context.Context, name string, cfg MetricsConfig) (TracingProvider, error) {
	if name == "" || name == "noop" {
		return &NoopTracingProvider{}, nil
	}

	registryMu.RLock()
	factory, ok := tracingFactories[name]
	registryMu.RUnlock()

	if !ok {
		return nil, fmt.Errorf("unknown tracing provider: %s", name)
	}

	return factory(ctx, cfg)
}

// ListMetricsProviders returns a list of registered metrics provider names.
func ListMetricsProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(metricsFactories))
	for name := range metricsFactories {
		names = append(names, name)
	}
	return names
}

// ListTracingProviders returns a list of registered tracing provider names.
func ListTracingProviders() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()

	names := make([]string, 0, len(tracingFactories))
	for name := range tracingFactories {
		names = append(names, name)
	}
	return names
}
