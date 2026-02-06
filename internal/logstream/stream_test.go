package logstream

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilter_Matches(t *testing.T) {
	tests := []struct {
		name     string
		filter   *Filter
		entry    *LogEntry
		expected bool
	}{
		{
			name:   "nil filter matches everything",
			filter: nil,
			entry: &LogEntry{
				Level:     "info",
				EventType: "order.created",
				Status:    "delivered",
			},
			expected: true,
		},
		{
			name:   "empty filter matches everything",
			filter: &Filter{},
			entry: &LogEntry{
				Level:     "info",
				EventType: "order.created",
				Status:    "delivered",
			},
			expected: true,
		},
		{
			name: "event type filter - match",
			filter: &Filter{
				EventTypes: []string{"order.created", "order.updated"},
			},
			entry: &LogEntry{
				EventType: "order.created",
			},
			expected: true,
		},
		{
			name: "event type filter - no match",
			filter: &Filter{
				EventTypes: []string{"order.created", "order.updated"},
			},
			entry: &LogEntry{
				EventType: "payment.completed",
			},
			expected: false,
		},
		{
			name: "endpoint filter - match",
			filter: &Filter{
				EndpointIDs: []string{"ep-123", "ep-456"},
			},
			entry: &LogEntry{
				EndpointID: "ep-123",
			},
			expected: true,
		},
		{
			name: "endpoint filter - no match",
			filter: &Filter{
				EndpointIDs: []string{"ep-123", "ep-456"},
			},
			entry: &LogEntry{
				EndpointID: "ep-789",
			},
			expected: false,
		},
		{
			name: "status filter - match",
			filter: &Filter{
				Statuses: []string{"failed", "dead"},
			},
			entry: &LogEntry{
				Status: "failed",
			},
			expected: true,
		},
		{
			name: "status filter - no match",
			filter: &Filter{
				Statuses: []string{"failed", "dead"},
			},
			entry: &LogEntry{
				Status: "delivered",
			},
			expected: false,
		},
		{
			name: "client filter - match",
			filter: &Filter{
				ClientIDs: []string{"client-a", "client-b"},
			},
			entry: &LogEntry{
				ClientID: "client-a",
			},
			expected: true,
		},
		{
			name: "client filter - no match",
			filter: &Filter{
				ClientIDs: []string{"client-a", "client-b"},
			},
			entry: &LogEntry{
				ClientID: "client-c",
			},
			expected: false,
		},
		{
			name: "level filter - debug shows all",
			filter: &Filter{
				Level: "debug",
			},
			entry: &LogEntry{
				Level: "info",
			},
			expected: true,
		},
		{
			name: "level filter - info hides debug",
			filter: &Filter{
				Level: "info",
			},
			entry: &LogEntry{
				Level: "debug",
			},
			expected: false,
		},
		{
			name: "level filter - warn shows warn and error",
			filter: &Filter{
				Level: "warn",
			},
			entry: &LogEntry{
				Level: "error",
			},
			expected: true,
		},
		{
			name: "level filter - error hides warn",
			filter: &Filter{
				Level: "error",
			},
			entry: &LogEntry{
				Level: "warn",
			},
			expected: false,
		},
		{
			name: "combined filters - all match",
			filter: &Filter{
				EventTypes:  []string{"order.created"},
				EndpointIDs: []string{"ep-123"},
				Statuses:    []string{"delivered"},
				Level:       "info",
			},
			entry: &LogEntry{
				Level:      "info",
				EventType:  "order.created",
				EndpointID: "ep-123",
				Status:     "delivered",
			},
			expected: true,
		},
		{
			name: "combined filters - one doesn't match",
			filter: &Filter{
				EventTypes:  []string{"order.created"},
				EndpointIDs: []string{"ep-123"},
				Statuses:    []string{"delivered"},
			},
			entry: &LogEntry{
				EventType:  "order.created",
				EndpointID: "ep-123",
				Status:     "failed", // doesn't match
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filter.Matches(tt.entry)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestHub_SubscribeUnsubscribe(t *testing.T) {
	hub := NewHub(10, 100)

	// Subscribe
	sub1 := hub.Subscribe("sub-1", nil)
	assert.NotNil(t, sub1)
	assert.Equal(t, "sub-1", sub1.ID)
	assert.Equal(t, 1, hub.SubscriberCount())

	sub2 := hub.Subscribe("sub-2", &Filter{Level: "warn"})
	assert.NotNil(t, sub2)
	assert.Equal(t, 2, hub.SubscriberCount())

	// Unsubscribe
	hub.Unsubscribe("sub-1")
	assert.Equal(t, 1, hub.SubscriberCount())

	// Unsubscribe non-existent (should not panic)
	hub.Unsubscribe("non-existent")
	assert.Equal(t, 1, hub.SubscriberCount())

	hub.Unsubscribe("sub-2")
	assert.Equal(t, 0, hub.SubscriberCount())
}

func TestHub_Publish(t *testing.T) {
	hub := NewHub(10, 100)

	// Subscribe with no filter
	sub1 := hub.Subscribe("sub-1", nil)

	// Subscribe with filter
	sub2 := hub.Subscribe("sub-2", &Filter{
		Statuses: []string{"failed", "dead"},
	})

	// Publish entry that matches both
	entry1 := &LogEntry{
		Timestamp: time.Now(),
		Level:     "warn",
		EventID:   "evt-1",
		Status:    "failed",
		Message:   "delivery failed",
	}
	hub.Publish(entry1)

	// Both should receive
	select {
	case received := <-sub1.Ch:
		assert.Equal(t, entry1, received)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub1 did not receive entry")
	}

	select {
	case received := <-sub2.Ch:
		assert.Equal(t, entry1, received)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub2 did not receive entry")
	}

	// Publish entry that only matches sub1 (no filter)
	entry2 := &LogEntry{
		Timestamp: time.Now(),
		Level:     "info",
		EventID:   "evt-2",
		Status:    "delivered",
		Message:   "delivery successful",
	}
	hub.Publish(entry2)

	// Only sub1 should receive
	select {
	case received := <-sub1.Ch:
		assert.Equal(t, entry2, received)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("sub1 did not receive entry")
	}

	// sub2 should not receive (filtered out)
	select {
	case <-sub2.Ch:
		t.Fatal("sub2 should not receive filtered entry")
	case <-time.After(50 * time.Millisecond):
		// Expected
	}

	hub.Unsubscribe("sub-1")
	hub.Unsubscribe("sub-2")
}

func TestHub_PublishDropsWhenBufferFull(t *testing.T) {
	// Small buffer
	hub := NewHub(2, 100)

	sub := hub.Subscribe("sub-1", nil)

	// Fill buffer
	for i := 0; i < 5; i++ {
		hub.Publish(&LogEntry{
			EventID: string(rune('a' + i)),
			Message: "test",
		})
	}

	// Should have received only buffer size (2) entries
	received := 0
	for {
		select {
		case <-sub.Ch:
			received++
		default:
			goto done
		}
	}
done:
	assert.Equal(t, 2, received, "should only receive buffer size entries")

	hub.Unsubscribe("sub-1")
}

func TestHub_ConcurrentPublish(t *testing.T) {
	hub := NewHub(100, 100)

	sub := hub.Subscribe("sub-1", nil)

	var wg sync.WaitGroup
	publishCount := 50
	goroutines := 10

	// Publish from multiple goroutines
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for i := 0; i < publishCount; i++ {
				hub.Publish(&LogEntry{
					EventID: string(rune(goroutineID*publishCount + i)),
					Message: "concurrent test",
				})
			}
		}(g)
	}

	// Collect entries
	var received atomic.Int32
	done := make(chan struct{})
	go func() {
		for {
			select {
			case <-sub.Ch:
				received.Add(1)
			case <-done:
				return
			}
		}
	}()

	wg.Wait()
	time.Sleep(100 * time.Millisecond)
	close(done)

	// Should receive up to buffer size, potentially less due to drops
	assert.LessOrEqual(t, int(received.Load()), goroutines*publishCount)
	assert.Greater(t, int(received.Load()), 0)

	hub.Unsubscribe("sub-1")
}

func TestHub_ConcurrentSubscribeUnsubscribe(t *testing.T) {
	hub := NewHub(10, 100)

	var wg sync.WaitGroup

	// Concurrent subscribe/unsubscribe
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			subID := string(rune('a' + id))
			sub := hub.Subscribe(subID, nil)
			require.NotNil(t, sub)
			time.Sleep(time.Millisecond)
			hub.Unsubscribe(subID)
		}(i)
	}

	// Concurrent publish while subscribing/unsubscribing
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 100; i++ {
			hub.Publish(&LogEntry{Message: "test"})
			time.Sleep(time.Microsecond)
		}
	}()

	wg.Wait()

	// Should end with 0 subscribers
	assert.Equal(t, 0, hub.SubscriberCount())
}

