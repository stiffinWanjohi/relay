"""Webhook signature verification."""

import hashlib
import hmac
import time
from typing import Any, Optional, Union

from .errors import (
    InvalidSignatureError,
    InvalidTimestampError,
    MissingSignatureError,
    MissingTimestampError,
)

DEFAULT_TOLERANCE_SECONDS = 300  # 5 minutes


def verify_signature(
    payload: Union[str, bytes],
    signature: str,
    timestamp: str,
    secret: str,
    *,
    tolerance_seconds: int = DEFAULT_TOLERANCE_SECONDS,
    current_timestamp: Optional[int] = None,
) -> None:
    """
    Verify a webhook signature from Relay.

    Args:
        payload: The raw request body as a string or bytes
        signature: The X-Relay-Signature header value
        timestamp: The X-Relay-Timestamp header value
        secret: Your webhook signing secret
        tolerance_seconds: Maximum age of the timestamp (default: 300)
        current_timestamp: Override current time for testing

    Raises:
        MissingSignatureError: If signature is missing
        MissingTimestampError: If timestamp is missing
        InvalidTimestampError: If timestamp is invalid or too old
        InvalidSignatureError: If signature doesn't match
    """
    if not signature:
        raise MissingSignatureError()
    if not timestamp:
        raise MissingTimestampError()

    # Parse timestamp
    try:
        ts = int(timestamp)
    except ValueError as err:
        raise InvalidTimestampError("Timestamp is not a valid number") from err

    # Check timestamp is within tolerance
    now = current_timestamp if current_timestamp is not None else int(time.time())
    if abs(now - ts) > tolerance_seconds:
        raise InvalidTimestampError(
            f"Timestamp is outside the tolerance window ({tolerance_seconds}s)"
        )

    # Compute expected signature
    payload_str = payload if isinstance(payload, str) else payload.decode("utf-8")
    signed_payload = f"{timestamp}.{payload_str}"
    expected_signature = compute_signature(signed_payload, secret)

    # Compare signatures using timing-safe comparison
    if not hmac.compare_digest(signature, expected_signature):
        raise InvalidSignatureError()


def verify_signature_with_rotation(
    payload: Union[str, bytes],
    signature: str,
    timestamp: str,
    secrets: list[str],
    *,
    tolerance_seconds: int = DEFAULT_TOLERANCE_SECONDS,
    current_timestamp: Optional[int] = None,
) -> None:
    """
    Verify a webhook using multiple secrets (for secret rotation).

    Returns successfully if ANY secret validates the signature.

    Args:
        payload: The raw request body
        signature: The X-Relay-Signature header value
        timestamp: The X-Relay-Timestamp header value
        secrets: List of secrets to try (current + previous)
        tolerance_seconds: Maximum age of the timestamp (default: 300)
        current_timestamp: Override current time for testing

    Raises:
        MissingSignatureError: If signature is missing
        MissingTimestampError: If timestamp is missing
        InvalidTimestampError: If timestamp is invalid or too old
        InvalidSignatureError: If no secret validates the signature
    """
    if not signature:
        raise MissingSignatureError()
    if not timestamp:
        raise MissingTimestampError()
    if not secrets:
        raise InvalidSignatureError()

    # Parse and validate timestamp
    try:
        ts = int(timestamp)
    except ValueError as err:
        raise InvalidTimestampError("Timestamp is not a valid number") from err

    now = current_timestamp if current_timestamp is not None else int(time.time())
    if abs(now - ts) > tolerance_seconds:
        raise InvalidTimestampError(
            f"Timestamp is outside the tolerance window ({tolerance_seconds}s)"
        )

    # Try each secret
    payload_str = payload if isinstance(payload, str) else payload.decode("utf-8")
    signed_payload = f"{timestamp}.{payload_str}"

    for secret in secrets:
        expected_signature = compute_signature(signed_payload, secret)
        if hmac.compare_digest(signature, expected_signature):
            return  # Valid signature found

    raise InvalidSignatureError()


def compute_signature(payload: str, secret: str) -> str:
    """
    Compute an HMAC-SHA256 signature in the Relay format.

    Args:
        payload: The signed payload (timestamp.body)
        secret: The signing secret

    Returns:
        The signature in "v1=<hex>" format
    """
    mac = hmac.new(secret.encode("utf-8"), payload.encode("utf-8"), hashlib.sha256)
    return f"v1={mac.hexdigest()}"


def extract_webhook_headers(headers: dict[str, Any]) -> tuple[str, str]:
    """
    Extract webhook headers from a request headers object.

    Works with various header dict formats (case-insensitive).

    Args:
        headers: Headers dict from the request

    Returns:
        Tuple of (signature, timestamp)
    """

    def get_header(name: str) -> str:
        # Try exact match first
        if name in headers:
            value = headers[name]
            return value[0] if isinstance(value, list) else str(value) if value else ""
        # Try lowercase
        lower_name = name.lower()
        if lower_name in headers:
            value = headers[lower_name]
            return value[0] if isinstance(value, list) else str(value) if value else ""
        # Try case-insensitive search
        for key, value in headers.items():
            if key.lower() == lower_name:
                return value[0] if isinstance(value, list) else str(value) if value else ""
        return ""

    return (
        get_header("X-Relay-Signature"),
        get_header("X-Relay-Timestamp"),
    )
