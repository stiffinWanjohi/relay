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

**Variables:**
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

---

## Queries

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
