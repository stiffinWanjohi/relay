"""Relay API client."""

from typing import Any, Optional, Union

import httpx

from .errors import APIError
from .types import (
    BatchRetryResult,
    Endpoint,
    EndpointList,
    EndpointStatus,
    Event,
    EventList,
    EventStatus,
    QueueStats,
    SecretRotationResult,
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

    # Event operations

    def create_event(
        self,
        idempotency_key: str,
        *,
        destination: Optional[str] = None,
        event_type: Optional[str] = None,
        payload: Any,
        headers: Optional[dict[str, str]] = None,
        max_attempts: Optional[int] = None,
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

        data = self._request(
            "POST",
            "/api/v1/events",
            json=body,
            headers={"X-Idempotency-Key": idempotency_key},
        )
        return Event.from_dict(data)

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

    # Endpoint operations

    def create_endpoint(
        self,
        url: str,
        event_types: list[str],
        *,
        description: Optional[str] = None,
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

    # Stats & Health

    def get_stats(self) -> QueueStats:
        """
        Get queue statistics.

        Returns:
            Current queue statistics
        """
        data = self._request("GET", "/api/v1/stats")
        return QueueStats.from_dict(data)

    def health_check(self) -> dict[str, str]:
        """
        Check service health.

        Returns:
            Health status
        """
        return self._request("GET", "/health")
