"""Tests for signature verification."""

import time

import pytest

from relay_sdk import (
    InvalidSignatureError,
    InvalidTimestampError,
    MissingSignatureError,
    MissingTimestampError,
    compute_signature,
    extract_webhook_headers,
    verify_signature,
    verify_signature_with_rotation,
)


class TestComputeSignature:
    def test_computes_hmac_sha256_signature(self) -> None:
        payload = "1234567890.{\"test\":true}"
        secret = "whsec_test_secret"
        signature = compute_signature(payload, secret)

        assert signature.startswith("v1=")
        assert len(signature) == 3 + 64  # "v1=" + 64 hex chars

    def test_produces_consistent_signatures(self) -> None:
        payload = "1234567890.{\"order_id\":123}"
        secret = "my_secret"

        sig1 = compute_signature(payload, secret)
        sig2 = compute_signature(payload, secret)

        assert sig1 == sig2

    def test_different_secrets_produce_different_signatures(self) -> None:
        payload = "1234567890.{\"test\":true}"

        sig1 = compute_signature(payload, "secret1")
        sig2 = compute_signature(payload, "secret2")

        assert sig1 != sig2


class TestVerifySignature:
    def test_verifies_valid_signature(self) -> None:
        secret = "whsec_test_secret"
        payload = '{"order_id":123}'
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}"
        signature = compute_signature(signed_payload, secret)

        # Should not raise
        verify_signature(payload, signature, timestamp, secret)

    def test_raises_missing_signature_error(self) -> None:
        timestamp = str(int(time.time()))

        with pytest.raises(MissingSignatureError):
            verify_signature('{"test":true}', "", timestamp, "secret")

    def test_raises_missing_timestamp_error(self) -> None:
        with pytest.raises(MissingTimestampError):
            verify_signature('{"test":true}', "v1=abc123", "", "secret")

    def test_raises_invalid_timestamp_for_non_numeric(self) -> None:
        with pytest.raises(InvalidTimestampError, match="not a valid number"):
            verify_signature('{"test":true}', "v1=abc123", "not-a-number", "secret")

    def test_raises_invalid_timestamp_for_expired(self) -> None:
        secret = "whsec_test_secret"
        payload = '{"test":true}'
        old_timestamp = str(int(time.time()) - 600)  # 10 minutes ago
        signed_payload = f"{old_timestamp}.{payload}"
        signature = compute_signature(signed_payload, secret)

        with pytest.raises(InvalidTimestampError, match="outside the tolerance window"):
            verify_signature(payload, signature, old_timestamp, secret)

    def test_raises_invalid_signature_for_wrong_signature(self) -> None:
        timestamp = str(int(time.time()))

        with pytest.raises(InvalidSignatureError):
            verify_signature('{"test":true}', "v1=wrong_signature", timestamp, "secret")

    def test_raises_invalid_signature_for_wrong_secret(self) -> None:
        secret = "whsec_test_secret"
        payload = '{"test":true}'
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}"
        signature = compute_signature(signed_payload, secret)

        with pytest.raises(InvalidSignatureError):
            verify_signature(payload, signature, timestamp, "wrong_secret")

    def test_accepts_custom_tolerance(self) -> None:
        secret = "whsec_test_secret"
        payload = '{"test":true}'
        old_timestamp = str(int(time.time()) - 400)  # 6+ minutes ago
        signed_payload = f"{old_timestamp}.{payload}"
        signature = compute_signature(signed_payload, secret)

        # Should fail with default 5 minute tolerance
        with pytest.raises(InvalidTimestampError):
            verify_signature(payload, signature, old_timestamp, secret)

        # Should pass with 10 minute tolerance
        verify_signature(payload, signature, old_timestamp, secret, tolerance_seconds=600)

    def test_works_with_bytes_payload(self) -> None:
        secret = "whsec_test_secret"
        payload = '{"order_id":123}'
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}"
        signature = compute_signature(signed_payload, secret)

        # Should not raise with bytes
        verify_signature(payload.encode("utf-8"), signature, timestamp, secret)


class TestVerifySignatureWithRotation:
    def test_verifies_with_current_secret(self) -> None:
        current_secret = "current_secret"
        previous_secret = "previous_secret"
        payload = '{"test":true}'
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}"
        signature = compute_signature(signed_payload, current_secret)

        # Should not raise
        verify_signature_with_rotation(
            payload, signature, timestamp, [current_secret, previous_secret]
        )

    def test_verifies_with_previous_secret(self) -> None:
        current_secret = "current_secret"
        previous_secret = "previous_secret"
        payload = '{"test":true}'
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}"
        signature = compute_signature(signed_payload, previous_secret)

        # Should not raise
        verify_signature_with_rotation(
            payload, signature, timestamp, [current_secret, previous_secret]
        )

    def test_raises_when_no_secret_matches(self) -> None:
        payload = '{"test":true}'
        timestamp = str(int(time.time()))
        signed_payload = f"{timestamp}.{payload}"
        signature = compute_signature(signed_payload, "unknown_secret")

        with pytest.raises(InvalidSignatureError):
            verify_signature_with_rotation(
                payload, signature, timestamp, ["current_secret", "previous_secret"]
            )

    def test_raises_with_empty_secrets_list(self) -> None:
        timestamp = str(int(time.time()))

        with pytest.raises(InvalidSignatureError):
            verify_signature_with_rotation('{"test":true}', "v1=abc", timestamp, [])


class TestExtractWebhookHeaders:
    def test_extracts_from_plain_dict(self) -> None:
        headers = {
            "X-Relay-Signature": "v1=abc123",
            "X-Relay-Timestamp": "1234567890",
        }

        signature, timestamp = extract_webhook_headers(headers)

        assert signature == "v1=abc123"
        assert timestamp == "1234567890"

    def test_extracts_lowercase_headers(self) -> None:
        headers = {
            "x-relay-signature": "v1=abc123",
            "x-relay-timestamp": "1234567890",
        }

        signature, timestamp = extract_webhook_headers(headers)

        assert signature == "v1=abc123"
        assert timestamp == "1234567890"

    def test_handles_missing_headers(self) -> None:
        headers: dict[str, str] = {}

        signature, timestamp = extract_webhook_headers(headers)

        assert signature == ""
        assert timestamp == ""

    def test_handles_list_values(self) -> None:
        headers = {
            "X-Relay-Signature": ["v1=abc123"],
            "X-Relay-Timestamp": ["1234567890"],
        }

        signature, timestamp = extract_webhook_headers(headers)

        assert signature == "v1=abc123"
        assert timestamp == "1234567890"
