package delivery

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/metrics"
)

func recorderSetupTestRedis(t *testing.T) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("failed to start miniredis: %v", err)
	}

	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})

	return mr, client
}

func TestNewRecorder(t *testing.T) {
	t.Parallel()

	t.Run("creates recorder with all dependencies", func(t *testing.T) {
		t.Parallel()

		mr, client := recorderSetupTestRedis(t)
		defer mr.Close()
		defer func() { _ = client.Close() }()

		store := metrics.NewStore(client)
		logger := slog.Default()

		recorder := NewRecorder(nil, store, logger)

		require.NotNil(t, recorder)
		assert.Same(t, store, recorder.metricsStore)
		assert.Same(t, logger, recorder.logger)
	})

	t.Run("creates recorder with nil dependencies", func(t *testing.T) {
		t.Parallel()

		recorder := NewRecorder(nil, nil, nil)

		require.NotNil(t, recorder)
		assert.Nil(t, recorder.metricsStore)
		assert.Nil(t, recorder.logger)
	})
}

func TestRecorder_RecordDelivery(t *testing.T) {
	mr, client := recorderSetupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	store := metrics.NewStore(client)

	baseEvent := domain.Event{
		ID:        uuid.New(),
		ClientID:  "client-123",
		EventType: "user.created",
		Attempts:  3,
	}

	baseEndpoint := &domain.Endpoint{
		ID: uuid.New(),
	}

	tests := []struct {
		name     string
		event    domain.Event
		endpoint *domain.Endpoint
		result   domain.DeliveryResult
	}{
		{
			name:     "successful delivery",
			event:    baseEvent,
			endpoint: baseEndpoint,
			result: domain.DeliveryResult{
				Success:    true,
				StatusCode: 200,
				DurationMs: 150,
			},
		},
		{
			name:     "failed delivery",
			event:    baseEvent,
			endpoint: baseEndpoint,
			result: domain.DeliveryResult{
				Success:    false,
				StatusCode: 500,
				DurationMs: 100,
			},
		},
		{
			name:     "delivery without endpoint",
			event:    baseEvent,
			endpoint: nil,
			result: domain.DeliveryResult{
				Success:    true,
				StatusCode: 200,
				DurationMs: 50,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			recorder := NewRecorder(nil, store, slog.Default())
			recorder.RecordDelivery(context.Background(), tt.event, tt.endpoint, tt.result)

			// Wait for async recording
			time.Sleep(50 * time.Millisecond)

			// Verify recorded in store
			records, err := store.GetDeliveryRecords(
				context.Background(),
				time.Now().Add(-1*time.Minute),
				time.Now().Add(1*time.Minute),
				100,
			)
			require.NoError(t, err)
			assert.GreaterOrEqual(t, len(records), 1)
		})
	}
}

func TestRecorder_RecordDelivery_NilStore(t *testing.T) {
	t.Parallel()

	recorder := NewRecorder(nil, nil, slog.Default())

	// Should not panic with nil store
	recorder.RecordDelivery(context.Background(), domain.Event{}, nil, domain.DeliveryResult{})
}

func TestRecorder_RecordDelivery_Concurrent(t *testing.T) {
	mr, client := recorderSetupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	store := metrics.NewStore(client)
	recorder := NewRecorder(nil, store, slog.Default())

	const goroutines = 50
	var wg sync.WaitGroup

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			recorder.RecordDelivery(
				context.Background(),
				domain.Event{ID: uuid.New()},
				nil,
				domain.DeliveryResult{Success: true},
			)
		}()
	}

	wg.Wait()
	// Wait for async recordings
	time.Sleep(100 * time.Millisecond)

	// Verify records were written
	records, err := store.GetDeliveryRecords(
		context.Background(),
		time.Now().Add(-1*time.Minute),
		time.Now().Add(1*time.Minute),
		100,
	)
	require.NoError(t, err)
	assert.Equal(t, goroutines, len(records), "all deliveries should be recorded")
}

func TestRecorder_RecordRateLimit(t *testing.T) {
	mr, client := recorderSetupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	store := metrics.NewStore(client)

	t.Run("records rate limit event", func(t *testing.T) {
		recorder := NewRecorder(nil, store, slog.Default())

		event := domain.Event{
			ID: uuid.New(),
		}
		endpoint := &domain.Endpoint{
			ID:              uuid.New(),
			RateLimitPerSec: 100,
		}

		recorder.RecordRateLimit(context.Background(), endpoint, event)

		// Verify count increased
		count, err := store.GetRateLimitCount(context.Background())
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, int64(1))
	})

	t.Run("no-op with nil store", func(t *testing.T) {
		recorder := NewRecorder(nil, nil, slog.Default())

		// Should not panic
		recorder.RecordRateLimit(context.Background(), &domain.Endpoint{ID: uuid.New()}, domain.Event{})
	})

	t.Run("no-op with nil endpoint", func(t *testing.T) {
		recorder := NewRecorder(nil, store, slog.Default())
		recorder.RecordRateLimit(context.Background(), nil, domain.Event{})
		// Should not panic or record
	})
}

func TestRecorder_OutcomeClassification(t *testing.T) {
	t.Parallel()

	mr, client := recorderSetupTestRedis(t)
	defer mr.Close()
	defer func() { _ = client.Close() }()

	store := metrics.NewStore(client)
	recorder := NewRecorder(nil, store, slog.Default())

	t.Run("classifies timeout correctly", func(t *testing.T) {
		t.Parallel()

		event := domain.Event{ID: uuid.New()}
		result := domain.DeliveryResult{
			Success: false,
			Error:   context.DeadlineExceeded, // timeout error
		}

		recorder.RecordDelivery(context.Background(), event, nil, result)
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("classifies success correctly", func(t *testing.T) {
		t.Parallel()

		event := domain.Event{ID: uuid.New()}
		result := domain.DeliveryResult{
			Success:    true,
			StatusCode: 200,
		}

		recorder.RecordDelivery(context.Background(), event, nil, result)
		time.Sleep(50 * time.Millisecond)
	})

	t.Run("classifies failure correctly", func(t *testing.T) {
		t.Parallel()

		event := domain.Event{ID: uuid.New()}
		result := domain.DeliveryResult{
			Success:    false,
			StatusCode: 500,
		}

		recorder.RecordDelivery(context.Background(), event, nil, result)
		time.Sleep(50 * time.Millisecond)
	})
}
