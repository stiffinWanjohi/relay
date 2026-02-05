package rest

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/alerting"
	"github.com/stiffinWanjohi/relay/internal/auth"
	"github.com/stiffinWanjohi/relay/internal/config"
	"github.com/stiffinWanjohi/relay/internal/connector"
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/eventtype"
	"github.com/stiffinWanjohi/relay/internal/metrics"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

// Handler provides REST API handlers.
type Handler struct {
	store             *event.Store
	eventTypeStore    *eventtype.Store
	queue             *queue.Queue
	dedup             *dedup.Checker
	alertEngine       *alerting.Engine
	alertingStore     *alerting.Store
	connectorRegistry *connector.Registry
	connectorStore    *connector.Store
	metricsStore      *metrics.Store
}

// NewHandler creates a new REST API handler.
func NewHandler(store *event.Store, eventTypeStore *eventtype.Store, q *queue.Queue, d *dedup.Checker) *Handler {
	return &Handler{
		store:          store,
		eventTypeStore: eventTypeStore,
		queue:          q,
		dedup:          d,
	}
}

// WithAlertEngine sets the alerting engine for the handler.
func (h *Handler) WithAlertEngine(engine *alerting.Engine) *Handler {
	h.alertEngine = engine
	return h
}

// WithAlertingStore sets the alerting store for the handler.
func (h *Handler) WithAlertingStore(store *alerting.Store) *Handler {
	h.alertingStore = store
	return h
}

// WithConnectorRegistry sets the connector registry for the handler.
func (h *Handler) WithConnectorRegistry(registry *connector.Registry) *Handler {
	h.connectorRegistry = registry
	return h
}

// WithConnectorStore sets the connector store for the handler.
func (h *Handler) WithConnectorStore(store *connector.Store) *Handler {
	h.connectorStore = store
	return h
}

// WithMetricsStore sets the metrics store for the handler.
func (h *Handler) WithMetricsStore(store *metrics.Store) *Handler {
	h.metricsStore = store
	return h
}

// Response helpers

func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func respondError(w http.ResponseWriter, status int, message, code string) {
	respondJSON(w, status, map[string]string{
		"error": message,
		"code":  code,
	})
}

// CreateEventRequest represents the request body for creating an event.
type CreateEventRequest struct {
	Destination string            `json:"destination,omitempty"`
	EventType   string            `json:"eventType,omitempty"`
	Payload     json.RawMessage   `json:"payload"`
	Headers     map[string]string `json:"headers,omitempty"`
	MaxAttempts *int              `json:"maxAttempts,omitempty"`
}

// CreateEvent handles POST /api/v1/events
func (h *Handler) CreateEvent(w http.ResponseWriter, r *http.Request) {
	idempotencyKey := r.Header.Get("X-Idempotency-Key")
	if idempotencyKey == "" {
		respondError(w, http.StatusBadRequest, "X-Idempotency-Key header is required", "MISSING_IDEMPOTENCY_KEY")
		return
	}

	var req CreateEventRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	if req.Destination == "" && req.EventType == "" {
		respondError(w, http.StatusBadRequest, "Either destination or eventType is required", "VALIDATION_ERROR")
		return
	}

	if len(req.Payload) == 0 {
		respondError(w, http.StatusBadRequest, "Payload is required", "VALIDATION_ERROR")
		return
	}

	// Create new event
	evt := domain.NewEvent(idempotencyKey, req.Destination, req.Payload, req.Headers)
	if req.MaxAttempts != nil && *req.MaxAttempts > 0 {
		evt.MaxAttempts = *req.MaxAttempts
	}

	// Check idempotency
	existingID, err := h.dedup.CheckAndSet(r.Context(), idempotencyKey, evt.ID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
		return
	}

	if existingID != uuid.Nil {
		// Return existing event
		existing, err := h.store.GetByID(r.Context(), existingID)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "Internal server error", "INTERNAL_ERROR")
			return
		}
		respondJSON(w, http.StatusConflict, eventToResponse(existing))
		return
	}

	created, err := h.store.Create(r.Context(), evt)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to create event", "INTERNAL_ERROR")
		return
	}

	if err := h.queue.Enqueue(r.Context(), created.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to queue event", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusCreated, eventToResponse(created))
}

// GetEvent handles GET /api/v1/events/{eventId}
func (h *Handler) GetEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "eventId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid event ID", "BAD_REQUEST")
		return
	}

	evt, err := h.store.GetByID(r.Context(), eventID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Event not found", "NOT_FOUND")
		return
	}

	attempts, err := h.store.GetDeliveryAttempts(r.Context(), eventID)
	if err != nil {
		attempts = nil // Continue without attempts
	}

	respondJSON(w, http.StatusOK, eventWithAttemptsToResponse(evt, attempts))
}

