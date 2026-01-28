package relay

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNewClient(t *testing.T) {
	c := NewClient("https://api.example.com", "test-api-key")

	if c.baseURL != "https://api.example.com" {
		t.Errorf("expected baseURL https://api.example.com, got %s", c.baseURL)
	}
	if c.apiKey != "test-api-key" {
		t.Errorf("expected apiKey test-api-key, got %s", c.apiKey)
	}
	if c.httpClient == nil {
		t.Error("expected httpClient to be initialized")
	}
	if c.httpClient.Timeout != 30*time.Second {
		t.Errorf("expected default timeout 30s, got %v", c.httpClient.Timeout)
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	customClient := &http.Client{Timeout: 60 * time.Second}
	c := NewClient("https://api.example.com", "test-api-key", WithHTTPClient(customClient))

	if c.httpClient != customClient {
		t.Error("expected custom HTTP client to be set")
	}
}

func TestNewClient_WithTimeout(t *testing.T) {
	c := NewClient("https://api.example.com", "test-api-key", WithTimeout(10*time.Second))

	if c.httpClient.Timeout != 10*time.Second {
		t.Errorf("expected timeout 10s, got %v", c.httpClient.Timeout)
	}
}

func TestClient_DoRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-API-Key") != "test-key" {
			t.Errorf("expected X-API-Key header 'test-key', got %s", r.Header.Get("X-API-Key"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("expected Content-Type application/json, got %s", r.Header.Get("Content-Type"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	var result map[string]string
	err := c.do(context.Background(), http.MethodGet, "/test", nil, &result, nil)
	if err != nil {
		t.Fatalf("do failed: %v", err)
	}

	if result["status"] != "ok" {
		t.Errorf("expected status 'ok', got %s", result["status"])
	}
}

func TestClient_DoRequest_WithBody(t *testing.T) {
	var receivedBody map[string]string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &receivedBody)

		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	reqBody := map[string]string{"key": "value"}
	err := c.do(context.Background(), http.MethodPost, "/test", reqBody, nil, nil)
	if err != nil {
		t.Fatalf("do failed: %v", err)
	}

	if receivedBody["key"] != "value" {
		t.Errorf("expected key 'value', got %s", receivedBody["key"])
	}
}

func TestClient_DoRequest_CustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Custom-Header") != "custom-value" {
			t.Errorf("expected X-Custom-Header 'custom-value', got %s", r.Header.Get("X-Custom-Header"))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	headers := map[string]string{"X-Custom-Header": "custom-value"}
	err := c.do(context.Background(), http.MethodGet, "/test", nil, nil, headers)
	if err != nil {
		t.Fatalf("do failed: %v", err)
	}
}

func TestClient_DoRequest_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid request","code":"INVALID_REQUEST"}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	err := c.do(context.Background(), http.MethodGet, "/test", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusBadRequest {
		t.Errorf("expected status 400, got %d", apiErr.StatusCode)
	}
	if apiErr.Message != "invalid request" {
		t.Errorf("expected message 'invalid request', got %s", apiErr.Message)
	}
	if apiErr.Code != "INVALID_REQUEST" {
		t.Errorf("expected code 'INVALID_REQUEST', got %s", apiErr.Code)
	}
}

func TestClient_DoRequest_NonJSONError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("internal server error"))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	err := c.do(context.Background(), http.MethodGet, "/test", nil, nil, nil)
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	apiErr, ok := err.(*APIError)
	if !ok {
		t.Fatalf("expected APIError, got %T", err)
	}
	if apiErr.StatusCode != http.StatusInternalServerError {
		t.Errorf("expected status 500, got %d", apiErr.StatusCode)
	}
}

func TestAPIError_Error(t *testing.T) {
	tests := []struct {
		name     string
		err      APIError
		expected string
	}{
		{
			name:     "with code",
			err:      APIError{StatusCode: 400, Message: "invalid request", Code: "INVALID"},
			expected: "relay: invalid request (INVALID, status 400)",
		},
		{
			name:     "without code",
			err:      APIError{StatusCode: 500, Message: "server error"},
			expected: "relay: server error (status 500)",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.err.Error()
			if result != tc.expected {
				t.Errorf("expected %s, got %s", tc.expected, result)
			}
		})
	}
}

func TestClient_CreateEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/events" {
			t.Errorf("expected /api/v1/events, got %s", r.URL.Path)
		}
		if r.Header.Get("X-Idempotency-Key") != "test-key" {
			t.Errorf("expected X-Idempotency-Key 'test-key', got %s", r.Header.Get("X-Idempotency-Key"))
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id": "evt_123",
			"idempotencyKey": "test-key",
			"destination": "https://example.com/webhook",
			"status": "queued",
			"attempts": 0,
			"maxAttempts": 5,
			"createdAt": "2024-01-01T00:00:00Z"
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-api-key")

	event, err := c.CreateEvent(context.Background(), "test-key", CreateEventRequest{
		Destination: "https://example.com/webhook",
		Payload:     map[string]bool{"test": true},
	})
	if err != nil {
		t.Fatalf("CreateEvent failed: %v", err)
	}

	if event.ID != "evt_123" {
		t.Errorf("expected ID 'evt_123', got %s", event.ID)
	}
	if event.Status != EventStatusQueued {
		t.Errorf("expected status queued, got %s", event.Status)
	}
}

