package delivery

import (
	"context"
	"time"
)

// Processor defines the interface for delivery processing strategies.
// Different processors handle different delivery modes (standard parallel, FIFO ordered).
type Processor interface {
	// Start begins the processing loop.
	Start(ctx context.Context)

	// Stop signals the processor to stop and waits for graceful shutdown.
	Stop()
}

// ProcessorStats provides statistics about a processor's state.
type ProcessorStats struct {
	// ActiveWorkers is the number of active worker goroutines
	ActiveWorkers int

	// ProcessedCount is the total number of messages processed
	ProcessedCount int64

	// FailureCount is the total number of failed deliveries
	FailureCount int64

	// LastProcessedAt is the timestamp of the last processed message
	LastProcessedAt time.Time
}
