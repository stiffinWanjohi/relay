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


class AlertMetric(str, Enum):
    """Metrics that can trigger alerts."""

    FAILURE_RATE = "FAILURE_RATE"
    SUCCESS_RATE = "SUCCESS_RATE"
    LATENCY = "LATENCY"
    QUEUE_DEPTH = "QUEUE_DEPTH"
    ERROR_COUNT = "ERROR_COUNT"
    DELIVERY_COUNT = "DELIVERY_COUNT"


class AlertOperator(str, Enum):
    """Comparison operators for alert conditions."""

    GT = "GT"
    GTE = "GTE"
    LT = "LT"
    LTE = "LTE"
    EQ = "EQ"
    NE = "NE"


class AlertActionType(str, Enum):
    """Types of alert actions."""

    SLACK = "SLACK"
    EMAIL = "EMAIL"
    WEBHOOK = "WEBHOOK"
    PAGERDUTY = "PAGERDUTY"


class ConnectorType(str, Enum):
    """Types of connectors."""

    SLACK = "SLACK"
    DISCORD = "DISCORD"
    TEAMS = "TEAMS"
    EMAIL = "EMAIL"
    WEBHOOK = "WEBHOOK"


class TimeGranularity(str, Enum):
    """Time granularity for analytics queries."""

    MINUTE = "MINUTE"
    HOUR = "HOUR"
    DAY = "DAY"
    WEEK = "WEEK"


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
    priority: int
    attempts: int
    max_attempts: int
    created_at: datetime
    event_type: Optional[str] = None
    payload: Optional[Any] = None
    headers: Optional[dict[str, str]] = None
    scheduled_at: Optional[datetime] = None
    delivered_at: Optional[datetime] = None
    next_attempt_at: Optional[datetime] = None
    delivery_attempts: list[DeliveryAttempt] = field(default_factory=list)

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Event":
        """Create from API response dict."""
        scheduled_at = None
        if data.get("scheduledAt"):
            scheduled_at = datetime.fromisoformat(data["scheduledAt"].replace("Z", "+00:00"))

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
            priority=data.get("priority", 5),
            attempts=data["attempts"],
            max_attempts=data["maxAttempts"],
            created_at=datetime.fromisoformat(data["createdAt"].replace("Z", "+00:00")),
            event_type=data.get("eventType"),
            payload=data.get("payload"),
            headers=data.get("headers"),
            scheduled_at=scheduled_at,
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
    fifo: bool
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
    filter: Optional[dict[str, Any]] = None
    transformation: Optional[str] = None
    fifo_partition_key: Optional[str] = None
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
            fifo=data.get("fifo", False),
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
            filter=data.get("filter"),
            transformation=data.get("transformation"),
            fifo_partition_key=data.get("fifoPartitionKey"),
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
class PriorityQueueStats:
    """Priority queue statistics."""

    high: int
    normal: int
    low: int
    delayed: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "PriorityQueueStats":
        """Create from API response dict."""
        return cls(
            high=data.get("high", 0),
            normal=data.get("normal", 0),
            low=data.get("low", 0),
            delayed=data.get("delayed", 0),
        )


@dataclass
class FIFOQueueStats:
    """FIFO queue statistics for a partition."""

    endpoint_id: str
    partition_key: str
    queue_length: int
    is_locked: bool
    has_in_flight: bool

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "FIFOQueueStats":
        """Create from API response dict."""
        return cls(
            endpoint_id=data["endpointId"],
            partition_key=data["partitionKey"],
            queue_length=data["queueLength"],
            is_locked=data["isLocked"],
            has_in_flight=data["hasInFlight"],
        )


@dataclass
class FIFOEndpointStats:
    """FIFO statistics for an endpoint."""

    endpoint_id: str
    total_partitions: int
    total_queued_messages: int
    partitions: list[FIFOQueueStats]

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "FIFOEndpointStats":
        """Create from API response dict."""
        return cls(
            endpoint_id=data["endpointId"],
            total_partitions=data["totalPartitions"],
            total_queued_messages=data["totalQueuedMessages"],
            partitions=[FIFOQueueStats.from_dict(p) for p in data.get("partitions", [])],
        )


@dataclass
class FIFODrainResult:
    """Result of draining a FIFO queue."""

    endpoint_id: str
    messages_drained: int
    moved_to_standard: bool
    partition_key: Optional[str] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "FIFODrainResult":
        """Create from API response dict."""
        return cls(
            endpoint_id=data["endpointId"],
            messages_drained=data["messagesDrained"],
            moved_to_standard=data["movedToStandard"],
            partition_key=data.get("partitionKey"),
        )


