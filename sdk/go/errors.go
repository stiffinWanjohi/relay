package relay

import (
	"errors"
	"fmt"
	"net/http"
)

// Common errors returned by the client.
var (
	// ErrUnauthorized is returned when the API key is invalid or missing.
	ErrUnauthorized = errors.New("unauthorized: invalid or missing API key")

	// ErrNotFound is returned when the requested resource doesn't exist.
	ErrNotFound = errors.New("not found")

	// ErrConflict is returned for idempotency conflicts.
	ErrConflict = errors.New("conflict: resource already exists")

	// ErrRateLimited is returned when the rate limit is exceeded.
	ErrRateLimited = errors.New("rate limited: too many requests")

	// ErrBadRequest is returned for invalid request parameters.
	ErrBadRequest = errors.New("bad request: invalid parameters")

	// ErrServerError is returned for internal server errors.
	ErrServerError = errors.New("server error")
)

// APIError represents an error response from the Relay API.
type APIError struct {
	// StatusCode is the HTTP status code.
	StatusCode int `json:"statusCode"`

	// Code is the error code from the API.
	Code string `json:"code,omitempty"`

	// Message is the error message.
	Message string `json:"error"`

	// Details contains additional error details.
	Details map[string]any `json:"details,omitempty"`
}

// Error implements the error interface.
func (e *APIError) Error() string {
	if e.Code != "" {
		return fmt.Sprintf("relay: %s (HTTP %d, code: %s)", e.Message, e.StatusCode, e.Code)
	}
	return fmt.Sprintf("relay: %s (HTTP %d)", e.Message, e.StatusCode)
}

// Unwrap returns the underlying error for errors.Is/As support.
func (e *APIError) Unwrap() error {
	switch e.StatusCode {
	case http.StatusUnauthorized:
		return ErrUnauthorized
	case http.StatusNotFound:
		return ErrNotFound
	case http.StatusConflict:
		return ErrConflict
	case http.StatusTooManyRequests:
		return ErrRateLimited
	case http.StatusBadRequest, http.StatusUnprocessableEntity:
		return ErrBadRequest
	default:
		if e.StatusCode >= 500 {
			return ErrServerError
		}
		return nil
	}
}

// IsUnauthorized returns true if the error is an authorization error.
func IsUnauthorized(err error) bool {
	return errors.Is(err, ErrUnauthorized)
}

// IsNotFound returns true if the error is a not found error.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}

// IsConflict returns true if the error is a conflict error.
func IsConflict(err error) bool {
	return errors.Is(err, ErrConflict)
}

// IsRateLimited returns true if the error is a rate limit error.
func IsRateLimited(err error) bool {
	return errors.Is(err, ErrRateLimited)
}

// IsBadRequest returns true if the error is a bad request error.
func IsBadRequest(err error) bool {
	return errors.Is(err, ErrBadRequest)
}

// IsServerError returns true if the error is a server error.
func IsServerError(err error) bool {
	return errors.Is(err, ErrServerError)
}

// ValidationError represents a validation error with field-level details.
type ValidationError struct {
	Field   string `json:"field"`
	Message string `json:"message"`
}

// Error implements the error interface.
func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s - %s", e.Field, e.Message)
}

// SignatureError represents a webhook signature verification error.
type SignatureError struct {
	Reason string
}

// Error implements the error interface.
func (e *SignatureError) Error() string {
	return fmt.Sprintf("signature verification failed: %s", e.Reason)
}

// Common signature errors.
var (
	// ErrInvalidSignature is returned when the signature doesn't match.
	ErrInvalidSignature = &SignatureError{Reason: "signature mismatch"}

	// ErrMissingSignature is returned when the signature header is missing.
	ErrMissingSignature = &SignatureError{Reason: "missing signature header"}

	// ErrInvalidTimestamp is returned when the timestamp is invalid or expired.
	ErrInvalidTimestamp = &SignatureError{Reason: "invalid or expired timestamp"}

	// ErrMissingTimestamp is returned when the timestamp header is missing.
	ErrMissingTimestamp = &SignatureError{Reason: "missing timestamp header"}
)
