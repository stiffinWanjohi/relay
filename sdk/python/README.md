# Relay Python SDK

Official Python SDK for the [Relay](https://github.com/stiffinWanjohi/relay) webhook delivery service.

## Installation

```bash
pip install relay-sdk
```

## Quick Start

```python
from relay_sdk import RelayClient

client = RelayClient("https://your-relay-instance.com", "your-api-key")

# Send a webhook
event = client.create_event(
    "order-123",  # idempotency key
    destination="https://example.com/webhook",
    payload={"order_id": 123, "status": "completed"},
)

print(f"Event created: {event.id}, status: {event.status}")
```

## Usage

### Creating Events

```python
from relay_sdk import RelayClient

client = RelayClient("https://relay.example.com", "rly_xxx")

# Direct delivery to a URL
event = client.create_event(
    "unique-idempotency-key",
    destination="https://example.com/webhook",
    payload={"user_id": 456},
    headers={"X-Custom-Header": "value"},
)

# Fan-out by event type (delivers to all subscribed endpoints)
event = client.create_event(
    "user-created-456",
    event_type="user.created",
    payload={"user_id": 456, "email": "user@example.com"},
)
```

### Managing Events

```python
from relay_sdk import EventStatus

# Get event details with delivery attempts
event = client.get_event("event-uuid")
print(f"Attempts: {len(event.delivery_attempts)}")

# List events with filtering
result = client.list_events(status=EventStatus.FAILED, limit=20)
for event in result.data:
    print(f"{event.id}: {event.status}")

# Replay a failed event
replayed = client.replay_event("event-uuid")

# Batch retry multiple events
result = client.batch_retry_by_ids(["id1", "id2", "id3"])
print(f"Retried: {result.total_succeeded}/{result.total_requested}")

# Batch retry by status
result = client.batch_retry_by_status(EventStatus.FAILED, limit=100)

# Batch retry by endpoint
result = client.batch_retry_by_endpoint("endpoint-uuid", limit=50)
```

### Managing Endpoints

```python
from relay_sdk import EndpointStatus

# Create an endpoint
endpoint = client.create_endpoint(
    "https://your-app.com/webhooks",
    ["order.created", "order.updated"],
    max_retries=5,
    timeout_ms=30000,
)

# List endpoints
result = client.list_endpoints(status=EndpointStatus.ACTIVE)

# Update an endpoint
endpoint = client.update_endpoint(
    "endpoint-uuid",
    status=EndpointStatus.PAUSED,
    max_retries=10,
)

# Rotate signing secret
result = client.rotate_endpoint_secret("endpoint-uuid")
print(f"New secret: {result.new_secret}")  # Save this securely!

# Clear previous secret after rotation is complete
client.clear_previous_secret("endpoint-uuid")

# Delete an endpoint
client.delete_endpoint("endpoint-uuid")
```

### Stats and Health

```python
# Get queue statistics
stats = client.get_stats()
print(f"Queued: {stats.queued}, Delivered: {stats.delivered}")

# Health check
health = client.health_check()
print(f"Status: {health['status']}")
```

## Verifying Webhook Signatures

When receiving webhooks, verify the signature to ensure authenticity:

```python
from relay_sdk import verify_signature, extract_webhook_headers

# Flask example
@app.route("/webhook", methods=["POST"])
def handle_webhook():
    signature, timestamp = extract_webhook_headers(request.headers)
    
    try:
        verify_signature(
            request.data,
            signature,
            timestamp,
            os.environ["WEBHOOK_SECRET"],
        )
    except Exception as e:
        return "Invalid signature", 401
    
    # Signature valid - process the webhook
    payload = request.json
    print(f"Received: {payload}")
    
    return "OK", 200
```

### Handling Secret Rotation

During secret rotation, verify against both current and previous secrets:

```python
from relay_sdk import verify_signature_with_rotation, extract_webhook_headers

@app.route("/webhook", methods=["POST"])
def handle_webhook():
    signature, timestamp = extract_webhook_headers(request.headers)
    
    secrets = [
        os.environ["CURRENT_SECRET"],
        os.environ.get("PREVIOUS_SECRET", ""),
    ]
    secrets = [s for s in secrets if s]  # Filter empty
    
    try:
        verify_signature_with_rotation(
            request.data,
            signature,
            timestamp,
            secrets,
        )
    except Exception:
        return "Invalid signature", 401
    
    return "OK", 200
```

## Error Handling

```python
from relay_sdk import RelayClient, APIError

client = RelayClient("https://relay.example.com", "rly_xxx")

try:
    event = client.get_event("non-existent-id")
except APIError as e:
    if e.is_not_found():
        print("Event not found")
    elif e.is_unauthorized():
        print("Invalid API key")
    elif e.is_rate_limited():
        print("Rate limited, retry later")
    else:
        print(f"API error: {e.message} ({e.status_code})")
```

### Error Types

| Error | Description |
|-------|-------------|
| `APIError` | HTTP error from the API |
| `InvalidSignatureError` | Webhook signature mismatch |
| `MissingSignatureError` | Missing signature header |
| `InvalidTimestampError` | Timestamp invalid or expired |
| `MissingTimestampError` | Missing timestamp header |

## Context Manager

The client can be used as a context manager:

```python
with RelayClient("https://relay.example.com", "rly_xxx") as client:
    event = client.create_event(
        "order-123",
        destination="https://example.com/webhook",
        payload={"order_id": 123},
    )
```

## Configuration

```python
client = RelayClient(
    "https://relay.example.com",
    "api-key",
    timeout=60.0,  # Request timeout in seconds (default: 30)
    headers={"X-Custom-Header": "value"},  # Additional headers
)
```

## Type Hints

Full type hint support with dataclasses:

```python
from relay_sdk import (
    Event,
    EventStatus,
    Endpoint,
    EndpointStatus,
    BatchRetryResult,
    QueueStats,
)
```

## License

MIT
