# Features

## Event Type Catalog

Define event types with optional JSONSchema validation. Event types become first-class entities that can be browsed, documented, and validated against.

```graphql
# Create an event type with schema validation
mutation {
  createEventType(input: {
    name: "order.created"
    description: "Fired when a new order is placed"
    schema: {
      "type": "object",
      "required": ["order_id", "customer_id", "total"],
      "properties": {
        "order_id": { "type": "string" },
        "customer_id": { "type": "string" },
        "total": { "type": "number", "minimum": 0 }
      }
    }
    schemaVersion: "1.0"
  }) {
    id
    name
  }
}

# List all event types
query {
  eventTypes(first: 10) {
    edges {
      node { id name description schemaVersion }
    }
  }
}
```

When a schema is defined, payloads are validated on `sendEvent`. Invalid payloads are rejected with detailed error messages.

## Event Filtering (Content-Based Routing)

Route events to endpoints based on payload content using JSONPath expressions and comparison operators.

```graphql
mutation {
  createEndpoint(input: {
    url: "https://premium.example.com/hook"
    eventTypes: ["order.created"]
    filter: {
      "and": [
        { "path": "$.data.amount", "operator": "gt", "value": 100 },
        { "or": [
          { "path": "$.data.country", "operator": "eq", "value": "US" },
          { "path": "$.data.country", "operator": "eq", "value": "CA" }
        ]}
      ]
    }
  }) { id }
}
```

**Supported Operators:**
- `eq`, `ne` - Equality/inequality
- `gt`, `gte`, `lt`, `lte` - Numeric comparisons
- `contains`, `startsWith`, `endsWith` - String operations
- `exists` - Check if path exists
- `regex` - Regular expression matching

**Logical Combinators:** `and`, `or`, `not` (max 10 levels deep)

Events only delivered to endpoints where filters match. Unmatched events logged but not delivered.

## Payload Transformations

Transform webhook payloads before delivery using JavaScript. Modify URL, method, headers, body, or cancel delivery conditionally.

```graphql
mutation {
  createEndpoint(input: {
    url: "https://slack.example.com/hook"
    eventTypes: ["alert.*"]
    transformation: """
      function transform(webhook) {
        return {
          method: 'POST',
          url: webhook.url,
          headers: { 'Content-Type': 'application/json' },
          payload: {
            text: `Alert: ${webhook.payload.message}`,
            channel: '#alerts'
          },
          cancel: webhook.payload.severity === 'debug'
        };
      }
    """
  }) { id }
}
```

**Test transformations before saving:**

```graphql
mutation {
  testTransformation(
    code: "function transform(w) { return { ...w, payload: { wrapped: w.payload } }; }"
    samplePayload: { "order_id": 123 }
  ) {
    success
    result
    error
  }
}
```

**Safety Features:**
- 1-second execution timeout
- No filesystem, network, or system API access
- Memory limits enforced
- Errors logged, original payload preserved on failure

## FIFO Delivery (Ordered Delivery)

Guarantee ordered delivery for endpoints that require it. Events are delivered sequentially per endpoint/partition.

```graphql
mutation {
  createEndpoint(input: {
    url: "https://inventory.example.com/hook"
    eventTypes: ["stock.*"]
    fifo: true
    partitionKey: "$.product_id"  # Optional: parallel streams per product
  }) { id }
}
```

**How it works:**
- FIFO endpoints use separate queues per endpoint/partition
- Only one in-flight message at a time per queue
- Next message waits for ack/nack before sending
- Partition keys enable parallelism within ordering (e.g., `$.customer_id`)

**Management API:**

```graphql
# View FIFO queue statistics
query {
  fifoQueueStats(endpointId: "ep_123") {
    totalPartitions
    totalMessages
    partitionStats { partitionKey queueDepth locked }
  }
}

# Release stuck locks (after crash recovery)
mutation {
  releaseFIFOLock(endpointId: "ep_123", partitionKey: "customer-456")
}

# Drain a FIFO queue (optionally move to standard delivery)
mutation {
  drainFIFOQueue(endpointId: "ep_123", moveToStandard: true) {
    messagesProcessed
  }
}

# Recover stale messages from crashed workers
mutation {
  recoverStaleFIFOMessages { recoveredCount }
}
```

**Trade-offs:**
- Reduced throughput compared to parallel delivery
- Failures block subsequent events until retry succeeds
- Use partition keys to balance ordering guarantees with throughput

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

### Python SDK

Official Python client library:

```python
from relay_sdk import RelayClient, verify_signature, EventStatus

client = RelayClient("http://localhost:8080", "rly_xxx")

# Send event
event = client.create_event(
    "order-123",
    destination="https://example.com/webhook",
    payload={"order_id": 123},
)

# Batch retry failed events
result = client.batch_retry_by_status(EventStatus.FAILED, limit=100)

# Verify incoming webhooks
verify_signature(payload, signature, timestamp, "whsec_secret")
```

[Python SDK Documentation →](../sdk/python/README.md)
