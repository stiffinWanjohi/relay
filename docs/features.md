# Features

## Global Rate Limiting

Protect your system from overload with configurable rate limits at both global and per-client levels.

```bash
# Set global rate limit (all requests)
GLOBAL_RATE_LIMIT_RPS=1000

# Set per-client rate limit (requires authentication)
CLIENT_RATE_LIMIT_RPS=100
```

**Features:**
- **Global limit**: Applies to all API requests across all clients
- **Per-client limit**: Each authenticated client has independent limits
- **429 response**: Returns `Too Many Requests` with helpful headers
- **Redis-backed**: Distributed rate limiting across multiple instances

**Response headers on rate limit:**
```
HTTP/1.1 429 Too Many Requests
Retry-After: 1
X-RateLimit-Limit: 100
X-RateLimit-Reset: 1707123456
Content-Type: application/json

{"error":"rate limit exceeded","retry_after":1}
```

Rate limits use a sliding window algorithm for accurate enforcement.

---

## Webhook Debugger

Test and debug webhooks with temporary endpoints that capture full request details.

**CLI Usage:**
```bash
# Start a debug session (creates temporary endpoint)
relay debug

# Use an existing endpoint
relay debug --endpoint abc123def456

# List captured requests
relay debug --endpoint abc123def456 --list
```

**API Endpoints:**

```bash
# Create a debug endpoint
curl -X POST http://localhost:8080/debug/endpoints

# Response:
{
  "id": "abc123def456",
  "url": "http://localhost:8080/debug/abc123def456",
  "created_at": "2026-02-05T10:00:00Z",
  "expires_at": "2026-02-05T11:00:00Z"
}

# Send webhooks to the debug URL
curl -X POST http://localhost:8080/debug/abc123def456/webhook \
  -H "Content-Type: application/json" \
  -d '{"event": "test"}'

# List captured requests
curl http://localhost:8080/debug/endpoints/abc123def456/requests

# Stream requests in real-time (SSE)
curl http://localhost:8080/debug/endpoints/abc123def456/stream

# Replay a captured request
curl -X POST http://localhost:8080/debug/endpoints/abc123def456/requests/req123/replay \
  -d '{"url": "https://your-server.com/webhook"}'
```

**Features:**
- **Temporary endpoints**: Auto-expire after 1 hour
- **Full request capture**: Headers, body, timing, remote address
- **Real-time streaming**: SSE for live updates in CLI or browser
- **Request replay**: Resend captured requests to any URL
- **Max 100 requests**: Per endpoint to prevent memory issues

---

## Connectors (Pre-built Integrations)

Pre-built integrations for common destinations with automatic payload transformation.

**Supported Connectors:**
- **Slack** - Send messages to Slack channels via webhooks
- **Discord** - Post to Discord channels with embeds
- **Microsoft Teams** - Message cards for Teams channels
- **Email** - SMTP-based email notifications
- **Webhook** - Generic webhook (pass-through)

**Connector Configuration:**

```json
{
  "type": "slack",
  "config": {
    "webhook_url": "https://hooks.slack.com/services/xxx",
    "channel": "#alerts",
    "username": "Relay Bot",
    "icon_emoji": ":robot_face:"
  },
  "template": {
    "text": "{{.event_type}}: {{.message}}",
    "title": "New Event",
    "color": "danger"
  }
}
```

**Template Variables:**

Templates use Go's `text/template` syntax with access to:
- `{{.event_type}}` - The event type name
- `{{.payload}}` - The full payload object
- `{{.field_name}}` - Any top-level field from the payload

**Slack Example:**

```go
connector.NewSlackConnector("https://hooks.slack.com/xxx",
    connector.WithChannel("#alerts"),
    connector.WithUsername("Relay"),
    connector.WithTemplate(connector.Template{
        Text:  "{{.event_type}}: {{.message}}",
        Color: "danger",
    }),
)
```

**Discord Example:**

```go
connector.NewDiscordConnector("https://discord.com/api/webhooks/xxx",
    connector.WithUsername("Relay"),
    connector.WithTemplate(connector.Template{
        Text:  "{{.message}}",
        Title: "{{.event_type}}",
        Color: "blue",
    }),
)
```

**Teams Example:**

```go
connector.NewTeamsConnector("https://outlook.office.com/webhook/xxx",
    connector.WithTemplate(connector.Template{
        Text:  "{{.message}}",
        Title: "{{.event_type}}",
        Color: "0076D7",
    }),
)
```