// ListEvents handles GET /api/v1/events
func (h *Handler) ListEvents(w http.ResponseWriter, r *http.Request) {
	status := r.URL.Query().Get("status")
	limitStr := r.URL.Query().Get("limit")

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	var events []domain.Event
	var err error

	if status != "" {
		events, err = h.store.ListByStatus(r.Context(), domain.EventStatus(status), limit, 0)
	} else {
		events, err = h.store.ListByStatus(r.Context(), domain.EventStatusQueued, limit, 0)
	}

	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list events", "INTERNAL_ERROR")
		return
	}

	response := make([]map[string]any, len(events))
	for i, evt := range events {
		response[i] = eventToResponse(evt)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": response,
		"pagination": map[string]any{
			"hasMore": len(events) == limit,
			"total":   len(events),
		},
	})
}

// ReplayEvent handles POST /api/v1/events/{eventId}/replay
func (h *Handler) ReplayEvent(w http.ResponseWriter, r *http.Request) {
	eventID, err := uuid.Parse(chi.URLParam(r, "eventId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid event ID", "BAD_REQUEST")
		return
	}

	evt, err := h.store.GetByID(r.Context(), eventID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Event not found", "NOT_FOUND")
		return
	}

	if evt.Status != domain.EventStatusFailed && evt.Status != domain.EventStatusDead {
		respondError(w, http.StatusConflict, "Only failed or dead events can be replayed", "INVALID_STATE")
		return
	}

	evt = evt.Replay()

	updated, err := h.store.Update(r.Context(), evt)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update event", "INTERNAL_ERROR")
		return
	}

	if err := h.queue.Enqueue(r.Context(), updated.ID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to queue event", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, eventToResponse(updated))
}

// GetStats handles GET /api/v1/stats
func (h *Handler) GetStats(w http.ResponseWriter, r *http.Request) {
	stats, err := h.queue.Stats(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get stats", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, stats)
}

// Helper functions

func eventToResponse(evt domain.Event) map[string]any {
	resp := map[string]any{
		"id":             evt.ID.String(),
		"idempotencyKey": evt.IdempotencyKey,
		"destination":    evt.Destination,
		"status":         string(evt.Status),
		"attempts":       evt.Attempts,
		"maxAttempts":    evt.MaxAttempts,
		"createdAt":      evt.CreatedAt,
	}

	if evt.DeliveredAt != nil {
		resp["deliveredAt"] = evt.DeliveredAt
	}
	if evt.NextAttemptAt != nil {
		resp["nextAttemptAt"] = evt.NextAttemptAt
	}

	return resp
}

func eventWithAttemptsToResponse(evt domain.Event, attempts []domain.DeliveryAttempt) map[string]any {
	resp := eventToResponse(evt)
	resp["payload"] = evt.Payload
	resp["headers"] = evt.Headers

	attemptsList := make([]map[string]any, len(attempts))
	for i, a := range attempts {
		attemptsList[i] = map[string]any{
			"id":            a.ID.String(),
			"attemptNumber": a.AttemptNumber,
			"statusCode":    a.StatusCode,
			"responseBody":  a.ResponseBody,
			"error":         a.Error,
			"durationMs":    a.DurationMs,
			"attemptedAt":   a.AttemptedAt,
		}
	}
	resp["deliveryAttempts"] = attemptsList

	return resp
}

// BatchRetryRequest represents the request body for batch retry.
type BatchRetryRequest struct {
	EventIDs   []string `json:"event_ids,omitempty"`
	Status     string   `json:"status,omitempty"`
	EndpointID string   `json:"endpoint_id,omitempty"`
	Limit      int      `json:"limit,omitempty"`
}

// BatchRetry handles POST /api/v1/events/batch/retry
func (h *Handler) BatchRetry(w http.ResponseWriter, r *http.Request) {
	var req BatchRetryRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Set default limit
	limit := req.Limit
	if limit <= 0 || limit > 1000 {
		limit = 100
	}

	var result *event.BatchRetryResult
	var err error

	switch {
	case len(req.EventIDs) > 0:
		// Retry by IDs
		ids := make([]uuid.UUID, 0, len(req.EventIDs))
		for _, idStr := range req.EventIDs {
			id, parseErr := uuid.Parse(idStr)
			if parseErr != nil {
				respondError(w, http.StatusBadRequest, "Invalid event ID: "+idStr, "BAD_REQUEST")
				return
			}
			ids = append(ids, id)
		}
		result, err = h.store.RetryEventsByIDs(r.Context(), ids)

	case req.EndpointID != "":
		// Retry by endpoint
		endpointID, parseErr := uuid.Parse(req.EndpointID)
		if parseErr != nil {
			respondError(w, http.StatusBadRequest, "Invalid endpoint ID", "BAD_REQUEST")
			return
		}
		status := domain.EventStatusFailed
		if req.Status == "dead" {
			status = domain.EventStatusDead
		}
		result, err = h.store.RetryEventsByEndpoint(r.Context(), endpointID, status, limit)

	case req.Status != "":
		// Retry by status
		status := domain.EventStatus(req.Status)
		if status != domain.EventStatusFailed && status != domain.EventStatusDead {
			respondError(w, http.StatusBadRequest, "Only 'failed' or 'dead' status can be retried", "VALIDATION_ERROR")
			return
		}
		result, err = h.store.RetryEventsByStatus(r.Context(), status, limit)

	default:
		respondError(w, http.StatusBadRequest, "Must provide event_ids, status, or endpoint_id", "VALIDATION_ERROR")
		return
	}

	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error(), "INTERNAL_ERROR")
		return
	}

	// Enqueue all successfully retried events
	for _, evt := range result.Succeeded {
		if qErr := h.queue.Enqueue(r.Context(), evt.ID); qErr != nil {
			// Log but don't fail - events are already reset for retry
			continue
		}
	}

	// Build response
	succeededList := make([]map[string]any, len(result.Succeeded))
	for i, evt := range result.Succeeded {
		succeededList[i] = eventToResponse(evt)
	}

	failedList := make([]map[string]any, len(result.Failed))
	for i, f := range result.Failed {
		failedList[i] = map[string]any{
			"eventId": f.EventID.String(),
			"error":   f.Error,
		}
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"succeeded":      succeededList,
		"failed":         failedList,
		"totalRequested": len(result.Succeeded) + len(result.Failed),
		"totalSucceeded": len(result.Succeeded),
	})
}

