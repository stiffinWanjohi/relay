# API Reference

GraphQL endpoint: `POST /graphql`

Playground (if enabled): `GET /playground`

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
    createdAt
  }
}
```

**Variables:**
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

### sendEvent

Send event to all endpoints subscribed to the event type.

```graphql
mutation SendEvent($input: SendEventInput!, $key: String!) {
  sendEvent(input: $input, idempotencyKey: $key) {
    id
    status
    destination
    endpointId
  }
}
```

**Variables:**
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
