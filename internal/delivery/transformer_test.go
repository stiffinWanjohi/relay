package delivery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stiffinWanjohi/relay/internal/domain"
)

func TestNewTransformer(t *testing.T) {
	t.Parallel()

	transformer := NewTransformer()
	require.NotNil(t, transformer)
	assert.NotNil(t, transformer.executor)
}

func TestNewTransformerWithExecutor(t *testing.T) {
	t.Parallel()

	mockExecutor := &transformerMockExecutor{}
	transformer := NewTransformerWithExecutor(mockExecutor)

	require.NotNil(t, transformer)
	assert.Same(t, mockExecutor, transformer.executor)
}

func TestTransformer_Apply(t *testing.T) {
	t.Parallel()

	baseEvent := domain.Event{
		ID:          uuid.New(),
		Destination: "https://original.example.com/webhook",
		Headers:     map[string]string{"Content-Type": "application/json"},
		Payload:     json.RawMessage(`{"key": "value"}`),
	}

	tests := []struct {
		name           string
		event          domain.Event
		endpoint       *domain.Endpoint
		executorResult domain.TransformationResult
		executorErr    error
		wantEvent      domain.Event
		wantErr        bool
	}{
		{
			name:     "nil endpoint returns original event",
			event:    baseEvent,
			endpoint: nil,
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: baseEvent.Destination,
				Headers:     baseEvent.Headers,
				Payload:     baseEvent.Payload,
			},
			wantErr: false,
		},
		{
			name:  "endpoint without transformation returns original event",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "", // empty string = no transformation
			},
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: baseEvent.Destination,
				Headers:     baseEvent.Headers,
				Payload:     baseEvent.Payload,
			},
			wantErr: false,
		},
		{
			name:  "transformation changes URL",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "return { url: 'https://new.example.com/webhook' }",
			},
			executorResult: domain.TransformationResult{
				URL:     "https://new.example.com/webhook",
				Headers: baseEvent.Headers,
				Payload: json.RawMessage(`{"key": "value"}`),
			},
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: "https://new.example.com/webhook",
				Headers:     baseEvent.Headers,
				Payload:     baseEvent.Payload,
			},
			wantErr: false,
		},
		{
			name:  "transformation changes headers",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "return { headers: { 'X-Custom': 'value' } }",
			},
			executorResult: domain.TransformationResult{
				URL:     baseEvent.Destination,
				Headers: map[string]string{"X-Custom": "value"},
				Payload: json.RawMessage(`{"key": "value"}`),
			},
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: baseEvent.Destination,
				Headers:     map[string]string{"X-Custom": "value"},
				Payload:     baseEvent.Payload,
			},
			wantErr: false,
		},
		{
			name:  "transformation changes payload",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "return { body: { modified: true } }",
			},
			executorResult: domain.TransformationResult{
				URL:     baseEvent.Destination,
				Headers: baseEvent.Headers,
				Payload: json.RawMessage(`{"modified": true}`),
			},
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: baseEvent.Destination,
				Headers:     baseEvent.Headers,
				Payload:     json.RawMessage(`{"modified": true}`),
			},
			wantErr: false,
		},
		{
			name:  "transformation changes everything",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "return { url: 'https://new.example.com', headers: { 'X-New': 'header' }, body: { all: 'new' } }",
			},
			executorResult: domain.TransformationResult{
				URL:     "https://new.example.com",
				Headers: map[string]string{"X-New": "header"},
				Payload: json.RawMessage(`{"all": "new"}`),
			},
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: "https://new.example.com",
				Headers:     map[string]string{"X-New": "header"},
				Payload:     json.RawMessage(`{"all": "new"}`),
			},
			wantErr: false,
		},
		{
			name:  "transformation error returns error",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "throw new Error('test error')",
			},
			executorErr: errors.New("transformation failed: test error"),
			wantErr:     true,
		},
		{
			name:  "nil headers in result keeps original",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "return { url: request.url }",
			},
			executorResult: domain.TransformationResult{
				URL:     baseEvent.Destination,
				Headers: nil,
				Payload: json.RawMessage(`{"key": "value"}`),
			},
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: baseEvent.Destination,
				Headers:     baseEvent.Headers, // original kept when nil
				Payload:     baseEvent.Payload,
			},
			wantErr: false,
		},
		{
			name:  "nil payload in result keeps original",
			event: baseEvent,
			endpoint: &domain.Endpoint{
				ID:             uuid.New(),
				Transformation: "return { url: request.url }",
			},
			executorResult: domain.TransformationResult{
				URL:     baseEvent.Destination,
				Headers: baseEvent.Headers,
				Payload: nil,
			},
			wantEvent: domain.Event{
				ID:          baseEvent.ID,
				Destination: baseEvent.Destination,
				Headers:     baseEvent.Headers,
				Payload:     baseEvent.Payload, // original kept when nil
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			mockExecutor := &transformerMockExecutor{
				result: tt.executorResult,
				err:    tt.executorErr,
			}

			transformer := NewTransformerWithExecutor(mockExecutor)
			logger := slog.Default()

			result, err := transformer.Apply(context.Background(), tt.event, tt.endpoint, logger)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.wantEvent.ID, result.ID)
			assert.Equal(t, tt.wantEvent.Destination, result.Destination)
			assert.Equal(t, tt.wantEvent.Headers, result.Headers)
			if tt.wantEvent.Payload != nil {
				assert.JSONEq(t, string(tt.wantEvent.Payload), string(result.Payload))
			}
		})
	}
}

