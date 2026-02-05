package delivery

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/observability"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

func TestDefaultWorkerConfig(t *testing.T) {
	config := DefaultWorkerConfig()

	if config.Concurrency != 10 {
		t.Errorf("Concurrency = %d, want 10", config.Concurrency)
	}
	if config.VisibilityTime != 30*time.Second {
		t.Errorf("VisibilityTime = %v, want 30s", config.VisibilityTime)
	}
	if config.CircuitConfig.FailureThreshold != 5 {
		t.Errorf("CircuitConfig.FailureThreshold = %d, want 5", config.CircuitConfig.FailureThreshold)
	}
}

func TestNewWorker(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultWorkerConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"

	// Can't test with nil store in real scenarios, but we can test config is applied
	w := &Worker{
		queue:          q,
		store:          nil, // Would need mock store
		sender:         NewSender(config.SigningKey),
		circuit:        NewCircuitBreaker(config.CircuitConfig),
		retry:          NewRetryPolicy(),
		
		stopCh:         make(chan struct{}),
		concurrency:    config.Concurrency,
		visibilityTime: config.VisibilityTime,
	}
	defer w.circuit.Stop()

	if w.concurrency != 10 {
		t.Errorf("concurrency = %d, want 10", w.concurrency)
	}
	if w.visibilityTime != 30*time.Second {
		t.Errorf("visibilityTime = %v, want 30s", w.visibilityTime)
	}
}

func TestWorker_Stop(t *testing.T) {
	config := DefaultWorkerConfig()

	w := &Worker{
		circuit: NewCircuitBreaker(config.CircuitConfig),
		
		stopCh:  make(chan struct{}),
	}
	defer w.circuit.Stop()

	// Stop should close the channel
	w.Stop()

	// Verify channel is closed
	select {
	case <-w.stopCh:
		// Expected - channel is closed
	default:
		t.Error("stopCh should be closed")
	}
}

func TestWorker_Wait(t *testing.T) {
	config := DefaultWorkerConfig()

	w := &Worker{
		circuit: NewCircuitBreaker(config.CircuitConfig),
		
		stopCh:  make(chan struct{}),
	}
	defer w.circuit.Stop()

	// Add a goroutine
	w.wg.Add(1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		w.wg.Done()
	}()

	// Wait should block until goroutine completes
	done := make(chan struct{})
	go func() {
		w.Wait()
		close(done)
	}()

	select {
	case <-done:
		// Success
	case <-time.After(time.Second):
		t.Error("Wait() should have completed")
	}
}

