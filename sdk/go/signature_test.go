package relay

import (
	"strconv"
	"testing"
	"time"
)

func TestComputeSignature(t *testing.T) {
	payload := []byte(`{"test": "data"}`)
	timestamp := "1704067200"
	secret := "whsec_test123"

	sig := ComputeSignature(payload, timestamp, secret)

	if sig == "" {
		t.Error("expected non-empty signature")
	}

	// Same inputs should produce same signature
	sig2 := ComputeSignature(payload, timestamp, secret)
	if sig != sig2 {
		t.Error("same inputs should produce same signature")
	}

	// Different secret should produce different signature
	sig3 := ComputeSignature(payload, timestamp, "different_secret")
	if sig == sig3 {
		t.Error("different secrets should produce different signatures")
	}
}

func TestVerifySignature(t *testing.T) {
	payload := []byte(`{"event": "test.event", "data": {"id": 123}}`)
	secret := "whsec_mysecretkey"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	signature := ComputeSignature(payload, timestamp, secret)

	err := VerifySignature(payload, signature, timestamp, secret)
	if err != nil {
		t.Errorf("VerifySignature failed: %v", err)
	}
}

func TestVerifySignature_InvalidSignature(t *testing.T) {
	payload := []byte(`{"event": "test.event"}`)
	secret := "whsec_mysecretkey"
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	err := VerifySignature(payload, "invalid_signature", timestamp, secret)
	if err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestVerifySignature_ExpiredTimestamp(t *testing.T) {
	payload := []byte(`{"event": "test.event"}`)
	secret := "whsec_mysecretkey"
	oldTime := time.Now().Add(-10 * time.Minute).Unix()
	timestamp := strconv.FormatInt(oldTime, 10)

	signature := ComputeSignature(payload, timestamp, secret)

	err := VerifySignature(payload, signature, timestamp, secret)
	if err == nil {
		t.Error("expected error for expired timestamp")
	}
}

func TestVerifySignature_FutureTimestamp(t *testing.T) {
	payload := []byte(`{"event": "test.event"}`)
	secret := "whsec_mysecretkey"
	futureTime := time.Now().Add(10 * time.Minute).Unix()
	timestamp := strconv.FormatInt(futureTime, 10)

	signature := ComputeSignature(payload, timestamp, secret)

	err := VerifySignature(payload, signature, timestamp, secret)
	if err == nil {
		t.Error("expected error for future timestamp")
	}
}

func TestVerifySignature_InvalidTimestampFormat(t *testing.T) {
	payload := []byte(`{"event": "test.event"}`)
	secret := "whsec_mysecretkey"

	err := VerifySignature(payload, "some_signature", "not_a_number", secret)
	if err == nil {
		t.Error("expected error for invalid timestamp format")
	}
}

func TestVerifySignatureWithTolerance(t *testing.T) {
	payload := []byte(`{"event": "test.event"}`)
	secret := "whsec_mysecretkey"
	oldTime := time.Now().Add(-3 * time.Minute).Unix()
	timestamp := strconv.FormatInt(oldTime, 10)

	signature := ComputeSignature(payload, timestamp, secret)

	// Should fail with default 5 minute tolerance (within range)
	err := VerifySignatureWithTolerance(payload, signature, timestamp, secret, 5*time.Minute)
	if err != nil {
		t.Errorf("expected success within tolerance: %v", err)
	}

	// Should fail with 1 minute tolerance
	err = VerifySignatureWithTolerance(payload, signature, timestamp, secret, 1*time.Minute)
	if err == nil {
		t.Error("expected error outside tolerance")
	}
}

func TestVerifySignatureWithSecrets(t *testing.T) {
	payload := []byte(`{"event": "test.event"}`)
	secrets := []string{"old_secret", "new_secret"}
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Sign with old secret
	signature := ComputeSignature(payload, timestamp, secrets[0])

	err := VerifySignatureWithSecrets(payload, signature, timestamp, secrets, 5*time.Minute)
	if err != nil {
		t.Errorf("expected success with old secret: %v", err)
	}

	// Sign with new secret
	signature = ComputeSignature(payload, timestamp, secrets[1])

	err = VerifySignatureWithSecrets(payload, signature, timestamp, secrets, 5*time.Minute)
	if err != nil {
		t.Errorf("expected success with new secret: %v", err)
	}

	// Sign with unknown secret
	signature = ComputeSignature(payload, timestamp, "unknown_secret")

	err = VerifySignatureWithSecrets(payload, signature, timestamp, secrets, 5*time.Minute)
	if err == nil {
		t.Error("expected error with unknown secret")
	}
}

func TestWebhookVerifier(t *testing.T) {
	secret := "whsec_test123"
	verifier := NewWebhookVerifier(secret)

	payload := []byte(`{"test": "data"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)
	signature := ComputeSignature(payload, timestamp, secret)

	err := verifier.Verify(payload, signature, timestamp)
	if err != nil {
		t.Errorf("Verify failed: %v", err)
	}
}

func TestWebhookVerifier_WithTolerance(t *testing.T) {
	secret := "whsec_test123"
	verifier := NewWebhookVerifier(secret).WithTolerance(10 * time.Minute)

	payload := []byte(`{"test": "data"}`)
	oldTime := time.Now().Add(-7 * time.Minute).Unix()
	timestamp := strconv.FormatInt(oldTime, 10)
	signature := ComputeSignature(payload, timestamp, secret)

	// Should pass with 10 minute tolerance
	err := verifier.Verify(payload, signature, timestamp)
	if err != nil {
		t.Errorf("expected success with custom tolerance: %v", err)
	}
}

func TestWebhookVerifierWithSecrets(t *testing.T) {
	secrets := []string{"old_secret", "new_secret"}
	verifier := NewWebhookVerifierWithSecrets(secrets)

	payload := []byte(`{"test": "data"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	// Sign with old secret
	signature := ComputeSignature(payload, timestamp, secrets[0])

	err := verifier.Verify(payload, signature, timestamp)
	if err != nil {
		t.Errorf("expected success with old secret: %v", err)
	}

	// Sign with new secret
	signature = ComputeSignature(payload, timestamp, secrets[1])

	err = verifier.Verify(payload, signature, timestamp)
	if err != nil {
		t.Errorf("expected success with new secret: %v", err)
	}
}

func TestWebhookVerifier_InvalidSignature(t *testing.T) {
	verifier := NewWebhookVerifier("whsec_test123")

	payload := []byte(`{"test": "data"}`)
	timestamp := strconv.FormatInt(time.Now().Unix(), 10)

	err := verifier.Verify(payload, "invalid_sig", timestamp)
	if err == nil {
		t.Error("expected error for invalid signature")
	}
}

func TestSignatureConstants(t *testing.T) {
	if SignatureHeader != "X-Relay-Signature" {
		t.Errorf("expected SignatureHeader 'X-Relay-Signature', got %s", SignatureHeader)
	}
	if TimestampHeader != "X-Relay-Timestamp" {
		t.Errorf("expected TimestampHeader 'X-Relay-Timestamp', got %s", TimestampHeader)
	}
	if DefaultTimestampTolerance != 5*time.Minute {
		t.Errorf("expected DefaultTimestampTolerance 5m, got %v", DefaultTimestampTolerance)
	}
}
