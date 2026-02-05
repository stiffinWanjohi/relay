package transform

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"
	"time"

	"github.com/dop251/goja"
	"github.com/google/uuid"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

// CompiledScript holds a pre-compiled script for reuse.
type CompiledScript struct {
	program    *goja.Program
	code       string
	compiledAt time.Time
}

// TransformationMetrics tracks transformation execution metrics.
type TransformationMetrics struct {
	TotalExecutions  int64
	SuccessCount     int64
	FailureCount     int64
	TimeoutCount     int64
	CancelledCount   int64
	MemoryLimitCount int64
	TotalExecutionMs int64
	LastExecutionMs  int64
	CacheHits        int64
	CacheMisses      int64
	mu               sync.RWMutex
}

// Snapshot returns a copy of the current metrics.
func (m *TransformationMetrics) Snapshot() TransformationMetrics {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return TransformationMetrics{
		TotalExecutions:  m.TotalExecutions,
		SuccessCount:     m.SuccessCount,
		FailureCount:     m.FailureCount,
		TimeoutCount:     m.TimeoutCount,
		CancelledCount:   m.CancelledCount,
		MemoryLimitCount: m.MemoryLimitCount,
		TotalExecutionMs: m.TotalExecutionMs,
		LastExecutionMs:  m.LastExecutionMs,
		CacheHits:        m.CacheHits,
		CacheMisses:      m.CacheMisses,
	}
}

func (m *TransformationMetrics) recordExecution(durationMs int64, success bool, timeout bool, cancelled bool, memoryLimit bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.TotalExecutions++
	m.TotalExecutionMs += durationMs
	m.LastExecutionMs = durationMs
	if memoryLimit {
		m.MemoryLimitCount++
		m.FailureCount++
	} else if timeout {
		m.TimeoutCount++
		m.FailureCount++
	} else if cancelled {
		m.CancelledCount++
		m.SuccessCount++ // Cancelled is considered a "success" - transformation worked as intended
	} else if success {
		m.SuccessCount++
	} else {
		m.FailureCount++
	}
}

func (m *TransformationMetrics) recordCacheHit() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CacheHits++
}

func (m *TransformationMetrics) recordCacheMiss() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CacheMisses++
}

// GojaExecutor implements TransformationExecutor using goja (pure Go JavaScript).
type GojaExecutor struct {
	config       domain.TransformationConfig
	scriptCache  map[string]*CompiledScript
	cacheMu      sync.RWMutex
	metrics      *TransformationMetrics
	maxCacheSize int
}

