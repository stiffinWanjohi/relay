package notification

import (
	"context"
	"log/slog"
	"sync"
)

// Notifier defines the interface for sending notifications.
type Notifier interface {
	// NotifyCircuitTrip is called when a circuit breaker trips to OPEN state.
	NotifyCircuitTrip(ctx context.Context, endpoint, destination string, failures int) error

	// NotifyCircuitRecover is called when a circuit breaker recovers to CLOSED state.
	NotifyCircuitRecover(ctx context.Context, endpoint, destination string) error

	// NotifyEndpointDisabled is called when an endpoint is disabled due to errors.
	NotifyEndpointDisabled(ctx context.Context, endpoint, reason string) error

	// Close releases any resources held by the notifier.
	Close() error
}

// Service manages multiple notifiers and dispatches notifications.
type Service struct {
	notifiers []Notifier
	async     bool
	logger    *slog.Logger
	wg        sync.WaitGroup
}

// Config holds notification service configuration.
type Config struct {
	Enabled         bool
	Async           bool
	SlackWebhookURL string
	SMTPHost        string
	SMTPPort        int
	SMTPUsername    string
	SMTPPassword    string
	EmailFrom       string
	EmailTo         []string
	NotifyOnTrip    bool
	NotifyOnRecover bool
}

// NewService creates a new notification service with the given configuration.
func NewService(cfg Config, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}

	s := &Service{
		notifiers: make([]Notifier, 0),
		async:     cfg.Async,
		logger:    logger,
	}

	if !cfg.Enabled {
		s.notifiers = append(s.notifiers, &NoopNotifier{})
		return s
	}

	// Add Slack notifier if configured
	if cfg.SlackWebhookURL != "" {
		s.notifiers = append(s.notifiers, NewSlackNotifier(cfg.SlackWebhookURL, logger))
		logger.Info("slack notifier enabled")
	}

	// Add Email notifier if configured
	if cfg.SMTPHost != "" && len(cfg.EmailTo) > 0 {
		s.notifiers = append(s.notifiers, NewEmailNotifier(
			cfg.SMTPHost,
			cfg.SMTPPort,
			cfg.SMTPUsername,
			cfg.SMTPPassword,
			cfg.EmailFrom,
			cfg.EmailTo,
			logger,
		))
		logger.Info("email notifier enabled", "recipients", cfg.EmailTo)
	}

	// If no notifiers configured, use noop
	if len(s.notifiers) == 0 {
		s.notifiers = append(s.notifiers, &NoopNotifier{})
		logger.Info("no notifiers configured, using noop")
	}

	return s
}

// NotifyCircuitTrip dispatches circuit trip notifications to all notifiers.
func (s *Service) NotifyCircuitTrip(ctx context.Context, endpoint, destination string, failures int) {
	s.dispatch(func(n Notifier) error {
		return n.NotifyCircuitTrip(ctx, endpoint, destination, failures)
	})
}

// NotifyCircuitRecover dispatches circuit recovery notifications to all notifiers.
func (s *Service) NotifyCircuitRecover(ctx context.Context, endpoint, destination string) {
	s.dispatch(func(n Notifier) error {
		return n.NotifyCircuitRecover(ctx, endpoint, destination)
	})
}

// NotifyEndpointDisabled dispatches endpoint disabled notifications to all notifiers.
func (s *Service) NotifyEndpointDisabled(ctx context.Context, endpoint, reason string) {
	s.dispatch(func(n Notifier) error {
		return n.NotifyEndpointDisabled(ctx, endpoint, reason)
	})
}

// dispatch sends notifications to all notifiers, either synchronously or asynchronously.
func (s *Service) dispatch(fn func(Notifier) error) {
	for _, n := range s.notifiers {
		notifier := n
		if s.async {
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				if err := fn(notifier); err != nil {
					s.logger.Error("notification failed", "error", err)
				}
			}()
		} else {
			if err := fn(notifier); err != nil {
				s.logger.Error("notification failed", "error", err)
			}
		}
	}
}

// Close closes all notifiers and waits for pending async notifications.
func (s *Service) Close() error {
	s.wg.Wait()
	var lastErr error
	for _, n := range s.notifiers {
		if err := n.Close(); err != nil {
			lastErr = err
		}
	}
	return lastErr
}
