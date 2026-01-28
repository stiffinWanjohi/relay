package relay

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"time"
)

const (
	// SignatureHeader is the header containing the webhook signature.
	SignatureHeader = "X-Relay-Signature"
	// TimestampHeader is the header containing the request timestamp.
	TimestampHeader = "X-Relay-Timestamp"
	// DefaultTimestampTolerance is the default tolerance for timestamp validation.
	DefaultTimestampTolerance = 5 * time.Minute
)

// VerifySignature verifies a webhook signature.
// payload is the raw request body, signature is from X-Relay-Signature header,
// timestamp is from X-Relay-Timestamp header, and secret is your signing secret.
func VerifySignature(payload []byte, signature, timestamp, secret string) error {
	return VerifySignatureWithTolerance(payload, signature, timestamp, secret, DefaultTimestampTolerance)
}

// VerifySignatureWithTolerance verifies a webhook signature with custom timestamp tolerance.
func VerifySignatureWithTolerance(payload []byte, signature, timestamp, secret string, tolerance time.Duration) error {
	// Validate timestamp
	ts, err := strconv.ParseInt(timestamp, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid timestamp format: %w", err)
	}

	reqTime := time.Unix(ts, 0)
	age := time.Since(reqTime)
	if age < -tolerance || age > tolerance {
		return fmt.Errorf("timestamp outside acceptable range: %v old", age)
	}

	// Compute expected signature
	expected := ComputeSignature(payload, timestamp, secret)

	// Compare signatures using constant-time comparison
	if !hmac.Equal([]byte(signature), []byte(expected)) {
		return fmt.Errorf("signature mismatch")
	}

	return nil
}

// VerifySignatureWithSecrets verifies a webhook signature against multiple secrets.
// Useful during secret rotation when both old and new secrets are valid.
func VerifySignatureWithSecrets(payload []byte, signature, timestamp string, secrets []string, tolerance time.Duration) error {
	for _, secret := range secrets {
		err := VerifySignatureWithTolerance(payload, signature, timestamp, secret, tolerance)
		if err == nil {
			return nil
		}
	}
	return fmt.Errorf("signature does not match any provided secret")
}

// ComputeSignature computes the HMAC-SHA256 signature for a payload.
func ComputeSignature(payload []byte, timestamp, secret string) string {
	// Signature is computed over: timestamp + "." + payload
	message := timestamp + "." + string(payload)

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

// WebhookVerifier provides a convenient way to verify webhooks with a fixed secret.
type WebhookVerifier struct {
	secrets   []string
	tolerance time.Duration
}

// NewWebhookVerifier creates a new webhook verifier with the given secret.
func NewWebhookVerifier(secret string) *WebhookVerifier {
	return &WebhookVerifier{
		secrets:   []string{secret},
		tolerance: DefaultTimestampTolerance,
	}
}

// NewWebhookVerifierWithSecrets creates a verifier that accepts multiple secrets.
// Useful during secret rotation.
func NewWebhookVerifierWithSecrets(secrets []string) *WebhookVerifier {
	return &WebhookVerifier{
		secrets:   secrets,
		tolerance: DefaultTimestampTolerance,
	}
}

// WithTolerance sets the timestamp tolerance.
func (v *WebhookVerifier) WithTolerance(d time.Duration) *WebhookVerifier {
	v.tolerance = d
	return v
}

// Verify verifies a webhook request.
func (v *WebhookVerifier) Verify(payload []byte, signature, timestamp string) error {
	return VerifySignatureWithSecrets(payload, signature, timestamp, v.secrets, v.tolerance)
}