@dataclass
class FIFORecoveryResult:
    """Result of recovering stale FIFO messages."""

    messages_recovered: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "FIFORecoveryResult":
        """Create from API response dict."""
        return cls(messages_recovered=data["messagesRecovered"])


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


# Analytics types


@dataclass
class AnalyticsStats:
    """Aggregated statistics for a time period."""

    period: str
    total_count: int
    success_count: int
    failure_count: int
    timeout_count: int
    success_rate: float
    failure_rate: float
    avg_latency_ms: float
    p50_latency_ms: float
    p95_latency_ms: float
    p99_latency_ms: float
    min_latency_ms: int
    max_latency_ms: int

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AnalyticsStats":
        """Create from API response dict."""
        return cls(
            period=data["period"],
            total_count=data["totalCount"],
            success_count=data["successCount"],
            failure_count=data["failureCount"],
            timeout_count=data["timeoutCount"],
            success_rate=data["successRate"],
            failure_rate=data["failureRate"],
            avg_latency_ms=data["avgLatencyMs"],
            p50_latency_ms=data["p50LatencyMs"],
            p95_latency_ms=data["p95LatencyMs"],
            p99_latency_ms=data["p99LatencyMs"],
            min_latency_ms=data["minLatencyMs"],
            max_latency_ms=data["maxLatencyMs"],
        )


@dataclass
class TimeSeriesPoint:
    """A single point in a time series."""

    timestamp: datetime
    value: float

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "TimeSeriesPoint":
        """Create from API response dict."""
        return cls(
            timestamp=datetime.fromisoformat(data["timestamp"].replace("Z", "+00:00")),
            value=data["value"],
        )


@dataclass
class BreakdownItem:
    """Breakdown item for analytics by dimension."""

    key: str
    count: int
    success_rate: float
    avg_latency_ms: float

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "BreakdownItem":
        """Create from API response dict."""
        return cls(
            key=data["key"],
            count=data["count"],
            success_rate=data["successRate"],
            avg_latency_ms=data["avgLatencyMs"],
        )


@dataclass
class LatencyPercentiles:
    """Latency percentiles."""

    p50: float
    p95: float
    p99: float

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "LatencyPercentiles":
        """Create from API response dict."""
        return cls(
            p50=data["p50"],
            p95=data["p95"],
            p99=data["p99"],
        )


# Alert types


@dataclass
class AlertCondition:
    """Condition that triggers an alert."""

    metric: AlertMetric
    operator: AlertOperator
    value: float
    window: str

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AlertCondition":
        """Create from API response dict."""
        return cls(
            metric=AlertMetric(data["metric"]),
            operator=AlertOperator(data["operator"]),
            value=data["value"],
            window=data["window"],
        )

    def to_dict(self) -> dict[str, Any]:
        """Convert to API request dict."""
        return {
            "metric": self.metric.value,
            "operator": self.operator.value,
            "value": self.value,
            "window": self.window,
        }


@dataclass
class AlertAction:
    """Action to take when alert fires."""

    type: AlertActionType
    webhook_url: Optional[str] = None
    channel: Optional[str] = None
    to: Optional[list[str]] = None
    subject: Optional[str] = None
    routing_key: Optional[str] = None
    severity: Optional[str] = None
    message: Optional[str] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AlertAction":
        """Create from API response dict."""
        return cls(
            type=AlertActionType(data["type"]),
            webhook_url=data.get("webhookUrl"),
            channel=data.get("channel"),
            to=data.get("to"),
            subject=data.get("subject"),
            routing_key=data.get("routingKey"),
            severity=data.get("severity"),
            message=data.get("message"),
        )

    def to_dict(self) -> dict[str, Any]:
        """Convert to API request dict."""
        result: dict[str, Any] = {"type": self.type.value}
        if self.webhook_url:
            result["webhookUrl"] = self.webhook_url
        if self.channel:
            result["channel"] = self.channel
        if self.to:
            result["to"] = self.to
        if self.subject:
            result["subject"] = self.subject
        if self.routing_key:
            result["routingKey"] = self.routing_key
        if self.severity:
            result["severity"] = self.severity
        if self.message:
            result["message"] = self.message
        return result


