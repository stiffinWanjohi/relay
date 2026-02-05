// Package logstream provides real-time delivery log streaming.
package logstream

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/stiffinWanjohi/relay/internal/logging"
)

var log = logging.Component("logstream")

// LogEntry represents a delivery log entry.
type LogEntry struct {
	Timestamp   time.Time         `json:"timestamp"`
	Level       string            `json:"level"`
	EventID     string            `json:"event_id"`
	EventType   string            `json:"event_type,omitempty"`
	EndpointID  string            `json:"endpoint_id,omitempty"`
	Destination string            `json:"destination,omitempty"`
	Status      string            `json:"status"`
	StatusCode  int               `json:"status_code,omitempty"`
	DurationMs  int64             `json:"duration_ms,omitempty"`
	Error       string            `json:"error,omitempty"`
	Attempt     int               `json:"attempt,omitempty"`
	MaxAttempts int               `json:"max_attempts,omitempty"`
	ClientID    string            `json:"client_id,omitempty"`
	Message     string            `json:"message"`
	Fields      map[string]string `json:"fields,omitempty"`
}

// Filter defines criteria for filtering log entries.
type Filter struct {
	EventTypes  []string `json:"event_types,omitempty"`
	EndpointIDs []string `json:"endpoint_ids,omitempty"`
	Statuses    []string `json:"statuses,omitempty"`
	ClientIDs   []string `json:"client_ids,omitempty"`
	Level       string   `json:"level,omitempty"` // debug, info, warn, error
}

// Matches returns true if the entry matches the filter.
func (f *Filter) Matches(entry *LogEntry) bool {
	if f == nil {
		return true
	}

	// Check level filter
	if f.Level != "" && !matchesLevel(entry.Level, f.Level) {
		return false
	}

	// Check event type filter
	if len(f.EventTypes) > 0 && !contains(f.EventTypes, entry.EventType) {
		return false
	}

	// Check endpoint filter
	if len(f.EndpointIDs) > 0 && !contains(f.EndpointIDs, entry.EndpointID) {
		return false
	}

	// Check status filter
	if len(f.Statuses) > 0 && !contains(f.Statuses, entry.Status) {
		return false
	}

	// Check client filter
	if len(f.ClientIDs) > 0 && !contains(f.ClientIDs, entry.ClientID) {
		return false
	}

	return true
}

// matchesLevel returns true if entry level >= filter level.
func matchesLevel(entryLevel, filterLevel string) bool {
	levels := map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}
	entryLvl, ok1 := levels[entryLevel]
	filterLvl, ok2 := levels[filterLevel]
	if !ok1 || !ok2 {
		return true
	}
	return entryLvl >= filterLvl
}

func contains(slice []string, s string) bool {
	return slices.Contains(slice, s)
}

// Subscriber represents a log stream subscriber.
type Subscriber struct {
	ID     string
	Filter *Filter
	Ch     chan *LogEntry
}

// Hub manages log streaming subscriptions.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[string]*Subscriber
	bufferSize  int
	rateLimit   int // max entries per second per subscriber
}

// NewHub creates a new log streaming hub.
func NewHub(bufferSize, rateLimit int) *Hub {
	if bufferSize <= 0 {
		bufferSize = 100
	}
	if rateLimit <= 0 {
		rateLimit = 100 // default 100 entries/sec
	}
	return &Hub{
		subscribers: make(map[string]*Subscriber),
		bufferSize:  bufferSize,
		rateLimit:   rateLimit,
	}
}

// Subscribe creates a new subscription with the given filter.
func (h *Hub) Subscribe(id string, filter *Filter) *Subscriber {
	h.mu.Lock()
	defer h.mu.Unlock()

	sub := &Subscriber{
		ID:     id,
		Filter: filter,
		Ch:     make(chan *LogEntry, h.bufferSize),
	}
	h.subscribers[id] = sub

	log.Debug("subscriber added", "subscriber_id", id, "total", len(h.subscribers))
	return sub
}

// Unsubscribe removes a subscription.
func (h *Hub) Unsubscribe(id string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if sub, ok := h.subscribers[id]; ok {
		close(sub.Ch)
		delete(h.subscribers, id)
		log.Debug("subscriber removed", "subscriber_id", id, "total", len(h.subscribers))
	}
}