// NewGojaExecutor creates a new goja-based transformation executor.
func NewGojaExecutor(config domain.TransformationConfig) *GojaExecutor {
	return &GojaExecutor{
		config:       config,
		scriptCache:  make(map[string]*CompiledScript),
		metrics:      &TransformationMetrics{},
		maxCacheSize: 1000, // Cache up to 1000 compiled scripts
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

// Metrics returns the current transformation metrics.
func (e *GojaExecutor) Metrics() TransformationMetrics {
	return e.metrics.Snapshot()
}

// getCompiledScript retrieves a compiled script from cache or compiles it.
func (e *GojaExecutor) getCompiledScript(code string) (*goja.Program, error) {
	e.cacheMu.RLock()
	cached, exists := e.scriptCache[code]
	e.cacheMu.RUnlock()

	if exists {
		e.metrics.recordCacheHit()
		return cached.program, nil
	}

	e.metrics.recordCacheMiss()

	// Wrap the user code in our execution template
	wrappedCode := fmt.Sprintf(`
		(function() {
			// User-provided transform function
			%s

			// Return the transform function for later use
			if (typeof transform !== 'function') {
				throw new Error('transform function is not defined');
			}
			return transform;
		})();
	`, code)

	program, err := goja.Compile("transformation", wrappedCode, true)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", domain.ErrTransformationExecution, err)
	}

	e.cacheMu.Lock()
	// Evict oldest entries if cache is full
	if len(e.scriptCache) >= e.maxCacheSize {
		// Simple eviction: remove ~10% of entries
		toRemove := e.maxCacheSize / 10
		removed := 0
		for key := range e.scriptCache {
			if removed >= toRemove {
				break
			}
			delete(e.scriptCache, key)
			removed++
		}
	}
	e.scriptCache[code] = &CompiledScript{
		program:    program,
		code:       code,
		compiledAt: time.Now(),
	}
	e.cacheMu.Unlock()

	return program, nil
}

// setupHelpers adds built-in helper functions to the VM.
func (e *GojaExecutor) setupHelpers(vm *goja.Runtime) {
	// Base64 encoding/decoding
	vm.Set("base64Encode", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		input := call.Arguments[0].String()
		encoded := base64.StdEncoding.EncodeToString([]byte(input))
		return vm.ToValue(encoded)
	})

	vm.Set("base64Decode", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		input := call.Arguments[0].String()
		decoded, err := base64.StdEncoding.DecodeString(input)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(string(decoded))
	})

	// HMAC-SHA256 for signing
	vm.Set("hmacSha256", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			panic(vm.NewGoError(fmt.Errorf("hmacSha256 requires 2 arguments: message and key")))
		}
		message := call.Arguments[0].String()
		key := call.Arguments[1].String()
		h := hmac.New(sha256.New, []byte(key))
		h.Write([]byte(message))
		signature := hex.EncodeToString(h.Sum(nil))
		return vm.ToValue(signature)
	})

	// SHA256 hash
	vm.Set("sha256", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("")
		}
		input := call.Arguments[0].String()
		hash := sha256.Sum256([]byte(input))
		return vm.ToValue(hex.EncodeToString(hash[:]))
	})

	// UUID generation
	vm.Set("uuid", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(uuid.New().String())
	})

	// Current timestamp (Unix milliseconds)
	vm.Set("now", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().UnixMilli())
	})

	// ISO timestamp string
	vm.Set("isoTimestamp", func(call goja.FunctionCall) goja.Value {
		return vm.ToValue(time.Now().UTC().Format(time.RFC3339))
	})

	// JSON parse (safer than built-in for our use case)
	vm.Set("parseJSON", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return goja.Null()
		}
		input := call.Arguments[0].String()
		var result any
		if err := json.Unmarshal([]byte(input), &result); err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(result)
	})

	// JSON stringify
	vm.Set("stringify", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) == 0 {
			return vm.ToValue("null")
		}
		val := call.Arguments[0].Export()
		bytes, err := json.Marshal(val)
		if err != nil {
			panic(vm.NewGoError(err))
		}
		return vm.ToValue(string(bytes))
	})
}

// ConsoleCapture captures console.log calls during execution.
type ConsoleCapture struct {
	Logs []string
	mu   sync.Mutex
}

func (c *ConsoleCapture) append(msg string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Logs = append(c.Logs, msg)
}

// setupConsole adds console.log support to the VM.
func (e *GojaExecutor) setupConsole(vm *goja.Runtime, capture *ConsoleCapture) {
	console := vm.NewObject()

	logFunc := func(call goja.FunctionCall) goja.Value {
		args := make([]any, len(call.Arguments))
		for i, arg := range call.Arguments {
			args[i] = arg.Export()
		}
		msg := fmt.Sprint(args...)
		capture.append(msg)
		return goja.Undefined()
	}

	console.Set("log", logFunc)
	console.Set("info", logFunc)
	console.Set("warn", logFunc)
	console.Set("error", logFunc)
	console.Set("debug", logFunc)

	vm.Set("console", console)
}

// Execute runs the transformation code with the given input.
// Each execution runs in an isolated VM - no shared state between executions.
func (e *GojaExecutor) Execute(ctx context.Context, code string, input domain.TransformationInput) (domain.TransformationResult, error) {
	return e.ExecuteWithLogs(ctx, code, input, nil)
}

