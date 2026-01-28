"""Relay SDK - Python client for Relay webhook delivery service."""

from .client import RelayClient
from .errors import (
    APIError,
    InvalidSignatureError,
    InvalidTimestampError,
    MissingSignatureError,
    MissingTimestampError,
    RelayError,
    SignatureError,
)
from .signature import (
    compute_signature,
    extract_webhook_headers,
    verify_signature,
    verify_signature_with_rotation,
)
from .types import (
    BatchRetryError,
    BatchRetryResult,
    DeliveryAttempt,
    Endpoint,
    EndpointList,
    EndpointStats,
    EndpointStatus,
    Event,
    EventList,
    EventStatus,
    Pagination,
    QueueStats,
)

__version__ = "1.0.0"

__all__ = [
    # Client
    "RelayClient",
    # Types
    "Event",
    "EventStatus",
    "EventList",
    "DeliveryAttempt",
    "Endpoint",
    "EndpointStatus",
    "EndpointStats",
    "EndpointList",
    "QueueStats",
    "Pagination",
    "BatchRetryResult",
    "BatchRetryError",
    # Errors
    "RelayError",
    "APIError",
    "SignatureError",
    "InvalidSignatureError",
    "MissingSignatureError",
    "InvalidTimestampError",
    "MissingTimestampError",
    # Signature
    "verify_signature",
    "verify_signature_with_rotation",
    "compute_signature",
    "extract_webhook_headers",
]
