package signature

import (
	"testing"
	"time"
)

func TestNewSigner(t *testing.T) {
	secret := "test-secret-key-32-chars-minimum"
	signer := NewSigner(secret)

	if signer == nil {
		t.Fatal("expected non-nil signer")
	}
	if string(signer.key) != secret {
		t.Error("signer key mismatch")
	}
}

func TestSigner_Sign(t *testing.T) {
	signer := NewSigner("test-secret")
	timestamp := int64(1706123456)
	payload := []byte(`{"order_id": 123}`)

	sig := signer.Sign(timestamp, payload)

	// Should start with version prefix
	if len(sig) < 3 || sig[:3] != "v1=" {
		t.Errorf("expected signature to start with 'v1=', got %q", sig)
	}

	// Should be deterministic
	sig2 := signer.Sign(timestamp, payload)
	if sig != sig2 {
		t.Error("signature should be deterministic")
	}

	// Different timestamp should produce different signature
	sig3 := signer.Sign(timestamp+1, payload)
	if sig == sig3 {
		t.Error("different timestamp should produce different signature")
	}

	// Different payload should produce different signature
	sig4 := signer.Sign(timestamp, []byte(`{"order_id": 456}`))
	if sig == sig4 {
		t.Error("different payload should produce different signature")
	}
}

func TestSigner_Sign_EmptyPayload(t *testing.T) {
	signer := NewSigner("test-secret")

	sig := signer.Sign(1234567890, []byte{})
	if sig == "" {
		t.Error("should produce signature for empty payload")
	}
	if sig[:3] != "v1=" {
		t.Error("should have version prefix")
	}
}

func TestSigner_Verify(t *testing.T) {
	signer := NewSigner("test-secret")
	timestamp := int64(1706123456)
	payload := []byte(`{"order_id": 123}`)

	sig := signer.Sign(timestamp, payload)

	if !signer.Verify(sig, timestamp, payload) {
		t.Error("should verify valid signature")
	}
}

func TestSigner_Verify_Invalid(t *testing.T) {
	signer := NewSigner("test-secret")
	timestamp := int64(1706123456)
	payload := []byte(`{"order_id": 123}`)

	sig := signer.Sign(timestamp, payload)

	// Wrong timestamp
	if signer.Verify(sig, timestamp+1, payload) {
		t.Error("should reject wrong timestamp")
	}

	// Wrong payload
	if signer.Verify(sig, timestamp, []byte(`{"order_id": 456}`)) {
		t.Error("should reject wrong payload")
	}

	// Tampered signature
	if signer.Verify("v1=0000000000000000000000000000000000000000000000000000000000000000", timestamp, payload) {
		t.Error("should reject tampered signature")
	}

	// Wrong format
	if signer.Verify("invalid", timestamp, payload) {
		t.Error("should reject invalid format")
	}
}

func TestSigner_Verify_DifferentSecret(t *testing.T) {
	signer1 := NewSigner("secret-one")
	signer2 := NewSigner("secret-two")

	timestamp := int64(1706123456)
	payload := []byte(`{"data": "test"}`)

	sig := signer1.Sign(timestamp, payload)

	if signer2.Verify(sig, timestamp, payload) {
		t.Error("should reject signature from different secret")
	}
}

func TestFormatTimestamp(t *testing.T) {
	tests := []struct {
		timestamp int64
		want      string
	}{
		{0, "0"},
		{1706123456, "1706123456"},
		{1000000000000, "1000000000000"},
		{-1, "-1"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			got := FormatTimestamp(tt.timestamp)
			if got != tt.want {
				t.Errorf("FormatTimestamp(%d) = %q, want %q", tt.timestamp, got, tt.want)
			}
		})
	}
}

func TestParseTimestamp(t *testing.T) {
	tests := []struct {
		input   string
		want    int64
		wantErr bool
	}{
		{"0", 0, false},
		{"1706123456", 1706123456, false},
		{"1000000000000", 1000000000000, false},
		{"-1", -1, false},
		{"", 0, true},
		{"abc", 0, true},
		{"12.34", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got, err := ParseTimestamp(tt.input)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseTimestamp(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
				return
			}
			if !tt.wantErr && got != tt.want {
				t.Errorf("ParseTimestamp(%q) = %d, want %d", tt.input, got, tt.want)
			}
		})
	}
}

func TestFormatAndParseTimestamp_Roundtrip(t *testing.T) {
	timestamps := []int64{0, 1, 1706123456, 9999999999}

	for _, ts := range timestamps {
		formatted := FormatTimestamp(ts)
		parsed, err := ParseTimestamp(formatted)
		if err != nil {
			t.Errorf("failed to parse formatted timestamp %d: %v", ts, err)
			continue
		}
		if parsed != ts {
			t.Errorf("roundtrip failed: %d -> %q -> %d", ts, formatted, parsed)
		}
	}
}

