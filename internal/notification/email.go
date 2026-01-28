package notification

import (
	"context"
	"fmt"
	"log/slog"
	"net/smtp"
	"strings"
	"time"
)

// EmailNotifier sends notifications via email using SMTP.
type EmailNotifier struct {
	host     string
	port     int
	username string
	password string
	from     string
	to       []string
	logger   *slog.Logger
}

// NewEmailNotifier creates a new email notifier.
func NewEmailNotifier(host string, port int, username, password, from string, to []string, logger *slog.Logger) *EmailNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &EmailNotifier{
		host:     host,
		port:     port,
		username: username,
		password: password,
		from:     from,
		to:       to,
		logger:   logger,
	}
}

// NotifyCircuitTrip sends a circuit breaker trip notification via email.
func (e *EmailNotifier) NotifyCircuitTrip(_ context.Context, endpoint, destination string, failures int) error {
	subject := "[ALERT] Circuit Breaker Tripped - Relay Webhook Service"
	body := fmt.Sprintf(`Circuit Breaker Alert

The circuit breaker for an endpoint has tripped after %d consecutive failures.

Details:
- Endpoint: %s
- Destination: %s
- Failures: %d
- Time: %s

Deliveries to this endpoint have been temporarily suspended. The circuit breaker will automatically attempt recovery after the reset timeout.

--
Relay Webhook Service`, failures, endpoint, destination, failures, time.Now().UTC().Format(time.RFC3339))

	return e.send(subject, body)
}

// NotifyCircuitRecover sends a circuit breaker recovery notification via email.
func (e *EmailNotifier) NotifyCircuitRecover(_ context.Context, endpoint, destination string) error {
	subject := "[RESOLVED] Circuit Breaker Recovered - Relay Webhook Service"
	body := fmt.Sprintf(`Circuit Breaker Recovery

The circuit breaker has recovered and deliveries have resumed.

Details:
- Endpoint: %s
- Destination: %s
- Time: %s

Normal delivery operations have resumed.

--
Relay Webhook Service`, endpoint, destination, time.Now().UTC().Format(time.RFC3339))

	return e.send(subject, body)
}

// NotifyEndpointDisabled sends an endpoint disabled notification via email.
func (e *EmailNotifier) NotifyEndpointDisabled(_ context.Context, endpoint, reason string) error {
	subject := "[WARNING] Endpoint Disabled - Relay Webhook Service"
	body := fmt.Sprintf(`Endpoint Disabled

An endpoint has been disabled due to repeated failures.

Details:
- Endpoint: %s
- Reason: %s
- Time: %s

Please review the endpoint configuration and re-enable it when the issue is resolved.

--
Relay Webhook Service`, endpoint, reason, time.Now().UTC().Format(time.RFC3339))

	return e.send(subject, body)
}

// send sends an email via SMTP.
func (e *EmailNotifier) send(subject, body string) error {
	addr := fmt.Sprintf("%s:%d", e.host, e.port)

	// Build message
	msg := strings.Builder{}
	fmt.Fprintf(&msg, "From: %s\r\n", e.from)
	fmt.Fprintf(&msg, "To: %s\r\n", strings.Join(e.to, ", "))
	fmt.Fprintf(&msg, "Subject: %s\r\n", subject)
	msg.WriteString("MIME-Version: 1.0\r\n")
	msg.WriteString("Content-Type: text/plain; charset=\"utf-8\"\r\n")
	msg.WriteString("\r\n")
	msg.WriteString(body)

	// Set up authentication if credentials are provided
	var auth smtp.Auth
	if e.username != "" && e.password != "" {
		auth = smtp.PlainAuth("", e.username, e.password, e.host)
	}

	if err := smtp.SendMail(addr, auth, e.from, e.to, []byte(msg.String())); err != nil {
		return fmt.Errorf("failed to send email: %w", err)
	}

	e.logger.Debug("email notification sent", "recipients", e.to)
	return nil
}

// Close releases resources.
func (e *EmailNotifier) Close() error {
	return nil
}
