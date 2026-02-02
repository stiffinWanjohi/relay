package transform

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

func TestV8Executor_Execute_BasicTransformation(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			return {
				method: 'POST',
				url: webhook.url,
				headers: webhook.headers,
				payload: { wrapped: webhook.payload },
				cancel: false
			};
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com/webhook",
		map[string]string{"Content-Type": "application/json"},
		json.RawMessage(`{"order_id": "123"}`),
	)

	ctx := context.Background()
	result, err := executor.Execute(ctx, code, input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Method != "POST" {
		t.Errorf("expected method POST, got %s", result.Method)
	}
	if result.URL != "https://example.com/webhook" {
		t.Errorf("expected URL https://example.com/webhook, got %s", result.URL)
	}
	if result.Cancel {
		t.Error("expected cancel to be false")
	}

	// Check the payload was wrapped
	var payload map[string]any
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}
	if _, ok := payload["wrapped"]; !ok {
		t.Error("expected payload to have 'wrapped' key")
	}
}

func TestV8Executor_Execute_ModifyURL(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			return {
				method: webhook.method,
				url: webhook.url + '/modified',
				headers: webhook.headers,
				payload: webhook.payload,
				cancel: false
			};
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{}`),
	)

	ctx := context.Background()
	result, err := executor.Execute(ctx, code, input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.URL != "https://example.com/modified" {
		t.Errorf("expected URL https://example.com/modified, got %s", result.URL)
	}
}

func TestV8Executor_Execute_CancelDelivery(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			// Cancel if amount is too small
			var payload = webhook.payload;
			if (payload.amount < 100) {
				return {
					method: webhook.method,
					url: webhook.url,
					headers: webhook.headers,
					payload: webhook.payload,
					cancel: true
				};
			}
			return webhook;
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{"amount": 50}`),
	)

	ctx := context.Background()
	_, err := executor.Execute(ctx, code, input)
	if err != domain.ErrTransformationCancelled {
		t.Errorf("expected ErrTransformationCancelled, got %v", err)
	}
}

func TestV8Executor_Execute_ModifyHeaders(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			var headers = webhook.headers || {};
			headers['X-Custom-Header'] = 'custom-value';
			headers['X-Source'] = 'transformation';
			return {
				method: webhook.method,
				url: webhook.url,
				headers: headers,
				payload: webhook.payload,
				cancel: false
			};
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		map[string]string{"Content-Type": "application/json"},
		json.RawMessage(`{}`),
	)

	ctx := context.Background()
	result, err := executor.Execute(ctx, code, input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if result.Headers["X-Custom-Header"] != "custom-value" {
		t.Errorf("expected X-Custom-Header to be 'custom-value', got %s", result.Headers["X-Custom-Header"])
	}
	if result.Headers["X-Source"] != "transformation" {
		t.Errorf("expected X-Source to be 'transformation', got %s", result.Headers["X-Source"])
	}
	// Original header should be preserved
	if result.Headers["Content-Type"] != "application/json" {
		t.Errorf("expected Content-Type to be preserved, got %s", result.Headers["Content-Type"])
	}
}

func TestV8Executor_Execute_Timeout(t *testing.T) {
	config := domain.TransformationConfig{
		Timeout:        100 * time.Millisecond,
		MaxMemoryBytes: 128 * 1024 * 1024,
	}
	executor := NewV8Executor(config)
	defer executor.Close()

	code := `
		function transform(webhook) {
			// Infinite loop
			while(true) {}
			return webhook;
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{}`),
	)

	ctx := context.Background()
	_, err := executor.Execute(ctx, code, input)
	if err != domain.ErrTransformationTimeout {
		t.Errorf("expected ErrTransformationTimeout, got %v", err)
	}
}

func TestV8Executor_Execute_SyntaxError(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function transform(webhook {  // missing closing paren
			return webhook;
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{}`),
	)

	ctx := context.Background()
	_, err := executor.Execute(ctx, code, input)
	if err == nil {
		t.Error("expected error for syntax error")
	}
}

func TestV8Executor_Execute_MissingTransformFunction(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function notTransform(webhook) {
			return webhook;
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{}`),
	)

	ctx := context.Background()
	_, err := executor.Execute(ctx, code, input)
	if err == nil {
		t.Error("expected error for missing transform function")
	}
}

func TestV8Executor_Validate_ValidCode(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			return {
				method: webhook.method,
				url: webhook.url,
				headers: webhook.headers,
				payload: webhook.payload,
				cancel: false
			};
		}
	`

	err := executor.Validate(code)
	if err != nil {
		t.Errorf("expected valid code to pass validation, got %v", err)
	}
}

func TestV8Executor_Validate_InvalidCode(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	testCases := []struct {
		name string
		code string
	}{
		{
			name: "syntax error",
			code: `function transform(webhook { return webhook; }`,
		},
		{
			name: "missing transform function",
			code: `function foo(webhook) { return webhook; }`,
		},
		{
			name: "transform is not a function",
			code: `var transform = 'not a function';`,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := executor.Validate(tc.code)
			if err == nil {
				t.Errorf("expected validation to fail for %s", tc.name)
			}
		})
	}
}

func TestV8Executor_Execute_DefaultValues(t *testing.T) {
	executor := NewDefaultV8Executor()
	defer executor.Close()

	// Test that defaults are applied when transform returns partial object
	code := `
		function transform(webhook) {
			return {
				// Only return cancel, others should use defaults
				cancel: false
			};
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		map[string]string{"X-Test": "value"},
		json.RawMessage(`{"key": "value"}`),
	)

	ctx := context.Background()
	result, err := executor.Execute(ctx, code, input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Defaults should be applied from input
	if result.Method != "POST" {
		t.Errorf("expected method POST (default), got %s", result.Method)
	}
	if result.URL != "https://example.com" {
		t.Errorf("expected URL https://example.com (default), got %s", result.URL)
	}
}

func TestTransformationInput_ToResult(t *testing.T) {
	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		map[string]string{"Content-Type": "application/json"},
		json.RawMessage(`{"key": "value"}`),
	)

	result := input.ToResult()

	if result.Method != input.Method {
		t.Errorf("expected method %s, got %s", input.Method, result.Method)
	}
	if result.URL != input.URL {
		t.Errorf("expected URL %s, got %s", input.URL, result.URL)
	}
	if result.Cancel {
		t.Error("expected cancel to be false")
	}
}

func TestTransformationResult_Validate(t *testing.T) {
	testCases := []struct {
		name    string
		result  domain.TransformationResult
		wantErr bool
	}{
		{
			name: "valid result",
			result: domain.TransformationResult{
				Method: "POST",
				URL:    "https://example.com",
			},
			wantErr: false,
		},
		{
			name: "missing method",
			result: domain.TransformationResult{
				Method: "",
				URL:    "https://example.com",
			},
			wantErr: true,
		},
		{
			name: "missing URL",
			result: domain.TransformationResult{
				Method: "POST",
				URL:    "",
			},
			wantErr: true,
		},
		{
			name: "invalid method",
			result: domain.TransformationResult{
				Method: "INVALID",
				URL:    "https://example.com",
			},
			wantErr: true,
		},
		{
			name: "valid GET method",
			result: domain.TransformationResult{
				Method: "GET",
				URL:    "https://example.com",
			},
			wantErr: false,
		},
		{
			name: "valid PUT method",
			result: domain.TransformationResult{
				Method: "PUT",
				URL:    "https://example.com",
			},
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.result.Validate()
			if (err != nil) != tc.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
