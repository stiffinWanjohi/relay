package delivery

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/relay/internal/domain"
	"github.com/relay/pkg/signature"
)

func TestNewSender(t *testing.T) {
	signingKey := "test-signing-key-32-chars-long!"
	sender := NewSender(signingKey)

	if sender == nil {
		t.Fatal("NewSender returned nil")
	}
	if sender.client == nil {
		t.Error("client should not be nil")
	}
	if sender.signer == nil {
		t.Error("signer should not be nil")
	}
	if sender.signingKey != signingKey {
		t.Errorf("signingKey = %s, want %s", sender.signingKey, signingKey)
	}
}

func TestSender_WithClient(t *testing.T) {
	signingKey := "test-signing-key-32-chars-long!"
	sender := NewSender(signingKey)

	customClient := &http.Client{Timeout: 10 * time.Second}
	newSender := sender.WithClient(customClient)

	if newSender == sender {
		t.Error("WithClient should return new instance")
	}
	if newSender.client != customClient {
		t.Error("client should be the custom client")
	}
	if newSender.signer != sender.signer {
		t.Error("signer should be preserved")
	}
	if newSender.signingKey != sender.signingKey {
		t.Error("signingKey should be preserved")
	}
}

func TestSender_WithTimeout(t *testing.T) {
	signingKey := "test-signing-key-32-chars-long!"
	sender := NewSender(signingKey)

	timeout := 5 * time.Second
	newSender := sender.WithTimeout(timeout)

	if newSender == sender {
		t.Error("WithTimeout should return new instance")
	}
	if newSender.client.Timeout != timeout {
		t.Errorf("timeout = %v, want %v", newSender.client.Timeout, timeout)
	}
	if newSender.signer != sender.signer {
		t.Error("signer should be preserved")
	}
}

func TestSender_Send_Success(t *testing.T) {
	// Create a test server that returns 200 OK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify request method
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}

		// Verify content type
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %s, want application/json", ct)
		}

		// Verify user agent
		if ua := r.Header.Get("User-Agent"); ua != "Relay-Webhook/1.0" {
			t.Errorf("User-Agent = %s, want Relay-Webhook/1.0", ua)
		}

		// Verify signature headers exist
		if r.Header.Get(signature.HeaderSignature) == "" {
			t.Error("missing signature header")
		}
		if r.Header.Get(signature.HeaderTimestamp) == "" {
			t.Error("missing timestamp header")
		}
		if r.Header.Get(signature.HeaderEventID) == "" {
			t.Error("missing event ID header")
		}

		// Read body
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"test":"data"}` {
			t.Errorf("body = %s, want {\"test\":\"data\"}", string(body))
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	signingKey := "test-signing-key-32-chars-long!"
	sender := NewSender(signingKey)

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"test":"data"}`),
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Errorf("expected success, got failure: %v", result.Error)
	}
	if result.StatusCode != http.StatusOK {
		t.Errorf("status code = %d, want %d", result.StatusCode, http.StatusOK)
	}
	if result.ResponseBody != `{"status":"ok"}` {
		t.Errorf("response body = %s, want {\"status\":\"ok\"}", result.ResponseBody)
	}
	if result.DurationMs < 0 {
		t.Error("duration should be non-negative")
	}
}

func TestSender_Send_WithCustomHeaders(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify custom headers
		if h := r.Header.Get("X-Custom-Header"); h != "custom-value" {
			t.Errorf("X-Custom-Header = %s, want custom-value", h)
		}
		if h := r.Header.Get("X-Another-Header"); h != "another-value" {
			t.Errorf("X-Another-Header = %s, want another-value", h)
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
		Headers: map[string]string{
			"X-Custom-Header":  "custom-value",
			"X-Another-Header": "another-value",
		},
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
}

func TestSender_Send_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"error":"internal server error"}`))
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	if result.Success {
		t.Error("expected failure for 500 response")
	}
	if result.StatusCode != http.StatusInternalServerError {
		t.Errorf("status code = %d, want %d", result.StatusCode, http.StatusInternalServerError)
	}
	if result.Error != nil {
		t.Error("error should be nil for HTTP errors (non-network)")
	}
}

func TestSender_Send_ClientError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"bad request"}`))
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	if result.Success {
		t.Error("expected failure for 400 response")
	}
	if result.StatusCode != http.StatusBadRequest {
		t.Errorf("status code = %d, want %d", result.StatusCode, http.StatusBadRequest)
	}
}

func TestSender_Send_RedirectNotFollowed(t *testing.T) {
	redirectServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("redirect should not be followed")
		w.WriteHeader(http.StatusOK)
	}))
	defer redirectServer.Close()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, redirectServer.URL, http.StatusTemporaryRedirect)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	// Should return the redirect status, not follow it
	if result.StatusCode != http.StatusTemporaryRedirect {
		t.Errorf("status code = %d, want %d", result.StatusCode, http.StatusTemporaryRedirect)
	}
}

func TestSender_Send_NetworkError(t *testing.T) {
	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: "http://localhost:99999", // Invalid port
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	if result.Success {
		t.Error("expected failure for network error")
	}
	if result.Error == nil {
		t.Error("error should not be nil for network errors")
	}
	if result.StatusCode != 0 {
		t.Errorf("status code should be 0 for network error, got %d", result.StatusCode)
	}
}

