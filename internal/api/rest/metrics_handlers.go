package rest

import (
	"net/http"
	"time"
)

// GetRateLimitStats handles GET /api/v1/metrics/rate-limits
func (h *Handler) GetRateLimitStats(w http.ResponseWriter, r *http.Request) {
	if h.metricsStore == nil {
		respondError(w, http.StatusServiceUnavailable, "Metrics not configured", "METRICS_DISABLED")
		return
	}

	// Parse time range
	now := time.Now().UTC()
	start := now.Add(-1 * time.Hour) // Default: last hour
	end := now

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	// Get rate limit count
	totalCount, err := h.metricsStore.GetRateLimitCount(r.Context())
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get rate limit count", "INTERNAL_ERROR")
		return
	}

	// Get recent rate limit events
	limit := int64(100)
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := parseInt(limitStr); err == nil && l > 0 {
			limit = int64(l)
		}
	}

	events, err := h.metricsStore.GetRateLimitEvents(r.Context(), start, end, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get rate limit events", "INTERNAL_ERROR")
		return
	}

	response := make([]map[string]any, 0, len(events))
	for _, event := range events {
		response = append(response, map[string]any{
			"timestamp":  event.Timestamp,
			"endpointId": event.EndpointID,
			"eventId":    event.EventID,
			"limit":      event.Limit,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"totalCount": totalCount,
		"events":     response,
		"period": map[string]any{
			"start": start,
			"end":   end,
		},
	})
}

// GetRateLimitStatsByEndpoint handles GET /api/v1/metrics/rate-limits/endpoint/{endpointId}
func (h *Handler) GetRateLimitStatsByEndpoint(w http.ResponseWriter, r *http.Request) {
	if h.metricsStore == nil {
		respondError(w, http.StatusServiceUnavailable, "Metrics not configured", "METRICS_DISABLED")
		return
	}

	endpointID := r.PathValue("endpointId")
	if endpointID == "" {
		respondError(w, http.StatusBadRequest, "Endpoint ID is required", "BAD_REQUEST")
		return
	}

	// Parse time range
	now := time.Now().UTC()
	start := now.Add(-1 * time.Hour)
	end := now

	if startStr := r.URL.Query().Get("start"); startStr != "" {
		if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			start = t
		}
	}
	if endStr := r.URL.Query().Get("end"); endStr != "" {
		if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			end = t
		}
	}

	limit := int64(100)
	if limitStr := r.URL.Query().Get("limit"); limitStr != "" {
		if l, err := parseInt(limitStr); err == nil && l > 0 {
			limit = int64(l)
		}
	}

	events, err := h.metricsStore.GetRateLimitEventsByEndpoint(r.Context(), endpointID, start, end, limit)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "Failed to get rate limit events", "INTERNAL_ERROR")
		return
	}

	response := make([]map[string]any, 0, len(events))
	for _, event := range events {
		response = append(response, map[string]any{
			"timestamp":  event.Timestamp,
			"endpointId": event.EndpointID,
			"eventId":    event.EventID,
			"limit":      event.Limit,
		})
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"endpointId": endpointID,
		"events":     response,
		"total":      len(response),
		"period": map[string]any{
			"start": start,
			"end":   end,
		},
	})
}
