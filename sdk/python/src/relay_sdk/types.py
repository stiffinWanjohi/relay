"""Type definitions for Relay SDK."""

from dataclasses import dataclass, field
from datetime import datetime
from enum import Enum
from typing import Any, Optional


class EventStatus(str, Enum):
    """Status of an event."""

    QUEUED = "queued"
    DELIVERING = "delivering"
    DELIVERED = "delivered"
    FAILED = "failed"
    DEAD = "dead"


class EndpointStatus(str, Enum):
    """Status of an endpoint."""

    ACTIVE = "active"
    PAUSED = "paused"
    DISABLED = "disabled"


@dataclass
class DeliveryAttempt:
    """A single delivery attempt."""

    id: str
    attempt_number: int
    duration_ms: int
    attempted_at: datetime
    status_code: Optional[int] = None
    response_body: Optional[str] = None
    error: Optional[str] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "DeliveryAttempt":
        """Create from API response dict."""
        return cls(
            id=data["id"],
            attempt_number=data["attemptNumber"],
            duration_ms=data["durationMs"],
            attempted_at=datetime.fromisoformat(data["attemptedAt"].replace("Z", "+00:00")),
            status_code=data.get("statusCode"),
            response_body=data.get("responseBody"),
            error=data.get("error"),
        )


@dataclass
class Event:
    """A webhook event."""

    id: str
    idempotency_key: str
    destination: str
    status: EventStatus
    attempts: int
    max_attempts: int
    created_at: datetime
    event_type: Optional[str] = None
    payload: Optional[Any] = None
    headers: Optional[dict[str, str]] = None
    delivered_at: Optional[datetime] = None
    next_attempt_at: Optional[datetime] = None
    delivery_attempts: list[DeliveryAttempt] = field(default_factory=list)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Event":
        """Create from API response dict."""
        delivered_at = None
        if data.get("deliveredAt"):
            delivered_at = datetime.fromisoformat(data["deliveredAt"].replace("Z", "+00:00"))

        next_attempt_at = None
        if data.get("nextAttemptAt"):
            next_attempt_at = datetime.fromisoformat(data["nextAttemptAt"].replace("Z", "+00:00"))

        delivery_attempts = [
            DeliveryAttempt.from_dict(a) for a in data.get("deliveryAttempts", [])
        ]

        return cls(
            id=data["id"],
            idempotency_key=data["idempotencyKey"],
            destination=data["destination"],
            status=EventStatus(data["status"]),
            attempts=data["attempts"],
            max_attempts=data["maxAttempts"],
            created_at=datetime.fromisoformat(data["createdAt"].replace("Z", "+00:00")),
            event_type=data.get("eventType"),
            payload=data.get("payload"),
            headers=data.get("headers"),
            delivered_at=delivered_at,
            next_attempt_at=next_attempt_at,
            delivery_attempts=delivery_attempts,
        )


@dataclass
class Pagination:
    """Pagination information."""

    has_more: bool
    total: int
    cursor: Optional[str] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Pagination":
        """Create from API response dict."""
        return cls(
            has_more=data["hasMore"],
            total=data["total"],
            cursor=data.get("cursor"),
        )


@dataclass
class EventList:
    """Paginated list of events."""

    data: list[Event]
    pagination: Pagination

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "EventList":
        """Create from API response dict."""
        return cls(
            data=[Event.from_dict(e) for e in data["data"]],
            pagination=Pagination.from_dict(data["pagination"]),
        )


@dataclass
class EndpointStats:
    """Statistics for an endpoint."""

    total_events: int
    delivered: int
    failed: int
    pending: int
    success_rate: float
    avg_latency_ms: float

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "EndpointStats":
        """Create from API response dict."""
        return cls(
            total_events=data["totalEvents"],
            delivered=data["delivered"],
            failed=data["failed"],
            pending=data["pending"],
            success_rate=data["successRate"],
            avg_latency_ms=data["avgLatencyMs"],
        )


