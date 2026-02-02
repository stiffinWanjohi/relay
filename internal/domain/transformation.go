package domain

import (
	"context"
	"encoding/json"
	"errors"
	"time"
)

// Transformation errors
var (
	// ErrTransformationTimeout is returned when a transformation exceeds the time limit.
	ErrTransformationTimeout = errors.New("transformation execution timeout")

	// ErrTransformationMemoryLimit is returned when a transformation exceeds memory limit.
	ErrTransformationMemoryLimit = errors.New("transformation exceeded memory limit")

	// ErrTransformationExecution is returned when JavaScript execution fails.
	ErrTransformationExecution = errors.New("transformation execution failed")

	// ErrTransformationInvalidResult is returned when the transformation returns invalid data.
	ErrTransformationInvalidResult = errors.New("transformation returned invalid result")

	// ErrTransformationCancelled is returned when the transformation requests delivery cancellation.
	ErrTransformationCancelled = errors.New("transformation cancelled delivery")
)

// TransformationInput represents the input passed to a transformation function.
type TransformationInput struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Payload json.RawMessage   `json:"payload"`
}

// TransformationResult represents the output from a transformation function.
type TransformationResult struct {
	Method  string            `json:"method"`
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
	Payload json.RawMessage   `json:"payload"`
	Cancel  bool              `json:"cancel"`
}

// TransformationConfig holds configuration for the transformation executor.
type TransformationConfig struct {
	// Timeout is the maximum execution time for a transformation.
	// Default: 1 second
	Timeout time.Duration

	// MaxMemoryBytes is the maximum memory a transformation can use.
	// Default: 128MB
	MaxMemoryBytes int64
}

// DefaultTransformationConfig returns the default transformation configuration.
func DefaultTransformationConfig() TransformationConfig {
	return TransformationConfig{
		Timeout:        1 * time.Second,
		MaxMemoryBytes: 128 * 1024 * 1024, // 128MB
	}
}

// TransformationExecutor executes JavaScript transformations in a sandboxed environment.
type TransformationExecutor interface {
	// Execute runs the transformation code with the given input.
	// Returns the transformed result or an error.
	Execute(ctx context.Context, code string, input TransformationInput) (TransformationResult, error)

	// Validate checks if the transformation code is syntactically valid.
	// Returns nil if valid, or an error describing the issue.
	Validate(code string) error

	// Close releases any resources held by the executor.
	Close()
}

// TransformationTestRequest represents a request to test a transformation.
type TransformationTestRequest struct {
	Code    string              `json:"code"`
	Input   TransformationInput `json:"input"`
	Timeout time.Duration       `json:"timeout,omitempty"`
}

// TransformationTestResponse represents the response from testing a transformation.
type TransformationTestResponse struct {
	Success     bool                  `json:"success"`
	Result      *TransformationResult `json:"result,omitempty"`
	Error       string                `json:"error,omitempty"`
	ExecutionMs int64                 `json:"executionMs"`
	MemoryUsed  int64                 `json:"memoryUsed,omitempty"`
	Logs        []string              `json:"logs,omitempty"`
}

// NewTransformationInput creates a TransformationInput from webhook delivery parameters.
func NewTransformationInput(method, url string, headers map[string]string, payload json.RawMessage) TransformationInput {
	if headers == nil {
		headers = make(map[string]string)
	}
	return TransformationInput{
		Method:  method,
		URL:     url,
		Headers: headers,
		Payload: payload,
	}
}

// ToResult converts input to a result (for pass-through when no transformation is needed).
func (t TransformationInput) ToResult() TransformationResult {
	return TransformationResult{
		Method:  t.Method,
		URL:     t.URL,
		Headers: t.Headers,
		Payload: t.Payload,
		Cancel:  false,
	}
}

// Validate checks if the TransformationResult has valid values.
func (r TransformationResult) Validate() error {
	if r.Method == "" {
		return errors.New("method is required")
	}
	if r.URL == "" {
		return errors.New("url is required")
	}
	// Validate method is a valid HTTP method
	validMethods := map[string]bool{
		"GET": true, "POST": true, "PUT": true, "PATCH": true,
		"DELETE": true, "HEAD": true, "OPTIONS": true,
	}
	if !validMethods[r.Method] {
		return errors.New("invalid HTTP method: " + r.Method)
	}
	return nil
}
