package transform

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/dop251/goja"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

// GojaExecutor implements TransformationExecutor using goja (pure Go JavaScript).
type GojaExecutor struct {
	config domain.TransformationConfig
	mu     sync.Mutex
}

// NewGojaExecutor creates a new goja-based transformation executor.
func NewGojaExecutor(config domain.TransformationConfig) *GojaExecutor {
	return &GojaExecutor{
		config: config,
	}
}

// NewDefaultGojaExecutor creates a goja executor with default configuration.
func NewDefaultGojaExecutor() *GojaExecutor {
	return NewGojaExecutor(domain.DefaultTransformationConfig())
}

// Aliases for backward compatibility
var (
	NewV8Executor        = NewGojaExecutor
	NewDefaultV8Executor = NewDefaultGojaExecutor
)

// Execute runs the transformation code with the given input.
func (e *GojaExecutor) Execute(ctx context.Context, code string, input domain.TransformationInput) (domain.TransformationResult, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// Create a new runtime for each execution for security isolation
	vm := goja.New()

	// Set up timeout handling
	timer := time.AfterFunc(e.config.Timeout, func() {
		vm.Interrupt("transformation timeout")
	})
	defer timer.Stop()

	// Also respect context cancellation
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("context cancelled")
		case <-time.After(e.config.Timeout + time.Second):
			// Safety timeout - ensure we don't leak goroutines
		}
	}()

	// Prepare the input as a JavaScript object
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return domain.TransformationResult{}, fmt.Errorf("%w: failed to marshal input: %v", domain.ErrTransformationExecution, err)
	}

	// Build the JavaScript to execute
	script := fmt.Sprintf(`
		(function() {
			var webhook = %s;
			
			// User-provided transform function
			%s
			
			// Call the transform function
			if (typeof transform !== 'function') {
				throw new Error('transform function is not defined');
			}
			
			var result = transform(webhook);
			
			// Validate result
			if (typeof result !== 'object' || result === null) {
				throw new Error('transform must return an object');
			}
			
			// Ensure required fields have defaults
			result.method = result.method || webhook.method;
			result.url = result.url || webhook.url;
			result.headers = result.headers || webhook.headers;
			result.payload = result.payload || webhook.payload;
			result.cancel = Boolean(result.cancel);
			
			return JSON.stringify(result);
		})();
	`, string(inputJSON), code)

	// Execute the script
	val, err := vm.RunString(script)
	if err != nil {
		// Check if it was an interrupt (timeout)
		if interrupted, ok := err.(*goja.InterruptedError); ok {
			if interrupted.Value() == "transformation timeout" || interrupted.Value() == "context cancelled" {
				return domain.TransformationResult{}, domain.ErrTransformationTimeout
			}
		}
		return domain.TransformationResult{}, fmt.Errorf("%w: %v", domain.ErrTransformationExecution, err)
	}

	// Parse the result
	resultJSON := val.String()
	var result domain.TransformationResult
	if err := json.Unmarshal([]byte(resultJSON), &result); err != nil {
		return domain.TransformationResult{}, fmt.Errorf("%w: failed to parse result: %v", domain.ErrTransformationInvalidResult, err)
	}

	// Validate the result
	if err := result.Validate(); err != nil {
		return domain.TransformationResult{}, fmt.Errorf("%w: %v", domain.ErrTransformationInvalidResult, err)
	}

	// Check if delivery was cancelled
	if result.Cancel {
		return result, domain.ErrTransformationCancelled
	}

	return result, nil
}

// Validate checks if the transformation code is syntactically valid.
func (e *GojaExecutor) Validate(code string) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	vm := goja.New()

	// Set a short timeout for validation
	timer := time.AfterFunc(1*time.Second, func() {
		vm.Interrupt("validation timeout")
	})
	defer timer.Stop()

	// Try to parse and check for transform function
	script := fmt.Sprintf(`
		(function() {
			%s
			if (typeof transform !== 'function') {
				throw new Error('transform function is not defined');
			}
			return true;
		})();
	`, code)

	_, err := vm.RunString(script)
	if err != nil {
		if interrupted, ok := err.(*goja.InterruptedError); ok {
			if interrupted.Value() == "validation timeout" {
				return fmt.Errorf("%w: validation timed out", domain.ErrTransformationExecution)
			}
		}
		return fmt.Errorf("%w: %v", domain.ErrTransformationExecution, err)
	}

	return nil
}

// Close releases resources held by the executor.
func (e *GojaExecutor) Close() {
	// Goja runtimes are garbage collected, nothing to clean up
}

// TestTransformation tests a transformation with a sample input.
func TestTransformation(code string, input domain.TransformationInput, timeout time.Duration) domain.TransformationTestResponse {
	start := time.Now()

	config := domain.TransformationConfig{
		Timeout:        timeout,
		MaxMemoryBytes: 128 * 1024 * 1024,
	}
	if timeout == 0 {
		config.Timeout = 1 * time.Second
	}

	executor := NewGojaExecutor(config)
	defer executor.Close()

	ctx := context.Background()
	result, err := executor.Execute(ctx, code, input)

	response := domain.TransformationTestResponse{
		ExecutionMs: time.Since(start).Milliseconds(),
	}

	if err != nil {
		response.Success = false
		if err == domain.ErrTransformationCancelled {
			response.Success = true
			response.Result = &result
		} else {
			response.Error = err.Error()
		}
	} else {
		response.Success = true
		response.Result = &result
	}

	return response
}
