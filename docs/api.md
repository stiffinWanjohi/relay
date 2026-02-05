# API Reference

GraphQL endpoint: `POST /graphql`

Playground (if enabled): `GET /playground`

Debug endpoints: `POST /debug/endpoints`

## Authentication

When `AUTH_ENABLED=true`, include API key:

```bash
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -H "X-API-Key: your-api-key" \
  -d '{"query":"..."}'
```

---

## Mutations

### createEvent

Create a webhook event for direct delivery to a URL.

```graphql
mutation CreateEvent($input: CreateEventInput!, $key: String!) {
  createEvent(input: $input, idempotencyKey: $key) {
    id
    status
    destination
    priority
    scheduledAt
    createdAt
  }
}
```

**Variables (basic):**
```json
{
  "input": {
    "destination": "https://example.com/webhook",
    "payload": { "orderId": "123", "amount": 99.99 },
    "headers": { "X-Custom": "value" },
    "maxAttempts": 10
  },
  "key": "order-123-created"
}
```

**Variables (with priority and scheduling):**
```json
{
  "input": {
    "destination": "https://example.com/webhook",
    "payload": { "orderId": "123" },
    "priority": 2,
    "delaySeconds": 300
  },
  "key": "delayed-order-123"
}
```

### sendEvent

Send event to all endpoints subscribed to the event type.

```graphql
mutation SendEvent($input: SendEventInput!, $key: String!) {
  sendEvent(input: $input, idempotencyKey: $key) {
    id
    status
    destination
    endpointId
    priority
    scheduledAt
  }
}
```

**Variables (basic):**
```json
{
  "input": {
    "eventType": "order.created",
    "payload": { "orderId": "123" },
    "headers": { "X-Custom": "value" }
  },
  "key": "order-123-created"
}
```

**Variables (with priority):**
```json
{
  "input": {
    "eventType": "order.created",
    "payload": { "orderId": "123" },
    "priority": 1
  },
  "key": "high-priority-order-123"
}
```

**Variables (scheduled delivery):**
```json
{
  "input": {
    "eventType": "reminder.send",
    "payload": { "message": "Don't forget!" },
    "deliverAt": "2026-02-05T10:00:00Z"
  },
  "key": "reminder-123"
}
```

**Variables (delayed delivery):**
```json
{
  "input": {
    "eventType": "email.send",
    "payload": { "to": "user@example.com" },
    "delaySeconds": 3600
  },
  "key": "email-456"
}
```

### createEventType

Create an event type with optional JSONSchema validation.

```graphql
mutation CreateEventType($input: CreateEventTypeInput!) {
  createEventType(input: $input) {
    id
    name
    description
    schema
    schemaVersion
    createdAt
  }
}
```

**Variables:**
```json
{
  "input": {
    "name": "order.created",
    "description": "Fired when a new order is placed",
    "schema": {
      "type": "object",
      "required": ["order_id"],
      "properties": {
        "order_id": { "type": "string" }
      }
    },
    "schemaVersion": "1.0"
  }
}
```

### updateEventType

Update an existing event type.

```graphql
mutation UpdateEventType($id: ID!, $input: UpdateEventTypeInput!) {
  updateEventType(id: $id, input: $input) {
    id
    name
    schemaVersion
    updatedAt
  }
}
```

### deleteEventType

Delete an event type.

```graphql
mutation DeleteEventType($id: ID!) {
  deleteEventType(id: $id)
}
```

### createEndpoint

Create a webhook endpoint subscription.

```graphql
mutation CreateEndpoint($input: CreateEndpointInput!) {
  createEndpoint(input: $input) {
    id
    url
    eventTypes
    status
  }
}
```

**Variables (basic):**
```json
{
  "input": {
    "url": "https://example.com/webhook",
    "eventTypes": ["order.created", "order.*"],
    "description": "Order notifications",
    "maxRetries": 15,
    "retryBackoffMs": 500,
    "timeoutMs": 10000,
    "rateLimitPerSec": 100,
    "customHeaders": { "Authorization": "Bearer token" }
  }
}
```

