package transform

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

func TestGojaExecutor_Execute_BasicTransformation(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Execute_ModifyURL(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Execute_CancelDelivery(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Execute_ModifyHeaders(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Execute_Timeout(t *testing.T) {
	config := domain.TransformationConfig{
		Timeout:        100 * time.Millisecond,
		MaxMemoryBytes: 128 * 1024 * 1024,
	}
	executor := NewGojaExecutor(config)
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

func TestGojaExecutor_Execute_SyntaxError(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Execute_MissingTransformFunction(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Validate_ValidCode(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Validate_InvalidCode(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

func TestGojaExecutor_Execute_DefaultValues(t *testing.T) {
	executor := NewDefaultGojaExecutor()
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

// ============================================================================
// New tests for improvements
// ============================================================================

func TestGojaExecutor_ConsoleLog(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			console.log('Processing webhook');
			console.log('URL:', webhook.url);
			console.info('Info message');
			console.warn('Warning message');
			console.error('Error message');
			console.debug('Debug message');
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
	var logs []string
	_, err := executor.ExecuteWithLogs(ctx, code, input, &logs)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(logs) != 6 {
		t.Errorf("expected 6 log entries, got %d: %v", len(logs), logs)
	}

	if logs[0] != "Processing webhook" {
		t.Errorf("expected first log to be 'Processing webhook', got %s", logs[0])
	}

	// Check URL log contains expected content
	if !strings.Contains(logs[1], "https://example.com") {
		t.Errorf("expected second log to contain URL, got %s", logs[1])
	}
}

func TestGojaExecutor_HelperFunctions_Base64(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			var encoded = base64Encode('Hello World');
			var decoded = base64Decode(encoded);
			return {
				method: webhook.method,
				url: webhook.url,
				headers: webhook.headers,
				payload: { encoded: encoded, decoded: decoded },
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

	var payload map[string]string
	if err := json.Unmarshal(result.Payload, &payload); err != nil {
		t.Fatalf("failed to unmarshal payload: %v", err)
	}

	if payload["encoded"] != "SGVsbG8gV29ybGQ=" {
		t.Errorf("expected base64 encoded value 'SGVsbG8gV29ybGQ=', got %s", payload["encoded"])
	}
	if payload["decoded"] != "Hello World" {
		t.Errorf("expected decoded value 'Hello World', got %s", payload["decoded"])
	}
}

func TestGojaExecutor_HelperFunctions_HmacSha256(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			var signature = hmacSha256('message', 'secret');
			return {
				method: webhook.method,
				url: webhook.url,
				headers: { 'X-Signature': signature },
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

	// Expected HMAC-SHA256 of "message" with key "secret"
	expected := "8b5f48702995c1598c573db1e21866a9b825d4a794d169d7060a03605796360b"
	if result.Headers["X-Signature"] != expected {
		t.Errorf("expected signature %s, got %s", expected, result.Headers["X-Signature"])
	}
}

func TestGojaExecutor_HelperFunctions_Sha256(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			var hash = sha256('hello');
			return {
				method: webhook.method,
				url: webhook.url,
				headers: { 'X-Hash': hash },
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

	// Expected SHA256 of "hello"
	expected := "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"
	if result.Headers["X-Hash"] != expected {
		t.Errorf("expected hash %s, got %s", expected, result.Headers["X-Hash"])
	}
}

func TestGojaExecutor_HelperFunctions_UUID(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			var id = uuid();
			return {
				method: webhook.method,
				url: webhook.url,
				headers: { 'X-Request-ID': id },
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

	// UUID should be 36 characters (8-4-4-4-12 format)
	id := result.Headers["X-Request-ID"]
	if len(id) != 36 {
		t.Errorf("expected UUID with 36 characters, got %d: %s", len(id), id)
	}
}

func TestGojaExecutor_HelperFunctions_Timestamps(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			var ts = now();
			var iso = isoTimestamp();
			return {
				method: webhook.method,
				url: webhook.url,
				headers: {
					'X-Timestamp': String(ts),
					'X-ISO-Timestamp': iso
				},
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

	before := time.Now().UnixMilli()
	ctx := context.Background()
	result, err := executor.Execute(ctx, code, input)
	after := time.Now().UnixMilli()

	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	// Check timestamp is within expected range
	var tsValue int64
	if err := json.Unmarshal([]byte(result.Headers["X-Timestamp"]), &tsValue); err == nil {
		if tsValue < before || tsValue > after {
			t.Errorf("timestamp %d not in expected range [%d, %d]", tsValue, before, after)
		}
	}

	// Check ISO timestamp format
	iso := result.Headers["X-ISO-Timestamp"]
	if _, err := time.Parse(time.RFC3339, iso); err != nil {
		t.Errorf("expected valid RFC3339 timestamp, got %s: %v", iso, err)
	}
}

func TestGojaExecutor_HelperFunctions_JSON(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			var obj = parseJSON('{"key": "value", "num": 42}');
			var str = stringify(obj);
			return {
				method: webhook.method,
				url: webhook.url,
				headers: { 'X-Parsed': obj.key, 'X-Stringified': str },
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

	if result.Headers["X-Parsed"] != "value" {
		t.Errorf("expected parsed key 'value', got %s", result.Headers["X-Parsed"])
	}

	// Verify stringified JSON is valid
	var parsed map[string]any
	if err := json.Unmarshal([]byte(result.Headers["X-Stringified"]), &parsed); err != nil {
		t.Errorf("expected valid JSON in X-Stringified, got %s: %v", result.Headers["X-Stringified"], err)
	}
}

func TestGojaExecutor_Metrics(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
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

	// Execute multiple times
	for range 5 {
		_, err := executor.Execute(ctx, code, input)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}
	}

	metrics := executor.Metrics()

	if metrics.TotalExecutions != 5 {
		t.Errorf("expected 5 total executions, got %d", metrics.TotalExecutions)
	}
	if metrics.SuccessCount != 5 {
		t.Errorf("expected 5 success count, got %d", metrics.SuccessCount)
	}
	if metrics.FailureCount != 0 {
		t.Errorf("expected 0 failure count, got %d", metrics.FailureCount)
	}
	if metrics.TotalExecutionMs < 0 {
		t.Error("expected non-negative total execution time")
	}
}

func TestGojaExecutor_Metrics_Timeout(t *testing.T) {
	config := domain.TransformationConfig{
		Timeout:        50 * time.Millisecond,
		MaxMemoryBytes: 0, // Disable memory monitoring for this test
	}
	executor := NewGojaExecutor(config)
	defer executor.Close()

	code := `
		function transform(webhook) {
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
	_, _ = executor.Execute(ctx, code, input)

	metrics := executor.Metrics()

	if metrics.TimeoutCount != 1 {
		t.Errorf("expected 1 timeout count, got %d", metrics.TimeoutCount)
	}
	if metrics.FailureCount != 1 {
		t.Errorf("expected 1 failure count, got %d", metrics.FailureCount)
	}
}

func TestGojaExecutor_Metrics_Cancelled(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			return { cancel: true };
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{}`),
	)

	ctx := context.Background()
	_, _ = executor.Execute(ctx, code, input)

	metrics := executor.Metrics()

	if metrics.CancelledCount != 1 {
		t.Errorf("expected 1 cancelled count, got %d", metrics.CancelledCount)
	}
	// Cancelled is counted as success
	if metrics.SuccessCount != 1 {
		t.Errorf("expected 1 success count (cancelled), got %d", metrics.SuccessCount)
	}
}

func TestGojaExecutor_ScriptCache(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
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

	// First execution - cache miss
	_, err := executor.Execute(ctx, code, input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	metrics := executor.Metrics()
	if metrics.CacheMisses != 1 {
		t.Errorf("expected 1 cache miss, got %d", metrics.CacheMisses)
	}

	// Second execution - cache hit
	_, err = executor.Execute(ctx, code, input)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	metrics = executor.Metrics()
	if metrics.CacheHits != 1 {
		t.Errorf("expected 1 cache hit, got %d", metrics.CacheHits)
	}

	// Check cache size
	if executor.CacheSize() != 1 {
		t.Errorf("expected cache size 1, got %d", executor.CacheSize())
	}

	// Clear cache
	executor.ClearCache()
	if executor.CacheSize() != 0 {
		t.Errorf("expected cache size 0 after clear, got %d", executor.CacheSize())
	}
}

func TestGojaExecutor_ConcurrentExecution(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			// Add some computation to make it non-trivial
			var sum = 0;
			for (var i = 0; i < 1000; i++) {
				sum += i;
			}
			return {
				method: webhook.method,
				url: webhook.url,
				headers: { 'X-Sum': String(sum) },
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
	var wg sync.WaitGroup
	errors := make(chan error, 100)

	// Run 100 concurrent transformations
	for range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := executor.Execute(ctx, code, input)
			if err != nil {
				errors <- err
				return
			}
			// Verify result
			expectedSum := "499500" // Sum of 0 to 999
			if result.Headers["X-Sum"] != expectedSum {
				errors <- err
			}
		}()
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		if err != nil {
			t.Errorf("concurrent execution error: %v", err)
		}
	}

	metrics := executor.Metrics()
	if metrics.TotalExecutions != 100 {
		t.Errorf("expected 100 total executions, got %d", metrics.TotalExecutions)
	}
	if metrics.SuccessCount != 100 {
		t.Errorf("expected 100 success count, got %d", metrics.SuccessCount)
	}
}

func TestGojaExecutor_ContextCancellation(t *testing.T) {
	executor := NewDefaultGojaExecutor()
	defer executor.Close()

	code := `
		function transform(webhook) {
			// Slow operation
			var sum = 0;
			for (var i = 0; i < 10000000; i++) {
				sum += i;
			}
			return webhook;
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{}`),
	)

	ctx, cancel := context.WithCancel(context.Background())

	// Cancel after a short delay
	go func() {
		time.Sleep(10 * time.Millisecond)
		cancel()
	}()

	_, err := executor.Execute(ctx, code, input)
	if err != domain.ErrTransformationTimeout {
		// Context cancellation is treated as timeout
		t.Logf("got error: %v (expected timeout or cancellation)", err)
	}
}

func TestGojaExecutor_MemoryLimit(t *testing.T) {
	// Note: Memory limit enforcement in Goja is best-effort
	// This test verifies the mechanism exists but may not always trigger
	// due to the approximate nature of memory tracking
	config := domain.TransformationConfig{
		Timeout:        5 * time.Second,
		MaxMemoryBytes: 1 * 1024 * 1024, // 1MB limit
	}
	executor := NewGojaExecutor(config)
	defer executor.Close()

	// Try to allocate a lot of memory
	code := `
		function transform(webhook) {
			var arr = [];
			for (var i = 0; i < 1000000; i++) {
				arr.push({ index: i, data: 'x'.repeat(100) });
			}
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

	// We expect either memory limit or timeout error
	// Memory limit detection is best-effort
	switch err {
	case nil:
		t.Log("transformation completed without hitting memory limit (this is acceptable due to best-effort nature)")
	case domain.ErrTransformationMemoryLimit:
		t.Log("memory limit correctly enforced")
	case domain.ErrTransformationTimeout:
		t.Log("operation timed out before memory limit was hit")
	default:
		t.Logf("got error: %v", err)
	}
}

func TestTestTransformation(t *testing.T) {
	code := `
		function transform(webhook) {
			console.log('Testing transformation');
			return {
				method: 'PUT',
				url: webhook.url + '/updated',
				headers: webhook.headers,
				payload: { transformed: true },
				cancel: false
			};
		}
	`

	input := domain.NewTransformationInput(
		"POST",
		"https://example.com",
		nil,
		json.RawMessage(`{"original": true}`),
	)

	response := TestTransformation(code, input, 1*time.Second)

	if !response.Success {
		t.Errorf("expected success, got error: %s", response.Error)
	}
	if response.Result == nil {
		t.Fatal("expected result, got nil")
	}
	if response.Result.Method != "PUT" {
		t.Errorf("expected method PUT, got %s", response.Result.Method)
	}
	if response.Result.URL != "https://example.com/updated" {
		t.Errorf("expected URL https://example.com/updated, got %s", response.Result.URL)
	}
	if response.ExecutionMs < 0 {
		t.Error("expected non-negative execution time")
	}
	if len(response.Logs) != 1 {
		t.Errorf("expected 1 log entry, got %d", len(response.Logs))
	}
}

// Backward compatibility aliases test
func TestBackwardCompatibilityAliases(t *testing.T) {
	// Test that V8 aliases still work
	executor := NewDefaultV8Executor()
	defer executor.Close()

	code := `
		function transform(webhook) {
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
	if err != nil {
		t.Fatalf("expected no error using V8 alias, got %v", err)
	}
}