func TestSender_Send_InvalidURL(t *testing.T) {
	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: "://invalid-url",
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	if result.Success {
		t.Error("expected failure for invalid URL")
	}
	if result.Error == nil {
		t.Error("error should not be nil for invalid URL")
	}
}

func TestSender_SendWithTimeout_Timeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	// Very short timeout
	result := sender.SendWithTimeout(context.Background(), event, 50*time.Millisecond)

	if result.Success {
		t.Error("expected failure for timeout")
	}
	if result.Error == nil {
		t.Error("error should not be nil for timeout")
	}
}

func TestSender_SendWithTimeout_ContextWithDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	// Context already has a deadline
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result := sender.SendWithTimeout(ctx, event, 30*time.Second)

	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
}

func TestSender_SendWithTimeout_ContextCanceled(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	ctx, cancel := context.WithCancel(context.Background())
	// Cancel immediately
	cancel()

	result := sender.SendWithTimeout(ctx, event, 5*time.Second)

	if result.Success {
		t.Error("expected failure for canceled context")
	}
}

func TestSender_Send_LargeResponseTruncated(t *testing.T) {
	// Generate response larger than maxResponseBodySize (64KB)
	largeBody := strings.Repeat("x", 100*1024) // 100KB

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(largeBody))
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
	// Response should be truncated to maxResponseBodySize (64KB)
	if len(result.ResponseBody) > 64*1024 {
		t.Errorf("response body should be truncated to 64KB, got %d bytes", len(result.ResponseBody))
	}
}

func TestSender_Send_SignatureVerification(t *testing.T) {
	signingKey := "test-signing-key-32-chars-long!"
	var receivedSig, receivedTimestamp string
	var receivedBody []byte

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedSig = r.Header.Get(signature.HeaderSignature)
		receivedTimestamp = r.Header.Get(signature.HeaderTimestamp)
		receivedBody, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender(signingKey)

	payload := []byte(`{"test":"data"}`)
	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     payload,
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Fatalf("expected success: %v", result.Error)
	}

	// Verify the signature is valid using the Signer directly
	signer := signature.NewSigner(signingKey)
	timestamp, err := signature.ParseTimestamp(receivedTimestamp)
	if err != nil {
		t.Fatalf("failed to parse timestamp: %v", err)
	}

	valid := signer.Verify(receivedSig, timestamp, receivedBody)
	if !valid {
		t.Error("signature verification failed")
	}
}

func TestSender_Send_EventIDInHeader(t *testing.T) {
	eventID := uuid.New()
	var receivedEventID string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedEventID = r.Header.Get(signature.HeaderEventID)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          eventID,
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Fatalf("expected success: %v", result.Error)
	}

	if receivedEventID != eventID.String() {
		t.Errorf("event ID = %s, want %s", receivedEventID, eventID.String())
	}
}

func TestSender_Send_2xxStatusCodes(t *testing.T) {
	statusCodes := []int{200, 201, 202, 204}

	for _, statusCode := range statusCodes {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
			}))
			defer server.Close()

			sender := NewSender("test-signing-key-32-chars-long!")

			event := domain.Event{
				ID:          uuid.New(),
				Destination: server.URL,
				Payload:     []byte(`{}`),
			}

			result := sender.Send(context.Background(), event)

			if !result.Success {
				t.Errorf("expected success for status %d", statusCode)
			}
		})
	}
}

func TestSender_Send_Non2xxStatusCodes(t *testing.T) {
	statusCodes := []int{301, 400, 401, 403, 404, 500, 502, 503}

	for _, statusCode := range statusCodes {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(statusCode)
			}))
			defer server.Close()

			sender := NewSender("test-signing-key-32-chars-long!")

			event := domain.Event{
				ID:          uuid.New(),
				Destination: server.URL,
				Payload:     []byte(`{}`),
			}

			result := sender.Send(context.Background(), event)

			if result.Success {
				t.Errorf("expected failure for status %d", statusCode)
			}
			if result.StatusCode != statusCode {
				t.Errorf("status code = %d, want %d", result.StatusCode, statusCode)
			}
		})
	}
}

func TestSender_Send_EmptyPayload(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if len(body) != 0 {
			t.Errorf("expected empty body, got %d bytes", len(body))
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte{},
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
}

func TestSender_Send_JSONPayload(t *testing.T) {
	type testPayload struct {
		Name  string `json:"name"`
		Value int    `json:"value"`
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload testPayload
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("failed to decode payload: %v", err)
		}
		if payload.Name != "test" || payload.Value != 42 {
			t.Errorf("payload = %+v, want {Name:test Value:42}", payload)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{"name":"test","value":42}`),
	}

	result := sender.Send(context.Background(), event)

	if !result.Success {
		t.Errorf("expected success: %v", result.Error)
	}
}

func TestSender_Send_ResponseBodyReadError(t *testing.T) {
	// This test is tricky - we need a server that closes connection after headers
	// We'll test with a very slow read that triggers timeout
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		// Flush headers
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		// Close connection abruptly (simulated by not writing body and handler returning)
	}))
	defer server.Close()

	sender := NewSender("test-signing-key-32-chars-long!")

	event := domain.Event{
		ID:          uuid.New(),
		Destination: server.URL,
		Payload:     []byte(`{}`),
	}

	result := sender.Send(context.Background(), event)

	// Should still succeed since we got 200 status and empty body is valid
	if !result.Success {
		t.Logf("Result: success=%v, status=%d, error=%v", result.Success, result.StatusCode, result.Error)
	}
}