**Variables (with filtering):**
```json
{
  "input": {
    "url": "https://premium.example.com/webhook",
    "eventTypes": ["order.created"],
    "filter": {
      "and": [
        { "path": "$.data.amount", "operator": "gt", "value": 100 },
        { "path": "$.data.country", "operator": "eq", "value": "US" }
      ]
    }
  }
}
```

**Variables (with transformation):**
```json
{
  "input": {
    "url": "https://slack.example.com/webhook",
    "eventTypes": ["alert.*"],
    "transformation": "function transform(w) { return { ...w, payload: { text: w.payload.message } }; }"
  }
}
```

**Variables (FIFO delivery):**
```json
{
  "input": {
    "url": "https://inventory.example.com/webhook",
    "eventTypes": ["stock.*"],
    "fifo": true,
    "partitionKey": "$.product_id"
  }
}
```

### updateEndpoint

Update an existing endpoint.

```graphql
mutation UpdateEndpoint($id: ID!, $input: UpdateEndpointInput!) {
  updateEndpoint(id: $id, input: $input) {
    id
    status
  }
}
```

### deleteEndpoint

Delete an endpoint.

```graphql
mutation DeleteEndpoint($id: ID!) {
  deleteEndpoint(id: $id)
}
```

### replayEvent

Retry a failed or dead event.

```graphql
mutation ReplayEvent($id: ID!) {
  replayEvent(id: $id) {
    id
    status
    attempts
  }
}
```

### replayDeadLetters

Replay all dead letters for a destination.

```graphql
mutation ReplayDeadLetters($destination: String!) {
  replayDeadLetters(destination: $destination) {
    replayedCount
    failedCount
  }
}
```

### testTransformation

Test a JavaScript transformation against a sample payload.

```graphql
mutation TestTransformation($code: String!, $samplePayload: JSON!) {
  testTransformation(code: $code, samplePayload: $samplePayload) {
    success
    result
    error
  }
}
```

**Variables:**
```json
{
  "code": "function transform(w) { return { ...w, payload: { wrapped: w.payload } }; }",
  "samplePayload": { "order_id": 123 }
}
```

### releaseFIFOLock

Release a stuck FIFO lock (useful after crash recovery).

```graphql
mutation ReleaseFIFOLock($endpointId: ID!, $partitionKey: String) {
  releaseFIFOLock(endpointId: $endpointId, partitionKey: $partitionKey)
}
```

### drainFIFOQueue

Drain a FIFO queue, optionally moving messages to standard delivery.

```graphql
mutation DrainFIFOQueue($endpointId: ID!, $partitionKey: String, $moveToStandard: Boolean) {
  drainFIFOQueue(endpointId: $endpointId, partitionKey: $partitionKey, moveToStandard: $moveToStandard) {
    messagesProcessed
    messagesMoved
  }
}
```

### recoverStaleFIFOMessages

Recover messages stuck from crashed workers.

```graphql
mutation RecoverStaleFIFOMessages {
  recoverStaleFIFOMessages {
    recoveredCount
    errors
  }
}
```

---

## Queries

### eventType

Get a single event type by ID.

```graphql
query GetEventType($id: ID!) {
  eventType(id: $id) {
    id
    name
    description
    schema
    schemaVersion
    createdAt
    updatedAt
  }
}
```

### eventTypeByName

Get an event type by name.

```graphql
query GetEventTypeByName($name: String!) {
  eventTypeByName(name: $name) {
    id
    name
    description
    schema
    schemaVersion
  }
}
```

### eventTypes

List all event types with pagination.

```graphql
query ListEventTypes($first: Int, $after: String) {
  eventTypes(first: $first, after: $after) {
    totalCount
    pageInfo {
      hasNextPage
      endCursor
    }
    edges {
      node {
        id
        name
        description
        schemaVersion
      }
    }
  }
}
```

