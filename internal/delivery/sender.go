package delivery

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"time"

	"github.com/stiffinWanjohi/relay/internal/domain"
	"github.com/stiffinWanjohi/relay/internal/logging"
	"github.com/stiffinWanjohi/relay/pkg/signature"
)

var senderLog = logging.Component("sender")

const (
	// Default timeout for webhook delivery
	defaultTimeout = 30 * time.Second

	// Maximum response body size to read
	maxResponseBodySize = 64 * 1024 // 64KB
)

// Sender handles HTTP delivery of webhook events.
type Sender struct {
	client     *http.Client
	signer     *signature.Signer
	signingKey string
}

// NewSender creates a new webhook sender.
func NewSender(signingKey string) *Sender {
	// Configure transport for connection reuse and performance
	transport := &http.Transport{
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	return &Sender{
		client: &http.Client{
			Timeout:   defaultTimeout,
			Transport: transport,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Don't follow redirects automatically
				return http.ErrUseLastResponse
			},
		},
		signer:     signature.NewSigner(signingKey),
		signingKey: signingKey,
	}
}

// WithClient sets a custom HTTP client.
func (s *Sender) WithClient(client *http.Client) *Sender {
	return &Sender{
		client:     client,
		signer:     s.signer,
		signingKey: s.signingKey,
	}
}

// WithTimeout sets a custom timeout.
func (s *Sender) WithTimeout(timeout time.Duration) *Sender {
	client := &http.Client{
		Timeout:       timeout,
		CheckRedirect: s.client.CheckRedirect,
	}
	return &Sender{
		client:     client,
		signer:     s.signer,
		signingKey: s.signingKey,
	}
}

// Send delivers a webhook event to its destination using the default timeout.
func (s *Sender) Send(ctx context.Context, event domain.Event) domain.DeliveryResult {
	return s.SendWithTimeout(ctx, event, defaultTimeout)
}

// SendWithEndpoint delivers a webhook event using endpoint-specific configuration.
func (s *Sender) SendWithEndpoint(ctx context.Context, event domain.Event, endpoint *domain.Endpoint) domain.DeliveryResult {
	timeout := defaultTimeout
	if endpoint != nil && endpoint.TimeoutMs > 0 {
		timeout = endpoint.GetTimeoutDuration()
	}
	return s.sendWithConfig(ctx, event, endpoint, timeout)
}

// SendWithTimeout delivers a webhook event with a custom timeout.
func (s *Sender) SendWithTimeout(ctx context.Context, event domain.Event, timeout time.Duration) domain.DeliveryResult {
	return s.sendWithConfig(ctx, event, nil, timeout)
}

// sendWithConfig delivers a webhook event with optional endpoint configuration.
func (s *Sender) sendWithConfig(ctx context.Context, event domain.Event, endpoint *domain.Endpoint, timeout time.Duration) domain.DeliveryResult {
	start := time.Now()

	senderLog.Debug("sending webhook",
		"event_id", event.ID,
		"destination", event.Destination,
		"payload_size", len(event.Payload),
		"timeout_ms", timeout.Milliseconds(),
	)

	// Ensure we have a deadline for the request
	// If context doesn't have a deadline, create one with the specified timeout
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	// Create the request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, event.Destination, bytes.NewReader(event.Payload))
	if err != nil {
		senderLog.Error("failed to create request",
			"event_id", event.ID,
			"destination", event.Destination,
			"error", err,
		)
		return domain.NewFailureResult(0, "", err, time.Since(start).Milliseconds())
	}

	// Set default headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Relay-Webhook/1.0")

	// Set custom headers from the event
	for key, value := range event.Headers {
		req.Header.Set(key, value)
	}

	// Sign the request using endpoint-specific secret if available
	timestamp := time.Now().Unix()
	signingKey := s.getSigningKey(endpoint)
	signer := signature.NewSigner(signingKey)
	sig := signer.Sign(timestamp, event.Payload)

	req.Header.Set(signature.HeaderSignature, sig)
	req.Header.Set(signature.HeaderTimestamp, signature.FormatTimestamp(timestamp))
	req.Header.Set(signature.HeaderEventID, event.ID.String())

	// Send the request
	resp, err := s.client.Do(req)
	if err != nil {
		durationMs := time.Since(start).Milliseconds()
		senderLog.Warn("webhook delivery failed",
			"event_id", event.ID,
			"destination", event.Destination,
			"error", err,
			"duration_ms", durationMs,
		)
		return domain.NewFailureResult(0, "", err, durationMs)
	}
	defer func() { _ = resp.Body.Close() }()

	// Read the response body (limited)
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBodySize))
	if err != nil {
		durationMs := time.Since(start).Milliseconds()
		senderLog.Warn("failed to read response body",
			"event_id", event.ID,
			"destination", event.Destination,
			"status_code", resp.StatusCode,
			"error", err,
			"duration_ms", durationMs,
		)
		return domain.NewFailureResult(resp.StatusCode, "", err, durationMs)
	}

	durationMs := time.Since(start).Milliseconds()

	// Check if successful (2xx status code)
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		senderLog.Debug("webhook delivered successfully",
			"event_id", event.ID,
			"destination", event.Destination,
			"status_code", resp.StatusCode,
			"duration_ms", durationMs,
		)
		return domain.NewSuccessResult(resp.StatusCode, string(body), durationMs)
	}

	// Return failure for non-2xx
	senderLog.Warn("webhook returned non-2xx status",
		"event_id", event.ID,
		"destination", event.Destination,
		"status_code", resp.StatusCode,
		"duration_ms", durationMs,
	)
	return domain.NewFailureResult(resp.StatusCode, string(body), nil, durationMs)
}

// getSigningKey returns the signing key to use for an endpoint.
// Uses endpoint-specific secret if available, otherwise falls back to global key.
func (s *Sender) getSigningKey(endpoint *domain.Endpoint) string {
	if endpoint != nil && endpoint.HasCustomSecret() {
		return endpoint.SigningSecret
	}
	return s.signingKey
}