// ============================================================================
// Event Type Handlers
// ============================================================================

// CreateEventTypeRequest represents the request body for creating an event type.
type CreateEventTypeRequest struct {
	Name          string         `json:"name"`
	Description   string         `json:"description,omitempty"`
	Schema        map[string]any `json:"schema,omitempty"`
	SchemaVersion string         `json:"schemaVersion,omitempty"`
}

// UpdateEventTypeRequest represents the request body for updating an event type.
type UpdateEventTypeRequest struct {
	Description   *string        `json:"description,omitempty"`
	Schema        map[string]any `json:"schema,omitempty"`
	SchemaVersion *string        `json:"schemaVersion,omitempty"`
}

// CreateEventType handles POST /api/v1/event-types
func (h *Handler) CreateEventType(w http.ResponseWriter, r *http.Request) {
	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	var req CreateEventTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	if req.Name == "" {
		respondError(w, http.StatusBadRequest, "Event type name is required", "VALIDATION_ERROR")
		return
	}

	// Validate schema if provided
	var schemaBytes []byte
	if req.Schema != nil {
		schemaBytes, err = json.Marshal(req.Schema)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid schema format", "VALIDATION_ERROR")
			return
		}
		if err := config.ValidateJSONSchema(schemaBytes); err != nil {
			respondError(w, http.StatusBadRequest, err.Error(), "INVALID_SCHEMA")
			return
		}
	}

	et := domain.NewEventType(clientID, req.Name, req.Description)
	if len(schemaBytes) > 0 {
		et = et.WithSchema(schemaBytes, req.SchemaVersion)
	}

	created, err := h.eventTypeStore.Create(r.Context(), et)
	if err != nil {
		if err == domain.ErrDuplicateEventType {
			respondError(w, http.StatusConflict, "Event type already exists", "DUPLICATE")
			return
		}
		respondError(w, http.StatusInternalServerError, "Failed to create event type", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusCreated, eventTypeToResponse(created))
}

// GetEventType handles GET /api/v1/event-types/{eventTypeId}
func (h *Handler) GetEventType(w http.ResponseWriter, r *http.Request) {
	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	eventTypeID, err := uuid.Parse(chi.URLParam(r, "eventTypeId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid event type ID", "BAD_REQUEST")
		return
	}

	et, err := h.eventTypeStore.GetByID(r.Context(), eventTypeID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Event type not found", "NOT_FOUND")
		return
	}

	if et.ClientID != clientID {
		respondError(w, http.StatusNotFound, "Event type not found", "NOT_FOUND")
		return
	}

	respondJSON(w, http.StatusOK, eventTypeToResponse(et))
}