func TestTransformer_Apply_WithNilLogger(t *testing.T) {
	t.Parallel()

	event := domain.Event{
		ID:          uuid.New(),
		Destination: "https://example.com",
		Payload:     json.RawMessage(`{}`),
	}

	endpoint := &domain.Endpoint{
		ID:             uuid.New(),
		Transformation: "return { url: 'https://new.example.com' }",
	}

	mockExecutor := &transformerMockExecutor{
		result: domain.TransformationResult{
			URL:     "https://new.example.com",
			Payload: json.RawMessage(`{}`),
		},
	}

	transformer := NewTransformerWithExecutor(mockExecutor)

	// Should not panic with nil logger
	result, err := transformer.Apply(context.Background(), event, endpoint, nil)
	require.NoError(t, err)
	assert.Equal(t, "https://new.example.com", result.Destination)
}

func TestTransformer_Apply_ConcurrentExecution(t *testing.T) {
	t.Parallel()

	mockExecutor := &transformerMockExecutor{
		result: domain.TransformationResult{
			URL:     "https://transformed.example.com",
			Headers: map[string]string{"X-Test": "value"},
			Payload: json.RawMessage(`{"transformed": true}`),
		},
	}

	transformer := NewTransformerWithExecutor(mockExecutor)
	endpoint := &domain.Endpoint{
		ID:             uuid.New(),
		Transformation: "return {}",
	}

	const goroutines = 100
	results := make(chan error, goroutines)

	for i := range goroutines {
		go func(i int) {
			event := domain.Event{
				ID:          uuid.New(),
				Destination: "https://original.example.com",
				Payload:     json.RawMessage(`{}`),
			}
			_, err := transformer.Apply(context.Background(), event, endpoint, nil)
			results <- err
		}(i)
	}

	for range goroutines {
		err := <-results
		assert.NoError(t, err)
	}
}

// Mock executor for testing - unique name to avoid conflicts

type transformerMockExecutor struct {
	result    domain.TransformationResult
	err       error
	callCount int
	lastInput domain.TransformationInput
	lastCode  string
	mu        sync.Mutex
}

func (m *transformerMockExecutor) Execute(ctx context.Context, code string, input domain.TransformationInput) (domain.TransformationResult, error) {
	m.mu.Lock()
	m.callCount++
	m.lastInput = input
	m.lastCode = code
	m.mu.Unlock()

	if m.err != nil {
		return domain.TransformationResult{}, m.err
	}
	// Return configured result
	return m.result, nil
}

func (m *transformerMockExecutor) Validate(code string) error {
	return nil
}

func (m *transformerMockExecutor) Close() {}