// Publish sends a log entry to all matching subscribers.
func (h *Hub) Publish(entry *LogEntry) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, sub := range h.subscribers {
		if sub.Filter.Matches(entry) {
			select {
			case sub.Ch <- entry:
			default:
				// Channel full, drop entry to prevent blocking
				log.Debug("dropping log entry for slow subscriber", "subscriber_id", sub.ID)
			}
		}
	}
}

// SubscriberCount returns the number of active subscribers.
func (h *Hub) SubscriberCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.subscribers)
}

// Publisher is an interface for components that publish log entries.
type Publisher interface {
	Publish(entry *LogEntry)
}

// DeliveryLogger wraps the hub and provides convenience methods for logging deliveries.
type DeliveryLogger struct {
	hub *Hub
}

// NewDeliveryLogger creates a new delivery logger.
func NewDeliveryLogger(hub *Hub) *DeliveryLogger {
	return &DeliveryLogger{hub: hub}
}

// LogDeliveryStart logs the start of a delivery attempt.
func (l *DeliveryLogger) LogDeliveryStart(ctx context.Context, eventID, eventType, endpointID, destination, clientID string, attempt, maxAttempts int) {
	if l.hub == nil {
		return
	}

	l.hub.Publish(&LogEntry{
		Timestamp:   time.Now(),
		Level:       "info",
		EventID:     eventID,
		EventType:   eventType,
		EndpointID:  endpointID,
		Destination: destination,
		ClientID:    clientID,
		Status:      "delivering",
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		Message:     "starting delivery attempt",
	})
}

// LogDeliverySuccess logs a successful delivery.
func (l *DeliveryLogger) LogDeliverySuccess(ctx context.Context, eventID, eventType, endpointID, destination, clientID string, statusCode int, durationMs int64, attempt, maxAttempts int) {
	if l.hub == nil {
		return
	}

	l.hub.Publish(&LogEntry{
		Timestamp:   time.Now(),
		Level:       "info",
		EventID:     eventID,
		EventType:   eventType,
		EndpointID:  endpointID,
		Destination: destination,
		ClientID:    clientID,
		Status:      "delivered",
		StatusCode:  statusCode,
		DurationMs:  durationMs,
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		Message:     "delivery successful",
	})
}

// LogDeliveryFailure logs a failed delivery attempt.
func (l *DeliveryLogger) LogDeliveryFailure(ctx context.Context, eventID, eventType, endpointID, destination, clientID string, statusCode int, durationMs int64, errMsg string, attempt, maxAttempts int, willRetry bool) {
	if l.hub == nil {
		return
	}

	status := "failed"
	message := "delivery failed, will retry"
	level := "warn"
	if !willRetry {
		status = "dead"
		message = "delivery failed, max attempts reached"
		level = "error"
	}

	l.hub.Publish(&LogEntry{
		Timestamp:   time.Now(),
		Level:       level,
		EventID:     eventID,
		EventType:   eventType,
		EndpointID:  endpointID,
		Destination: destination,
		ClientID:    clientID,
		Status:      status,
		StatusCode:  statusCode,
		DurationMs:  durationMs,
		Error:       errMsg,
		Attempt:     attempt,
		MaxAttempts: maxAttempts,
		Message:     message,
	})
}

// LogCircuitTripped logs when a circuit breaker trips.
func (l *DeliveryLogger) LogCircuitTripped(ctx context.Context, endpointID, destination, clientID string, failureCount int) {
	if l.hub == nil {
		return
	}

	l.hub.Publish(&LogEntry{
		Timestamp:   time.Now(),
		Level:       "warn",
		EndpointID:  endpointID,
		Destination: destination,
		ClientID:    clientID,
		Status:      "circuit_open",
		Message:     "circuit breaker tripped",
		Fields: map[string]string{
			"failure_count": string(rune(failureCount)),
		},
	})
}

// LogCircuitRecovered logs when a circuit breaker recovers.
func (l *DeliveryLogger) LogCircuitRecovered(ctx context.Context, endpointID, destination, clientID string) {
	if l.hub == nil {
		return
	}

	l.hub.Publish(&LogEntry{
		Timestamp:   time.Now(),
		Level:       "info",
		EndpointID:  endpointID,
		Destination: destination,
		ClientID:    clientID,
		Status:      "circuit_closed",
		Message:     "circuit breaker recovered",
	})
}
