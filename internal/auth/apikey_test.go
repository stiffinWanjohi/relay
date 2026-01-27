package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// mockValidator implements APIKeyValidator for testing
type mockValidator struct {
	clientID string
	err      error
}

func (m *mockValidator) ValidateAPIKey(_ context.Context, _ string) (string, error) {
	return m.clientID, m.err
}

func TestMiddleware_ValidAPIKey_XAPIKeyHeader(t *testing.T) {
	validator := &mockValidator{clientID: "client-123", err: nil}
	middleware := Middleware(validator)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID, ok := ClientIDFromContext(r.Context())
		if !ok {
			t.Error("expected client ID in context")
		}
		if clientID != "client-123" {
			t.Errorf("expected client ID 'client-123', got %s", clientID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(APIKeyHeader, "test-api-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestMiddleware_ValidAPIKey_BearerToken(t *testing.T) {
	validator := &mockValidator{clientID: "client-456", err: nil}
	middleware := Middleware(validator)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientID, ok := ClientIDFromContext(r.Context())
		if !ok {
			t.Error("expected client ID in context")
		}
		if clientID != "client-456" {
			t.Errorf("expected client ID 'client-456', got %s", clientID)
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(AuthorizationHeader, BearerPrefix+"test-bearer-token")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("expected status 200, got %d", rec.Code)
	}
}

func TestMiddleware_MissingAPIKey(t *testing.T) {
	validator := &mockValidator{clientID: "client-123", err: nil}
	middleware := Middleware(validator)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called when API key is missing")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestMiddleware_InvalidAPIKey(t *testing.T) {
	validator := &mockValidator{clientID: "", err: ErrInvalidAPIKey}
	middleware := Middleware(validator)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		t.Error("handler should not be called when API key is invalid")
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(APIKeyHeader, "invalid-key")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected status 401, got %d", rec.Code)
	}
}

func TestExtractAPIKey_XAPIKeyHeader(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(APIKeyHeader, "my-api-key")

	result := extractAPIKey(req)

	if result != "my-api-key" {
		t.Errorf("expected 'my-api-key', got %s", result)
	}
}

func TestExtractAPIKey_BearerToken(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(AuthorizationHeader, "Bearer my-bearer-token")

	result := extractAPIKey(req)

	if result != "my-bearer-token" {
		t.Errorf("expected 'my-bearer-token', got %s", result)
	}
}

func TestExtractAPIKey_XAPIKeyPreferredOverBearer(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(APIKeyHeader, "x-api-key-value")
	req.Header.Set(AuthorizationHeader, "Bearer bearer-value")

	result := extractAPIKey(req)

	// X-API-Key should take precedence
	if result != "x-api-key-value" {
		t.Errorf("expected 'x-api-key-value', got %s", result)
	}
}

func TestExtractAPIKey_NoHeaders(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)

	result := extractAPIKey(req)

	if result != "" {
		t.Errorf("expected empty string, got %s", result)
	}
}

func TestExtractAPIKey_NonBearerAuthorization(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set(AuthorizationHeader, "Basic dXNlcjpwYXNz")

	result := extractAPIKey(req)

	// Should return empty since it's not Bearer
	if result != "" {
		t.Errorf("expected empty string for Basic auth, got %s", result)
	}
}

func TestClientIDFromContext_Found(t *testing.T) {
	ctx := context.WithValue(context.Background(), clientIDKey, "test-client")

	clientID, ok := ClientIDFromContext(ctx)

	if !ok {
		t.Error("expected ok to be true")
	}
	if clientID != "test-client" {
		t.Errorf("expected 'test-client', got %s", clientID)
	}
}

func TestClientIDFromContext_NotFound(t *testing.T) {
	ctx := context.Background()

	clientID, ok := ClientIDFromContext(ctx)

	if ok {
		t.Error("expected ok to be false")
	}
	if clientID != "" {
		t.Errorf("expected empty string, got %s", clientID)
	}
}

func TestClientIDFromContext_WrongType(t *testing.T) {
	ctx := context.WithValue(context.Background(), clientIDKey, 12345) // int instead of string

	clientID, ok := ClientIDFromContext(ctx)

	if ok {
		t.Error("expected ok to be false for wrong type")
	}
	if clientID != "" {
		t.Errorf("expected empty string, got %s", clientID)
	}
}

func TestRequireClientID_Found(t *testing.T) {
	ctx := context.WithValue(context.Background(), clientIDKey, "required-client")

	clientID, err := RequireClientID(ctx)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if clientID != "required-client" {
		t.Errorf("expected 'required-client', got %s", clientID)
	}
}

func TestRequireClientID_NotFound(t *testing.T) {
	ctx := context.Background()

	_, err := RequireClientID(ctx)

	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized, got %v", err)
	}
}

func TestRequireClientID_EmptyString(t *testing.T) {
	ctx := context.WithValue(context.Background(), clientIDKey, "")

	_, err := RequireClientID(ctx)

	if !errors.Is(err, ErrUnauthorized) {
		t.Errorf("expected ErrUnauthorized for empty client ID, got %v", err)
	}
}

func TestConstants(t *testing.T) {
	if APIKeyHeader != "X-API-Key" {
		t.Errorf("expected APIKeyHeader 'X-API-Key', got %s", APIKeyHeader)
	}
	if AuthorizationHeader != "Authorization" {
		t.Errorf("expected AuthorizationHeader 'Authorization', got %s", AuthorizationHeader)
	}
	if BearerPrefix != "Bearer " {
		t.Errorf("expected BearerPrefix 'Bearer ', got %s", BearerPrefix)
	}
}

func TestErrors(t *testing.T) {
	if ErrMissingAPIKey.Error() != "missing API key" {
		t.Errorf("unexpected ErrMissingAPIKey message: %s", ErrMissingAPIKey.Error())
	}
	if ErrInvalidAPIKey.Error() != "invalid API key" {
		t.Errorf("unexpected ErrInvalidAPIKey message: %s", ErrInvalidAPIKey.Error())
	}
	if ErrUnauthorized.Error() != "unauthorized" {
		t.Errorf("unexpected ErrUnauthorized message: %s", ErrUnauthorized.Error())
	}
}