**Email Example:**

```go
connector.NewEmailConnector("smtp.example.com", 587, "from@example.com", []string{"to@example.com"},
    connector.WithSMTPAuth("username", "password"),
    connector.WithTemplate(connector.Template{
        Subject: "[Alert] {{.event_type}}",
        Body:    "Event: {{.event_type}}\nMessage: {{.message}}",
    }),
)
```

**Payload Transformation:**

Connectors automatically transform event payloads to the format expected by each service:

| Connector | Output Format |
|-----------|--------------|
| Slack | `{"text": "...", "channel": "...", "attachments": [...]}` |
| Discord | `{"content": "...", "embeds": [...]}` |
| Teams | `{"@type": "MessageCard", "sections": [...]}` |
| Email | SMTP message with subject and body |

---

## Custom Alerting Rules

Define custom conditions for triggering alerts beyond basic circuit breaker notifications.

**Creating Alert Rules:**

```go
import "github.com/stiffinWanjohi/relay/internal/alerting"

// Create a rule for high failure rates
rule := alerting.NewRule(
    "High Failure Rate",
    alerting.Condition{
        Metric:   alerting.ConditionFailureRate,
        Operator: alerting.OpGreaterThan,
        Value:    0.1,  // 10% failure rate
        Window:   alerting.Duration(5 * time.Minute),
    },
    alerting.Action{
        Type: alerting.ActionSlack,
        Config: alerting.ActionConfig{
            WebhookURL: "https://hooks.slack.com/services/xxx",
            Channel:    "#oncall",
        },
        Message: "Failure rate exceeded 10% in the last 5 minutes",
    },
)

// Set cooldown to prevent alert storms
rule.Cooldown = alerting.Duration(15 * time.Minute)
```

**Supported Condition Metrics:**
- `failure_rate` - Percentage of failed deliveries (0.0 - 1.0)
- `latency` - Average delivery latency in milliseconds
- `queue_depth` - Number of messages in queue
- `error_count` - Total error count in window
- `success_rate` - Percentage of successful deliveries

**Supported Operators:**
- `gt` (greater than), `gte` (greater than or equal)
- `lt` (less than), `lte` (less than or equal)
- `eq` (equal), `ne` (not equal)

**Supported Actions:**
- `slack` - Send to Slack webhook
- `email` - Send via SMTP
- `webhook` - POST to any URL
- `pagerduty` - Create PagerDuty incident

**Using the Alert Engine:**

```go
// Create engine with metrics provider
engine := alerting.NewEngine(metricsProvider)

// Add rules
engine.AddRule(rule)

// Start evaluation loop (checks every minute by default)
engine.Start(ctx)

// Query alert history
history := engine.GetAlertHistory(10)  // Last 10 alerts
for _, alert := range history {
    fmt.Printf("Alert: %s fired at %v (value: %.2f)\n",
        alert.RuleName, alert.FiredAt, alert.Value)
}

// Enable/disable rules
engine.DisableRule(rule.ID)
engine.EnableRule(rule.ID)

// Remove rule
engine.RemoveRule(rule.ID)

// Stop engine
engine.Stop()
```

**Alert Rule Example (JSON):**

```json
{
  "name": "High Failure Rate",
  "condition": {
    "metric": "failure_rate",
    "operator": "gt",
    "value": 0.1,
    "window": "5m"
  },
  "action": {
    "type": "slack",
    "config": {
      "webhook_url": "https://hooks.slack.com/services/xxx",
      "channel": "#oncall"
    },
    "message": "Failure rate exceeded 10% in last 5 minutes"
  },
  "cooldown": "15m"
}
```

**Features:**
- **Cooldown periods**: Prevent alert storms with configurable cooldowns
- **Alert history**: Query past alerts with timestamps and values
- **Enable/disable**: Temporarily disable rules without deleting them
- **Multiple actions**: Slack, email, webhook, PagerDuty support
- **Background evaluation**: Automatic periodic checks (default: 1 minute)

---

## Log Streaming

Real-time streaming of delivery logs for monitoring and debugging.

**CLI Usage:**

```bash
# Stream all logs
relay logs --follow

# Short form
relay logs -f

# Filter by event type
relay logs -f --event-type=order.created,payment.completed

# Filter by endpoint
relay logs -f --endpoint=abc123,def456

# Filter by status
relay logs -f --status=failed,dead

# Filter by log level (minimum level)
relay logs -f --level=warn

# Output as JSON (for piping to jq, etc.)
relay logs -f --json

# Combine filters
relay logs -f --event-type=order.* --status=failed --level=error
```

