package delivery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

func BenchmarkDefaultConfig(b *testing.B) {
	for b.Loop() {
		_ = DefaultConfig()
	}
}

func BenchmarkConfigValidate(b *testing.B) {
	config := DefaultConfig()
	b.ResetTimer()

	for b.Loop() {
		config = config.Validate()
	}
	_ = config
}

func BenchmarkNewWorker(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	b.ResetTimer()

	for b.Loop() {
		w := NewWorker(q, nil, config)
		w.circuit.Stop()
	}
}

func BenchmarkWorker_Deliver(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
	}

	logger := workerLog.With("bench", true)
	ctx := context.Background()

	b.ResetTimer()

	for b.Loop() {
		evt.ID = uuid.New() // New ID for each delivery
		_, _ = w.Deliver(ctx, evt, nil, logger)
	}
}

func BenchmarkWorker_Deliver_Parallel(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	logger := workerLog.With("bench", true)
	ctx := context.Background()

	b.ResetTimer()

	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			evt := domain.Event{
				ID:          uuid.New(),
				Destination: server.URL,
				Payload:     []byte(`{"test": "data"}`),
				ClientID:    "test-client",
			}
			_, _ = w.Deliver(ctx, evt, nil, logger)
		}
	})
}

func BenchmarkWorker_CircuitStats(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	// Add some circuits
	for i := range 100 {
		w.circuit.RecordFailure("https://example" + string(rune(i)) + ".com")
	}

	b.ResetTimer()

	for b.Loop() {
		_ = w.CircuitStats()
	}
}

func BenchmarkWorker_StopAndWait(b *testing.B) {
	for b.Loop() {
		b.StopTimer()

		mr, err := miniredis.Run()
		if err != nil {
			b.Fatalf("failed to start miniredis: %v", err)
		}

		client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
		q := queue.NewQueue(client)
		config := DefaultConfig()
		config.EnableStandard = false
		config.EnableFIFO = false

		w := NewWorker(q, nil, config)

		b.StartTimer()

		_ = w.StopAndWait(time.Second)

		b.StopTimer()
		_ = client.Close()
		mr.Close()
	}
}

// Memory allocation benchmarks

func BenchmarkNewWorker_Allocs(b *testing.B) {
	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.EnableStandard = false
	config.EnableFIFO = false

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		w := NewWorker(q, nil, config)
		w.circuit.Stop()
	}
}

func BenchmarkWorker_Deliver_Allocs(b *testing.B) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	mr, err := miniredis.Run()
	if err != nil {
		b.Fatalf("failed to start miniredis: %v", err)
	}
	defer mr.Close()

	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() { _ = client.Close() }()

	q := queue.NewQueue(client)
	config := DefaultConfig()
	config.SigningKey = "test-signing-key-32-chars-long!"
	config.EnableStandard = false
	config.EnableFIFO = false

	w := NewWorker(q, nil, config)
	defer w.circuit.Stop()

	evt := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test": "data"}`),
		ClientID:    "test-client",
	}

	logger := workerLog.With("bench", true)
	ctx := context.Background()

	b.ReportAllocs()
	b.ResetTimer()

	for b.Loop() {
		evt.ID = uuid.New()
		_, _ = w.Deliver(ctx, evt, nil, logger)
	}
}
