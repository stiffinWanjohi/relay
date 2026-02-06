package logstream

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandler_Stats(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	// Add some subscribers
	hub.Subscribe("sub-1", nil)
	hub.Subscribe("sub-2", nil)

	req := httptest.NewRequest(http.MethodGet, "/stats", nil)
	w := httptest.NewRecorder()

	handler.Stats(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "application/json", w.Header().Get("Content-Type"))

	var result map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)

	assert.Equal(t, float64(2), result["subscribers"])

	hub.Unsubscribe("sub-1")
	hub.Unsubscribe("sub-2")
}

func TestHandler_Router(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	router := handler.Router()
	assert.NotNil(t, router)

	// Test that routes are registered
	r := chi.NewRouter()
	r.Mount("/logs", router)

	// Test stats endpoint
	req := httptest.NewRequest(http.MethodGet, "/logs/stats", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestParseFilterFromQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected *Filter
	}{
		{
			name:     "empty query",
			query:    "",
			expected: &Filter{},
		},
		{
			name:  "event_type filter",
			query: "event_type=order.created,payment.completed",
			expected: &Filter{
				EventTypes: []string{"order.created", "payment.completed"},
			},
		},
		{
			name:  "endpoint_id filter",
			query: "endpoint_id=ep-123,ep-456",
			expected: &Filter{
				EndpointIDs: []string{"ep-123", "ep-456"},
			},
		},
		{
			name:  "status filter",
			query: "status=failed,dead",
			expected: &Filter{
				Statuses: []string{"failed", "dead"},
			},
		},
		{
			name:  "client_id filter",
			query: "client_id=client-a,client-b",
			expected: &Filter{
				ClientIDs: []string{"client-a", "client-b"},
			},
		},
		{
			name:  "level filter",
			query: "level=WARN",
			expected: &Filter{
				Level: "warn",
			},
		},
		{
			name:  "combined filters",
			query: "event_type=order.created&status=failed&level=error",
			expected: &Filter{
				EventTypes: []string{"order.created"},
				Statuses:   []string{"failed"},
				Level:      "error",
			},
		},
		{
			name:  "whitespace handling",
			query: "event_type=+order.created+,+payment.completed+",
			expected: &Filter{
				EventTypes: []string{"order.created", "payment.completed"},
			},
		},
		{
			name:  "empty values ignored",
			query: "event_type=order.created,,payment.completed",
			expected: &Filter{
				EventTypes: []string{"order.created", "payment.completed"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/stream?"+tt.query, nil)
			filter := parseFilterFromQuery(req)

			assert.Equal(t, tt.expected.EventTypes, filter.EventTypes)
			assert.Equal(t, tt.expected.EndpointIDs, filter.EndpointIDs)
			assert.Equal(t, tt.expected.Statuses, filter.Statuses)
			assert.Equal(t, tt.expected.ClientIDs, filter.ClientIDs)
			assert.Equal(t, tt.expected.Level, filter.Level)
		})
	}
}

func TestSplitAndTrim(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{"a,b,c", []string{"a", "b", "c"}},
		{" a , b , c ", []string{"a", "b", "c"}},
		{"a,,b", []string{"a", "b"}},
		{"", []string{}},
		{"  ", []string{}},
		{"single", []string{"single"}},
		{",,,", []string{}},
	}

	for _, tt := range tests {
		result := splitAndTrim(tt.input)
		assert.Equal(t, tt.expected, result, "input: %q", tt.input)
	}
}

func TestHandler_StreamLogs_SSEHeaders(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	// Create a context that we can cancel
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Run in goroutine since it blocks
	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	// Give it time to set headers and write initial message
	time.Sleep(50 * time.Millisecond)

	// Cancel to stop the stream
	cancel()
	<-done

	// Check SSE headers
	assert.Equal(t, "text/event-stream", w.Header().Get("Content-Type"))
	assert.Equal(t, "no-cache", w.Header().Get("Cache-Control"))
	assert.Equal(t, "keep-alive", w.Header().Get("Connection"))
	assert.Equal(t, "no", w.Header().Get("X-Accel-Buffering"))

	// Check initial comment
	assert.Contains(t, w.Body.String(), ": connected")
}

