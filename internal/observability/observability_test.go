package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// TestMetricsProvider records metrics for testing
type TestMetricsProvider struct {
	Counters   []metricCall
	Gauges     []metricCall
	Histograms []metricCall
	Timings    []timingCall
}

type metricCall struct {
	Name  string
	Value float64
	Tags  map[string]string
}

type timingCall struct {
	Name     string
	Duration time.Duration
	Tags     map[string]string
}

func (t *TestMetricsProvider) Counter(ctx context.Context, name string, value int64, tags map[string]string) {
	t.Counters = append(t.Counters, metricCall{Name: name, Value: float64(value), Tags: tags})
}

func (t *TestMetricsProvider) Gauge(ctx context.Context, name string, value float64, tags map[string]string) {
	t.Gauges = append(t.Gauges, metricCall{Name: name, Value: value, Tags: tags})
}

func (t *TestMetricsProvider) Histogram(ctx context.Context, name string, value float64, tags map[string]string) {
	t.Histograms = append(t.Histograms, metricCall{Name: name, Value: value, Tags: tags})
}

func (t *TestMetricsProvider) Timing(ctx context.Context, name string, duration time.Duration, tags map[string]string) {
	t.Timings = append(t.Timings, timingCall{Name: name, Duration: duration, Tags: tags})
}

func (t *TestMetricsProvider) Flush(ctx context.Context) error { return nil }
func (t *TestMetricsProvider) Close(ctx context.Context) error { return nil }

func TestNewMetrics(t *testing.T) {
	provider := &NoopMetricsProvider{}
	metrics := NewMetrics(provider, "test")

	if metrics == nil {
		t.Fatal("expected non-nil metrics")
		return
	}
	if metrics.namespace != "test" {
		t.Errorf("expected namespace 'test', got %s", metrics.namespace)
	}
}

func TestMetrics_PrefixName(t *testing.T) {
	tests := []struct {
		namespace string
		name      string
		expected  string
	}{
		{"", "metric", "metric"},
		{"relay", "metric", "relay.metric"},
		{"my.app", "counter", "my.app.counter"},
	}

	for _, tc := range tests {
		metrics := NewMetrics(&NoopMetricsProvider{}, tc.namespace)
		result := metrics.prefixName(tc.name)
		if result != tc.expected {
			t.Errorf("prefixName(%q) with namespace %q = %q, expected %q", tc.name, tc.namespace, result, tc.expected)
		}
	}
}

func TestMetrics_HTTPRequestTotal(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "test")
	ctx := context.Background()

	metrics.HTTPRequestTotal(ctx, "GET", "/api/events", "200")

	if len(provider.Counters) != 1 {
		t.Fatalf("expected 1 counter, got %d", len(provider.Counters))
	}

	counter := provider.Counters[0]
	if counter.Name != "test.http.requests.total" {
		t.Errorf("expected name 'test.http.requests.total', got %s", counter.Name)
	}
	if counter.Tags["method"] != "GET" {
		t.Errorf("expected method tag 'GET', got %s", counter.Tags["method"])
	}
	if counter.Tags["path"] != "/api/events" {
		t.Errorf("expected path tag '/api/events', got %s", counter.Tags["path"])
	}
	if counter.Tags["status"] != "200" {
		t.Errorf("expected status tag '200', got %s", counter.Tags["status"])
	}
}

func TestMetrics_HTTPRequestDuration(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "test")
	ctx := context.Background()

	duration := 100 * time.Millisecond
	metrics.HTTPRequestDuration(ctx, "POST", "/api/events", duration)

	if len(provider.Timings) != 1 {
		t.Fatalf("expected 1 timing, got %d", len(provider.Timings))
	}

	timing := provider.Timings[0]
	if timing.Name != "test.http.request.duration" {
		t.Errorf("expected name 'test.http.request.duration', got %s", timing.Name)
	}
	if timing.Duration != duration {
		t.Errorf("expected duration %v, got %v", duration, timing.Duration)
	}
}

