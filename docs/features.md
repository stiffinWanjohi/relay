# Features

## Guaranteed Delivery

Transactional outbox pattern. Events are written to PostgreSQL in the same transaction as your application logic. A background processor moves them to Redis for delivery.

Even if Redis is down, events are safe. When it recovers, delivery resumes.

```
Your App → PostgreSQL (event + outbox) → Outbox Processor → Redis → Worker → Destination
```

## Idempotency

Every event requires an idempotency key. Send the same key twice:

- **Within 24h**: Returns original event, no duplicate created
- **After 24h**: Creates new event (use versioned keys like `order-123-v2`)

```graphql
mutation {
  createEvent(
    input: { destination: "https://example.com/hook", payload: { order: 123 } }
    idempotencyKey: "order-123"  # Same key = same event
  ) { id }
}
```

## Retry Policy

Exponential backoff with jitter:

| Attempt | Delay | Cumulative |
|---------|-------|------------|
| 1 | 1s | 1s |
| 2 | 5s | 6s |
| 3 | 30s | 36s |
| 4 | 2m | ~2.5m |
| 5 | 10m | ~12m |
| 6 | 30m | ~42m |
| 7 | 1h | ~1.7h |
| 8 | 2h | ~3.7h |
| 9 | 6h | ~9.7h |
| 10 | 24h | ~34h |

After attempt 10 → dead letter queue. Configurable per-endpoint.

## Circuit Breaker

Per-destination. Prevents hammering failing endpoints.

```
CLOSED → (5 failures) → OPEN → (5 min) → HALF-OPEN
                                              ↓
                                    success → CLOSED
                                    failure → OPEN
```

**States:**
- **Closed**: Normal, requests flow through
- **Open**: All requests fail fast, queued for later
- **Half-Open**: Single probe request, success closes circuit

## Failure Notifications

Get alerted when circuit breakers trip via Slack or email:

```bash
# Enable notifications
NOTIFICATION_ENABLED=true
NOTIFICATION_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...
NOTIFICATION_SMTP_HOST=smtp.example.com
NOTIFICATION_SMTP_PORT=587
NOTIFICATION_EMAIL_TO=ops@example.com,oncall@example.com
```

Notification types:
- **Circuit Trip**: When a circuit breaker opens due to failures
- **Circuit Recovery**: When a circuit breaker recovers
- **Endpoint Disabled**: When an endpoint is disabled

## Batch Retry

Retry multiple failed events at once:

```graphql
# Retry by IDs
mutation {
  retryEvents(ids: ["evt_1", "evt_2"]) {
    succeeded { id status }
    failed { eventId error }
  }
}

# Retry by status
mutation {
  retryEventsByStatus(status: FAILED, limit: 100) {
    succeeded { id }
    failed { eventId error }
  }
}

# Retry by endpoint
mutation {
  retryEventsByEndpoint(endpointId: "ep_123", status: DEAD, limit: 50) {
    succeeded { id }
    failed { eventId error }
  }
}
```

REST API:
```bash
curl -X POST http://localhost:8080/api/v1/events/batch/retry \
  -H "X-API-Key: $KEY" \
  -d '{"event_ids": ["evt_1", "evt_2"]}'
```

## Per-Endpoint Signing Secrets

Each endpoint can have its own signing secret with rotation support:

```graphql
# Rotate secret (generates new, keeps old for grace period)
mutation {
  rotateEndpointSecret(id: "ep_123") {
    endpoint { id hasCustomSecret }
    newSecret  # Only shown once!
  }
}

# Clear old secret after rotation is complete
mutation {
  clearPreviousSecret(id: "ep_123") { id }
}
```

During rotation, both old and new secrets are valid, allowing receivers to update without downtime.

## Event Fan-out

Subscribe endpoints to event types with wildcards:

```graphql
mutation {
  createEndpoint(input: {
    url: "https://analytics.example.com/hook"
    eventTypes: ["order.created", "order.*", "*"]
  }) { id }
}
```

Send one event, deliver to all matching endpoints.

## Per-Endpoint Configuration

Each endpoint can have custom settings:

```graphql
mutation {
  createEndpoint(input: {
    url: "https://critical-service.com/hook"
    eventTypes: ["payment.*"]
    
    maxRetries: 20           # More attempts for critical
    retryBackoffMs: 100      # Start faster
    retryBackoffMax: 3600000 # Cap at 1 hour
    timeoutMs: 10000         # 10s timeout
    rateLimitPerSec: 100     # Max 100 req/s
    circuitThreshold: 10     # More tolerance
  }) { id }
}
```

## Fair Scheduling

Deficit round-robin (DRR) ensures one noisy client doesn't starve others.

Each client gets a "quantum" of bandwidth. High-volume clients use their quantum faster but then wait while others catch up.

## Observability

**Metrics (OpenTelemetry):**
- `relay_events_total` - Events by status
- `relay_delivery_duration_ms` - Delivery latency histogram
- `relay_queue_depth` - Current queue size
- `relay_circuit_state` - Circuit breaker states

**Delivery History:**

```graphql
query {
  event(id: "...") {
    deliveryAttempts {
      attemptNumber
      statusCode
      durationMs
      error
      attemptedAt
    }
  }
}
```

## Queue Stats

```graphql
query {
  queueStats {
    queued      # Waiting to be sent
    delivering  # Currently being sent
    delivered   # Successfully delivered
    failed      # Will retry
    dead        # Gave up
  }
}
```

## SDKs

### Go SDK

Official Go client library:

```go
import "github.com/stiffinWanjohi/relay/sdk/go"

client := relay.NewClient("http://localhost:8080", "rly_xxx")

// Send event
event, err := client.CreateEvent(ctx, "order-123", relay.CreateEventRequest{
    Destination: "https://example.com/webhook",
    Payload:     map[string]any{"order_id": 123},
})

// Batch retry failed events
result, err := client.BatchRetryByStatus(ctx, relay.EventStatusFailed, 100)

// Verify incoming webhooks
err := relay.VerifySignature(payload, signature, timestamp, "whsec_secret")
```

[Go SDK Documentation →](../sdk/go/README.md)

### TypeScript SDK

Official TypeScript/JavaScript client library:

```typescript
import { RelayClient, verifySignature } from '@relay/sdk';

const client = new RelayClient('http://localhost:8080', 'rly_xxx');

// Send event
const event = await client.createEvent(
  { destination: 'https://example.com/webhook', payload: { order_id: 123 } },
  'order-123'
);

// Batch retry failed events
const result = await client.batchRetry({ status: 'failed', limit: 100 });

// Verify incoming webhooks
verifySignature(payload, signature, timestamp, 'whsec_secret');
```

[TypeScript SDK Documentation →](../sdk/typescript/README.md)