func TestDeliveryLogger_LogDeliverySuccess(t *testing.T) {
	hub := NewHub(10, 100)
	logger := NewDeliveryLogger(hub)

	sub := hub.Subscribe("test", nil)

	ctx := context.Background()
	logger.LogDeliverySuccess(ctx, "evt-123", "order.created", "ep-456", "https://example.com/webhook", "client-1", 200, 150, 1, 10)

	select {
	case entry := <-sub.Ch:
		assert.Equal(t, "info", entry.Level)
		assert.Equal(t, "evt-123", entry.EventID)
		assert.Equal(t, "order.created", entry.EventType)
		assert.Equal(t, "ep-456", entry.EndpointID)
		assert.Equal(t, "https://example.com/webhook", entry.Destination)
		assert.Equal(t, "client-1", entry.ClientID)
		assert.Equal(t, "delivered", entry.Status)
		assert.Equal(t, 200, entry.StatusCode)
		assert.Equal(t, int64(150), entry.DurationMs)
		assert.Equal(t, 1, entry.Attempt)
		assert.Equal(t, 10, entry.MaxAttempts)
		assert.Equal(t, "delivery successful", entry.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive log entry")
	}

	hub.Unsubscribe("test")
}

func TestDeliveryLogger_LogDeliveryFailure_WillRetry(t *testing.T) {
	hub := NewHub(10, 100)
	logger := NewDeliveryLogger(hub)

	sub := hub.Subscribe("test", nil)

	ctx := context.Background()
	logger.LogDeliveryFailure(ctx, "evt-123", "order.created", "ep-456", "https://example.com/webhook", "client-1", 500, 250, "internal server error", 3, 10, true)

	select {
	case entry := <-sub.Ch:
		assert.Equal(t, "warn", entry.Level)
		assert.Equal(t, "evt-123", entry.EventID)
		assert.Equal(t, "failed", entry.Status)
		assert.Equal(t, 500, entry.StatusCode)
		assert.Equal(t, int64(250), entry.DurationMs)
		assert.Equal(t, "internal server error", entry.Error)
		assert.Equal(t, 3, entry.Attempt)
		assert.Equal(t, "delivery failed, will retry", entry.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive log entry")
	}

	hub.Unsubscribe("test")
}

func TestDeliveryLogger_LogDeliveryFailure_NoRetry(t *testing.T) {
	hub := NewHub(10, 100)
	logger := NewDeliveryLogger(hub)

	sub := hub.Subscribe("test", nil)

	ctx := context.Background()
	logger.LogDeliveryFailure(ctx, "evt-123", "order.created", "ep-456", "https://example.com/webhook", "client-1", 500, 250, "internal server error", 10, 10, false)

	select {
	case entry := <-sub.Ch:
		assert.Equal(t, "error", entry.Level)
		assert.Equal(t, "dead", entry.Status)
		assert.Equal(t, "delivery failed, max attempts reached", entry.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive log entry")
	}

	hub.Unsubscribe("test")
}

func TestDeliveryLogger_LogDeliveryStart(t *testing.T) {
	hub := NewHub(10, 100)
	logger := NewDeliveryLogger(hub)

	sub := hub.Subscribe("test", nil)

	ctx := context.Background()
	logger.LogDeliveryStart(ctx, "evt-123", "order.created", "ep-456", "https://example.com/webhook", "client-1", 1, 10)

	select {
	case entry := <-sub.Ch:
		assert.Equal(t, "info", entry.Level)
		assert.Equal(t, "delivering", entry.Status)
		assert.Equal(t, "starting delivery attempt", entry.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive log entry")
	}

	hub.Unsubscribe("test")
}

func TestDeliveryLogger_LogCircuitTripped(t *testing.T) {
	hub := NewHub(10, 100)
	logger := NewDeliveryLogger(hub)

	sub := hub.Subscribe("test", nil)

	ctx := context.Background()
	logger.LogCircuitTripped(ctx, "ep-456", "https://example.com/webhook", "client-1", 5)

	select {
	case entry := <-sub.Ch:
		assert.Equal(t, "warn", entry.Level)
		assert.Equal(t, "circuit_open", entry.Status)
		assert.Equal(t, "circuit breaker tripped", entry.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive log entry")
	}

	hub.Unsubscribe("test")
}

func TestDeliveryLogger_LogCircuitRecovered(t *testing.T) {
	hub := NewHub(10, 100)
	logger := NewDeliveryLogger(hub)

	sub := hub.Subscribe("test", nil)

	ctx := context.Background()
	logger.LogCircuitRecovered(ctx, "ep-456", "https://example.com/webhook", "client-1")

	select {
	case entry := <-sub.Ch:
		assert.Equal(t, "info", entry.Level)
		assert.Equal(t, "circuit_closed", entry.Status)
		assert.Equal(t, "circuit breaker recovered", entry.Message)
	case <-time.After(100 * time.Millisecond):
		t.Fatal("did not receive log entry")
	}

	hub.Unsubscribe("test")
}

func TestDeliveryLogger_NilHub(t *testing.T) {
	// Should not panic with nil hub
	logger := NewDeliveryLogger(nil)

	ctx := context.Background()

	// None of these should panic
	logger.LogDeliveryStart(ctx, "evt-123", "order.created", "ep-456", "https://example.com", "client-1", 1, 10)
	logger.LogDeliverySuccess(ctx, "evt-123", "order.created", "ep-456", "https://example.com", "client-1", 200, 150, 1, 10)
	logger.LogDeliveryFailure(ctx, "evt-123", "order.created", "ep-456", "https://example.com", "client-1", 500, 250, "error", 1, 10, true)
	logger.LogCircuitTripped(ctx, "ep-456", "https://example.com", "client-1", 5)
	logger.LogCircuitRecovered(ctx, "ep-456", "https://example.com", "client-1")
}

func TestMatchesLevel(t *testing.T) {
	tests := []struct {
		entryLevel  string
		filterLevel string
		expected    bool
	}{
		{"debug", "debug", true},
		{"info", "debug", true},
		{"warn", "debug", true},
		{"error", "debug", true},
		{"debug", "info", false},
		{"info", "info", true},
		{"warn", "info", true},
		{"error", "info", true},
		{"debug", "warn", false},
		{"info", "warn", false},
		{"warn", "warn", true},
		{"error", "warn", true},
		{"debug", "error", false},
		{"info", "error", false},
		{"warn", "error", false},
		{"error", "error", true},
		{"unknown", "info", true}, // unknown level passes
		{"info", "unknown", true}, // unknown filter passes
	}

	for _, tt := range tests {
		t.Run(tt.entryLevel+"_"+tt.filterLevel, func(t *testing.T) {
			result := matchesLevel(tt.entryLevel, tt.filterLevel)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestContains(t *testing.T) {
	tests := []struct {
		slice    []string
		s        string
		expected bool
	}{
		{[]string{"a", "b", "c"}, "a", true},
		{[]string{"a", "b", "c"}, "b", true},
		{[]string{"a", "b", "c"}, "c", true},
		{[]string{"a", "b", "c"}, "d", false},
		{[]string{}, "a", false},
		{nil, "a", false},
	}

	for _, tt := range tests {
		result := contains(tt.slice, tt.s)
		assert.Equal(t, tt.expected, result)
	}
}

func TestHub_DefaultValues(t *testing.T) {
	// Test with zero/negative values
	hub := NewHub(0, 0)
	assert.Equal(t, 100, hub.bufferSize)
	assert.Equal(t, 100, hub.rateLimit)

	hub2 := NewHub(-5, -10)
	assert.Equal(t, 100, hub2.bufferSize)
	assert.Equal(t, 100, hub2.rateLimit)
}

func TestFilter_EmptySliceVsNil(t *testing.T) {
	entry := &LogEntry{
		EventType: "order.created",
		Status:    "delivered",
	}

	// Nil slice should match
	filter1 := &Filter{EventTypes: nil}
	assert.True(t, filter1.Matches(entry))

	// Empty slice should match (no filter)
	filter2 := &Filter{EventTypes: []string{}}
	assert.True(t, filter2.Matches(entry))

	// Slice with values should filter
	filter3 := &Filter{EventTypes: []string{"payment.completed"}}
	assert.False(t, filter3.Matches(entry))
}

func BenchmarkHub_Publish(b *testing.B) {
	hub := NewHub(1000, 10000)

	// Add some subscribers
	for i := 0; i < 10; i++ {
		sub := hub.Subscribe(string(rune('a'+i)), nil)
		go func() {
			for range sub.Ch {
				// Drain
			}
		}()
	}

	entry := &LogEntry{
		Timestamp:   time.Now(),
		Level:       "info",
		EventID:     "evt-123",
		EventType:   "order.created",
		EndpointID:  "ep-456",
		Destination: "https://example.com/webhook",
		Status:      "delivered",
		StatusCode:  200,
		DurationMs:  150,
		Message:     "delivery successful",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hub.Publish(entry)
	}
}

func BenchmarkFilter_Matches(b *testing.B) {
	filter := &Filter{
		EventTypes:  []string{"order.created", "order.updated", "payment.completed"},
		EndpointIDs: []string{"ep-123", "ep-456", "ep-789"},
		Statuses:    []string{"delivered", "failed"},
		Level:       "info",
	}

	entry := &LogEntry{
		Level:      "info",
		EventType:  "order.created",
		EndpointID: "ep-456",
		Status:     "delivered",
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		filter.Matches(entry)
	}
}