func TestWorker_StopAndWait_Success(t *testing.T) {
	config := DefaultWorkerConfig()

	w := &Worker{
		circuit: NewCircuitBreaker(config.CircuitConfig),
		
		stopCh:  make(chan struct{}),
	}
	defer w.circuit.Stop()

	// Add a goroutine that responds to stop signal
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		<-w.stopCh
	}()

	// StopAndWait should succeed
	err := w.StopAndWait(time.Second)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWorker_StopAndWait_Timeout(t *testing.T) {
	config := DefaultWorkerConfig()

	w := &Worker{
		circuit: NewCircuitBreaker(config.CircuitConfig),
		
		stopCh:  make(chan struct{}),
	}
	defer w.circuit.Stop()

	// Add a goroutine that doesn't respond to stop
	w.wg.Add(1)
	go func() {
		time.Sleep(10 * time.Second)
		w.wg.Done()
	}()

	// StopAndWait with short timeout should return error
	err := w.StopAndWait(50 * time.Millisecond)
	if err == nil {
		t.Error("expected timeout error")
	}
	if err.Error() != "worker shutdown timed out" {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestWorker_CircuitStats(t *testing.T) {
	config := DefaultWorkerConfig()

	w := &Worker{
		circuit: NewCircuitBreaker(config.CircuitConfig),
		
	}
	defer w.circuit.Stop()

	// Record some failures
	w.circuit.RecordFailure("https://example1.com")
	w.circuit.RecordFailure("https://example2.com")

	stats := w.CircuitStats()

	if len(stats.Circuits) != 2 {
		t.Errorf("expected 2 circuits, got %d", len(stats.Circuits))
	}
}

func TestExtractHost(t *testing.T) {
	tests := []struct {
		destination string
		expected    string
	}{
		{"https://example.com/webhook", "example.com"},
		{"https://api.example.com:8080/v1/hook", "api.example.com:8080"},
		{"http://localhost:3000/callback", "localhost:3000"},
		{"https://user:pass@secure.example.com/hook", "secure.example.com"},
		{"invalid-url", ""},
		{"", ""},
		{"://missing-scheme", "unknown"}, // url.Parse returns error for this
	}

	for _, tt := range tests {
		t.Run(tt.destination, func(t *testing.T) {
			result := extractHost(tt.destination)
			if result != tt.expected {
				t.Errorf("extractHost(%q) = %q, want %q", tt.destination, result, tt.expected)
			}
		})
	}
}

func TestClassifyFailureReason(t *testing.T) {
	tests := []struct {
		name     string
		result   domain.DeliveryResult
		expected string
	}{
		{
			name:     "timeout error",
			result:   domain.NewFailureResult(0, "", errors.New("connection timeout"), 100),
			expected: "timeout",
		},
		{
			name:     "connection refused",
			result:   domain.NewFailureResult(0, "", errors.New("dial tcp: connection refused"), 100),
			expected: "connection_refused",
		},
		{
			name:     "DNS error",
			result:   domain.NewFailureResult(0, "", errors.New("lookup example.com: no such host"), 100),
			expected: "dns_error",
		},
		{
			name:     "TLS error",
			result:   domain.NewFailureResult(0, "", errors.New("TLS handshake error"), 100),
			expected: "tls_error",
		},
		{
			name:     "generic network error",
			result:   domain.NewFailureResult(0, "", errors.New("network unreachable"), 100),
			expected: "network_error",
		},
		{
			name:     "server error 500",
			result:   domain.NewFailureResult(500, "Internal Server Error", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 502",
			result:   domain.NewFailureResult(502, "Bad Gateway", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 503",
			result:   domain.NewFailureResult(503, "Service Unavailable", nil, 100),
			expected: "server_error",
		},
		{
			name:     "server error 504",
			result:   domain.NewFailureResult(504, "Gateway Timeout", nil, 100),
			expected: "server_error",
		},
		{
			name:     "client error 400",
			result:   domain.NewFailureResult(400, "Bad Request", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 401",
			result:   domain.NewFailureResult(401, "Unauthorized", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 404",
			result:   domain.NewFailureResult(404, "Not Found", nil, 100),
			expected: "client_error",
		},
		{
			name:     "client error 429",
			result:   domain.NewFailureResult(429, "Too Many Requests", nil, 100),
			expected: "client_error",
		},
		{
			name:     "unknown status 0",
			result:   domain.NewFailureResult(0, "", nil, 100),
			expected: "unknown",
		},
		{
			name:     "redirect 301",
			result:   domain.NewFailureResult(301, "", nil, 100),
			expected: "unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := classifyFailureReason(tt.result)
			if result != tt.expected {
				t.Errorf("classifyFailureReason() = %q, want %q", result, tt.expected)
			}
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "hello", true},
		{"hello world", "lo wo", true},
		{"hello", "hello", true},
		{"hello", "world", false},
		{"", "a", false},
		{"a", "", true},
		{"", "", true},
		{"timeout occurred", "timeout", true},
		{"TIMEOUT", "timeout", false}, // Case sensitive
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.substr, func(t *testing.T) {
			result := contains(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("contains(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

func TestContainsAt(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "hello", true},
		{"hello world", "lo wo", true},
		{"hello world", "o w", true},
		{"hello", "world", false},
		{"abc", "abcd", false},
		{"a", "a", true},
		{"ab", "b", true},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.substr, func(t *testing.T) {
			result := containsAt(tt.s, tt.substr)
			if result != tt.expected {
				t.Errorf("containsAt(%q, %q) = %v, want %v", tt.s, tt.substr, result, tt.expected)
			}
		})
	}
}

// Integration test: Worker with mock server
func TestWorker_SenderIntegration(t *testing.T) {
	var mu sync.Mutex
	callCount := 0

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		callCount++
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test":"data"}`),
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}

	mu.Lock()
	defer mu.Unlock()
	if callCount != 1 {
		t.Errorf("expected 1 call, got %d", callCount)
	}
}

// Test retry policy with endpoint configuration
func TestWorker_RetryPolicyWithEndpoint(t *testing.T) {
	policy := NewRetryPolicy()

	endpoint := &domain.Endpoint{
		MaxRetries:       3,
		RetryBackoffMs:   100,
		RetryBackoffMult: 2.0,
		RetryBackoffMax:  10000,
	}

	// Test ShouldRetryForEndpoint
	if !policy.ShouldRetryForEndpoint(1, endpoint) {
		t.Error("should retry on attempt 1")
	}
	if !policy.ShouldRetryForEndpoint(2, endpoint) {
		t.Error("should retry on attempt 2")
	}
	if policy.ShouldRetryForEndpoint(3, endpoint) {
		t.Error("should not retry on attempt 3 (max is 3)")
	}

	// Test NextRetryDelayForEndpoint
	delay1 := policy.NextRetryDelayForEndpoint(1, endpoint)
	delay2 := policy.NextRetryDelayForEndpoint(2, endpoint)

	// Allow for jitter
	if delay1 < 90*time.Millisecond || delay1 > 120*time.Millisecond {
		t.Errorf("delay1 = %v, expected ~100ms", delay1)
	}
	if delay2 < 180*time.Millisecond || delay2 > 240*time.Millisecond {
		t.Errorf("delay2 = %v, expected ~200ms", delay2)
	}
}

// Test circuit breaker with endpoint configuration
func TestWorker_CircuitBreakerWithEndpoint(t *testing.T) {
	defaultConfig := DefaultCircuitConfig()

	endpoint := &domain.Endpoint{
		CircuitThreshold: 3,
		CircuitResetMs:   60000, // 1 minute
	}

	config := CircuitConfigFromEndpoint(endpoint, defaultConfig)

	if config.FailureThreshold != 3 {
		t.Errorf("FailureThreshold = %d, want 3", config.FailureThreshold)
	}
	if config.OpenDuration != time.Minute {
		t.Errorf("OpenDuration = %v, want 1m", config.OpenDuration)
	}

	cb := NewCircuitBreaker(config)
	defer cb.Stop()

	dest := "https://example.com"

	// 2 failures shouldn't open
	cb.RecordFailure(dest)
	cb.RecordFailure(dest)
	if cb.IsOpen(dest) {
		t.Error("circuit should not be open after 2 failures")
	}

	// 3rd failure should open
	cb.RecordFailure(dest)
	if !cb.IsOpen(dest) {
		t.Error("circuit should be open after 3 failures")
	}
}

// Test worker with metrics
func TestWorker_WithMetrics(t *testing.T) {
	config := DefaultWorkerConfig()

	metrics := observability.NewMetrics(&observability.NoopMetricsProvider{}, "test")

	w := &Worker{
		circuit: NewCircuitBreaker(config.CircuitConfig),
		
		metrics: metrics,
		stopCh:  make(chan struct{}),
	}
	defer w.circuit.Stop()

	if w.metrics != metrics {
		t.Error("metrics not set correctly")
	}
}

// Test worker with rate limiter
func TestWorker_WithRateLimiter(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	rateLimiter := NewRateLimiter(client)
	config := DefaultWorkerConfig()

	w := &Worker{
		circuit:     NewCircuitBreaker(config.CircuitConfig),
		rateLimiter: rateLimiter,
		
		stopCh:      make(chan struct{}),
	}
	defer w.circuit.Stop()

	if w.rateLimiter != rateLimiter {
		t.Error("rateLimiter not set correctly")
	}
}

// Test processLoop exits on context cancellation
func TestWorker_ProcessLoop_ContextCancellation(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultWorkerConfig()

	w := &Worker{
		queue:          q,
		circuit:        NewCircuitBreaker(config.CircuitConfig),
		retry:          NewRetryPolicy(),
		
		stopCh:         make(chan struct{}),
		visibilityTime: config.VisibilityTime,
	}
	defer w.circuit.Stop()

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan struct{})
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.processLoop(ctx, 0)
		close(done)
	}()

	// Cancel context
	cancel()

	// Process loop should exit
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("processLoop should have exited on context cancellation")
	}
}

// Test processLoop exits on stop signal
func TestWorker_ProcessLoop_StopSignal(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client).WithBlockingTimeout(100 * time.Millisecond)
	config := DefaultWorkerConfig()

	w := &Worker{
		queue:          q,
		circuit:        NewCircuitBreaker(config.CircuitConfig),
		retry:          NewRetryPolicy(),
		
		stopCh:         make(chan struct{}),
		visibilityTime: config.VisibilityTime,
	}
	defer w.circuit.Stop()

	ctx := context.Background()

	done := make(chan struct{})
	w.wg.Add(1)
	go func() {
		defer w.wg.Done()
		w.processLoop(ctx, 0)
		close(done)
	}()

	// Send stop signal
	time.Sleep(50 * time.Millisecond)
	w.Stop()

	// Process loop should exit
	select {
	case <-done:
		// Success
	case <-time.After(2 * time.Second):
		t.Error("processLoop should have exited on stop signal")
	}
}

// Test Start launches goroutines
func TestWorker_Start(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client).WithBlockingTimeout(50 * time.Millisecond)
	config := WorkerConfig{
		Concurrency:    2,
		VisibilityTime: 30 * time.Second,
		SigningKey:     "test-signing-key-32-chars-long!",
		CircuitConfig:  DefaultCircuitConfig(),
	}

	w := &Worker{
		queue:          q,
		circuit:        NewCircuitBreaker(config.CircuitConfig),
		retry:          NewRetryPolicy(),
		sender:         NewSender(config.SigningKey),
		
		stopCh:         make(chan struct{}),
		concurrency:    config.Concurrency,
		visibilityTime: config.VisibilityTime,
	}
	defer w.circuit.Stop()

	ctx := context.Background()

	// Start worker
	w.Start(ctx)

	// Give goroutines time to start
	time.Sleep(50 * time.Millisecond)

	// Stop and wait
	err = w.StopAndWait(2 * time.Second)
	if err != nil {
		t.Errorf("StopAndWait error: %v", err)
	}
}

// Benchmark for classifyFailureReason
func BenchmarkClassifyFailureReason(b *testing.B) {
	result := domain.NewFailureResult(500, "Internal Server Error", nil, 100)

	b.ResetTimer()
	for range b.N {
		classifyFailureReason(result)
	}
}

// Benchmark for extractHost
func BenchmarkExtractHost(b *testing.B) {
	url := "https://api.example.com:8080/v1/webhook"

	b.ResetTimer()
	for range b.N {
		extractHost(url)
	}
}

// Benchmark for contains
func BenchmarkContains(b *testing.B) {
	s := "connection timeout occurred while connecting to server"
	substr := "timeout"

	b.ResetTimer()
	for range b.N {
		contains(s, substr)
	}
}