### event

Get a single event by ID.

```graphql
query GetEvent($id: ID!) {
  event(id: $id) {
    id
    idempotencyKey
    destination
    payload
    status
    attempts
    maxAttempts
    priority
    scheduledAt
    createdAt
    deliveredAt
    deliveryAttempts {
      attemptNumber
      statusCode
      responseBody
      error
      durationMs
      attemptedAt
    }
  }
}
```

### events

List events with filtering.

```graphql
query ListEvents($status: EventStatus, $destination: String, $first: Int, $after: String) {
  events(status: $status, destination: $destination, first: $first, after: $after) {
    totalCount
    pageInfo {
      hasNextPage
      endCursor
    }
    edges {
      node {
        id
        destination
        status
        attempts
        createdAt
      }
    }
  }
}
```

### endpoint

Get endpoint details.

```graphql
query GetEndpoint($id: ID!) {
  endpoint(id: $id) {
    id
    url
    eventTypes
    status
    maxRetries
    timeoutMs
    rateLimitPerSec
    stats {
      totalEvents
      delivered
      failed
      pending
      successRate
      avgLatencyMs
    }
  }
}
```

### endpoints

List all endpoints.

```graphql
query ListEndpoints {
  endpoints {
    id
    url
    eventTypes
    status
  }
}
```

### queueStats

Get queue statistics.

```graphql
query QueueStats {
  queueStats {
    queued
    delivering
    delivered
    failed
    dead
    pending
    processing
    delayed
  }
}
```

### fifoQueueStats

Get FIFO queue statistics for an endpoint.

```graphql
query FIFOQueueStats($endpointId: ID!) {
  fifoQueueStats(endpointId: $endpointId) {
    totalPartitions
    totalMessages
    partitionStats {
      partitionKey
      queueDepth
      locked
    }
  }
}
```

### fifoPartitionStats

Get statistics for a specific FIFO partition.

```graphql
query FIFOPartitionStats($endpointId: ID!, $partitionKey: String!) {
  fifoPartitionStats(endpointId: $endpointId, partitionKey: $partitionKey) {
    partitionKey
    queueDepth
    locked
  }
}
```

### activeFIFOQueues

List all active FIFO queues.

```graphql
query ActiveFIFOQueues {
  activeFIFOQueues {
    partitionKey
    queueDepth
    locked
  }
}
```

---

## Types

### EventStatus

```graphql
enum EventStatus {
  QUEUED      # Waiting to be sent
  DELIVERING  # Currently being sent
  DELIVERED   # Successfully delivered
  FAILED      # Will retry
  DEAD        # Max attempts exceeded
}
```

### EndpointStatus

```graphql
enum EndpointStatus {
  ACTIVE    # Receiving events
  PAUSED    # Temporarily stopped
  DISABLED  # Permanently stopped
}
```

---

## Error Handling

Errors are returned in standard GraphQL format:

```json
{
  "errors": [
    {
      "message": "event not found",
      "path": ["event"],
      "extensions": {
        "code": "NOT_FOUND"
      }
    }
  ]
}
```

Common error codes:
- `NOT_FOUND` - Resource doesn't exist
- `DUPLICATE` - Idempotency key already used
- `INVALID_INPUT` - Validation failed
- `UNAUTHORIZED` - Missing or invalid API key

---

## Alerting API (REST)

The Alerting API provides management of alert rules and alert history.

### List Alert Rules

Get all configured alert rules.

```bash
GET /api/v1/alerting/rules
X-API-Key: your-api-key
```

**Response:**
```json
{
  "rules": [
    {
      "id": "abc123",
      "name": "High Failure Rate",
      "description": "Alert when failure rate exceeds 10%",
      "enabled": true,
      "condition": {
        "metric": "failure_rate",
        "operator": "gt",
        "value": 0.1,
        "window": "5m"
      },
      "action": {
        "type": "slack",
        "webhookUrl": "https://hooks.slack.com/...",
        "channel": "#alerts"
      },
      "cooldown": "15m",
      "createdAt": "2026-02-05T10:00:00Z",
      "updatedAt": "2026-02-05T10:00:00Z"
    }
  ],
  "total": 1
}
```

