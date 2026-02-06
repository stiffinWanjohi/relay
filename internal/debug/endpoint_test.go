package debug

import (
	"context"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func setupTestService(t *testing.T) (*Service, func()) {
	t.Helper()
	mr := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{
		Addr: mr.Addr(),
	})
	service := NewService(client, "http://localhost:8080")
	cleanup := func() {
		_ = client.Close()
		mr.Close()
	}
	return service, cleanup
}

func TestCreateEndpoint(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	endpoint, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	if endpoint.ID == "" {
		t.Error("expected endpoint ID to be set")
	}
	if endpoint.URL == "" {
		t.Error("expected endpoint URL to be set")
	}
	if endpoint.ExpiresAt.Before(time.Now()) {
		t.Error("expected endpoint to expire in the future")
	}
}

func TestGetEndpoint(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	created, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	retrieved, err := service.GetEndpoint(ctx, created.ID)
	if err != nil {
		t.Fatalf("failed to get endpoint: %v", err)
	}

	if retrieved == nil {
		t.Fatal("expected endpoint to be found")
		return
	}
	if retrieved.ID != created.ID {
		t.Errorf("expected ID %s, got %s", created.ID, retrieved.ID)
	}
}

func TestGetEndpoint_NotFound(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	endpoint, err := service.GetEndpoint(ctx, "nonexistent")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != nil {
		t.Error("expected endpoint to be nil")
	}
}

func TestDeleteEndpoint(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	created, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	if err := service.DeleteEndpoint(ctx, created.ID); err != nil {
		t.Fatalf("failed to delete endpoint: %v", err)
	}

	endpoint, err := service.GetEndpoint(ctx, created.ID)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if endpoint != nil {
		t.Error("expected endpoint to be deleted")
	}
}

func TestCaptureRequest(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	endpoint, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	req := &CapturedRequest{
		Method:      "POST",
		Path:        "/webhook",
		Headers:     map[string]string{"Content-Type": "application/json"},
		Body:        `{"event": "test"}`,
		ContentType: "application/json",
		RemoteAddr:  "127.0.0.1:12345",
	}

	if err := service.CaptureRequest(ctx, endpoint.ID, req); err != nil {
		t.Fatalf("failed to capture request: %v", err)
	}

	requests, err := service.GetRequests(ctx, endpoint.ID, 10)
	if err != nil {
		t.Fatalf("failed to get requests: %v", err)
	}

	if len(requests) != 1 {
		t.Fatalf("expected 1 request, got %d", len(requests))
	}

	if requests[0].Method != "POST" {
		t.Errorf("expected method POST, got %s", requests[0].Method)
	}
	if requests[0].Body != `{"event": "test"}` {
		t.Errorf("unexpected body: %s", requests[0].Body)
	}
}

func TestCaptureRequest_MaxRequests(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	endpoint, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	// Capture more than MaxRequestsPerEndpoint requests
	for i := range MaxRequestsPerEndpoint+10 {
		req := &CapturedRequest{
			Method: "POST",
			Path:   "/webhook",
			Body:   "test",
		}
		if err := service.CaptureRequest(ctx, endpoint.ID, req); err != nil {
			t.Fatalf("failed to capture request %d: %v", i, err)
		}
	}

	requests, err := service.GetRequests(ctx, endpoint.ID, 0)
	if err != nil {
		t.Fatalf("failed to get requests: %v", err)
	}

	if len(requests) > MaxRequestsPerEndpoint {
		t.Errorf("expected max %d requests, got %d", MaxRequestsPerEndpoint, len(requests))
	}
}

func TestCaptureRequest_NonexistentEndpoint(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	req := &CapturedRequest{
		Method: "POST",
		Path:   "/webhook",
	}

	// Should not error, just silently ignore
	if err := service.CaptureRequest(ctx, "nonexistent", req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGetRequest(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	endpoint, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	req := &CapturedRequest{
		Method: "POST",
		Path:   "/webhook",
		Body:   "test body",
	}
	if err := service.CaptureRequest(ctx, endpoint.ID, req); err != nil {
		t.Fatalf("failed to capture request: %v", err)
	}

	// Get the request by ID
	requests, _ := service.GetRequests(ctx, endpoint.ID, 1)
	if len(requests) == 0 {
		t.Fatal("expected at least one request")
	}

	retrieved, err := service.GetRequest(ctx, endpoint.ID, requests[0].ID)
	if err != nil {
		t.Fatalf("failed to get request: %v", err)
	}
	if retrieved == nil {
		t.Fatal("expected request to be found")
		return
	}
	if retrieved.Body != "test body" {
		t.Errorf("expected body 'test body', got %s", retrieved.Body)
	}
}

func TestSubscribe(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	ctx := context.Background()
	endpoint, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	// Subscribe to requests
	ch := service.Subscribe(endpoint.ID)

	// Capture a request
	go func() {
		time.Sleep(10 * time.Millisecond)
		req := &CapturedRequest{
			Method: "POST",
			Path:   "/webhook",
			Body:   "streamed",
		}
		_ = service.CaptureRequest(ctx, endpoint.ID, req)
	}()

	// Wait for the request
	select {
	case req := <-ch:
		if req.Body != "streamed" {
			t.Errorf("expected body 'streamed', got %s", req.Body)
		}
	case <-time.After(1 * time.Second):
		t.Error("timeout waiting for streamed request")
	}

	// Unsubscribe
	service.Unsubscribe(endpoint.ID, ch)
}

func TestWithTTL(t *testing.T) {
	service, cleanup := setupTestService(t)
	defer cleanup()

	customTTL := 30 * time.Minute
	service.WithTTL(customTTL)

	ctx := context.Background()
	endpoint, err := service.CreateEndpoint(ctx)
	if err != nil {
		t.Fatalf("failed to create endpoint: %v", err)
	}

	expectedExpiry := time.Now().Add(customTTL)
	if endpoint.ExpiresAt.Before(expectedExpiry.Add(-1*time.Second)) || endpoint.ExpiresAt.After(expectedExpiry.Add(1*time.Second)) {
		t.Errorf("expected expiry around %v, got %v", expectedExpiry, endpoint.ExpiresAt)
	}
}