func TestNewVerifier(t *testing.T) {
	verifier := NewVerifier("test-secret")

	if verifier == nil {
		t.Fatal("expected non-nil verifier")
	}
	if verifier.signer == nil {
		t.Error("expected non-nil signer")
	}
	if verifier.maxAge != 300 {
		t.Errorf("expected default maxAge 300, got %d", verifier.maxAge)
	}
	if verifier.allowedSkew != 60 {
		t.Errorf("expected default allowedSkew 60, got %d", verifier.allowedSkew)
	}
}

func TestVerifier_WithMaxAge(t *testing.T) {
	verifier := NewVerifier("test-secret").WithMaxAge(600)

	if verifier.maxAge != 600 {
		t.Errorf("expected maxAge 600, got %d", verifier.maxAge)
	}
	// Other fields preserved
	if verifier.allowedSkew != 60 {
		t.Errorf("expected allowedSkew 60, got %d", verifier.allowedSkew)
	}
}

func TestVerifier_WithAllowedSkew(t *testing.T) {
	verifier := NewVerifier("test-secret").WithAllowedSkew(120)

	if verifier.allowedSkew != 120 {
		t.Errorf("expected allowedSkew 120, got %d", verifier.allowedSkew)
	}
	// Other fields preserved
	if verifier.maxAge != 300 {
		t.Errorf("expected maxAge 300, got %d", verifier.maxAge)
	}
}

func TestVerifier_Verify_Valid(t *testing.T) {
	secret := "test-secret-key"
	signer := NewSigner(secret)
	verifier := NewVerifier(secret)

	now := time.Now().Unix()
	payload := []byte(`{"test": true}`)
	sig := signer.Sign(now, payload)

	err := verifier.Verify(sig, now, payload, now)
	if err != nil {
		t.Errorf("expected valid signature, got error: %v", err)
	}
}

func TestVerifier_Verify_Expired(t *testing.T) {
	secret := "test-secret-key"
	signer := NewSigner(secret)
	verifier := NewVerifier(secret).WithMaxAge(300) // 5 minutes

	oldTimestamp := time.Now().Unix() - 600 // 10 minutes ago
	now := time.Now().Unix()
	payload := []byte(`{"test": true}`)
	sig := signer.Sign(oldTimestamp, payload)

	err := verifier.Verify(sig, oldTimestamp, payload, now)
	if err != ErrSignatureExpired {
		t.Errorf("expected ErrSignatureExpired, got: %v", err)
	}
}

func TestVerifier_Verify_FutureTimestamp(t *testing.T) {
	secret := "test-secret-key"
	signer := NewSigner(secret)
	verifier := NewVerifier(secret).WithAllowedSkew(60) // 1 minute skew

	futureTimestamp := time.Now().Unix() + 120 // 2 minutes in future
	now := time.Now().Unix()
	payload := []byte(`{"test": true}`)
	sig := signer.Sign(futureTimestamp, payload)

	err := verifier.Verify(sig, futureTimestamp, payload, now)
	if err != ErrTimestampInFuture {
		t.Errorf("expected ErrTimestampInFuture, got: %v", err)
	}
}

func TestVerifier_Verify_FutureWithinSkew(t *testing.T) {
	secret := "test-secret-key"
	signer := NewSigner(secret)
	verifier := NewVerifier(secret).WithAllowedSkew(120) // 2 minute skew

	futureTimestamp := time.Now().Unix() + 60 // 1 minute in future (within skew)
	now := time.Now().Unix()
	payload := []byte(`{"test": true}`)
	sig := signer.Sign(futureTimestamp, payload)

	err := verifier.Verify(sig, futureTimestamp, payload, now)
	if err != nil {
		t.Errorf("expected valid (within skew), got error: %v", err)
	}
}

