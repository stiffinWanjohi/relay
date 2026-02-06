package delivery

import (
	"context"
	"log/slog"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/logstream"
	"github.com/stiffinWanjohi/relay/internal/observability"
)

// ResultHandler handles delivery success and failure outcomes.
type ResultHandler struct {
	store          *event.Store
	circuit        *CircuitBreaker
	retry          *RetryPolicy
	metrics        *observability.Metrics
	deliveryLogger *logstream.DeliveryLogger
	logger         *slog.Logger
}

// NewResultHandler creates a new ResultHandler.
func NewResultHandler(
	store *event.Store,
	circuit *CircuitBreaker,
	retry *RetryPolicy,
	metrics *observability.Metrics,
	deliveryLogger *logstream.DeliveryLogger,
	logger *slog.Logger,
) *ResultHandler {
	return &ResultHandler{
		store:          store,
		circuit:        circuit,
		retry:          retry,
		metrics:        metrics,
		deliveryLogger: deliveryLogger,
		logger:         logger,
	}
}

// HandleSuccess processes a successful delivery.
func (h *ResultHandler) HandleSuccess(
	ctx context.Context,
	evt domain.Event,
	endpoint *domain.Endpoint,
	circuitKey string,
	result domain.DeliveryResult,
) domain.Event {
	duration := time.Duration(result.DurationMs) * time.Millisecond

	h.logger.Info("delivery successful", "attempts", evt.Attempts)

	// Mark as delivered
	evt = evt.MarkDelivered()
	if h.store != nil {
		if _, err := h.store.Update(ctx, evt); err != nil {
			h.logger.Error("failed to update event status", "error", err)
		}
	}

	// Record success for circuit breaker
	h.circuit.RecordSuccess(circuitKey)

	// Record metrics
	if h.metrics != nil {
		destHost := extractHost(evt.Destination)
		h.metrics.EventDelivered(ctx, evt.ClientID, destHost, duration)
	}

	// Stream log entry
	if h.deliveryLogger != nil {
		endpointID := ""
		if endpoint != nil {
			endpointID = endpoint.ID.String()
		}
		h.deliveryLogger.LogDeliverySuccess(
			ctx,
			evt.ID.String(),
			evt.EventType,
			endpointID,
			evt.Destination,
			evt.ClientID,
			result.StatusCode,
			result.DurationMs,
			evt.Attempts,
			evt.MaxAttempts,
		)
	}

	return evt
}

// HandleFailure processes a failed delivery and determines retry behavior.
// Returns the updated event and the retry delay (0 if no retry).
func (h *ResultHandler) HandleFailure(
	ctx context.Context,
	evt domain.Event,
	endpoint *domain.Endpoint,
	circuitKey string,
	result domain.DeliveryResult,
) (domain.Event, time.Duration) {
	h.logger.Warn("delivery failed",
		"attempts", evt.Attempts,
		"status_code", result.StatusCode,
		"error", result.Error,
	)

	// Record failure for circuit breaker
	h.circuit.RecordFailure(circuitKey)

	// Record failure metric
	if h.metrics != nil {
		reason := classifyFailureReason(result)
		h.metrics.EventFailed(ctx, evt.ClientID, reason)
	}

	// Check if we should retry
	shouldRetry := evt.ShouldRetry() && h.retry.ShouldRetryForEndpoint(evt.Attempts, endpoint)

	// Stream log entry
	if h.deliveryLogger != nil {
		endpointID := ""
		if endpoint != nil {
			endpointID = endpoint.ID.String()
		}
		errMsg := ""
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		h.deliveryLogger.LogDeliveryFailure(
			ctx,
			evt.ID.String(),
			evt.EventType,
			endpointID,
			evt.Destination,
			evt.ClientID,
			result.StatusCode,
			result.DurationMs,
			errMsg,
			evt.Attempts,
			evt.MaxAttempts,
			shouldRetry,
		)
	}

	if shouldRetry {
		// Calculate delay using endpoint-specific backoff
		delay := h.retry.NextRetryDelayForEndpoint(evt.Attempts, endpoint)
		nextAttempt := time.Now().UTC().Add(delay)

		h.logger.Info("scheduling retry", "delay", delay, "next_attempt_at", nextAttempt)

		// Record retry metric
		if h.metrics != nil {
			h.metrics.EventRetry(ctx, evt.ClientID, evt.Attempts)
		}

		evt = evt.MarkFailed(nextAttempt)
		if h.store != nil {
			if _, err := h.store.Update(ctx, evt); err != nil {
				h.logger.Error("failed to update event status", "error", err)
			}
		}

		return evt, delay
	}

	// No more retries, mark as dead
	h.logger.Warn("max retries exceeded, marking as dead")

	evt = evt.MarkDead()
	if h.store != nil {
		if _, err := h.store.Update(ctx, evt); err != nil {
			h.logger.Error("failed to update event status", "error", err)
		}
	}

	return evt, 0
}

// CreateDeliveryAttempt creates a delivery attempt record in the store.
func (h *ResultHandler) CreateDeliveryAttempt(ctx context.Context, evt domain.Event, result domain.DeliveryResult) {
	if h.store == nil {
		return
	}

	attempt := domain.NewDeliveryAttempt(evt.ID, evt.Attempts)
	if result.Success {
		attempt = attempt.WithSuccess(result.StatusCode, result.ResponseBody, result.DurationMs)
	} else {
		errMsg := ""
		if result.Error != nil {
			errMsg = result.Error.Error()
		}
		attempt = attempt.WithFailure(result.StatusCode, result.ResponseBody, errMsg, result.DurationMs)
	}

	if _, err := h.store.CreateDeliveryAttempt(ctx, attempt); err != nil {
		h.logger.Error("failed to create delivery attempt", "error", err)
	}
}