func TestClient_GetEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/events/evt_123" {
			t.Errorf("expected /api/v1/events/evt_123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "evt_123",
			"status": "delivered",
			"attempts": 1
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	event, err := c.GetEvent(context.Background(), "evt_123")
	if err != nil {
		t.Fatalf("GetEvent failed: %v", err)
	}

	if event.ID != "evt_123" {
		t.Errorf("expected ID 'evt_123', got %s", event.ID)
	}
	if event.Status != EventStatusDelivered {
		t.Errorf("expected status delivered, got %s", event.Status)
	}
}

func TestClient_ReplayEvent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/events/evt_123/replay" {
			t.Errorf("expected /api/v1/events/evt_123/replay, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "evt_123",
			"status": "queued",
			"attempts": 0
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	event, err := c.ReplayEvent(context.Background(), "evt_123")
	if err != nil {
		t.Fatalf("ReplayEvent failed: %v", err)
	}

	if event.Status != EventStatusQueued {
		t.Errorf("expected status queued, got %s", event.Status)
	}
}

func TestClient_BatchRetryByIDs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/events/batch/retry" {
			t.Errorf("expected /api/v1/events/batch/retry, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"succeeded": [{"id": "evt_1"}, {"id": "evt_2"}],
			"failed": [],
			"totalRequested": 2,
			"totalSucceeded": 2
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	result, err := c.BatchRetryByIDs(context.Background(), []string{"evt_1", "evt_2"})
	if err != nil {
		t.Fatalf("BatchRetryByIDs failed: %v", err)
	}

	if result.TotalSucceeded != 2 {
		t.Errorf("expected 2 succeeded, got %d", result.TotalSucceeded)
	}
}

func TestClient_BatchRetryByStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)

		if body["status"] != "failed" {
			t.Errorf("expected status 'failed', got %v", body["status"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"succeeded": [{"id": "evt_1"}],
			"failed": [],
			"totalRequested": 1,
			"totalSucceeded": 1
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	result, err := c.BatchRetryByStatus(context.Background(), EventStatusFailed, 10)
	if err != nil {
		t.Fatalf("BatchRetryByStatus failed: %v", err)
	}

	if result.TotalSucceeded != 1 {
		t.Errorf("expected 1 succeeded, got %d", result.TotalSucceeded)
	}
}

func TestClient_CreateEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{
			"id": "ep_123",
			"url": "https://example.com/webhook",
			"eventTypes": ["order.created"],
			"status": "active"
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	endpoint, err := c.CreateEndpoint(context.Background(), CreateEndpointRequest{
		URL:        "https://example.com/webhook",
		EventTypes: []string{"order.created"},
	})
	if err != nil {
		t.Fatalf("CreateEndpoint failed: %v", err)
	}

	if endpoint.ID != "ep_123" {
		t.Errorf("expected ID 'ep_123', got %s", endpoint.ID)
	}
	if endpoint.Status != EndpointStatusActive {
		t.Errorf("expected status active, got %s", endpoint.Status)
	}
}

func TestClient_GetEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/endpoints/ep_123" {
			t.Errorf("expected /api/v1/endpoints/ep_123, got %s", r.URL.Path)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "ep_123",
			"url": "https://example.com/webhook",
			"status": "active"
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	endpoint, err := c.GetEndpoint(context.Background(), "ep_123")
	if err != nil {
		t.Fatalf("GetEndpoint failed: %v", err)
	}

	if endpoint.ID != "ep_123" {
		t.Errorf("expected ID 'ep_123', got %s", endpoint.ID)
	}
}

func TestClient_UpdateEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("expected PATCH, got %s", r.Method)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "ep_123",
			"url": "https://updated.example.com/webhook",
			"status": "paused"
		}`))
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	newURL := "https://updated.example.com/webhook"
	newStatus := EndpointStatusPaused
	endpoint, err := c.UpdateEndpoint(context.Background(), "ep_123", UpdateEndpointRequest{
		URL:    &newURL,
		Status: &newStatus,
	})
	if err != nil {
		t.Fatalf("UpdateEndpoint failed: %v", err)
	}

	if endpoint.URL != newURL {
		t.Errorf("expected URL %s, got %s", newURL, endpoint.URL)
	}
	if endpoint.Status != EndpointStatusPaused {
		t.Errorf("expected status paused, got %s", endpoint.Status)
	}
}

func TestClient_DeleteEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("expected DELETE, got %s", r.Method)
		}
		if r.URL.Path != "/api/v1/endpoints/ep_123" {
			t.Errorf("expected /api/v1/endpoints/ep_123, got %s", r.URL.Path)
		}

		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	c := NewClient(server.URL, "test-key")

	err := c.DeleteEndpoint(context.Background(), "ep_123")
	if err != nil {
		t.Fatalf("DeleteEndpoint failed: %v", err)
	}
}
