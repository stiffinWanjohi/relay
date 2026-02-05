package notification

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/stiffinWanjohi/relay/internal/logging"
)

var slackLog = logging.Component("notification.slack")

// SlackNotifier sends notifications to Slack via webhook.
type SlackNotifier struct {
	webhookURL string
	client     *http.Client
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
func NewSlackNotifier(webhookURL string) *SlackNotifier {
	return &SlackNotifier{
		webhookURL: webhookURL,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

// NotifyCircuitTrip sends a circuit breaker trip notification to Slack.
func (s *SlackNotifier) NotifyCircuitTrip(ctx context.Context, endpoint, destination string, failures int) error {
	slackLog.Debug("sending circuit trip notification", "endpoint", endpoint, "destination", destination, "failures", failures)

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

	return s.send(ctx, msg, "circuit_trip")
}

// NotifyCircuitRecover sends a circuit breaker recovery notification to Slack.
func (s *SlackNotifier) NotifyCircuitRecover(ctx context.Context, endpoint, destination string) error {
	slackLog.Debug("sending circuit recovery notification", "endpoint", endpoint, "destination", destination)

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

	return s.send(ctx, msg, "circuit_recover")
}

// NotifyEndpointDisabled sends an endpoint disabled notification to Slack.
func (s *SlackNotifier) NotifyEndpointDisabled(ctx context.Context, endpoint, reason string) error {
	slackLog.Debug("sending endpoint disabled notification", "endpoint", endpoint, "reason", reason)

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

	return s.send(ctx, msg, "endpoint_disabled")
}

// send posts a message to the Slack webhook.
func (s *SlackNotifier) send(ctx context.Context, msg slackMessage, notificationType string) error {
	body, err := json.Marshal(msg)
	if err != nil {
		slackLog.Error("failed to marshal slack message", "type", notificationType, "error", err)
		return fmt.Errorf("failed to marshal slack message: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.webhookURL, bytes.NewReader(body))
	if err != nil {
		slackLog.Error("failed to create request", "type", notificationType, "error", err)
		return fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		slackLog.Error("failed to send slack notification", "type", notificationType, "error", err)
		return fmt.Errorf("failed to send slack notification: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		slackLog.Warn("slack webhook returned error", "type", notificationType, "status_code", resp.StatusCode)
		return fmt.Errorf("slack webhook returned status %d", resp.StatusCode)
	}

	slackLog.Debug("slack notification sent", "type", notificationType)
	return nil
}

// Close releases resources.
func (s *SlackNotifier) Close() error {
	s.client.CloseIdleConnections()
	return nil
}
