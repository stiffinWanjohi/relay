package debug

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// Handler handles debug endpoint HTTP requests.
type Handler struct {
	service *Service
}

// NewHandler creates a new debug handler.
func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

// Router returns a chi router with debug routes.
func (h *Handler) Router() chi.Router {
	r := chi.NewRouter()

	// Management endpoints
	r.Post("/endpoints", h.CreateEndpoint)
	r.Get("/endpoints/{endpointId}", h.GetEndpoint)
	r.Delete("/endpoints/{endpointId}", h.DeleteEndpoint)
	r.Get("/endpoints/{endpointId}/requests", h.ListRequests)
	r.Get("/endpoints/{endpointId}/requests/{requestId}", h.GetRequest)
	r.Post("/endpoints/{endpointId}/requests/{requestId}/replay", h.ReplayRequest)

	// SSE endpoint for real-time updates
	r.Get("/endpoints/{endpointId}/stream", h.StreamRequests)

	// Capture endpoint - accepts any method and path
	r.HandleFunc("/{endpointId}", h.CaptureRequest)
	r.HandleFunc("/{endpointId}/*", h.CaptureRequest)

	return r
}

// CreateEndpoint creates a new debug endpoint.
func (h *Handler) CreateEndpoint(w http.ResponseWriter, r *http.Request) {
	endpoint, err := h.service.CreateEndpoint(r.Context())
	if err != nil {
		log.Error("failed to create debug endpoint", "error", err)
		http.Error(w, "failed to create endpoint", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(endpoint)
}

// GetEndpoint retrieves a debug endpoint.
func (h *Handler) GetEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID := chi.URLParam(r, "endpointId")

	endpoint, err := h.service.GetEndpoint(r.Context(), endpointID)
	if err != nil {
		log.Error("failed to get debug endpoint", "error", err, "endpoint_id", endpointID)
		http.Error(w, "failed to get endpoint", http.StatusInternalServerError)
		return
	}
	if endpoint == nil {
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(endpoint)
}

// DeleteEndpoint deletes a debug endpoint.
func (h *Handler) DeleteEndpoint(w http.ResponseWriter, r *http.Request) {
	endpointID := chi.URLParam(r, "endpointId")

	if err := h.service.DeleteEndpoint(r.Context(), endpointID); err != nil {
		log.Error("failed to delete debug endpoint", "error", err, "endpoint_id", endpointID)
		http.Error(w, "failed to delete endpoint", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ListRequests lists captured requests for a debug endpoint.
func (h *Handler) ListRequests(w http.ResponseWriter, r *http.Request) {
	endpointID := chi.URLParam(r, "endpointId")

	requests, err := h.service.GetRequests(r.Context(), endpointID, 0)
	if err != nil {
		log.Error("failed to list requests", "error", err, "endpoint_id", endpointID)
		http.Error(w, "failed to list requests", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"requests": requests,
		"count":    len(requests),
	})
}

// GetRequest retrieves a specific captured request.
func (h *Handler) GetRequest(w http.ResponseWriter, r *http.Request) {
	endpointID := chi.URLParam(r, "endpointId")
	requestID := chi.URLParam(r, "requestId")

	req, err := h.service.GetRequest(r.Context(), endpointID, requestID)
	if err != nil {
		log.Error("failed to get request", "error", err, "endpoint_id", endpointID, "request_id", requestID)
		http.Error(w, "failed to get request", http.StatusInternalServerError)
		return
	}
	if req == nil {
		http.Error(w, "request not found", http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(req)
}

// ReplayRequest replays a captured request to a specified URL.
func (h *Handler) ReplayRequest(w http.ResponseWriter, r *http.Request) {
	endpointID := chi.URLParam(r, "endpointId")
	requestID := chi.URLParam(r, "requestId")

	// Get target URL from request body
	var input struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}
	if input.URL == "" {
		http.Error(w, "url is required", http.StatusBadRequest)
		return
	}

	// Get the original request
	captured, err := h.service.GetRequest(r.Context(), endpointID, requestID)
	if err != nil {
		log.Error("failed to get request for replay", "error", err)
		http.Error(w, "failed to get request", http.StatusInternalServerError)
		return
	}
	if captured == nil {
		http.Error(w, "request not found", http.StatusNotFound)
		return
	}

	// Replay the request
	client := &http.Client{Timeout: 30 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), captured.Method, input.URL, strings.NewReader(captured.Body))
	if err != nil {
		http.Error(w, "failed to create request", http.StatusInternalServerError)
		return
	}

	// Copy original headers
	for k, v := range captured.Headers {
		req.Header.Set(k, v)
	}

	start := time.Now()
	resp, err := client.Do(req)
	duration := time.Since(start)

	if err != nil {
		log.Warn("replay request failed", "error", err, "url", input.URL)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"success":     false,
			"error":       err.Error(),
			"duration_ms": duration.Milliseconds(),
		})
		return
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(resp.Body)

	log.Info("request replayed",
		"endpoint_id", endpointID,
		"request_id", requestID,
		"target_url", input.URL,
		"status", resp.StatusCode,
		"duration_ms", duration.Milliseconds(),
	)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"success":     resp.StatusCode >= 200 && resp.StatusCode < 300,
		"status_code": resp.StatusCode,
		"duration_ms": duration.Milliseconds(),
		"response":    string(body),
	})
}

// StreamRequests streams captured requests via Server-Sent Events.
func (h *Handler) StreamRequests(w http.ResponseWriter, r *http.Request) {
	endpointID := chi.URLParam(r, "endpointId")

	// Verify endpoint exists
	endpoint, err := h.service.GetEndpoint(r.Context(), endpointID)
	if err != nil || endpoint == nil {
		http.Error(w, "endpoint not found", http.StatusNotFound)
		return
	}

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

	// Subscribe to new requests
	ch := h.service.Subscribe(endpointID)
	defer h.service.Unsubscribe(endpointID, ch)

	// Send initial ping
	_, _ = w.Write([]byte(": connected\n\n"))
	flusher.Flush()

	log.Debug("SSE stream started", "endpoint_id", endpointID)

	// Stream requests until client disconnects
	for {
		select {
		case <-r.Context().Done():
			log.Debug("SSE stream closed", "endpoint_id", endpointID)
			return
		case req, ok := <-ch:
			if !ok {
				return
			}
			data, _ := json.Marshal(req)
			_, _ = w.Write([]byte("event: request\n"))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(data)
			_, _ = w.Write([]byte("\n\n"))
			flusher.Flush()
		}
	}
}

// CaptureRequest captures an incoming webhook request.
func (h *Handler) CaptureRequest(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	endpointID := chi.URLParam(r, "endpointId")

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	// Extract headers
	headers := make(map[string]string)
	for k, v := range r.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	// Get the full path after the endpoint ID
	path := chi.URLParam(r, "*")
	if path == "" {
		path = "/"
	} else {
		path = "/" + path
	}

	captured := &CapturedRequest{
		ReceivedAt:  start,
		Method:      r.Method,
		Path:        path,
		Headers:     headers,
		Body:        string(body),
		ContentType: r.Header.Get("Content-Type"),
		RemoteAddr:  r.RemoteAddr,
		Duration:    time.Since(start),
	}

	if err := h.service.CaptureRequest(r.Context(), endpointID, captured); err != nil {
		log.Error("failed to capture request", "error", err, "endpoint_id", endpointID)
		http.Error(w, "failed to capture request", http.StatusInternalServerError)
		return
	}

	// Return success response
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"message":    "request captured",
		"request_id": captured.ID,
	})
}