func TestMetrics_EventCreated(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.EventCreated(ctx, "client-123")

	if len(provider.Counters) != 1 {
		t.Fatalf("expected 1 counter, got %d", len(provider.Counters))
	}

	counter := provider.Counters[0]
	if counter.Name != "relay.events.created" {
		t.Errorf("expected name 'relay.events.created', got %s", counter.Name)
	}
	if counter.Tags["client_id"] != "client-123" {
		t.Errorf("expected client_id tag 'client-123', got %s", counter.Tags["client_id"])
	}
}

func TestMetrics_EventDelivered(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.EventDelivered(ctx, "client-123", "example.com", 150*time.Millisecond)

	if len(provider.Counters) != 1 {
		t.Fatalf("expected 1 counter, got %d", len(provider.Counters))
	}
	if len(provider.Timings) != 1 {
		t.Fatalf("expected 1 timing, got %d", len(provider.Timings))
	}
}

func TestMetrics_EventFailed(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.EventFailed(ctx, "client-123", "timeout")

	if len(provider.Counters) != 1 {
		t.Fatalf("expected 1 counter, got %d", len(provider.Counters))
	}

	counter := provider.Counters[0]
	if counter.Tags["reason"] != "timeout" {
		t.Errorf("expected reason tag 'timeout', got %s", counter.Tags["reason"])
	}
}

func TestMetrics_EventRetry(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.EventRetry(ctx, "client-123", 3)

	if len(provider.Counters) != 1 {
		t.Fatalf("expected 1 counter, got %d", len(provider.Counters))
	}

	counter := provider.Counters[0]
	if counter.Tags["attempt"] != "3" {
		t.Errorf("expected attempt tag '3', got %s", counter.Tags["attempt"])
	}
}

func TestMetrics_QueueMetrics(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.QueueSize(ctx, 100)
	metrics.QueueEnqueued(ctx)
	metrics.QueueDequeued(ctx)

	if len(provider.Gauges) != 1 {
		t.Errorf("expected 1 gauge, got %d", len(provider.Gauges))
	}
	if len(provider.Counters) != 2 {
		t.Errorf("expected 2 counters, got %d", len(provider.Counters))
	}
}

func TestMetrics_CircuitBreakerStateChange(t *testing.T) {
	tests := []struct {
		state    string
		expected float64
	}{
		{"closed", 0.0},
		{"open", 1.0},
		{"half-open", 0.5},
		{"unknown", 0.0},
	}

	for _, tc := range tests {
		provider := &TestMetricsProvider{}
		metrics := NewMetrics(provider, "relay")
		ctx := context.Background()

		metrics.CircuitBreakerStateChange(ctx, "example.com", tc.state)

		if len(provider.Gauges) != 1 {
			t.Errorf("expected 1 gauge for state %s, got %d", tc.state, len(provider.Gauges))
			continue
		}

		if provider.Gauges[0].Value != tc.expected {
			t.Errorf("expected value %v for state %s, got %v", tc.expected, tc.state, provider.Gauges[0].Value)
		}
	}
}

func TestMetrics_CircuitBreakerTrip(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.CircuitBreakerTrip(ctx, "example.com")

	if len(provider.Counters) != 1 {
		t.Fatalf("expected 1 counter, got %d", len(provider.Counters))
	}

	counter := provider.Counters[0]
	if counter.Name != "relay.circuit_breaker.trips" {
		t.Errorf("expected name 'relay.circuit_breaker.trips', got %s", counter.Name)
	}
}

func TestMetrics_OutboxMetrics(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.OutboxProcessed(ctx, 50*time.Millisecond)
	metrics.OutboxFailed(ctx)
	metrics.OutboxPending(ctx, 25)

	if len(provider.Counters) != 2 {
		t.Errorf("expected 2 counters, got %d", len(provider.Counters))
	}
	if len(provider.Timings) != 1 {
		t.Errorf("expected 1 timing, got %d", len(provider.Timings))
	}
	if len(provider.Gauges) != 1 {
		t.Errorf("expected 1 gauge, got %d", len(provider.Gauges))
	}
}

