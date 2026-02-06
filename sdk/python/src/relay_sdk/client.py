"""Relay API client."""

from datetime import datetime
from typing import Any, Optional
from urllib.parse import quote

import httpx

from .errors import APIError
from .types import (
    Alert,
    AlertAction,
    AlertCondition,
    AlertRule,
    AnalyticsStats,
    BatchRetryResult,
    BreakdownItem,
    Connector,
    ConnectorConfig,
    ConnectorTemplate,
    ConnectorType,
    Endpoint,
    EndpointList,
    EndpointStatus,
    Event,
    EventList,
    EventStatus,
    FIFODrainResult,
    FIFOEndpointStats,
    FIFOQueueStats,
    FIFORecoveryResult,
    LatencyPercentiles,
    PriorityQueueStats,
    QueueStats,
    SecretRotationResult,
    TimeGranularity,
    TimeSeriesPoint,
)


class RelayClient:
    """Client for the Relay webhook delivery API."""

    def __init__(
        self,
        base_url: str,
        api_key: str,
        timeout: float = 30.0,
        headers: Optional[dict[str, str]] = None,
    ) -> None:
        """
        Initialize the Relay client.

        Args:
            base_url: Base URL of the Relay API (e.g., "https://relay.example.com")
            api_key: API key for authentication
            timeout: Request timeout in seconds (default: 30)
            headers: Additional headers to include in all requests
        """
        self.base_url = base_url.rstrip("/")
        self.api_key = api_key
        self._client = httpx.Client(
            base_url=self.base_url,
            timeout=timeout,
            headers={
                "Content-Type": "application/json",
                "X-API-Key": api_key,
                **(headers or {}),
            },
        )

    def close(self) -> None:
        """Close the HTTP client."""
        self._client.close()

    def __enter__(self) -> "RelayClient":
        return self

    def __exit__(self, *args: Any) -> None:
        self.close()

    def _request(
        self,
        method: str,
        path: str,
        json: Optional[Any] = None,
        headers: Optional[dict[str, str]] = None,
    ) -> Any:
        """Make an HTTP request."""
        response = self._client.request(method, path, json=json, headers=headers)

        if response.status_code >= 400:
            try:
                body = response.json()
            except Exception:
                body = response.text
            raise APIError.from_response(response.status_code, body)

        if response.status_code == 204:
            return None

        return response.json()

    # ==========================================
    # Event operations
    # ==========================================

    def create_event(
        self,
        idempotency_key: str,
        *,
        destination: Optional[str] = None,
        event_type: Optional[str] = None,
        payload: Any,
        headers: Optional[dict[str, str]] = None,
        max_attempts: Optional[int] = None,
        priority: Optional[int] = None,
        deliver_at: Optional[datetime] = None,
        delay_seconds: Optional[int] = None,
    ) -> Event:
        """
        Create a new webhook event.

        Args:
            idempotency_key: Unique key to prevent duplicate events
            destination: Webhook destination URL (required if no event_type)
            event_type: Event type for fan-out delivery
            payload: JSON payload to deliver
            headers: Custom headers to include in the webhook request
            max_attempts: Maximum delivery attempts (default: 10)
            priority: Priority 1-10 (1=highest, 10=lowest, default=5)
            deliver_at: Schedule for future delivery (absolute time)
            delay_seconds: Schedule for future delivery (relative delay)

        Returns:
            The created event
        """
        body: dict[str, Any] = {"payload": payload}
        if destination:
            body["destination"] = destination
        if event_type:
            body["eventType"] = event_type
        if headers:
            body["headers"] = headers
        if max_attempts is not None:
            body["maxAttempts"] = max_attempts
        if priority is not None:
            body["priority"] = priority
        if deliver_at is not None:
            body["deliverAt"] = deliver_at.isoformat()
        if delay_seconds is not None:
            body["delaySeconds"] = delay_seconds

        data = self._request(
            "POST",
            "/api/v1/events",
            json=body,
            headers={"X-Idempotency-Key": idempotency_key},
        )
        return Event.from_dict(data)

    def send_event(
        self,
        idempotency_key: str,
        *,
        event_type: str,
        payload: Any,
        headers: Optional[dict[str, str]] = None,
        priority: Optional[int] = None,
        deliver_at: Optional[datetime] = None,
        delay_seconds: Optional[int] = None,
    ) -> list[Event]:
        """
        Send an event by event type (fan-out to all subscribed endpoints).

        Args:
            idempotency_key: Unique key to prevent duplicate events
            event_type: Event type for routing
            payload: JSON payload to deliver
            headers: Custom headers to include in the webhook request
            priority: Priority 1-10 (1=highest, 10=lowest, default=5)
            deliver_at: Schedule for future delivery (absolute time)
            delay_seconds: Schedule for future delivery (relative delay)

        Returns:
            List of created events, one per subscribed endpoint
        """
        body: dict[str, Any] = {"eventType": event_type, "payload": payload}
        if headers:
            body["headers"] = headers
        if priority is not None:
            body["priority"] = priority
        if deliver_at is not None:
            body["deliverAt"] = deliver_at.isoformat()
        if delay_seconds is not None:
            body["delaySeconds"] = delay_seconds

        data = self._request(
            "POST",
            "/api/v1/events/send",
            json=body,
            headers={"X-Idempotency-Key": idempotency_key},
        )
        return [Event.from_dict(e) for e in data]

    def get_event(self, event_id: str) -> Event:
        """
        Get an event by ID with delivery history.

        Args:
            event_id: The event UUID

        Returns:
            The event with delivery attempts
        """
        data = self._request("GET", f"/api/v1/events/{event_id}")
        return Event.from_dict(data)

    def list_events(
        self,
        *,
        status: Optional[EventStatus] = None,
        limit: Optional[int] = None,
        cursor: Optional[str] = None,
    ) -> EventList:
        """
        List events with pagination.

        Args:
            status: Filter by event status
            limit: Maximum number of events to return
            cursor: Pagination cursor from previous response

        Returns:
            Paginated list of events
        """
        params: list[str] = []
        if status:
            params.append(f"status={status.value}")
        if limit:
            params.append(f"limit={limit}")
        if cursor:
            params.append(f"cursor={cursor}")

        path = "/api/v1/events"
        if params:
            path += "?" + "&".join(params)

        data = self._request("GET", path)
        return EventList.from_dict(data)

    def replay_event(self, event_id: str) -> Event:
        """
        Replay a failed or dead event.

        Args:
            event_id: The event UUID

        Returns:
            The replayed event
        """
        data = self._request("POST", f"/api/v1/events/{event_id}/replay")
        return Event.from_dict(data)

    def cancel_scheduled_event(self, event_id: str) -> Event:
        """
        Cancel a scheduled event before it is delivered.

        Only works for events that are still in QUEUED status with a future scheduledAt.

        Args:
            event_id: The event UUID

        Returns:
            The cancelled event
        """
        data = self._request("POST", f"/api/v1/events/{event_id}/cancel")
        return Event.from_dict(data)

    def batch_retry_by_ids(self, event_ids: list[str]) -> BatchRetryResult:
        """
        Retry multiple events by their IDs.

        Args:
            event_ids: List of event UUIDs to retry

        Returns:
            Result with succeeded and failed events
        """
        data = self._request("POST", "/api/v1/events/batch/retry", json={"event_ids": event_ids})
        return BatchRetryResult.from_dict(data)

    def batch_retry_by_status(
        self, status: EventStatus, limit: Optional[int] = None
    ) -> BatchRetryResult:
        """
        Retry events by status.

        Args:
            status: Event status to retry (failed or dead)
            limit: Maximum number of events to retry

        Returns:
            Result with succeeded and failed events
        """
        body: dict[str, Any] = {"status": status.value}
        if limit:
            body["limit"] = limit
        data = self._request("POST", "/api/v1/events/batch/retry", json=body)
        return BatchRetryResult.from_dict(data)

    def batch_retry_by_endpoint(
        self, endpoint_id: str, limit: Optional[int] = None
    ) -> BatchRetryResult:
        """
        Retry failed events for a specific endpoint.

        Args:
            endpoint_id: The endpoint UUID
            limit: Maximum number of events to retry

        Returns:
            Result with succeeded and failed events
        """
        body: dict[str, Any] = {"endpoint_id": endpoint_id}
        if limit:
            body["limit"] = limit
        data = self._request("POST", "/api/v1/events/batch/retry", json=body)
        return BatchRetryResult.from_dict(data)

    # ==========================================
    # Endpoint operations
    # ==========================================

    def create_endpoint(
        self,
        url: str,
        event_types: list[str],
        *,
        description: Optional[str] = None,
        filter: Optional[dict[str, Any]] = None,
        transformation: Optional[str] = None,
        fifo: Optional[bool] = None,
        fifo_partition_key: Optional[str] = None,
        max_retries: Optional[int] = None,
        retry_backoff_ms: Optional[int] = None,
        retry_backoff_max: Optional[int] = None,
        retry_backoff_mult: Optional[float] = None,
        timeout_ms: Optional[int] = None,
        rate_limit_per_sec: Optional[int] = None,
        circuit_threshold: Optional[int] = None,
        circuit_reset_ms: Optional[int] = None,
        custom_headers: Optional[dict[str, str]] = None,
    ) -> Endpoint:
        """
        Create a new webhook endpoint.

        Args:
            url: Webhook destination URL
            event_types: List of event types to subscribe to
            description: Optional description
            filter: Content-based routing filter (JSONPath rules)
            transformation: JavaScript code to transform payload
            fifo: Enable FIFO (ordered) delivery
            fifo_partition_key: JSONPath expression for partition key
            max_retries: Maximum retry attempts
            retry_backoff_ms: Initial backoff in milliseconds
            retry_backoff_max: Maximum backoff in milliseconds
            retry_backoff_mult: Backoff multiplier
            timeout_ms: Request timeout in milliseconds
            rate_limit_per_sec: Rate limit (requests per second)
            circuit_threshold: Circuit breaker failure threshold
            circuit_reset_ms: Circuit breaker reset time in milliseconds
            custom_headers: Custom headers to include in requests

        Returns:
            The created endpoint
        """
        body: dict[str, Any] = {"url": url, "eventTypes": event_types}
        if description:
            body["description"] = description
        if filter is not None:
            body["filter"] = filter
        if transformation is not None:
            body["transformation"] = transformation
        if fifo is not None:
            body["fifo"] = fifo
        if fifo_partition_key is not None:
            body["fifoPartitionKey"] = fifo_partition_key
        if max_retries is not None:
            body["maxRetries"] = max_retries
        if retry_backoff_ms is not None:
            body["retryBackoffMs"] = retry_backoff_ms
        if retry_backoff_max is not None:
            body["retryBackoffMax"] = retry_backoff_max
        if retry_backoff_mult is not None:
            body["retryBackoffMult"] = retry_backoff_mult
        if timeout_ms is not None:
            body["timeoutMs"] = timeout_ms
        if rate_limit_per_sec is not None:
            body["rateLimitPerSec"] = rate_limit_per_sec
        if circuit_threshold is not None:
            body["circuitThreshold"] = circuit_threshold
        if circuit_reset_ms is not None:
            body["circuitResetMs"] = circuit_reset_ms
        if custom_headers:
            body["customHeaders"] = custom_headers

        data = self._request("POST", "/api/v1/endpoints", json=body)
        return Endpoint.from_dict(data)

    def get_endpoint(self, endpoint_id: str) -> Endpoint:
        """
        Get an endpoint by ID.

        Args:
            endpoint_id: The endpoint UUID

        Returns:
            The endpoint
        """
        data = self._request("GET", f"/api/v1/endpoints/{endpoint_id}")
        return Endpoint.from_dict(data)

    def list_endpoints(
        self,
        *,
        status: Optional[EndpointStatus] = None,
        limit: Optional[int] = None,
        cursor: Optional[str] = None,
    ) -> EndpointList:
        """
        List endpoints with pagination.

        Args:
            status: Filter by endpoint status
            limit: Maximum number of endpoints to return
            cursor: Pagination cursor from previous response

        Returns:
            Paginated list of endpoints
        """
        params: list[str] = []
        if status:
            params.append(f"status={status.value}")
        if limit:
            params.append(f"limit={limit}")
        if cursor:
            params.append(f"cursor={cursor}")

        path = "/api/v1/endpoints"
        if params:
            path += "?" + "&".join(params)

        data = self._request("GET", path)
        return EndpointList.from_dict(data)

    def update_endpoint(
        self,
        endpoint_id: str,
        *,
        url: Optional[str] = None,
        description: Optional[str] = None,
        event_types: Optional[list[str]] = None,
        status: Optional[EndpointStatus] = None,
        filter: Optional[dict[str, Any]] = None,
        transformation: Optional[str] = None,
        fifo: Optional[bool] = None,
        fifo_partition_key: Optional[str] = None,
        max_retries: Optional[int] = None,
        retry_backoff_ms: Optional[int] = None,
        retry_backoff_max: Optional[int] = None,
        retry_backoff_mult: Optional[float] = None,
        timeout_ms: Optional[int] = None,
        rate_limit_per_sec: Optional[int] = None,
        circuit_threshold: Optional[int] = None,
        circuit_reset_ms: Optional[int] = None,
        custom_headers: Optional[dict[str, str]] = None,
    ) -> Endpoint:
        """
        Update an endpoint.

        Args:
            endpoint_id: The endpoint UUID
            url: New webhook URL
            description: New description
            event_types: New event types
            status: New status
            filter: Content-based routing filter (set to {} to remove)
            transformation: JavaScript transformation (set to "" to remove)
            fifo: Enable/disable FIFO delivery
            fifo_partition_key: New partition key expression
            max_retries: New max retries
            retry_backoff_ms: New initial backoff
            retry_backoff_max: New max backoff
            retry_backoff_mult: New backoff multiplier
            timeout_ms: New timeout
            rate_limit_per_sec: New rate limit
            circuit_threshold: New circuit threshold
            circuit_reset_ms: New circuit reset time
            custom_headers: New custom headers

        Returns:
            The updated endpoint
        """
        body: dict[str, Any] = {}
        if url is not None:
            body["url"] = url
        if description is not None:
            body["description"] = description
        if event_types is not None:
            body["eventTypes"] = event_types
        if status is not None:
            body["status"] = status.value
        if filter is not None:
            body["filter"] = filter
        if transformation is not None:
            body["transformation"] = transformation
        if fifo is not None:
            body["fifo"] = fifo
        if fifo_partition_key is not None:
            body["fifoPartitionKey"] = fifo_partition_key
        if max_retries is not None:
            body["maxRetries"] = max_retries
        if retry_backoff_ms is not None:
            body["retryBackoffMs"] = retry_backoff_ms
        if retry_backoff_max is not None:
            body["retryBackoffMax"] = retry_backoff_max
        if retry_backoff_mult is not None:
            body["retryBackoffMult"] = retry_backoff_mult
        if timeout_ms is not None:
            body["timeoutMs"] = timeout_ms
        if rate_limit_per_sec is not None:
            body["rateLimitPerSec"] = rate_limit_per_sec
        if circuit_threshold is not None:
            body["circuitThreshold"] = circuit_threshold
        if circuit_reset_ms is not None:
            body["circuitResetMs"] = circuit_reset_ms
        if custom_headers is not None:
            body["customHeaders"] = custom_headers

        data = self._request("PATCH", f"/api/v1/endpoints/{endpoint_id}", json=body)
        return Endpoint.from_dict(data)

    def delete_endpoint(self, endpoint_id: str) -> None:
        """
        Delete an endpoint.

        Args:
            endpoint_id: The endpoint UUID
        """
        self._request("DELETE", f"/api/v1/endpoints/{endpoint_id}")

    def pause_endpoint(self, endpoint_id: str) -> Endpoint:
        """
        Pause an endpoint (stops delivery but keeps events queued).

        Args:
            endpoint_id: The endpoint UUID

        Returns:
            The paused endpoint
        """
        data = self._request("POST", f"/api/v1/endpoints/{endpoint_id}/pause")
        return Endpoint.from_dict(data)

    def resume_endpoint(self, endpoint_id: str) -> Endpoint:
        """
        Resume a paused endpoint.

        Args:
            endpoint_id: The endpoint UUID

        Returns:
            The resumed endpoint
        """
        data = self._request("POST", f"/api/v1/endpoints/{endpoint_id}/resume")
        return Endpoint.from_dict(data)

    def rotate_endpoint_secret(self, endpoint_id: str) -> SecretRotationResult:
        """
        Rotate an endpoint's signing secret.

        The new secret is returned only once. The previous secret remains
        valid during the rotation grace period.

        Args:
            endpoint_id: The endpoint UUID

        Returns:
            The endpoint and new secret
        """
        data = self._request("POST", f"/api/v1/endpoints/{endpoint_id}/rotate-secret")
        return SecretRotationResult.from_dict(data)

    def clear_previous_secret(self, endpoint_id: str) -> Endpoint:
        """
        Clear the previous secret after rotation is complete.

        Args:
            endpoint_id: The endpoint UUID

        Returns:
            The updated endpoint
        """
        data = self._request("POST", f"/api/v1/endpoints/{endpoint_id}/clear-previous-secret")
        return Endpoint.from_dict(data)

    # ==========================================
    # Stats & Health
    # ==========================================

    def get_stats(self) -> QueueStats:
        """
        Get queue statistics.

        Returns:
            Current queue statistics
        """
        data = self._request("GET", "/api/v1/stats")
        return QueueStats.from_dict(data)

    def get_priority_queue_stats(self) -> PriorityQueueStats:
        """
        Get priority queue statistics.

        Returns:
            Priority queue statistics
        """
        data = self._request("GET", "/api/v1/stats/priority")
        return PriorityQueueStats.from_dict(data)

    def health_check(self) -> dict[str, str]:
        """
        Check service health.

        Returns:
            Health status
        """
        return self._request("GET", "/health")

    # ==========================================
    # FIFO Queue Management
    # ==========================================

    def get_fifo_queue_stats(self, endpoint_id: str) -> FIFOEndpointStats:
        """
        Get FIFO queue stats for a specific endpoint.

        Args:
            endpoint_id: The endpoint UUID

        Returns:
            FIFO statistics for the endpoint
        """
        data = self._request("GET", f"/api/v1/fifo/{endpoint_id}/stats")
        return FIFOEndpointStats.from_dict(data)

    def get_fifo_partition_stats(self, endpoint_id: str, partition_key: str) -> FIFOQueueStats:
        """
        Get FIFO queue stats for a specific partition.

        Args:
            endpoint_id: The endpoint UUID
            partition_key: The partition key

        Returns:
            FIFO statistics for the partition
        """
        data = self._request(
            "GET", f"/api/v1/fifo/{endpoint_id}/partitions/{quote(partition_key)}/stats"
        )
        return FIFOQueueStats.from_dict(data)

    def list_active_fifo_queues(self) -> list[FIFOQueueStats]:
        """
        List all active FIFO queues.

        Returns:
            List of active FIFO queue stats
        """
        data = self._request("GET", "/api/v1/fifo/active")
        return [FIFOQueueStats.from_dict(q) for q in data]

    def release_fifo_lock(self, endpoint_id: str, partition_key: Optional[str] = None) -> bool:
        """
        Forcibly release a stuck FIFO lock (admin operation).

        Args:
            endpoint_id: The endpoint UUID
            partition_key: Optional partition key

        Returns:
            True if lock was released
        """
        body: dict[str, str] = {}
        if partition_key:
            body["partitionKey"] = partition_key
        return self._request("POST", f"/api/v1/fifo/{endpoint_id}/release-lock", json=body)

    def drain_fifo_queue(
        self,
        endpoint_id: str,
        partition_key: Optional[str] = None,
        move_to_standard: Optional[bool] = None,
    ) -> FIFODrainResult:
        """
        Drain a FIFO queue and optionally move messages to standard queue.

        Args:
            endpoint_id: The endpoint UUID
            partition_key: Optional partition key
            move_to_standard: Whether to move messages to standard queue

        Returns:
            Drain result
        """
        body: dict[str, Any] = {}
        if partition_key:
            body["partitionKey"] = partition_key
        if move_to_standard is not None:
            body["moveToStandard"] = move_to_standard
        data = self._request("POST", f"/api/v1/fifo/{endpoint_id}/drain", json=body)
        return FIFODrainResult.from_dict(data)

    def recover_stale_fifo_messages(self) -> FIFORecoveryResult:
        """
        Recover all stale in-flight FIFO messages (admin operation).

        Returns:
            Recovery result
        """
        data = self._request("POST", "/api/v1/fifo/recover")
        return FIFORecoveryResult.from_dict(data)

    # ==========================================
    # Analytics
    # ==========================================

    def get_analytics_stats(self, start: datetime, end: datetime) -> AnalyticsStats:
        """
        Get aggregated delivery statistics for a time range.

        Args:
            start: Start of time range
            end: End of time range

        Returns:
            Aggregated statistics
        """
        data = self._request(
            "GET", f"/api/v1/analytics/stats?start={start.isoformat()}&end={end.isoformat()}"
        )
        return AnalyticsStats.from_dict(data)

    def get_success_rate_time_series(
        self, start: datetime, end: datetime, granularity: TimeGranularity
    ) -> list[TimeSeriesPoint]:
        """
        Get success rate time series.

        Args:
            start: Start of time range
            end: End of time range
            granularity: Time granularity

        Returns:
            List of time series points
        """
        data = self._request(
            "GET",
            f"/api/v1/analytics/success-rate?start={start.isoformat()}&end={end.isoformat()}&granularity={granularity.value}",
        )
        return [TimeSeriesPoint.from_dict(p) for p in data]

    def get_latency_time_series(
        self, start: datetime, end: datetime, granularity: TimeGranularity
    ) -> list[TimeSeriesPoint]:
        """
        Get latency time series (average).

        Args:
            start: Start of time range
            end: End of time range
            granularity: Time granularity

        Returns:
            List of time series points
        """
        data = self._request(
            "GET",
            f"/api/v1/analytics/latency?start={start.isoformat()}&end={end.isoformat()}&granularity={granularity.value}",
        )
        return [TimeSeriesPoint.from_dict(p) for p in data]

    def get_latency_percentiles(self, start: datetime, end: datetime) -> LatencyPercentiles:
        """
        Get latency percentiles for a time range.

        Args:
            start: Start of time range
            end: End of time range

        Returns:
            Latency percentiles
        """
        data = self._request(
            "GET",
            f"/api/v1/analytics/latency-percentiles?start={start.isoformat()}&end={end.isoformat()}",
        )
        return LatencyPercentiles.from_dict(data)

    def get_breakdown_by_event_type(
        self, start: datetime, end: datetime, limit: Optional[int] = None
    ) -> list[BreakdownItem]:
        """
        Get breakdown by event type.

        Args:
            start: Start of time range
            end: End of time range
            limit: Maximum items to return

        Returns:
            List of breakdown items
        """
        path = (
            f"/api/v1/analytics/breakdown/event-type"
            f"?start={start.isoformat()}&end={end.isoformat()}"
        )
        if limit:
            path += f"&limit={limit}"
        data = self._request("GET", path)
        return [BreakdownItem.from_dict(b) for b in data]

    def get_breakdown_by_endpoint(
        self, start: datetime, end: datetime, limit: Optional[int] = None
    ) -> list[BreakdownItem]:
        """
        Get breakdown by endpoint.

        Args:
            start: Start of time range
            end: End of time range
            limit: Maximum items to return

        Returns:
            List of breakdown items
        """
        path = (
            f"/api/v1/analytics/breakdown/endpoint"
            f"?start={start.isoformat()}&end={end.isoformat()}"
        )
        if limit:
            path += f"&limit={limit}"
        data = self._request("GET", path)
        return [BreakdownItem.from_dict(b) for b in data]

    # ==========================================
    # Alert Rules
    # ==========================================

    def create_alert_rule(
        self,
        name: str,
        condition: AlertCondition,
        action: AlertAction,
        *,
        description: Optional[str] = None,
        cooldown: Optional[str] = None,
    ) -> AlertRule:
        """
        Create a new alert rule.

        Args:
            name: Rule name
            condition: Alert condition
            action: Alert action
            description: Optional description
            cooldown: Cooldown period (e.g., "5m", "1h")

        Returns:
            The created alert rule
        """
        body: dict[str, Any] = {
            "name": name,
            "condition": condition.to_dict(),
            "action": action.to_dict(),
        }
        if description:
            body["description"] = description
        if cooldown:
            body["cooldown"] = cooldown
        data = self._request("POST", "/api/v1/alerts/rules", json=body)
        return AlertRule.from_dict(data)

    def get_alert_rule(self, rule_id: str) -> AlertRule:
        """
        Get an alert rule by ID.

        Args:
            rule_id: The rule UUID

        Returns:
            The alert rule
        """
        data = self._request("GET", f"/api/v1/alerts/rules/{rule_id}")
        return AlertRule.from_dict(data)

    def list_alert_rules(self) -> list[AlertRule]:
        """
        List all alert rules.

        Returns:
            List of alert rules
        """
        data = self._request("GET", "/api/v1/alerts/rules")
        return [AlertRule.from_dict(r) for r in data]

    def update_alert_rule(
        self,
        rule_id: str,
        *,
        name: Optional[str] = None,
        description: Optional[str] = None,
        enabled: Optional[bool] = None,
        condition: Optional[AlertCondition] = None,
        action: Optional[AlertAction] = None,
        cooldown: Optional[str] = None,
    ) -> AlertRule:
        """
        Update an alert rule.

        Args:
            rule_id: The rule UUID
            name: New name
            description: New description
            enabled: Enable/disable rule
            condition: New condition
            action: New action
            cooldown: New cooldown period

        Returns:
            The updated alert rule
        """
        body: dict[str, Any] = {}
        if name is not None:
            body["name"] = name
        if description is not None:
            body["description"] = description
        if enabled is not None:
            body["enabled"] = enabled
        if condition is not None:
            body["condition"] = condition.to_dict()
        if action is not None:
            body["action"] = action.to_dict()
        if cooldown is not None:
            body["cooldown"] = cooldown
        data = self._request("PATCH", f"/api/v1/alerts/rules/{rule_id}", json=body)
        return AlertRule.from_dict(data)

    def delete_alert_rule(self, rule_id: str) -> None:
        """
        Delete an alert rule.

        Args:
            rule_id: The rule UUID
        """
        self._request("DELETE", f"/api/v1/alerts/rules/{rule_id}")

    def enable_alert_rule(self, rule_id: str) -> AlertRule:
        """
        Enable an alert rule.

        Args:
            rule_id: The rule UUID

        Returns:
            The enabled alert rule
        """
        data = self._request("POST", f"/api/v1/alerts/rules/{rule_id}/enable")
        return AlertRule.from_dict(data)

    def disable_alert_rule(self, rule_id: str) -> AlertRule:
        """
        Disable an alert rule.

        Args:
            rule_id: The rule UUID

        Returns:
            The disabled alert rule
        """
        data = self._request("POST", f"/api/v1/alerts/rules/{rule_id}/disable")
        return AlertRule.from_dict(data)

    def get_alert_history(self, limit: Optional[int] = None) -> list[Alert]:
        """
        Get alert history.

        Args:
            limit: Maximum alerts to return

        Returns:
            List of fired alerts
        """
        path = "/api/v1/alerts/history"
        if limit:
            path += f"?limit={limit}"
        data = self._request("GET", path)
        return [Alert.from_dict(a) for a in data]

    # ==========================================
    # Connectors
    # ==========================================

    def create_connector(
        self,
        name: str,
        connector_type: ConnectorType,
        config: ConnectorConfig,
        template: Optional[ConnectorTemplate] = None,
    ) -> Connector:
        """
        Create a new connector.

        Args:
            name: Connector name
            connector_type: Type of connector
            config: Connector configuration
            template: Message template

        Returns:
            The created connector
        """
        body: dict[str, Any] = {
            "name": name,
            "type": connector_type.value,
            "config": config.to_dict(),
        }
        if template:
            body["template"] = template.to_dict()
        data = self._request("POST", "/api/v1/connectors", json=body)
        return Connector.from_dict(data)

    def get_connector(self, name: str) -> Connector:
        """
        Get a connector by name.

        Args:
            name: Connector name

        Returns:
            The connector
        """
        data = self._request("GET", f"/api/v1/connectors/{quote(name)}")
        return Connector.from_dict(data)

    def list_connectors(self) -> list[Connector]:
        """
        List all connectors.

        Returns:
            List of connectors
        """
        data = self._request("GET", "/api/v1/connectors")
        return [Connector.from_dict(c) for c in data]

    def update_connector(
        self,
        name: str,
        *,
        config: Optional[ConnectorConfig] = None,
        template: Optional[ConnectorTemplate] = None,
    ) -> Connector:
        """
        Update a connector.

        Args:
            name: Connector name
            config: New configuration
            template: New template

        Returns:
            The updated connector
        """
        body: dict[str, Any] = {}
        if config is not None:
            body["config"] = config.to_dict()
        if template is not None:
            body["template"] = template.to_dict()
        data = self._request("PATCH", f"/api/v1/connectors/{quote(name)}", json=body)
        return Connector.from_dict(data)

    def delete_connector(self, name: str) -> None:
        """
        Delete a connector.

        Args:
            name: Connector name
        """
        self._request("DELETE", f"/api/v1/connectors/{quote(name)}")