**HTTP API:**

```bash
# Stream logs via SSE (Server-Sent Events)
curl -N http://localhost:8080/logs/stream

# With filters
curl -N "http://localhost:8080/logs/stream?event_type=order.created&status=failed&level=warn"

# Get streaming stats
curl http://localhost:8080/logs/stats
```

**Log Entry Format:**

```json
{
  "timestamp": "2026-02-05T14:30:00Z",
  "level": "info",
  "event_id": "evt_abc123",
  "event_type": "order.created",
  "endpoint_id": "ep_def456",
  "destination": "https://example.com/webhook",
  "status": "delivered",
  "status_code": 200,
  "duration_ms": 150,
  "attempt": 1,
  "max_attempts": 10,
  "client_id": "client_xyz",
  "message": "delivery successful"
}
```

**Filter Parameters:**
- `event_type` - Comma-separated list of event types to include
- `endpoint_id` - Comma-separated list of endpoint IDs to include
- `status` - Comma-separated list of statuses: `queued`, `delivering`, `delivered`, `failed`, `dead`
- `client_id` - Comma-separated list of client IDs
- `level` - Minimum log level: `debug`, `info`, `warn`, `error`

**Log Levels:**
- `debug` - Detailed information for debugging
- `info` - Normal operational messages (delivery start, success)
- `warn` - Warning messages (failures that will retry, circuit trips)
- `error` - Error messages (max retries exceeded, dead letter)

**Statuses:**
- `delivering` - Delivery attempt in progress
- `delivered` - Successfully delivered
- `failed` - Delivery failed, will retry
- `dead` - Max retries exceeded, moved to dead letter
- `circuit_open` - Circuit breaker tripped
- `circuit_closed` - Circuit breaker recovered

**Features:**
- **Real-time**: Uses Server-Sent Events (SSE) for instant updates
- **Filtering**: Filter by event type, endpoint, status, client, and log level
- **Rate limiting**: Built-in rate limiting to prevent stream overload
- **Buffering**: Per-subscriber buffer to handle temporary slowdowns
- **CLI formatting**: Colorized output with smart truncation

---

## Webhook Analytics

Analytics and reporting for webhook delivery performance with time-series data and breakdowns.

**GraphQL Queries:**

```graphql
# Get overall stats for a time range
query {
  analyticsStats(timeRange: { start: "2026-02-01T00:00:00Z", end: "2026-02-05T23:59:59Z" }) {
    period
    totalCount
    successCount
    failureCount
    successRate
    avgLatencyMs
    p50LatencyMs
    p95LatencyMs
    p99LatencyMs
  }
}

# Get success rate over time
query {
  successRateTimeSeries(
    timeRange: { start: "2026-02-01T00:00:00Z", end: "2026-02-05T23:59:59Z" }
    granularity: HOUR
  ) {
    timestamp
    value
  }
}

# Get breakdown by event type
query {
  breakdownByEventType(
    timeRange: { start: "2026-02-01T00:00:00Z", end: "2026-02-05T23:59:59Z" }
    limit: 10
  ) {
    key
    count
    successRate
    avgLatencyMs
  }
}

# Get breakdown by endpoint
query {
  breakdownByEndpoint(
    timeRange: { start: "2026-02-01T00:00:00Z", end: "2026-02-05T23:59:59Z" }
    limit: 10
  ) {
    key
    count
    successRate
    avgLatencyMs
  }
}

# Get breakdown by status code
query {
  breakdownByStatusCode(
    timeRange: { start: "2026-02-01T00:00:00Z", end: "2026-02-05T23:59:59Z" }
  ) {
    key
    count
    successRate
    avgLatencyMs
  }
}

# Get latency percentiles over time
query {
  latencyTimeSeries(
    timeRange: { start: "2026-02-01T00:00:00Z", end: "2026-02-05T23:59:59Z" }
    granularity: HOUR
    percentile: P95
  ) {
    timestamp
    value
  }
}

# Get throughput (events per minute/hour/day)
query {
  throughputTimeSeries(
    timeRange: { start: "2026-02-01T00:00:00Z", end: "2026-02-05T23:59:59Z" }
    granularity: HOUR
  ) {
    timestamp
    value
  }
}
```

