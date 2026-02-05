package metrics

import (
	"context"
	"time"

	"github.com/stiffinWanjohi/relay/internal/alerting"
)

// Ensure AlertingAdapter implements alerting.MetricsProvider.
var _ alerting.MetricsProvider = (*AlertingAdapter)(nil)

// AlertingAdapter adapts the metrics Store to the alerting.MetricsProvider interface.
type AlertingAdapter struct {
	store *Store
}

// NewAlertingAdapter creates a new alerting adapter.
func NewAlertingAdapter(store *Store) *AlertingAdapter {
	return &AlertingAdapter{store: store}
}

// GetFailureRate returns the failure rate within the given time window.
func (a *AlertingAdapter) GetFailureRate(ctx context.Context, window time.Duration) (float64, error) {
	return a.store.GetFailureRate(ctx, window)
}

// GetAverageLatency returns the average latency in milliseconds within the given time window.
func (a *AlertingAdapter) GetAverageLatency(ctx context.Context, window time.Duration) (float64, error) {
	return a.store.GetAverageLatency(ctx, window)
}

// GetQueueDepth returns the current queue depth.
func (a *AlertingAdapter) GetQueueDepth(ctx context.Context) (int64, error) {
	return a.store.GetQueueDepth(ctx)
}

// GetErrorCount returns the number of errors within the given time window.
func (a *AlertingAdapter) GetErrorCount(ctx context.Context, window time.Duration) (int64, error) {
	return a.store.GetErrorCount(ctx, window)
}

// GetSuccessRate returns the success rate within the given time window.
func (a *AlertingAdapter) GetSuccessRate(ctx context.Context, window time.Duration) (float64, error) {
	return a.store.GetSuccessRate(ctx, window)
}

// GetDeliveryCount returns the number of deliveries within the given time window.
func (a *AlertingAdapter) GetDeliveryCount(ctx context.Context, window time.Duration) (int64, error) {
	return a.store.GetDeliveryCount(ctx, window)
}
