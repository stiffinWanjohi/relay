"""Tests for Relay client."""

import pytest
from pytest_httpx import HTTPXMock

from relay_sdk import APIError, EventStatus, RelayClient


class TestRelayClient:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_strips_trailing_slash(self, httpx_mock: HTTPXMock) -> None:
        httpx_mock.add_response(json={"status": "ok"})

        client = RelayClient("https://api.example.com/", "key123")
        client.health_check()

        request = httpx_mock.get_request()
        assert request is not None
        assert str(request.url) == "https://api.example.com/health"

    def test_context_manager(self, httpx_mock: HTTPXMock) -> None:
        httpx_mock.add_response(json={"status": "ok"})

        with RelayClient("https://api.example.com", "key") as client:
            result = client.health_check()
            assert result["status"] == "ok"


class TestCreateEvent:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_creates_event_with_idempotency_key(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock
    ) -> None:
        httpx_mock.add_response(
            json={
                "id": "evt_123",
                "idempotencyKey": "order-123",
                "destination": "https://example.com/webhook",
                "status": "queued",
                "attempts": 0,
                "maxAttempts": 10,
                "createdAt": "2024-01-01T00:00:00Z",
            }
        )

        event = client.create_event(
            "order-123",
            destination="https://example.com/webhook",
            payload={"order_id": 123},
        )

        request = httpx_mock.get_request()
        assert request is not None
        assert request.headers["X-Idempotency-Key"] == "order-123"
        assert request.headers["X-API-Key"] == "rly_test_key"
        assert event.id == "evt_123"
        assert event.status == EventStatus.QUEUED


class TestGetEvent:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_gets_event_by_id(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "id": "evt_456",
                "idempotencyKey": "test-key",
                "destination": "https://example.com/webhook",
                "status": "delivered",
                "attempts": 1,
                "maxAttempts": 10,
                "createdAt": "2024-01-01T00:00:00Z",
                "deliveredAt": "2024-01-01T00:00:01Z",
                "deliveryAttempts": [
                    {
                        "id": "att_1",
                        "attemptNumber": 1,
                        "statusCode": 200,
                        "durationMs": 150,
                        "attemptedAt": "2024-01-01T00:00:01Z",
                    }
                ],
            }
        )

        event = client.get_event("evt_456")

        assert event.id == "evt_456"
        assert event.status == EventStatus.DELIVERED
        assert len(event.delivery_attempts) == 1
        assert event.delivery_attempts[0].status_code == 200


class TestListEvents:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_lists_events_with_filters(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "data": [
                    {
                        "id": "evt_1",
                        "idempotencyKey": "key-1",
                        "destination": "https://example.com",
                        "status": "failed",
                        "attempts": 3,
                        "maxAttempts": 10,
                        "createdAt": "2024-01-01T00:00:00Z",
                    }
                ],
                "pagination": {"hasMore": True, "cursor": "next", "total": 100},
            }
        )

        result = client.list_events(status=EventStatus.FAILED, limit=10)

        request = httpx_mock.get_request()
        assert request is not None
        assert "status=failed" in str(request.url)
        assert "limit=10" in str(request.url)
        assert len(result.data) == 1
        assert result.pagination.has_more is True

    def test_lists_events_without_filters(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "data": [],
                "pagination": {"hasMore": False, "total": 0},
            }
        )

        result = client.list_events()

        request = httpx_mock.get_request()
        assert request is not None
        assert str(request.url) == "https://api.relay.example.com/api/v1/events"
        assert len(result.data) == 0


class TestBatchRetry:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_batch_retry_by_ids(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "succeeded": [
                    {
                        "id": "evt_1",
                        "idempotencyKey": "key-1",
                        "destination": "https://example.com",
                        "status": "queued",
                        "attempts": 0,
                        "maxAttempts": 10,
                        "createdAt": "2024-01-01T00:00:00Z",
                    }
                ],
                "failed": [],
                "totalRequested": 1,
                "totalSucceeded": 1,
            }
        )

        result = client.batch_retry_by_ids(["evt_1", "evt_2"])

        assert result.total_succeeded == 1
        assert len(result.succeeded) == 1

    def test_batch_retry_by_status(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "succeeded": [],
                "failed": [],
                "totalRequested": 0,
                "totalSucceeded": 0,
            }
        )

        client.batch_retry_by_status(EventStatus.FAILED, limit=50)

        request = httpx_mock.get_request()
        assert request is not None
        import json
        body = json.loads(request.content)
        assert body["status"] == "failed"
        assert body["limit"] == 50