// ExecuteWithLogs runs the transformation and captures console.log output.
func (e *GojaExecutor) ExecuteWithLogs(ctx context.Context, code string, input domain.TransformationInput, logs *[]string) (domain.TransformationResult, error) {
	start := time.Now()

	// Create a new runtime for each execution - provides complete isolation
	// This is safe for concurrent use as each execution gets its own VM
	vm := goja.New()

	// Set up timeout handling
	done := make(chan struct{})
	timer := time.AfterFunc(e.config.Timeout, func() {
		vm.Interrupt("transformation timeout")
	})

	// Set up memory monitoring if limit is configured
	var memoryMonitorStop chan struct{}
	if e.config.MaxMemoryBytes > 0 {
		memoryMonitorStop = make(chan struct{})
		go e.monitorMemory(vm, memoryMonitorStop)
	}

	// Also respect context cancellation
	go func() {
		select {
		case <-ctx.Done():
			vm.Interrupt("context cancelled")
		case <-done:
			// Execution completed, exit goroutine
		}
	}()

	defer func() {
		timer.Stop()
		if memoryMonitorStop != nil {
			close(memoryMonitorStop)
		}
		close(done)
	}()

	// Set up console capture
	capture := &ConsoleCapture{}
	e.setupConsole(vm, capture)

	// Set up built-in helpers
	e.setupHelpers(vm)

	// Get or compile the script
	program, err := e.getCompiledScript(code)
	if err != nil {
		e.metrics.recordExecution(time.Since(start).Milliseconds(), false, false, false, false)
		return domain.TransformationResult{}, err
	}

	// Run the compiled program to get the transform function
	transformVal, err := vm.RunProgram(program)
	if err != nil {
		durationMs := time.Since(start).Milliseconds()
		if isTimeout(err) {
			e.metrics.recordExecution(durationMs, false, true, false, false)
			return domain.TransformationResult{}, domain.ErrTransformationTimeout
		}
		if isMemoryLimit(err) {
			e.metrics.recordExecution(durationMs, false, false, false, true)
			return domain.TransformationResult{}, domain.ErrTransformationMemoryLimit
		}
		e.metrics.recordExecution(durationMs, false, false, false, false)
		return domain.TransformationResult{}, fmt.Errorf("%w: %v", domain.ErrTransformationExecution, err)
	}

	// Get the transform function
	transformFunc, ok := goja.AssertFunction(transformVal)
	if !ok {
		e.metrics.recordExecution(time.Since(start).Milliseconds(), false, false, false, false)
		return domain.TransformationResult{}, fmt.Errorf("%w: transform is not a function", domain.ErrTransformationExecution)
	}

	// Convert input to JS object
	inputObj := vm.NewObject()
	inputObj.Set("method", input.Method)
	inputObj.Set("url", input.URL)
	inputObj.Set("headers", input.Headers)

	// Parse payload JSON to JS object
	var payloadObj any
	if len(input.Payload) > 0 {
		if err := json.Unmarshal(input.Payload, &payloadObj); err != nil {
			e.metrics.recordExecution(time.Since(start).Milliseconds(), false, false, false, false)
			return domain.TransformationResult{}, fmt.Errorf("%w: failed to parse input payload: %v", domain.ErrTransformationExecution, err)
		}
	}
	inputObj.Set("payload", payloadObj)

	// Call the transform function
	resultVal, err := transformFunc(goja.Undefined(), inputObj)
	if err != nil {
		durationMs := time.Since(start).Milliseconds()
		if isTimeout(err) {
			e.metrics.recordExecution(durationMs, false, true, false, false)
			return domain.TransformationResult{}, domain.ErrTransformationTimeout
		}
		if isMemoryLimit(err) {
			e.metrics.recordExecution(durationMs, false, false, false, true)
			return domain.TransformationResult{}, domain.ErrTransformationMemoryLimit
		}
		e.metrics.recordExecution(durationMs, false, false, false, false)
		return domain.TransformationResult{}, fmt.Errorf("%w: %v", domain.ErrTransformationExecution, err)
	}

	// Capture logs if requested
	if logs != nil {
		*logs = capture.Logs
	}

	// Export the result
	resultExport := resultVal.Export()
	resultMap, ok := resultExport.(map[string]any)
	if !ok {
		e.metrics.recordExecution(time.Since(start).Milliseconds(), false, false, false, false)
		return domain.TransformationResult{}, fmt.Errorf("%w: transform must return an object", domain.ErrTransformationInvalidResult)
	}

	// Build the result with defaults
	result := domain.TransformationResult{
		Method:  input.Method,
		URL:     input.URL,
		Headers: input.Headers,
		Payload: input.Payload,
		Cancel:  false,
	}

	// Apply returned values
	if method, ok := resultMap["method"].(string); ok && method != "" {
		result.Method = method
	}
	if url, ok := resultMap["url"].(string); ok && url != "" {
		result.URL = url
	}
	if headers, ok := resultMap["headers"].(map[string]any); ok {
		result.Headers = make(map[string]string)
		for k, v := range headers {
			if str, ok := v.(string); ok {
				result.Headers[k] = str
			}
		}
	}
	if payload, exists := resultMap["payload"]; exists && payload != nil {
		payloadBytes, err := json.Marshal(payload)
		if err != nil {
			e.metrics.recordExecution(time.Since(start).Milliseconds(), false, false, false, false)
			return domain.TransformationResult{}, fmt.Errorf("%w: failed to marshal result payload: %v", domain.ErrTransformationInvalidResult, err)
		}
		result.Payload = payloadBytes
	}
	if cancel, ok := resultMap["cancel"].(bool); ok {
		result.Cancel = cancel
	}

	// Validate the result
	if err := result.Validate(); err != nil {
		e.metrics.recordExecution(time.Since(start).Milliseconds(), false, false, false, false)
		return domain.TransformationResult{}, fmt.Errorf("%w: %v", domain.ErrTransformationInvalidResult, err)
	}

	durationMs := time.Since(start).Milliseconds()

	// Check if delivery was cancelled
	if result.Cancel {
		e.metrics.recordExecution(durationMs, true, false, true, false)
		return result, domain.ErrTransformationCancelled
	}

	e.metrics.recordExecution(durationMs, true, false, false, false)
	return result, nil
}

