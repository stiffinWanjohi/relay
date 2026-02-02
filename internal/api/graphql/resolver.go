package graphql

import (
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/eventtype"
	"github.com/stiffinWanjohi/relay/internal/queue"
	"github.com/stiffinWanjohi/relay/internal/transform"
)

// Resolver is the root resolver.
type Resolver struct {
	Store          *event.Store
	EventTypeStore *eventtype.Store
	Queue          *queue.Queue
	Dedup          *dedup.Checker
	Transformer    domain.TransformationExecutor
}

// NewResolver creates a new resolver.
func NewResolver(store *event.Store, eventTypeStore *eventtype.Store, q *queue.Queue, d *dedup.Checker) *Resolver {
	return &Resolver{
		Store:          store,
		EventTypeStore: eventTypeStore,
		Queue:          q,
		Dedup:          d,
		Transformer:    transform.NewDefaultV8Executor(),
	}
}