class TestEndpointOperations:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_creates_endpoint(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "id": "ep_123",
                "url": "https://example.com/hook",
                "eventTypes": ["order.created"],
                "status": "active",
                "maxRetries": 10,
                "retryBackoffMs": 1000,
                "retryBackoffMax": 86400000,
                "retryBackoffMult": 2.0,
                "timeoutMs": 30000,
                "rateLimitPerSec": 0,
                "circuitThreshold": 5,
                "circuitResetMs": 300000,
                "hasCustomSecret": False,
                "createdAt": "2024-01-01T00:00:00Z",
                "updatedAt": "2024-01-01T00:00:00Z",
            }
        )

        endpoint = client.create_endpoint(
            "https://example.com/hook",
            ["order.created"],
            max_retries=10,
        )

        assert endpoint.id == "ep_123"
        assert endpoint.url == "https://example.com/hook"

    def test_deletes_endpoint(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(status_code=204)

        client.delete_endpoint("ep_123")

        request = httpx_mock.get_request()
        assert request is not None
        assert request.method == "DELETE"

    def test_rotates_secret(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "endpoint": {
                    "id": "ep_123",
                    "url": "https://example.com/hook",
                    "eventTypes": ["order.created"],
                    "status": "active",
                    "maxRetries": 10,
                    "retryBackoffMs": 1000,
                    "retryBackoffMax": 86400000,
                    "retryBackoffMult": 2.0,
                    "timeoutMs": 30000,
                    "rateLimitPerSec": 0,
                    "circuitThreshold": 5,
                    "circuitResetMs": 300000,
                    "hasCustomSecret": True,
                    "createdAt": "2024-01-01T00:00:00Z",
                    "updatedAt": "2024-01-01T00:00:00Z",
                },
                "newSecret": "whsec_new_secret_123",
            }
        )

        result = client.rotate_endpoint_secret("ep_123")

        assert result.new_secret == "whsec_new_secret_123"
        assert result.endpoint.has_custom_secret is True


class TestErrorHandling:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_raises_api_error_for_404(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            status_code=404,
            json={"error": "Event not found", "code": "NOT_FOUND"},
        )

        with pytest.raises(APIError) as exc_info:
            client.get_event("unknown")

        assert exc_info.value.status_code == 404
        assert exc_info.value.is_not_found()
        assert exc_info.value.code == "NOT_FOUND"

    def test_raises_api_error_for_401(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            status_code=401,
            json={"error": "Invalid API key"},
        )

        with pytest.raises(APIError) as exc_info:
            client.list_events()

        assert exc_info.value.is_unauthorized()

    def test_raises_api_error_for_500(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            status_code=500,
            json={"error": "Internal server error"},
        )

        with pytest.raises(APIError) as exc_info:
            client.health_check()

        assert exc_info.value.is_server_error()


class TestStatsAndHealth:
    @pytest.fixture
    def client(self) -> RelayClient:
        return RelayClient("https://api.relay.example.com", "rly_test_key")

    def test_gets_stats(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(
            json={
                "queued": 10,
                "delivering": 5,
                "delivered": 100,
                "failed": 3,
                "dead": 1,
                "pending": 15,
                "processing": 5,
                "delayed": 2,
            }
        )

        stats = client.get_stats()

        assert stats.queued == 10
        assert stats.delivered == 100

    def test_health_check(
        self,
        client: RelayClient,
        httpx_mock: HTTPXMock,
    ) -> None:
        httpx_mock.add_response(json={"status": "ok"})

        result = client.health_check()

        assert result["status"] == "ok"