// isTimeout checks if the error is a timeout interrupt.
func isTimeout(err error) bool {
	if interrupted, ok := err.(*goja.InterruptedError); ok {
		val := interrupted.Value()
		if str, ok := val.(string); ok {
			return str == "transformation timeout" || str == "context cancelled"
		}
	}
	return false
}

// isMemoryLimit checks if the error is a memory limit interrupt.
func isMemoryLimit(err error) bool {
	if interrupted, ok := err.(*goja.InterruptedError); ok {
		val := interrupted.Value()
		if str, ok := val.(string); ok {
			return str == "memory limit exceeded"
		}
	}
	return false
}

// monitorMemory periodically checks memory usage and interrupts the VM if exceeded.
// This is a best-effort approach since Goja doesn't have native memory limits.
// It uses Go's runtime memory stats to estimate JS memory usage.
func (e *GojaExecutor) monitorMemory(vm *goja.Runtime, stop chan struct{}) {
	ticker := time.NewTicker(10 * time.Millisecond) // Check every 10ms
	defer ticker.Stop()

	var baseline runtime.MemStats
	runtime.ReadMemStats(&baseline)

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			var current runtime.MemStats
			runtime.ReadMemStats(&current)

			// Estimate memory used by this execution
			// This is approximate - tracks total Go heap growth
			usedBytes := int64(current.HeapAlloc) - int64(baseline.HeapAlloc)
			if usedBytes < 0 {
				usedBytes = 0 // GC may have run
			}

			if usedBytes > e.config.MaxMemoryBytes {
				vm.Interrupt("memory limit exceeded")
				return
			}
		}
	}
}

// Validate checks if the transformation code is syntactically valid.
func (e *GojaExecutor) Validate(code string) error {
	vm := goja.New()

	// Set a short timeout for validation
	timer := time.AfterFunc(1*time.Second, func() {
		vm.Interrupt("validation timeout")
	})
	defer timer.Stop()

	// Try to compile and check for transform function
	_, err := e.getCompiledScript(code)
	if err != nil {
		return err
	}

	// Also verify it actually defines a transform function by running
	script := fmt.Sprintf(`
		(function() {
			%s
			if (typeof transform !== 'function') {
				throw new Error('transform function is not defined');
			}
			return true;
		})();
	`, code)

	_, err = vm.RunString(script)
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

// ClearCache removes all cached compiled scripts.
func (e *GojaExecutor) ClearCache() {
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	e.scriptCache = make(map[string]*CompiledScript)
}

// CacheSize returns the number of cached scripts.
func (e *GojaExecutor) CacheSize() int {
	e.cacheMu.RLock()
	defer e.cacheMu.RUnlock()
	return len(e.scriptCache)
}

// Close releases resources held by the executor.
func (e *GojaExecutor) Close() {
	e.ClearCache()
	// Force a GC to clean up any lingering VM memory
	runtime.GC()
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
	var logs []string
	result, err := executor.ExecuteWithLogs(ctx, code, input, &logs)

	response := domain.TransformationTestResponse{
		ExecutionMs: time.Since(start).Milliseconds(),
		Logs:        logs,
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
