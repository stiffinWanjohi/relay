package notification

import "context"

// NoopNotifier is a no-operation notifier that does nothing.
// Used when notifications are disabled or as a fallback.
type NoopNotifier struct{}

// NotifyCircuitTrip does nothing.
func (n *NoopNotifier) NotifyCircuitTrip(_ context.Context, _, _ string, _ int) error {
	return nil
}

// NotifyCircuitRecover does nothing.
func (n *NoopNotifier) NotifyCircuitRecover(_ context.Context, _, _ string) error {
	return nil
}

// NotifyEndpointDisabled does nothing.
func (n *NoopNotifier) NotifyEndpointDisabled(_ context.Context, _, _ string) error {
	return nil
}

// Close does nothing.
func (n *NoopNotifier) Close() error {
	return nil
}
