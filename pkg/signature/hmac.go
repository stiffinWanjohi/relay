package signature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
)

const (
	// Header names for webhook signatures
	HeaderSignature = "X-Relay-Signature"
	HeaderTimestamp = "X-Relay-Timestamp"
	HeaderEventID   = "X-Relay-Event-ID"

	// Signature version prefix
	signatureVersion = "v1"
)

// Signer creates HMAC-SHA256 signatures for webhooks.
type Signer struct {
	key []byte
}

// NewSigner creates a new signer with the given secret key.
func NewSigner(secret string) *Signer {
	return &Signer{
		key: []byte(secret),
	}
}

// Sign creates a signature for the given timestamp and payload.
// Format: v1=<hmac-sha256(timestamp.payload, secret)>
func (s *Signer) Sign(timestamp int64, payload []byte) string {
	message := fmt.Sprintf("%d.%s", timestamp, string(payload))
	mac := hmac.New(sha256.New, s.key)
	mac.Write([]byte(message))
	sig := hex.EncodeToString(mac.Sum(nil))
	return fmt.Sprintf("%s=%s", signatureVersion, sig)
}

// Verify verifies a signature against the given timestamp and payload.
func (s *Signer) Verify(signature string, timestamp int64, payload []byte) bool {
	expected := s.Sign(timestamp, payload)
	return hmac.Equal([]byte(signature), []byte(expected))
}

// FormatTimestamp formats a Unix timestamp for the header.
func FormatTimestamp(timestamp int64) string {
	return strconv.FormatInt(timestamp, 10)
}

// ParseTimestamp parses a timestamp from the header.
func ParseTimestamp(s string) (int64, error) {
	return strconv.ParseInt(s, 10, 64)
}

// Verifier verifies webhook signatures.
type Verifier struct {
	signer      *Signer
	maxAge      int64 // Maximum age in seconds
	allowedSkew int64 // Allowed clock skew in seconds
}

// NewVerifier creates a new signature verifier.
func NewVerifier(secret string) *Verifier {
	return &Verifier{
		signer:      NewSigner(secret),
		maxAge:      300, // 5 minutes default
		allowedSkew: 60,  // 1 minute default
	}
}

// WithMaxAge sets the maximum age for signatures.
func (v *Verifier) WithMaxAge(seconds int64) *Verifier {
	return &Verifier{
		signer:      v.signer,
		maxAge:      seconds,
		allowedSkew: v.allowedSkew,
	}
}

// WithAllowedSkew sets the allowed clock skew.
func (v *Verifier) WithAllowedSkew(seconds int64) *Verifier {
	return &Verifier{
		signer:      v.signer,
		maxAge:      v.maxAge,
		allowedSkew: seconds,
	}
}

// Verify verifies a signature with timestamp validation.
func (v *Verifier) Verify(signature string, timestamp int64, payload []byte, now int64) error {
	// Check timestamp is not too old
	if now-timestamp > v.maxAge {
		return ErrSignatureExpired
	}

	// Check timestamp is not in the future (with skew allowance)
	if timestamp-now > v.allowedSkew {
		return ErrTimestampInFuture
	}

	// Verify the signature
	if !v.signer.Verify(signature, timestamp, payload) {
		return ErrInvalidSignature
	}

	return nil
}

// Signature errors
var (
	ErrInvalidSignature  = fmt.Errorf("invalid signature")
	ErrSignatureExpired  = fmt.Errorf("signature expired")
	ErrTimestampInFuture = fmt.Errorf("timestamp is in the future")
)