func TestHandler_StreamLogs_ReceivesEntries(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Start streaming in background
	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	// Wait for connection
	time.Sleep(50 * time.Millisecond)

	// Publish an entry
	entry := &LogEntry{
		Timestamp:  time.Now(),
		Level:      "info",
		EventID:    "evt-test-123",
		EventType:  "order.created",
		Status:     "delivered",
		StatusCode: 200,
		DurationMs: 150,
		Message:    "test delivery",
	}
	hub.Publish(entry)

	// Wait for entry to be sent
	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()

	// Should contain the event
	assert.Contains(t, body, "event: log")
	assert.Contains(t, body, "evt-test-123")
	assert.Contains(t, body, "order.created")
	assert.Contains(t, body, "delivered")
}

func TestHandler_StreamLogs_WithFilter(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Request with filter - only failed status
	req := httptest.NewRequest(http.MethodGet, "/stream?status=failed", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish matching entry
	hub.Publish(&LogEntry{
		EventID: "evt-failed",
		Status:  "failed",
		Message: "should receive",
	})

	// Publish non-matching entry
	hub.Publish(&LogEntry{
		EventID: "evt-delivered",
		Status:  "delivered",
		Message: "should not receive",
	})

	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()

	// Should contain failed entry
	assert.Contains(t, body, "evt-failed")
	assert.Contains(t, body, "should receive")

	// Should NOT contain delivered entry
	assert.NotContains(t, body, "evt-delivered")
	assert.NotContains(t, body, "should not receive")
}

func TestHandler_StreamLogs_MultipleFilters(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Request with multiple filters
	req := httptest.NewRequest(http.MethodGet, "/stream?event_type=order.created&status=failed&level=warn", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish matching entry
	hub.Publish(&LogEntry{
		EventID:   "evt-match",
		EventType: "order.created",
		Status:    "failed",
		Level:     "warn",
		Message:   "matches all filters",
	})

	// Publish non-matching entry (wrong event type)
	hub.Publish(&LogEntry{
		EventID:   "evt-wrong-type",
		EventType: "payment.completed",
		Status:    "failed",
		Level:     "warn",
	})

	// Publish non-matching entry (wrong status)
	hub.Publish(&LogEntry{
		EventID:   "evt-wrong-status",
		EventType: "order.created",
		Status:    "delivered",
		Level:     "warn",
	})

	// Publish non-matching entry (level too low)
	hub.Publish(&LogEntry{
		EventID:   "evt-wrong-level",
		EventType: "order.created",
		Status:    "failed",
		Level:     "debug",
	})

	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()

	assert.Contains(t, body, "evt-match")
	assert.NotContains(t, body, "evt-wrong-type")
	assert.NotContains(t, body, "evt-wrong-status")
	assert.NotContains(t, body, "evt-wrong-level")
}

func TestHandler_StreamLogs_SSEFormat(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish entry
	hub.Publish(&LogEntry{
		Timestamp: time.Date(2026, 2, 5, 14, 30, 0, 0, time.UTC),
		EventID:   "evt-sse-test",
		Level:     "info",
		Status:    "delivered",
		Message:   "test message",
	})

	time.Sleep(50 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()

	// Parse SSE format
	lines := strings.Split(body, "\n")
	foundEvent := false
	foundData := false

	for i, line := range lines {
		if line == "event: log" {
			foundEvent = true
			// Next line should be data
			if i+1 < len(lines) && strings.HasPrefix(lines[i+1], "data: ") {
				foundData = true
				data := strings.TrimPrefix(lines[i+1], "data: ")

				// Parse JSON
				var entry LogEntry
				err := json.Unmarshal([]byte(data), &entry)
				require.NoError(t, err)

				assert.Equal(t, "evt-sse-test", entry.EventID)
				assert.Equal(t, "info", entry.Level)
				assert.Equal(t, "delivered", entry.Status)
			}
		}
	}

	assert.True(t, foundEvent, "should find 'event: log' line")
	assert.True(t, foundData, "should find 'data:' line with valid JSON")
}

func TestHandler_StreamLogs_CleanupOnDisconnect(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	ctx, cancel := context.WithCancel(context.Background())

	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	// Initial subscriber count
	assert.Equal(t, 0, hub.SubscriberCount())

	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	// Wait for subscription
	time.Sleep(50 * time.Millisecond)
	assert.Equal(t, 1, hub.SubscriberCount())

	// Disconnect
	cancel()
	<-done

	// Subscriber should be cleaned up
	assert.Equal(t, 0, hub.SubscriberCount())
}

func TestHandler_Integration(t *testing.T) {
	hub := NewHub(100, 100)
	handler := NewHandler(hub)
	logger := NewDeliveryLogger(hub)

	r := chi.NewRouter()
	r.Mount("/logs", handler.Router())

	// Start SSE stream
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/logs/stream?status=delivered,failed", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		r.ServeHTTP(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Use DeliveryLogger to publish entries (simulating worker)
	bgCtx := context.Background()

	logger.LogDeliverySuccess(bgCtx, "evt-1", "order.created", "ep-1", "https://example.com", "client-1", 200, 150, 1, 10)
	logger.LogDeliveryFailure(bgCtx, "evt-2", "payment.failed", "ep-2", "https://example.com", "client-1", 500, 250, "server error", 3, 10, true)
	logger.LogDeliveryStart(bgCtx, "evt-3", "user.signup", "ep-3", "https://example.com", "client-1", 1, 10) // Should be filtered out (status=delivering)

	time.Sleep(100 * time.Millisecond)

	cancel()
	<-done

	body := w.Body.String()

	// Should receive delivered and failed
	assert.Contains(t, body, "evt-1")
	assert.Contains(t, body, "evt-2")

	// Should NOT receive delivering (not in filter)
	assert.NotContains(t, body, "evt-3")
}

// Test for proper handling of rapid publishing
func TestHandler_StreamLogs_RapidPublish(t *testing.T) {
	hub := NewHub(50, 1000)
	handler := NewHandler(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Rapidly publish many entries
	for i := 0; i < 100; i++ {
		hub.Publish(&LogEntry{
			EventID: string(rune('A' + (i % 26))),
			Status:  "delivered",
		})
	}

	time.Sleep(100 * time.Millisecond)

	cancel()
	<-done

	// Should have received some entries (may drop some if buffer fills)
	body := w.Body.String()
	assert.Contains(t, body, "event: log")
}

// Benchmark for stream handling
func BenchmarkHandler_StreamLogs(b *testing.B) {
	hub := NewHub(1000, 10000)
	handler := NewHandler(hub)

	for i := 0; i < b.N; i++ {
		ctx, cancel := context.WithCancel(context.Background())

		req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
		w := httptest.NewRecorder()

		done := make(chan struct{})
		go func() {
			handler.StreamLogs(w, req)
			close(done)
		}()

		// Publish some entries
		for j := 0; j < 10; j++ {
			hub.Publish(&LogEntry{
				EventID: "bench-test",
				Status:  "delivered",
			})
		}

		cancel()
		<-done
	}
}

// Test that reader can parse SSE stream
func TestHandler_StreamLogs_ReaderParsing(t *testing.T) {
	hub := NewHub(10, 100)
	handler := NewHandler(hub)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	req := httptest.NewRequest(http.MethodGet, "/stream", nil).WithContext(ctx)
	w := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler.StreamLogs(w, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)

	// Publish entries
	entries := []LogEntry{
		{EventID: "evt-1", Status: "delivered", Level: "info"},
		{EventID: "evt-2", Status: "failed", Level: "warn"},
		{EventID: "evt-3", Status: "dead", Level: "error"},
	}
	for _, e := range entries {
		entry := e // capture
		hub.Publish(&entry)
	}

	time.Sleep(100 * time.Millisecond)

	cancel()
	<-done

	// Parse the SSE stream like a client would
	body := w.Body.String()
	reader := bufio.NewReader(strings.NewReader(body))

	var receivedEvents []LogEntry
	var dataBuffer strings.Builder

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			break
		}
		line = strings.TrimSuffix(line, "\n")

		if strings.HasPrefix(line, "data: ") {
			dataBuffer.WriteString(strings.TrimPrefix(line, "data: "))
		} else if line == "" && dataBuffer.Len() > 0 {
			var entry LogEntry
			if err := json.Unmarshal([]byte(dataBuffer.String()), &entry); err == nil {
				receivedEvents = append(receivedEvents, entry)
			}
			dataBuffer.Reset()
		}
	}

	// Should have received all 3 entries
	require.Len(t, receivedEvents, 3)

	eventIDs := make([]string, len(receivedEvents))
	for i, e := range receivedEvents {
		eventIDs[i] = e.EventID
	}

	assert.Contains(t, eventIDs, "evt-1")
	assert.Contains(t, eventIDs, "evt-2")
	assert.Contains(t, eventIDs, "evt-3")
}
