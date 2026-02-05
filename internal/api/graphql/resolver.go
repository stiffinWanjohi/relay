package graphql

import (
	"log/slog"

	"github.com/stiffinWanjohi/relay/internal/alerting"
	"github.com/stiffinWanjohi/relay/internal/connector"
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/eventtype"
	"github.com/stiffinWanjohi/relay/internal/metrics"
	"github.com/stiffinWanjohi/relay/internal/queue"
	"github.com/stiffinWanjohi/relay/internal/transform"
)

// Resolver is the root resolver.
type Resolver struct {
	Store             *event.Store
	EventTypeStore    *eventtype.Store
	Queue             *queue.Queue
	Dedup             *dedup.Checker
	Transformer       domain.TransformationExecutor
	MetricsStore      *metrics.Store
	AlertEngine       *alerting.Engine
	ConnectorRegistry *connector.Registry
	Logger            *slog.Logger
}

// NewResolver creates a new resolver.
func NewResolver(store *event.Store, eventTypeStore *eventtype.Store, q *queue.Queue, d *dedup.Checker) *Resolver {
	return &Resolver{
		Store:          store,
		EventTypeStore: eventTypeStore,
		Queue:          q,
		Dedup:          d,
		Transformer:    transform.NewDefaultV8Executor(),
		Logger:         slog.Default(),
	}
}

// WithMetricsStore sets the metrics store for analytics queries.
func (r *Resolver) WithMetricsStore(store *metrics.Store) *Resolver {
	r.MetricsStore = store
	return r
}

// WithAlertEngine sets the alerting engine for alert rule management.
func (r *Resolver) WithAlertEngine(engine *alerting.Engine) *Resolver {
	r.AlertEngine = engine
	return r
}

// WithConnectorRegistry sets the connector registry for connector management.
func (r *Resolver) WithConnectorRegistry(registry *connector.Registry) *Resolver {
	r.ConnectorRegistry = registry
	return r
}

// WithLogger sets a custom logger for the resolver.
func (r *Resolver) WithLogger(logger *slog.Logger) *Resolver {
	r.Logger = logger
	return r
}