// GetEventTypeByName handles GET /api/v1/event-types/by-name/{name}
func (h *Handler) GetEventTypeByName(w http.ResponseWriter, r *http.Request) {
	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	name := chi.URLParam(r, "name")
	if name == "" {
		respondError(w, http.StatusBadRequest, "Event type name is required", "BAD_REQUEST")
		return
	}

	et, err := h.eventTypeStore.GetByName(r.Context(), clientID, name)
	if err != nil {
		respondError(w, http.StatusNotFound, "Event type not found", "NOT_FOUND")
		return
	}

	respondJSON(w, http.StatusOK, eventTypeToResponse(et))
}

// ListEventTypes handles GET /api/v1/event-types
func (h *Handler) ListEventTypes(w http.ResponseWriter, r *http.Request) {
	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")

	limit := 20
	if limitStr != "" {
		if l, err := strconv.Atoi(limitStr); err == nil && l > 0 && l <= 100 {
			limit = l
		}
	}

	offset := 0
	if offsetStr != "" {
		if o, err := strconv.Atoi(offsetStr); err == nil && o >= 0 {
			offset = o
		}
	}

	eventTypes, err := h.eventTypeStore.List(r.Context(), clientID, limit, offset)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to list event types", "INTERNAL_ERROR")
		return
	}

	response := make([]map[string]any, len(eventTypes))
	for i, et := range eventTypes {
		response[i] = eventTypeToResponse(et)
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"data": response,
		"pagination": map[string]any{
			"hasMore": len(eventTypes) == limit,
			"total":   len(eventTypes),
			"offset":  offset,
			"limit":   limit,
		},
	})
}

// UpdateEventType handles PUT /api/v1/event-types/{eventTypeId}
func (h *Handler) UpdateEventType(w http.ResponseWriter, r *http.Request) {
	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	eventTypeID, err := uuid.Parse(chi.URLParam(r, "eventTypeId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid event type ID", "BAD_REQUEST")
		return
	}

	et, err := h.eventTypeStore.GetByID(r.Context(), eventTypeID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Event type not found", "NOT_FOUND")
		return
	}

	if et.ClientID != clientID {
		respondError(w, http.StatusNotFound, "Event type not found", "NOT_FOUND")
		return
	}

	var req UpdateEventTypeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "Invalid request body", "BAD_REQUEST")
		return
	}

	// Apply updates
	if req.Description != nil {
		et = et.WithDescription(*req.Description)
	}

	if req.Schema != nil {
		schemaBytes, err := json.Marshal(req.Schema)
		if err != nil {
			respondError(w, http.StatusBadRequest, "Invalid schema format", "VALIDATION_ERROR")
			return
		}
		if err := config.ValidateJSONSchema(schemaBytes); err != nil {
			respondError(w, http.StatusBadRequest, err.Error(), "INVALID_SCHEMA")
			return
		}
		version := et.SchemaVersion
		if req.SchemaVersion != nil {
			version = *req.SchemaVersion
		}
		et = et.WithSchema(schemaBytes, version)
	} else if req.SchemaVersion != nil {
		et = et.WithSchema(et.Schema, *req.SchemaVersion)
	}

	updated, err := h.eventTypeStore.Update(r.Context(), et)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to update event type", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, eventTypeToResponse(updated))
}

// DeleteEventType handles DELETE /api/v1/event-types/{eventTypeId}
func (h *Handler) DeleteEventType(w http.ResponseWriter, r *http.Request) {
	clientID, err := auth.RequireClientID(r.Context())
	if err != nil {
		respondError(w, http.StatusUnauthorized, "Authentication required", "UNAUTHORIZED")
		return
	}

	eventTypeID, err := uuid.Parse(chi.URLParam(r, "eventTypeId"))
	if err != nil {
		respondError(w, http.StatusBadRequest, "Invalid event type ID", "BAD_REQUEST")
		return
	}

	et, err := h.eventTypeStore.GetByID(r.Context(), eventTypeID)
	if err != nil {
		respondError(w, http.StatusNotFound, "Event type not found", "NOT_FOUND")
		return
	}

	if et.ClientID != clientID {
		respondError(w, http.StatusNotFound, "Event type not found", "NOT_FOUND")
		return
	}

	if err := h.eventTypeStore.Delete(r.Context(), eventTypeID); err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to delete event type", "INTERNAL_ERROR")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{"deleted": true})
}

func eventTypeToResponse(et domain.EventType) map[string]any {
	resp := map[string]any{
		"id":        et.ID.String(),
		"clientId":  et.ClientID,
		"name":      et.Name,
		"createdAt": et.CreatedAt,
		"updatedAt": et.UpdatedAt,
	}

	if et.Description != "" {
		resp["description"] = et.Description
	}

	if len(et.Schema) > 0 {
		var schema map[string]any
		if err := json.Unmarshal(et.Schema, &schema); err == nil {
			resp["schema"] = schema
		}
	}

	if et.SchemaVersion != "" {
		resp["schemaVersion"] = et.SchemaVersion
	}

	return resp
}