func TestMetrics_DBMetrics(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.DBConnections(ctx, 5, 10)
	metrics.DBQueryDuration(ctx, "SELECT", 10*time.Millisecond)

	if len(provider.Gauges) != 2 {
		t.Errorf("expected 2 gauges, got %d", len(provider.Gauges))
	}
	if len(provider.Timings) != 1 {
		t.Errorf("expected 1 timing, got %d", len(provider.Timings))
	}
}

func TestMetrics_RedisMetrics(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "relay")
	ctx := context.Background()

	metrics.RedisConnections(ctx, 20)
	metrics.RedisCommandDuration(ctx, "GET", 5*time.Millisecond)

	if len(provider.Gauges) != 1 {
		t.Errorf("expected 1 gauge, got %d", len(provider.Gauges))
	}
	if len(provider.Timings) != 1 {
		t.Errorf("expected 1 timing, got %d", len(provider.Timings))
	}
}

func TestMetrics_Flush(t *testing.T) {
	provider := &NoopMetricsProvider{}
	metrics := NewMetrics(provider, "test")
	ctx := context.Background()

	err := metrics.Flush(ctx)
	if err != nil {
		t.Errorf("Flush failed: %v", err)
	}
}

func TestMetrics_Close(t *testing.T) {
	provider := &NoopMetricsProvider{}
	metrics := NewMetrics(provider, "test")
	ctx := context.Background()

	err := metrics.Close(ctx)
	if err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestIntToString(t *testing.T) {
	tests := []struct {
		input    int
		expected string
	}{
		{0, "0"},
		{1, "1"},
		{10, "10"},
		{123, "123"},
		{999999, "999999"},
	}

	for _, tc := range tests {
		result := intToString(tc.input)
		if result != tc.expected {
			t.Errorf("intToString(%d) = %s, expected %s", tc.input, result, tc.expected)
		}
	}
}

// Noop provider tests

func TestNoopMetricsProvider(t *testing.T) {
	provider := &NoopMetricsProvider{}
	ctx := context.Background()

	// All methods should be callable without error
	provider.Counter(ctx, "test", 1, nil)
	provider.Gauge(ctx, "test", 1.0, nil)
	provider.Histogram(ctx, "test", 1.0, nil)
	provider.Timing(ctx, "test", time.Second, nil)

	if err := provider.Flush(ctx); err != nil {
		t.Errorf("Flush failed: %v", err)
	}
	if err := provider.Close(ctx); err != nil {
		t.Errorf("Close failed: %v", err)
	}
}

func TestNoopTracingProvider(t *testing.T) {
	provider := &NoopTracingProvider{}
	ctx := context.Background()

	newCtx, span := provider.StartSpan(ctx, "test")
	if newCtx != ctx {
		t.Error("expected same context")
	}
	if span == nil {
		t.Error("expected non-nil span")
	}

	span.End()
	span.SetAttribute("key", "value")
	span.SetStatus(SpanStatusOK, "")
	span.RecordError(nil)
	span.AddEvent("event", nil)

	spanCtx := span.SpanContext()
	if spanCtx.IsValid() {
		t.Error("expected invalid span context for noop span")
	}

	s := provider.SpanFromContext(ctx)
	if s == nil {
		t.Error("expected non-nil span from context")
	}

	provider.Inject(ctx, nil)
	extractedCtx := provider.Extract(ctx, nil)
	if extractedCtx != ctx {
		t.Error("expected same context from Extract")
	}

	if err := provider.Shutdown(ctx); err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

// Tracing tests

func TestSpanContext_IsValid(t *testing.T) {
	tests := []struct {
		traceID  string
		spanID   string
		expected bool
	}{
		{"", "", false},
		{"trace-123", "", false},
		{"", "span-123", false},
		{"trace-123", "span-123", true},
	}

	for _, tc := range tests {
		sc := SpanContext{TraceID: tc.traceID, SpanID: tc.spanID}
		if sc.IsValid() != tc.expected {
			t.Errorf("SpanContext{TraceID: %q, SpanID: %q}.IsValid() = %v, expected %v",
				tc.traceID, tc.spanID, sc.IsValid(), tc.expected)
		}
	}
}

func TestApplyOptions(t *testing.T) {
	opts := ApplyOptions(
		WithSpanKind(SpanKindClient),
		WithAttributes(map[string]any{"key": "value"}),
	)

	if opts.Kind != SpanKindClient {
		t.Errorf("expected SpanKindClient, got %v", opts.Kind)
	}
	if opts.Attributes["key"] != "value" {
		t.Errorf("expected attribute 'key' = 'value', got %v", opts.Attributes["key"])
	}
}

func TestNewTracer(t *testing.T) {
	provider := &NoopTracingProvider{}
	tracer := NewTracer(provider, "test-service")

	if tracer == nil {
		t.Fatal("expected non-nil tracer")
		return
	}
	if tracer.serviceName != "test-service" {
		t.Errorf("expected service name 'test-service', got %s", tracer.serviceName)
	}
}

func TestTracer_Operations(t *testing.T) {
	provider := &NoopTracingProvider{}
	tracer := NewTracer(provider, "test")
	ctx := context.Background()

	newCtx, span := tracer.StartSpan(ctx, "test")
	if span == nil {
		t.Error("expected non-nil span")
	}
	span.End()

	s := tracer.SpanFromContext(newCtx)
	if s == nil {
		t.Error("expected non-nil span from context")
	}

	carrier := make(HTTPHeaderCarrier)
	tracer.Inject(ctx, carrier)

	extractedCtx := tracer.Extract(ctx, carrier)
	if extractedCtx == nil {
		t.Error("expected non-nil context")
	}

	err := tracer.Shutdown(ctx)
	if err != nil {
		t.Errorf("Shutdown failed: %v", err)
	}
}

func TestHTTPHeaderCarrier(t *testing.T) {
	carrier := make(HTTPHeaderCarrier)

	carrier.Set("X-Trace-ID", "trace-123")
	if carrier.Get("X-Trace-ID") != "trace-123" {
		t.Errorf("expected 'trace-123', got %s", carrier.Get("X-Trace-ID"))
	}

	if carrier.Get("nonexistent") != "" {
		t.Error("expected empty string for nonexistent key")
	}

	carrier.Set("X-Span-ID", "span-456")
	keys := carrier.Keys()
	if len(keys) != 2 {
		t.Errorf("expected 2 keys, got %d", len(keys))
	}
}

// Middleware tests

func TestHTTPMiddleware(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "test")
	tracer := NewTracer(&NoopTracingProvider{}, "test")

	mw := HTTPMiddleware(metrics, tracer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}

	// Verify metrics were recorded
	if len(provider.Counters) != 1 {
		t.Errorf("expected 1 counter, got %d", len(provider.Counters))
	}
	if len(provider.Timings) != 1 {
		t.Errorf("expected 1 timing, got %d", len(provider.Timings))
	}
}

func TestHTTPMiddleware_ErrorStatus(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "test")

	mw := HTTPMiddleware(metrics, nil)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))

	req := httptest.NewRequest(http.MethodGet, "/api/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", rec.Code)
	}
}

