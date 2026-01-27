package observability

// ProviderRegistry allows registering and retrieving providers by name.
// Use this to support multiple observability backends that can be selected
// via configuration.
type ProviderRegistry struct {
	metricsProviders map[string]func() (MetricsProvider, error)
	tracingProviders map[string]func() (TracingProvider, error)
}

// NewProviderRegistry creates a new provider registry.
func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		metricsProviders: make(map[string]func() (MetricsProvider, error)),
		tracingProviders: make(map[string]func() (TracingProvider, error)),
	}
}

// RegisterMetricsProvider registers a metrics provider factory.
func (r *ProviderRegistry) RegisterMetricsProvider(name string, factory func() (MetricsProvider, error)) {
	r.metricsProviders[name] = factory
}

// RegisterTracingProvider registers a tracing provider factory.
func (r *ProviderRegistry) RegisterTracingProvider(name string, factory func() (TracingProvider, error)) {
	r.tracingProviders[name] = factory
}

// GetMetricsProvider returns a metrics provider by name.
func (r *ProviderRegistry) GetMetricsProvider(name string) (MetricsProvider, error) {
	factory, ok := r.metricsProviders[name]
	if !ok {
		return &NoopMetricsProvider{}, nil
	}
	return factory()
}

// GetTracingProvider returns a tracing provider by name.
func (r *ProviderRegistry) GetTracingProvider(name string) (TracingProvider, error) {
	factory, ok := r.tracingProviders[name]
	if !ok {
		return &NoopTracingProvider{}, nil
	}
	return factory()
}