### Get Alert Rule

Get a specific alert rule by ID.

```bash
GET /api/v1/alerting/rules/{ruleId}
X-API-Key: your-api-key
```

### Create Alert Rule

Create a new alert rule.

```bash
POST /api/v1/alerting/rules
X-API-Key: your-api-key
Content-Type: application/json

{
  "name": "High Failure Rate",
  "description": "Alert when failure rate exceeds 10%",
  "condition": {
    "metric": "failure_rate",
    "operator": "gt",
    "value": 0.1,
    "window": "5m"
  },
  "action": {
    "type": "slack",
    "webhookUrl": "https://hooks.slack.com/...",
    "channel": "#alerts"
  },
  "cooldown": "15m"
}
```

### Update Alert Rule

Update an existing alert rule.

```bash
PUT /api/v1/alerting/rules/{ruleId}
X-API-Key: your-api-key
Content-Type: application/json

{
  "name": "Updated Rule Name",
  "enabled": false
}
```

### Delete Alert Rule

Delete an alert rule.

```bash
DELETE /api/v1/alerting/rules/{ruleId}
X-API-Key: your-api-key
```

### Enable Alert Rule

Enable a previously disabled alert rule.

```bash
POST /api/v1/alerting/rules/{ruleId}/enable
X-API-Key: your-api-key
```

### Disable Alert Rule

Disable an alert rule without deleting it.

```bash
POST /api/v1/alerting/rules/{ruleId}/disable
X-API-Key: your-api-key
```

### Evaluate Alert Rules

Manually trigger evaluation of all enabled alert rules.

```bash
POST /api/v1/alerting/evaluate
X-API-Key: your-api-key
```

### Get Alert History

Get the history of triggered alerts.

```bash
GET /api/v1/alerting/history?limit=100
X-API-Key: your-api-key
```

**Response:**
```json
{
  "alerts": [
    {
      "id": "alert123",
      "ruleId": "rule456",
      "ruleName": "High Failure Rate",
      "metric": "failure_rate",
      "value": 0.15,
      "threshold": 0.1,
      "message": "Failure rate exceeded 10%",
      "firedAt": "2026-02-05T10:30:00Z"
    }
  ],
  "total": 1
}
```

---

## Connectors API (REST)

The Connectors API provides management of pre-built integrations.

### List Connectors

Get all registered connectors.

```bash
GET /api/v1/connectors
X-API-Key: your-api-key
```

**Response:**
```json
{
  "connectors": [
    {
      "id": "conn123",
      "name": "slack-alerts",
      "type": "slack",
      "enabled": true,
      "config": {
        "webhookUrl": "https://hooks.slack.com/...",
        "channel": "#alerts"
      },
      "template": {
        "text": "{{.event_type}}: {{.message}}"
      },
      "createdAt": "2026-02-05T10:00:00Z",
      "updatedAt": "2026-02-05T10:00:00Z"
    }
  ],
  "total": 1
}
```

### Get Connector

Get a specific connector by name.

```bash
GET /api/v1/connectors/{name}
X-API-Key: your-api-key
```

### Create Connector

Create a new connector.

```bash
POST /api/v1/connectors
X-API-Key: your-api-key
Content-Type: application/json

{
  "name": "slack-alerts",
  "type": "slack",
  "config": {
    "webhookUrl": "https://hooks.slack.com/...",
    "channel": "#alerts",
    "username": "Relay Bot"
  },
  "template": {
    "text": "{{.event_type}}: {{.message}}",
    "color": "danger"
  }
}
```

**Supported Connector Types:**
- `slack` - Slack webhooks
- `discord` - Discord webhooks
- `teams` - Microsoft Teams webhooks
- `email` - SMTP email
- `webhook` - Generic webhook (pass-through)