@dataclass
class AlertRule:
    """Alert rule for monitoring webhook delivery."""

    id: str
    name: str
    enabled: bool
    condition: AlertCondition
    action: AlertAction
    cooldown: str
    created_at: datetime
    updated_at: datetime
    description: Optional[str] = None
    last_fired_at: Optional[datetime] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "AlertRule":
        """Create from API response dict."""
        last_fired_at = None
        if data.get("lastFiredAt"):
            last_fired_at = datetime.fromisoformat(data["lastFiredAt"].replace("Z", "+00:00"))

        return cls(
            id=data["id"],
            name=data["name"],
            enabled=data["enabled"],
            condition=AlertCondition.from_dict(data["condition"]),
            action=AlertAction.from_dict(data["action"]),
            cooldown=data["cooldown"],
            created_at=datetime.fromisoformat(data["createdAt"].replace("Z", "+00:00")),
            updated_at=datetime.fromisoformat(data["updatedAt"].replace("Z", "+00:00")),
            description=data.get("description"),
            last_fired_at=last_fired_at,
        )


@dataclass
class Alert:
    """Alert that has been fired."""

    id: str
    rule_id: str
    rule_name: str
    metric: str
    value: float
    threshold: float
    message: str
    fired_at: datetime

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Alert":
        """Create from API response dict."""
        return cls(
            id=data["id"],
            rule_id=data["ruleId"],
            rule_name=data["ruleName"],
            metric=data["metric"],
            value=data["value"],
            threshold=data["threshold"],
            message=data["message"],
            fired_at=datetime.fromisoformat(data["firedAt"].replace("Z", "+00:00")),
        )


# Connector types


@dataclass
class ConnectorConfig:
    """Configuration for a connector."""

    webhook_url: Optional[str] = None
    channel: Optional[str] = None
    username: Optional[str] = None
    icon_emoji: Optional[str] = None
    icon_url: Optional[str] = None
    smtp_host: Optional[str] = None
    smtp_port: Optional[int] = None
    from_email: Optional[str] = None
    to_emails: Optional[list[str]] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ConnectorConfig":
        """Create from API response dict."""
        return cls(
            webhook_url=data.get("webhookUrl"),
            channel=data.get("channel"),
            username=data.get("username"),
            icon_emoji=data.get("iconEmoji"),
            icon_url=data.get("iconUrl"),
            smtp_host=data.get("smtpHost"),
            smtp_port=data.get("smtpPort"),
            from_email=data.get("fromEmail"),
            to_emails=data.get("toEmails"),
        )

    def to_dict(self) -> dict[str, Any]:
        """Convert to API request dict."""
        result: dict[str, Any] = {}
        if self.webhook_url:
            result["webhookUrl"] = self.webhook_url
        if self.channel:
            result["channel"] = self.channel
        if self.username:
            result["username"] = self.username
        if self.icon_emoji:
            result["iconEmoji"] = self.icon_emoji
        if self.icon_url:
            result["iconUrl"] = self.icon_url
        if self.smtp_host:
            result["smtpHost"] = self.smtp_host
        if self.smtp_port:
            result["smtpPort"] = self.smtp_port
        if self.from_email:
            result["fromEmail"] = self.from_email
        if self.to_emails:
            result["toEmails"] = self.to_emails
        return result


@dataclass
class ConnectorTemplate:
    """Template for connector messages."""

    text: Optional[str] = None
    title: Optional[str] = None
    body: Optional[str] = None
    color: Optional[str] = None
    subject: Optional[str] = None

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "ConnectorTemplate":
        """Create from API response dict."""
        return cls(
            text=data.get("text"),
            title=data.get("title"),
            body=data.get("body"),
            color=data.get("color"),
            subject=data.get("subject"),
        )

    def to_dict(self) -> dict[str, Any]:
        """Convert to API request dict."""
        result: dict[str, Any] = {}
        if self.text:
            result["text"] = self.text
        if self.title:
            result["title"] = self.title
        if self.body:
            result["body"] = self.body
        if self.color:
            result["color"] = self.color
        if self.subject:
            result["subject"] = self.subject
        return result


@dataclass
class Connector:
    """A pre-built integration connector."""

    name: str
    type: ConnectorType
    config: ConnectorConfig
    template: ConnectorTemplate

    @classmethod
    def from_dict(cls, data: dict[str, Any]) -> "Connector":
        """Create from API response dict."""
        return cls(
            name=data["name"],
            type=ConnectorType(data["type"]),
            config=ConnectorConfig.from_dict(data["config"]),
            template=ConnectorTemplate.from_dict(data.get("template", {})),
        )