func TestMetricsOnlyMiddleware(t *testing.T) {
	provider := &TestMetricsProvider{}
	metrics := NewMetrics(provider, "test")

	mw := MetricsOnlyMiddleware(metrics)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if len(provider.Counters) != 1 {
		t.Errorf("expected 1 counter, got %d", len(provider.Counters))
	}
}

func TestTracingOnlyMiddleware(t *testing.T) {
	tracer := NewTracer(&NoopTracingProvider{}, "test")

	mw := TracingOnlyMiddleware(tracer)

	handler := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

// Provider registry tests

func TestRegisterMetricsProvider(t *testing.T) {
	// Register a test provider
	RegisterMetricsProvider("test-provider", func(ctx context.Context, cfg MetricsConfig) (MetricsProvider, error) {
		return &NoopMetricsProvider{}, nil
	})

	providers := ListMetricsProviders()
	found := false
	for _, p := range providers {
		if p == "test-provider" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected test-provider to be registered")
	}
}

func TestRegisterTracingProvider(t *testing.T) {
	RegisterTracingProvider("test-tracer", func(ctx context.Context, cfg MetricsConfig) (TracingProvider, error) {
		return &NoopTracingProvider{}, nil
	})

	providers := ListTracingProviders()
	found := false
	for _, p := range providers {
		if p == "test-tracer" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected test-tracer to be registered")
	}
}

func TestNewMetricsProviderByName_Noop(t *testing.T) {
	ctx := context.Background()

	// Empty name returns noop
	provider, err := NewMetricsProviderByName(ctx, "", MetricsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := provider.(*NoopMetricsProvider); !ok {
		t.Error("expected NoopMetricsProvider for empty name")
	}

	// "noop" name returns noop
	provider, err = NewMetricsProviderByName(ctx, "noop", MetricsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := provider.(*NoopMetricsProvider); !ok {
		t.Error("expected NoopMetricsProvider for 'noop' name")
	}
}

func TestNewMetricsProviderByName_Unknown(t *testing.T) {
	ctx := context.Background()

	_, err := NewMetricsProviderByName(ctx, "unknown-provider", MetricsConfig{})
	if err == nil {
		t.Error("expected error for unknown provider")
	}
}

func TestNewTracingProviderByName_Noop(t *testing.T) {
	ctx := context.Background()

	provider, err := NewTracingProviderByName(ctx, "", MetricsConfig{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := provider.(*NoopTracingProvider); !ok {
		t.Error("expected NoopTracingProvider for empty name")
	}
}

func TestNewTracingProviderByName_Unknown(t *testing.T) {
	ctx := context.Background()

	_, err := NewTracingProviderByName(ctx, "unknown-tracer", MetricsConfig{})
	if err == nil {
		t.Error("expected error for unknown tracer")
	}
}

// Constants tests

func TestSpanKindConstants(t *testing.T) {
	if SpanKindInternal != 0 {
		t.Errorf("expected SpanKindInternal = 0, got %d", SpanKindInternal)
	}
	if SpanKindServer != 1 {
		t.Errorf("expected SpanKindServer = 1, got %d", SpanKindServer)
	}
	if SpanKindClient != 2 {
		t.Errorf("expected SpanKindClient = 2, got %d", SpanKindClient)
	}
}

func TestSpanStatusConstants(t *testing.T) {
	if SpanStatusUnset != 0 {
		t.Errorf("expected SpanStatusUnset = 0, got %d", SpanStatusUnset)
	}
	if SpanStatusOK != 1 {
		t.Errorf("expected SpanStatusOK = 1, got %d", SpanStatusOK)
	}
	if SpanStatusError != 2 {
		t.Errorf("expected SpanStatusError = 2, got %d", SpanStatusError)
	}
}

func TestSpanNameConstants(t *testing.T) {
	if SpanHTTPRequest != "http.request" {
		t.Errorf("expected SpanHTTPRequest = 'http.request', got %s", SpanHTTPRequest)
	}
	if SpanEventCreate != "event.create" {
		t.Errorf("expected SpanEventCreate = 'event.create', got %s", SpanEventCreate)
	}
}

func TestAttributeKeyConstants(t *testing.T) {
	if AttrEventID != "relay.event.id" {
		t.Errorf("expected AttrEventID = 'relay.event.id', got %s", AttrEventID)
	}
	if AttrHTTPMethod != "http.method" {
		t.Errorf("expected AttrHTTPMethod = 'http.method', got %s", AttrHTTPMethod)
	}
}