@dataclass
class Endpoint:
    """A webhook endpoint."""

    id: str
    url: str
    event_types: list[str]
    status: EndpointStatus
    max_retries: int
    retry_backoff_ms: int
    retry_backoff_max: int
    retry_backoff_mult: float
    timeout_ms: int
    rate_limit_per_sec: int
    circuit_threshold: int
    circuit_reset_ms: int
    has_custom_secret: bool
    created_at: datetime
    updated_at: datetime
    description: Optional[str] = None
    custom_headers: Optional[dict[str, str]] = None
    secret_rotated_at: Optional[datetime] = None
    stats: Optional[EndpointStats] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Endpoint":
        """Create from API response dict."""
        secret_rotated_at = None
        if data.get("secretRotatedAt"):
            secret_rotated_at = datetime.fromisoformat(
                data["secretRotatedAt"].replace("Z", "+00:00")
            )

        stats = None
        if data.get("stats"):
            stats = EndpointStats.from_dict(data["stats"])

        return cls(
            id=data["id"],
            url=data["url"],
            event_types=data["eventTypes"],
            status=EndpointStatus(data["status"]),
            max_retries=data["maxRetries"],
            retry_backoff_ms=data["retryBackoffMs"],
            retry_backoff_max=data["retryBackoffMax"],
            retry_backoff_mult=data["retryBackoffMult"],
            timeout_ms=data["timeoutMs"],
            rate_limit_per_sec=data["rateLimitPerSec"],
            circuit_threshold=data["circuitThreshold"],
            circuit_reset_ms=data["circuitResetMs"],
            has_custom_secret=data["hasCustomSecret"],
            created_at=datetime.fromisoformat(data["createdAt"].replace("Z", "+00:00")),
            updated_at=datetime.fromisoformat(data["updatedAt"].replace("Z", "+00:00")),
            description=data.get("description"),
            custom_headers=data.get("customHeaders"),
            secret_rotated_at=secret_rotated_at,
            stats=stats,
        )


@dataclass
class EndpointList:
    """Paginated list of endpoints."""

    data: list[Endpoint]
    pagination: Pagination

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "EndpointList":
        """Create from API response dict."""
        return cls(
            data=[Endpoint.from_dict(e) for e in data["data"]],
            pagination=Pagination.from_dict(data["pagination"]),
        )


@dataclass
class QueueStats:
    """Queue statistics."""

    queued: int
    delivering: int
    delivered: int
    failed: int
    dead: int
    pending: int
    processing: int
    delayed: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "QueueStats":
        """Create from API response dict."""
        return cls(
            queued=data.get("queued", 0),
            delivering=data.get("delivering", 0),
            delivered=data.get("delivered", 0),
            failed=data.get("failed", 0),
            dead=data.get("dead", 0),
            pending=data.get("pending", 0),
            processing=data.get("processing", 0),
            delayed=data.get("delayed", 0),
        )


@dataclass
class BatchRetryError:
    """An event that failed to retry."""

    event_id: str
    error: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "BatchRetryError":
        """Create from API response dict."""
        return cls(
            event_id=data["eventId"],
            error=data["error"],
        )


@dataclass
class BatchRetryResult:
    """Result of a batch retry operation."""

    succeeded: list[Event]
    failed: list[BatchRetryError]
    total_requested: int
    total_succeeded: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "BatchRetryResult":
        """Create from API response dict."""
        return cls(
            succeeded=[Event.from_dict(e) for e in data["succeeded"]],
            failed=[BatchRetryError.from_dict(e) for e in data["failed"]],
            total_requested=data["totalRequested"],
            total_succeeded=data["totalSucceeded"],
        )


@dataclass
class SecretRotationResult:
    """Result of a secret rotation."""

    endpoint: Endpoint
    new_secret: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "SecretRotationResult":
        """Create from API response dict."""
        return cls(
            endpoint=Endpoint.from_dict(data["endpoint"]),
            new_secret=data["newSecret"],
        )
