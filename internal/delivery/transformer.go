package delivery

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/transform"
)

// Transformer wraps payload transformation logic.
type Transformer struct {
	executor domain.TransformationExecutor
}

// NewTransformer creates a new Transformer with the default V8 executor.
func NewTransformer() *Transformer {
	return &Transformer{
		executor: transform.NewDefaultV8Executor(),
	}
}

// NewTransformerWithExecutor creates a new Transformer with a custom executor.
func NewTransformerWithExecutor(executor domain.TransformationExecutor) *Transformer {
	return &Transformer{
		executor: executor,
	}
}

// Apply applies the endpoint's transformation to the event.
// Returns the transformed event or an error if transformation fails.
// If the endpoint has no transformation configured, returns the original event unchanged.
func (t *Transformer) Apply(ctx context.Context, evt domain.Event, endpoint *domain.Endpoint, logger *slog.Logger) (domain.Event, error) {
	if endpoint == nil || !endpoint.HasTransformation() {
		return evt, nil
	}

	// Build transformation input
	input := domain.NewTransformationInput(
		"POST",
		evt.Destination,
		evt.Headers,
		evt.Payload,
	)

	// Execute transformation
	result, err := t.executor.Execute(ctx, endpoint.Transformation, input)
	if err != nil {
		return evt, err
	}

	// Apply transformation result to event
	transformedEvt := evt

	// Update destination URL if changed
	if result.URL != evt.Destination {
		transformedEvt.Destination = result.URL
	}

	// Update headers
	if result.Headers != nil {
		transformedEvt.Headers = result.Headers
	}

	// Update payload
	if result.Payload != nil {
		transformedEvt.Payload = json.RawMessage(result.Payload)
	}

	if logger != nil {
		logger.Debug("transformation applied",
			slog.String("original_url", evt.Destination),
			slog.String("transformed_url", transformedEvt.Destination),
		)
	}

	return transformedEvt, nil
}