### Update Connector

Update an existing connector.

```bash
PUT /api/v1/connectors/{name}
X-API-Key: your-api-key
Content-Type: application/json

{
  "config": {
    "webhookUrl": "https://hooks.slack.com/new-url"
  }
}
```

### Delete Connector

Delete a connector.

```bash
DELETE /api/v1/connectors/{name}
X-API-Key: your-api-key
```

---

## Metrics API (REST)

The Metrics API provides access to rate limiting and performance metrics.

### Get Rate Limit Statistics

Get aggregated rate limit metrics across all endpoints.

```bash
GET /api/v1/metrics/rate-limits?limit=100
X-API-Key: your-api-key
```

**Response:**
```json
{
  "totalEvents": 150,
  "eventsByEndpoint": {
    "ep_123": 100,
    "ep_456": 50
  },
  "events": [
    {
      "endpointId": "ep_123",
      "eventId": "evt_789",
      "limitPerSecond": 100,
      "queueDepth": 50,
      "timestamp": "2026-02-05T10:30:00Z"
    }
  ]
}
```

### Get Rate Limit Statistics by Endpoint

Get rate limit metrics for a specific endpoint.

```bash
GET /api/v1/metrics/rate-limits/endpoint/{endpointId}?limit=100
X-API-Key: your-api-key
```

---

## Debug API (REST)

The Debug API provides webhook testing capabilities with temporary endpoints.

### Create Debug Endpoint

Create a new temporary debug endpoint.

```bash
POST /debug/endpoints
```

**Response:**
```json
{
  "id": "abc123def456789",
  "created_at": "2026-02-05T10:00:00Z",
  "expires_at": "2026-02-05T11:00:00Z",
  "url": "http://localhost:8080/debug/abc123def456789"
}
```

### Get Debug Endpoint

Get details of an existing debug endpoint.

```bash
GET /debug/endpoints/{endpointId}
```

### Delete Debug Endpoint

Delete a debug endpoint and all captured requests.

```bash
DELETE /debug/endpoints/{endpointId}
```

### List Captured Requests

Get all captured requests for a debug endpoint.

```bash
GET /debug/endpoints/{endpointId}/requests
```

**Response:**
```json
{
  "requests": [
    {
      "id": "req123",
      "received_at": "2026-02-05T10:05:00Z",
      "method": "POST",
      "path": "/webhook",
      "headers": {
        "Content-Type": "application/json",
        "X-Custom-Header": "value"
      },
      "body": "{\"event\": \"test\"}",
      "content_type": "application/json",
      "remote_addr": "192.168.1.1:54321",
      "duration_ns": 1234567
    }
  ],
  "count": 1
}
```

### Get Captured Request

Get a specific captured request by ID.

```bash
GET /debug/endpoints/{endpointId}/requests/{requestId}
```

### Replay Request

Replay a captured request to a specified URL.

```bash
POST /debug/endpoints/{endpointId}/requests/{requestId}/replay
Content-Type: application/json

{
  "url": "https://your-server.com/webhook"
}
```

**Response:**
```json
{
  "success": true,
  "status_code": 200,
  "duration_ms": 150,
  "response": "{\"received\": true}"
}
```

### Stream Requests (SSE)

Stream captured requests in real-time using Server-Sent Events.

```bash
GET /debug/endpoints/{endpointId}/stream
Accept: text/event-stream
```

**Event Format:**
```
event: request
data: {"id":"req123","method":"POST","path":"/webhook",...}

event: request
data: {"id":"req124","method":"POST","path":"/webhook",...}
```

### Capture Webhook

Send webhooks to a debug endpoint for capture. Accepts any HTTP method and path.

```bash
POST /debug/{endpointId}
POST /debug/{endpointId}/any/path/here
```

**Response:**
```json
{
  "message": "request captured",
  "request_id": "req123"
}
```
