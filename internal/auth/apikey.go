package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

// contextKey is a custom type for context keys to avoid collisions.
type contextKey string

const (
	// APIKeyHeader is the header name for API key authentication.
	APIKeyHeader = "X-API-Key"

	// AuthorizationHeader is the standard Authorization header.
	AuthorizationHeader = "Authorization"

	// BearerPrefix is the prefix for Bearer token authentication.
	BearerPrefix = "Bearer "

	// clientIDKey is the context key for storing the authenticated client ID.
	clientIDKey contextKey = "clientID"
)

// Errors for authentication.
var (
	ErrMissingAPIKey   = errors.New("missing API key")
	ErrInvalidAPIKey   = errors.New("invalid API key")
	ErrUnauthorized    = errors.New("unauthorized")
)

// APIKeyValidator validates API keys and returns the associated client ID.
type APIKeyValidator interface {
	ValidateAPIKey(ctx context.Context, apiKey string) (clientID string, err error)
}

// Middleware creates an HTTP middleware for API key authentication.
func Middleware(validator APIKeyValidator) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			apiKey := extractAPIKey(r)
			if apiKey == "" {
				http.Error(w, ErrMissingAPIKey.Error(), http.StatusUnauthorized)
				return
			}

			clientID, err := validator.ValidateAPIKey(r.Context(), apiKey)
			if err != nil {
				http.Error(w, ErrInvalidAPIKey.Error(), http.StatusUnauthorized)
				return
			}

			// Store client ID in context for downstream handlers
			ctx := context.WithValue(r.Context(), clientIDKey, clientID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// extractAPIKey extracts the API key from the request.
// It checks both X-API-Key header and Authorization: Bearer header.
func extractAPIKey(r *http.Request) string {
	// Check X-API-Key header first
	if apiKey := r.Header.Get(APIKeyHeader); apiKey != "" {
		return apiKey
	}

	// Check Authorization header with Bearer prefix
	authHeader := r.Header.Get(AuthorizationHeader)
	if strings.HasPrefix(authHeader, BearerPrefix) {
		return strings.TrimPrefix(authHeader, BearerPrefix)
	}

	return ""
}

// ClientIDFromContext retrieves the client ID from the context.
func ClientIDFromContext(ctx context.Context) (string, bool) {
	clientID, ok := ctx.Value(clientIDKey).(string)
	return clientID, ok
}

// RequireClientID retrieves the client ID from context or returns an error.
func RequireClientID(ctx context.Context) (string, error) {
	clientID, ok := ClientIDFromContext(ctx)
	if !ok || clientID == "" {
		return "", ErrUnauthorized
	}
	return clientID, nil
}
