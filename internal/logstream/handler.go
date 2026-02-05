package logstream

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

// Handler handles log streaming HTTP requests.
type Handler struct {
	hub *Hub
}

// NewHandler creates a new log streaming handler.
func NewHandler(hub *Hub) *Handler {
	return &Handler{hub: hub}
}

// Router returns a chi router with log streaming routes.
func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()

	// SSE endpoint for streaming logs
	r.Get("/stream", h.StreamLogs)

	// Stats endpoint
	r.Get("/stats", h.Stats)

	return r
}

// StreamLogs streams delivery logs via Server-Sent Events.
// Query parameters for filtering:
// - event_type: comma-separated list of event types
// - endpoint_id: comma-separated list of endpoint IDs
// - status: comma-separated list of statuses (queued, delivering, delivered, failed, dead)
// - client_id: comma-separated list of client IDs
// - level: minimum log level (debug, info, warn, error)
func (h *Handler) StreamLogs(w http.ResponseWriter, r *http.Request) {
	// Parse filter from query parameters
	filter := parseFilterFromQuery(r)

	// Set SSE headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // Disable nginx buffering

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	// Generate subscriber ID
	subscriberID := uuid.New().String()

	// Subscribe to log entries
	sub := h.hub.Subscribe(subscriberID, filter)
	defer h.hub.Unsubscribe(subscriberID)

	// Send initial ping/connected message
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	log.Debug("log stream started", "subscriber_id", subscriberID)

	// Stream logs until client disconnects
	for {
		select {
		case <-r.Context().Done():
			log.Debug("log stream closed", "subscriber_id", subscriberID)
			return
		case entry, ok := <-sub.Ch:
			if !ok {
				return
			}
			data, err := json.Marshal(entry)
			if err != nil {
				continue
			}
			_, _ = w.Write([]byte("event: log\n"))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

// Stats returns streaming statistics.
func (h *Handler) Stats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"subscribers": h.hub.SubscriberCount(),
	})
}

// parseFilterFromQuery parses filter parameters from the request query string.
func parseFilterFromQuery(r *http.Request) *Filter {
	filter := &Filter{}

	if eventTypes := r.URL.Query().Get("event_type"); eventTypes != "" {
		filter.EventTypes = splitAndTrim(eventTypes)
	}

	if endpointIDs := r.URL.Query().Get("endpoint_id"); endpointIDs != "" {
		filter.EndpointIDs = splitAndTrim(endpointIDs)
	}

	if statuses := r.URL.Query().Get("status"); statuses != "" {
		filter.Statuses = splitAndTrim(statuses)
	}

	if clientIDs := r.URL.Query().Get("client_id"); clientIDs != "" {
		filter.ClientIDs = splitAndTrim(clientIDs)
	}

	if level := r.URL.Query().Get("level"); level != "" {
		filter.Level = strings.ToLower(strings.TrimSpace(level))
	}

	return filter
}

func splitAndTrim(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