func TestVerifier_Verify_InvalidSignature(t *testing.T) {
	verifier := NewVerifier("test-secret-key")

	now := time.Now().Unix()
	payload := []byte(`{"test": true}`)

	err := verifier.Verify("v1=invalid", now, payload, now)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestVerifier_Verify_WrongSecret(t *testing.T) {
	signer := NewSigner("secret-one")
	verifier := NewVerifier("secret-two")

	now := time.Now().Unix()
	payload := []byte(`{"test": true}`)
	sig := signer.Sign(now, payload)

	err := verifier.Verify(sig, now, payload, now)
	if err != ErrInvalidSignature {
		t.Errorf("expected ErrInvalidSignature, got: %v", err)
	}
}

func TestVerifier_ChainedConfiguration(t *testing.T) {
	verifier := NewVerifier("secret").
		WithMaxAge(600).
		WithAllowedSkew(120)

	if verifier.maxAge != 600 {
		t.Errorf("expected maxAge 600, got %d", verifier.maxAge)
	}
	if verifier.allowedSkew != 120 {
		t.Errorf("expected allowedSkew 120, got %d", verifier.allowedSkew)
	}
}

func TestHeaderConstants(t *testing.T) {
	if HeaderSignature != "X-Relay-Signature" {
		t.Errorf("unexpected HeaderSignature: %s", HeaderSignature)
	}
	if HeaderTimestamp != "X-Relay-Timestamp" {
		t.Errorf("unexpected HeaderTimestamp: %s", HeaderTimestamp)
	}
	if HeaderEventID != "X-Relay-Event-ID" {
		t.Errorf("unexpected HeaderEventID: %s", HeaderEventID)
	}
}

func TestSignatureErrors(t *testing.T) {
	if ErrInvalidSignature == nil {
		t.Error("ErrInvalidSignature should not be nil")
	}
	if ErrSignatureExpired == nil {
		t.Error("ErrSignatureExpired should not be nil")
	}
	if ErrTimestampInFuture == nil {
		t.Error("ErrTimestampInFuture should not be nil")
	}

	// Verify error messages are meaningful
	if ErrInvalidSignature.Error() == "" {
		t.Error("ErrInvalidSignature should have message")
	}
	if ErrSignatureExpired.Error() == "" {
		t.Error("ErrSignatureExpired should have message")
	}
	if ErrTimestampInFuture.Error() == "" {
		t.Error("ErrTimestampInFuture should have message")
	}
}

func TestSigner_SignatureFormat(t *testing.T) {
	signer := NewSigner("test-secret")
	sig := signer.Sign(1706123456, []byte(`{}`))

	// v1= prefix + 64 hex characters (32 bytes SHA256)
	expectedLen := 3 + 64
	if len(sig) != expectedLen {
		t.Errorf("expected signature length %d, got %d", expectedLen, len(sig))
	}

	// Verify hex characters
	for i := 3; i < len(sig); i++ {
		c := sig[i]
		isHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHex {
			t.Errorf("non-hex character at position %d: %c", i, c)
		}
	}
}

func TestSigner_LargePayload(t *testing.T) {
	signer := NewSigner("test-secret")

	// 1MB payload
	largePayload := make([]byte, 1024*1024)
	for i := range largePayload {
		largePayload[i] = byte(i % 256)
	}

	sig := signer.Sign(1706123456, largePayload)
	if !signer.Verify(sig, 1706123456, largePayload) {
		t.Error("should verify large payload")
	}
}

func TestSigner_SpecialCharactersInPayload(t *testing.T) {
	signer := NewSigner("test-secret")

	payloads := [][]byte{
		[]byte(`{"emoji": "🎉"}`),
		[]byte(`{"newline": "line1\nline2"}`),
		[]byte(`{"tab": "col1\tcol2"}`),
		[]byte(`{"null": "\u0000"}`),
		[]byte(`{"unicode": "日本語"}`),
	}

	for _, payload := range payloads {
		sig := signer.Sign(1706123456, payload)
		if !signer.Verify(sig, 1706123456, payload) {
			t.Errorf("should verify payload: %s", string(payload))
		}
	}
}

func TestVerifier_EdgeCases(t *testing.T) {
	secret := "test-secret"
	signer := NewSigner(secret)
	verifier := NewVerifier(secret).WithMaxAge(300).WithAllowedSkew(60)

	now := int64(1706123456)
	payload := []byte(`{}`)

	// Exactly at max age boundary (now - timestamp == maxAge is still valid, > is expired)
	sig := signer.Sign(now-300, payload)
	err := verifier.Verify(sig, now-300, payload, now)
	if err != nil {
		t.Errorf("exactly at max age boundary should be valid, got: %v", err)
	}

	// Just past max age
	sig = signer.Sign(now-301, payload)
	err = verifier.Verify(sig, now-301, payload, now)
	if err != ErrSignatureExpired {
		t.Errorf("just past max age should be expired, got: %v", err)
	}

	// Exactly at skew boundary (timestamp - now == allowedSkew is still valid, > is future)
	sig = signer.Sign(now+60, payload)
	err = verifier.Verify(sig, now+60, payload, now)
	if err != nil {
		t.Errorf("exactly at skew boundary should be valid, got: %v", err)
	}

	// Just past skew boundary
	sig = signer.Sign(now+61, payload)
	err = verifier.Verify(sig, now+61, payload, now)
	if err != ErrTimestampInFuture {
		t.Errorf("just past skew boundary should be future, got: %v", err)
	}
}
