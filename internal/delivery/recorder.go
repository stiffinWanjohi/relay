package delivery

import (
	"context"
	"log/slog"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/metrics"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

// Recorder handles delivery metrics recording.
type Recorder struct {
	metrics      *observability.Metrics
	metricsStore *metrics.Store
	logger       *slog.Logger
}

// NewRecorder creates a new Recorder.
func NewRecorder(m *observability.Metrics, store *metrics.Store, logger *slog.Logger) *Recorder {
	return &Recorder{
		metrics:      m,
		metricsStore: store,
		logger:       logger,
	}
}

// RecordDelivery records delivery metrics to the metrics store.
func (r *Recorder) RecordDelivery(ctx context.Context, evt domain.Event, endpoint *domain.Endpoint, result domain.DeliveryResult) {
	if r.metricsStore == nil {
		return
	}

	// Determine outcome
	var outcome metrics.DeliveryOutcome
	switch {
	case result.Success:
		outcome = metrics.OutcomeSuccess
	case result.Error != nil && containsSubstr(result.Error.Error(), "timeout"):
		outcome = metrics.OutcomeTimeout
	default:
		outcome = metrics.OutcomeFailure
	}

	// Build record
	record := metrics.DeliveryRecord{
		EventID:    evt.ID.String(),
		Outcome:    outcome,
		StatusCode: result.StatusCode,
		LatencyMs:  result.DurationMs,
		AttemptNum: evt.Attempts,
		ClientID:   evt.ClientID,
	}

	// Add endpoint ID if available
	if endpoint != nil {
		record.EndpointID = endpoint.ID.String()
	}

	// Add event type if available
	if evt.EventType != "" {
		record.EventType = evt.EventType
	}

	// Add error message if present
	if result.Error != nil {
		record.Error = result.Error.Error()
	}

	// Record the delivery asynchronously to avoid blocking the delivery path
	go func() {
		if err := r.metricsStore.RecordDelivery(context.Background(), record); err != nil {
			if r.logger != nil {
				r.logger.Warn("failed to record delivery metrics", "error", err)
			}
		}
	}()
}

// RecordRateLimit records a rate limit event.
func (r *Recorder) RecordRateLimit(ctx context.Context, endpoint *domain.Endpoint, evt domain.Event) {
	if r.metricsStore == nil || endpoint == nil {
		return
	}

	_ = r.metricsStore.RecordRateLimit(ctx, metrics.RateLimitEvent{
		EndpointID: endpoint.ID.String(),
		EventID:    evt.ID.String(),
		Limit:      endpoint.RateLimitPerSec,
	})
}
