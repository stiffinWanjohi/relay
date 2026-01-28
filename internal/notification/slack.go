package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// SlackNotifier sends notifications to Slack via webhook.
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
	logger     *slog.Logger
}

// slackMessage represents a Slack webhook message.
type slackMessage struct {
	Text        string            `json:"text,omitempty"`
	Attachments []slackAttachment `json:"attachments,omitempty"`
}

type slackAttachment struct {
	Color  string       `json:"color"`
	Title  string       `json:"title"`
	Text   string       `json:"text"`
	Fields []slackField `json:"fields,omitempty"`
	Footer string       `json:"footer,omitempty"`
	Ts     int64        `json:"ts,omitempty"`
}

type slackField struct {
	Title string `json:"title"`
	Value string `json:"value"`
	Short bool   `json:"short"`
}

// NewSlackNotifier creates a new Slack notifier.
func NewSlackNotifier(webhookURL string, logger *slog.Logger) *SlackNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &SlackNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		logger: logger,
	}
}

// NotifyCircuitTrip sends a circuit breaker trip notification to Slack.
func (s *SlackNotifier) NotifyCircuitTrip(ctx context.Context, endpoint, destination string, failures int) error {
	msg := slackMessage{
		Attachments: []slackAttachment{
			{
				Color: "danger",
				Title: "Circuit Breaker Tripped",
				Text:  fmt.Sprintf("The circuit breaker for endpoint has tripped after %d consecutive failures.", failures),
				Fields: []slackField{
					{Title: "Endpoint", Value: endpoint, Short: true},
					{Title: "Destination", Value: destination, Short: true},
					{Title: "Failures", Value: fmt.Sprintf("%d", failures), Short: true},
				},
				Footer: "Relay Webhook Service",
				Ts:     time.Now().Unix(),
			},
		},
	}

	return s.send(ctx, msg)
}

// NotifyCircuitRecover sends a circuit breaker recovery notification to Slack.
func (s *SlackNotifier) NotifyCircuitRecover(ctx context.Context, endpoint, destination string) error {
	msg := slackMessage{
		Attachments: []slackAttachment{
			{
				Color: "good",
				Title: "Circuit Breaker Recovered",
				Text:  "The circuit breaker has recovered and deliveries have resumed.",
				Fields: []slackField{
					{Title: "Endpoint", Value: endpoint, Short: true},
					{Title: "Destination", Value: destination, Short: true},
				},
				Footer: "Relay Webhook Service",
				Ts:     time.Now().Unix(),
			},
		},
	}

	return s.send(ctx, msg)
}

// NotifyEndpointDisabled sends an endpoint disabled notification to Slack.
func (s *SlackNotifier) NotifyEndpointDisabled(ctx context.Context, endpoint, reason string) error {
	msg := slackMessage{
		Attachments: []slackAttachment{
			{
				Color: "warning",
				Title: "Endpoint Disabled",
				Text:  "An endpoint has been disabled due to repeated failures.",
				Fields: []slackField{
					{Title: "Endpoint", Value: endpoint, Short: true},
					{Title: "Reason", Value: reason, Short: false},
				},
				Footer: "Relay Webhook Service",
				Ts:     time.Now().Unix(),
			},
		},
	}

	return s.send(ctx, msg)
}

// send posts a message to the Slack webhook.
func (s *SlackNotifier) send(ctx context.Context, msg slackMessage) error {
	body, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send slack notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	s.logger.Debug("slack notification sent successfully")
	return nil
}

// Close releases resources.
func (s *SlackNotifier) Close() error {
	s.client.CloseIdleConnections()
	return nil
}
