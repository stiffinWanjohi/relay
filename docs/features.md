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