**Time Granularities:**
- `MINUTE` - Per-minute data points
- `HOUR` - Hourly aggregations  
- `DAY` - Daily aggregations
- `WEEK` - Weekly aggregations

**Latency Percentiles:**
- `P50` - Median latency
- `P95` - 95th percentile
- `P99` - 99th percentile

**Metrics Collected:**
- Delivery success/failure counts
- Response latencies (stored for percentile calculations)
- Event types and endpoint destinations
- HTTP status codes

**Storage:**
- Redis-backed with sorted sets for time-series data
- Configurable retention period (default: 7 days)
- Efficient aggregation using Redis ZRANGEBYSCORE

**Integration with Alerting:**

The metrics store provides data for custom alerting rules:

```go
// Alerting adapter wraps metrics store
adapter := metrics.NewAlertingAdapter(metricsStore)

// Use with alerting engine
engine := alerting.NewEngine(adapter)
engine.AddRule(alerting.NewRule(
    "High Failure Rate",
    alerting.Condition{
        Metric:   alerting.ConditionFailureRate,
        Operator: alerting.OpGreaterThan,
        Value:    0.1,
        Window:   alerting.Duration(5 * time.Minute),
    },
    // ... action config
))
```

---

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

## Priority Queues

Assign priority levels to events. Higher priority events are delivered before lower priority ones.

```graphql
# Send a high-priority event
mutation {
  sendEvent(input: {
    eventType: "order.created"
    payload: { order_id: 123 }
    priority: 1  # Highest priority
  }, idempotencyKey: "order-123") {
    id
    priority
    status
  }
}

# Send a low-priority analytics event
mutation {
  sendEvent(input: {
    eventType: "analytics.track"
    payload: { event: "page_view" }
    priority: 9  # Low priority
  }, idempotencyKey: "analytics-456") {
    id
    priority
  }
}
```

**Priority Scale:**
- 1-3: High priority (processed first)
- 4-7: Normal priority (default is 5)
- 8-10: Low priority (processed last)

**Implementation:**
- Three-tier queue system: high, normal, low
- Worker dequeues from high → normal → low
- Starvation prevention ensures low-priority events still process

## Delayed/Scheduled Delivery

Schedule events for future delivery. Specify an absolute timestamp or relative delay.

```graphql
# Schedule for a specific time
mutation {
  sendEvent(input: {
    eventType: "reminder.send"
    payload: { message: "Don't forget your appointment!" }
    deliverAt: "2026-02-05T10:00:00Z"
  }, idempotencyKey: "reminder-123") {
    id
    scheduledAt
    status
  }
}

# Schedule with a delay (1 hour)
mutation {
  sendEvent(input: {
    eventType: "email.send"
    payload: { to: "user@example.com", subject: "Follow up" }
    delaySeconds: 3600
  }, idempotencyKey: "email-456") {
    id
    scheduledAt
  }
}
```

**Constraints:**
- Cannot specify both `deliverAt` and `delaySeconds`
- Maximum delay: 30 days
- Events delivered at scheduled time (±1 second accuracy)
- Scheduled time visible in event details

**Combined with Priority:**

```graphql
mutation {
  sendEvent(input: {
    eventType: "report.generate"
    payload: { report_id: 789 }
    priority: 1
    delaySeconds: 300  # 5 minutes from now, high priority when ready
  }, idempotencyKey: "report-789") {
    id
    priority
    scheduledAt
  }
}
```

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

**Architecture:**

The FIFO delivery system uses the **FIFOProcessor**, a pluggable strategy within the unified Worker:

```
Worker
├── StandardProcessor (parallel delivery - disabled for FIFO endpoints)
└── FIFOProcessor (ordered delivery)
    ├── Endpoint Discovery Loop
    ├── Per-Endpoint/Partition Goroutines
    └── In-Flight Delivery Tracking
```

**How it works:**
- **FIFOProcessor** dynamically discovers FIFO-enabled endpoints
- Creates one goroutine per endpoint/partition for sequential processing
- Only one in-flight message at a time per partition
- Next message waits for ack/nack before sending
- Partition keys enable parallelism within ordering (e.g., `$.customer_id`)
- Shares delivery logic with StandardProcessor via `Worker.Deliver()`

**Graceful Shutdown:**
- Tracks all in-flight deliveries
- Waits for configurable grace period (`WORKER_FIFO_GRACE_PERIOD`)
- Ensures deliveries complete before shutdown

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
