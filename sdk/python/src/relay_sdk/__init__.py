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
    # Alert types
    Alert,
    AlertAction,
    AlertActionType,
    AlertCondition,
    AlertMetric,
    AlertOperator,
    AlertRule,
    # Analytics types
    AnalyticsStats,
    BatchRetryError,
    BatchRetryResult,
    BreakdownItem,
    # Connector types
    Connector,
    ConnectorConfig,
    ConnectorTemplate,
    ConnectorType,
    DeliveryAttempt,
    Endpoint,
    EndpointList,
    EndpointStats,
    EndpointStatus,
    Event,
    EventList,
    EventStatus,
    # FIFO types
    FIFODrainResult,
    FIFOEndpointStats,
    FIFOQueueStats,
    FIFORecoveryResult,
    LatencyPercentiles,
    Pagination,
    # Queue stats
    PriorityQueueStats,
    QueueStats,
    SecretRotationResult,
    TimeGranularity,
    TimeSeriesPoint,
)

__version__ = "1.1.0"

__all__ = [
    # Client
    "RelayClient",
    # Event types
    "Event",
    "EventStatus",
    "EventList",
    "DeliveryAttempt",
    # Endpoint types
    "Endpoint",
    "EndpointStatus",
    "EndpointStats",
    "EndpointList",
    # Queue stats
    "QueueStats",
    "PriorityQueueStats",
    # FIFO types
    "FIFOQueueStats",
    "FIFOEndpointStats",
    "FIFODrainResult",
    "FIFORecoveryResult",
    # Pagination
    "Pagination",
    # Batch operations
    "BatchRetryResult",
    "BatchRetryError",
    "SecretRotationResult",
    # Analytics types
    "AnalyticsStats",
    "TimeSeriesPoint",
    "BreakdownItem",
    "LatencyPercentiles",
    "TimeGranularity",
    # Alert types
    "AlertRule",
    "Alert",
    "AlertCondition",
    "AlertAction",
    "AlertMetric",
    "AlertOperator",
    "AlertActionType",
    # Connector types
    "Connector",
    "ConnectorType",
    "ConnectorConfig",
    "ConnectorTemplate",
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
