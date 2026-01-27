package graphql

import (
	"github.com/stiffinWanjohi/relay/internal/dedup"
	"github.com/stiffinWanjohi/relay/internal/event"
	"github.com/stiffinWanjohi/relay/internal/queue"
)

// Resolver is the root resolver.
type Resolver struct {
	Store *event.Store
	Queue *queue.Queue
	Dedup *dedup.Checker
}

// NewResolver creates a new resolver.
func NewResolver(store *event.Store, q *queue.Queue, d *dedup.Checker) *Resolver {
	return &Resolver{
		Store: store,
		Queue: q,
		Dedup: d,
	}
}
