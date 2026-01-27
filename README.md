# Relay

Webhook delivery that doesn't lose events.

## The Problem

Webhooks fail in four ways:

1. **Endpoint down** - event lost forever
2. **Network blip** - event delivered twice
3. **Slow endpoint** - blocks everything
4. **Failure** - nobody knows

Relay fixes all four through a combination of transactional outbox pattern, idempotency keys, exponential backoff retries, per-destination circuit breakers, and comprehensive delivery tracking.

## Features

- **Guaranteed Delivery** - Transactional outbox ensures events are never lost, even if the queue is unavailable
- **Exactly-Once Semantics** - Idempotency keys prevent duplicate deliveries across retries and replays
- **Intelligent Retries** - Exponential backoff with jitter, configurable per-endpoint
- **Circuit Breaker** - Per-destination circuit breakers prevent cascading failures
- **Event Fan-out** - Route events to multiple endpoints based on event type subscriptions
- **Per-Endpoint Configuration** - Custom retry policies, timeouts, rate limits per endpoint
- **Fair Multi-Tenant Scheduling** - Deficit round-robin prevents noisy neighbors from starving others
- **Full Observability** - OpenTelemetry metrics, complete delivery attempt history

## Architecture

```
┌────────────────────────────────────────────────────────────────────────┐
│                                   CLIENT                               │
└────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌────────────────────────────────────────────────────────────────────────┐
│                              API SERVER                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐    │
│  │   GraphQL   │  │    Auth     │  │  Validation │  │   Dedup     │    │
│  │   Handler   │──│  Middleware │──│  (SSRF,etc) │──│   Check     │    │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘    │
└────────────────────────────────────────────────────────────────────────┘
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
            ┌─────────────┐   ┌─────────────┐   ┌─────────────┐
            │  PostgreSQL │   │  PostgreSQL │   │    Redis    │
            │   (Events)  │   │   (Outbox)  │   │   (Dedup)   │
            └─────────────┘   └─────────────┘   └─────────────┘
                                      │
                                      ▼
┌────────────────────────────────────────────────────────────────────────┐
│                           OUTBOX PROCESSOR                             │
│  Polls outbox table, enqueues to Redis, deletes processed entries      │
└────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌─────────────────────────────────────────────────────────────────────────┐
│                              REDIS QUEUE                                │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                      │
│  │ Main Queue  │  │  Processing │  │   Delayed   │  BRPOPLPUSH for      │
│  │   (LPUSH)   │  │    Queue    │  │   (ZSET)    │  visibility timeout  │
│  └─────────────┘  └─────────────┘  └─────────────┘                      │
└─────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                              WORKER POOL                                  │
│  ┌────────────────────────────────────────────────────────────────────┐   │
│  │                         Per-Worker Loop                            │   │
│  │  1. Dequeue (with visibility timeout)                              │   │
│  │  2. Check circuit breaker state                                    │   │
│  │  3. Check rate limiter                                             │   │
│  │  4. Send HTTP request with signature                               │   │
│  │  5. Record success/failure                                         │   │
│  │  6. Ack or Nack (with backoff delay)                               │   │
│  └────────────────────────────────────────────────────────────────────┘   │
│                                                                           │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐   │
│  │   Circuit    │  │    Rate      │  │    Retry     │  │     DRR      │   │
│  │   Breaker    │  │   Limiter    │  │   Policy     │  │  Scheduler   │   │
│  │ (per-dest)   │  │ (per-endpt)  │  │ (per-endpt)  │  │ (per-client) │   │
│  └──────────────┘  └──────────────┘  └──────────────┘  └──────────────┘   │
└───────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
                            ┌─────────────────┐
                            │   DESTINATION   │
                            │    ENDPOINT     │
                            └─────────────────┘
```

## Quick Start

```bash
# Clone and start
git clone https://github.com/your-org/relay.git
cd relay

# Start all services (PostgreSQL, Redis, API, Worker)
docker compose up -d

# Run database migrations
docker compose run --rm migrate

# Verify services are running
docker compose ps

# Access GraphQL Playground
open http://localhost:8080/playground
```

## Core Concepts

### Events

An event represents a webhook payload to be delivered. Events flow through these states:

```
QUEUED ──► DELIVERING ──► DELIVERED
                │
                ▼
             FAILED ──► (retry) ──► QUEUED
                │
                ▼ (max attempts exceeded)
              DEAD
```

### Endpoints

Endpoints define webhook destinations with their configuration:

```graphql
type Endpoint {
  id: ID!
  url: String!                    # Destination URL
  eventTypes: [String!]!          # Subscribed event types (e.g., ["order.*"])
  status: EndpointStatus!         # ACTIVE, PAUSED, DISABLED
  
  # Retry configuration
  maxRetries: Int!                # Maximum delivery attempts (default: 10)
  retryBackoffMs: Int!            # Initial backoff in ms (default: 1000)
  retryBackoffMax: Int!           # Maximum backoff in ms (default: 86400000)
  retryBackoffMult: Float!        # Backoff multiplier (default: 2.0)
  
  # Delivery configuration
  timeoutMs: Int!                 # HTTP timeout in ms (default: 30000)
  rateLimitPerSec: Int!           # Requests per second (0 = unlimited)
  
  # Circuit breaker
  circuitThreshold: Int!          # Failures before opening (default: 5)
  circuitResetMs: Int!            # Time before half-open (default: 300000)
}
```

### Event Types and Fan-out

When you send an event with a type (e.g., `order.created`), Relay finds all active endpoints subscribed to that type and creates separate delivery records for each:

```graphql
# Create endpoints with subscriptions
mutation {
  endpoint1: createEndpoint(input: {
    url: "https://analytics.example.com/webhook"
    eventTypes: ["order.created", "order.updated"]
  }) { id }
  
  endpoint2: createEndpoint(input: {
    url: "https://fulfillment.example.com/webhook"
    eventTypes: ["order.created"]
  }) { id }
}

# Send event - delivered to BOTH endpoints
mutation {
  sendEvent(
    input: { eventType: "order.created", payload: { orderId: "123" } }
    idempotencyKey: "order-123-v1"
  ) {
    id
    destination  # Returns array with both endpoints
  }
}
```

### Idempotency

Every event requires an idempotency key. If you send the same key twice:

1. **Within dedup window (24h)**: Returns the original event, no duplicate created
2. **After dedup window**: Creates new event (use versioned keys like `order-123-v2`)

For fan-out, Relay generates composite keys: `{your-key}:{endpoint-id}` to track per-endpoint delivery.

## API Reference

### Mutations

#### Create Event (Direct URL)

```graphql
mutation CreateEvent($input: CreateEventInput!, $key: String!) {
  createEvent(input: $input, idempotencyKey: $key) {
    id
    status
    destination
  }
}

# Variables
{
  "input": {
    "destination": "https://example.com/webhook",
    "payload": { "orderId": "123", "amount": 99.99 },
    "headers": { "X-Custom-Header": "value" },
    "maxAttempts": 5
  },
  "key": "order-123-created"
}
```

#### Send Event (Fan-out by Type)

```graphql
mutation SendEvent($input: SendEventInput!, $key: String!) {
  sendEvent(input: $input, idempotencyKey: $key) {
    id
    status
    destination
    endpointId
  }
}

# Variables
{
  "input": {
    "eventType": "order.created",
    "payload": { "orderId": "123", "amount": 99.99 },
    "headers": { "X-Custom-Header": "value" }
  },
  "key": "order-123-created"
}
```

#### Create Endpoint

```graphql
mutation CreateEndpoint($input: CreateEndpointInput!) {
  createEndpoint(input: $input) {
    id
    url
    eventTypes
    status
  }
}

# Variables
{
  "input": {
    "url": "https://example.com/webhook",
    "eventTypes": ["order.created", "order.updated", "order.cancelled"],
    "description": "Order notifications",
    "maxRetries": 15,
    "retryBackoffMs": 500,
    "timeoutMs": 10000,
    "rateLimitPerSec": 100,
    "customHeaders": { "Authorization": "Bearer token123" }
  }
}
```

#### Replay Event

```graphql
mutation ReplayEvent($id: ID!) {
  replayEvent(id: $id) {
    id
    status
    attempts
  }
}
```

### Queries

#### Get Event with Delivery History

```graphql
query GetEvent($id: ID!) {
  event(id: $id) {
    id
    idempotencyKey
    destination
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
    endpoint {
      id
      url
      status
    }
  }
}
```

#### List Events with Filtering

```graphql
query ListEvents($status: EventStatus, $first: Int, $after: String) {
  events(status: $status, first: $first, after: $after) {
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

#### Queue Statistics

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

#### Endpoint Statistics

```graphql
query GetEndpoint($id: ID!) {
  endpoint(id: $id) {
    id
    url
    status
    stats {
      totalEvents
      delivered
      failed
      pending
      successRate
      avgLatencyMs
    }
    recentEvents(first: 10) {
      id
      status
      createdAt
    }
  }
}
```

## Webhook Signature Verification

Every outgoing webhook includes cryptographic signatures for verification:

```
POST /webhook HTTP/1.1
Host: example.com
Content-Type: application/json
X-Relay-Event-ID: 550e8400-e29b-41d4-a716-446655440000
X-Relay-Timestamp: 1706123456
X-Relay-Signature: v1=5257a869e7ecebeda32affa62cdca3fa51cad7e77a0e56ff536d0ce8e108d8bd
```

### Signature Format

```
v1=HMAC-SHA256(timestamp.payload, signing_key)
```

### Verification Examples

**Python:**

```python
import hmac
import hashlib
import time

def verify_webhook(payload: bytes, headers: dict, secret: str, tolerance_seconds: int = 300) -> bool:
    timestamp = headers.get('X-Relay-Timestamp')
    signature = headers.get('X-Relay-Signature')
    
    if not timestamp or not signature:
        return False
    
    # Check timestamp is recent (prevent replay attacks)
    if abs(time.time() - int(timestamp)) > tolerance_seconds:
        return False
    
    # Compute expected signature
    signed_payload = f"{timestamp}.{payload.decode()}"
    expected = hmac.new(
        secret.encode(),
        signed_payload.encode(),
        hashlib.sha256
    ).hexdigest()
    
    return hmac.compare_digest(f"v1={expected}", signature)
```

**Go:**

```go
func VerifyWebhook(payload []byte, timestamp, signature, secret string) bool {
    // Check timestamp freshness
    ts, err := strconv.ParseInt(timestamp, 10, 64)
    if err != nil || time.Now().Unix()-ts > 300 {
        return false
    }
    
    // Compute HMAC
    signedPayload := fmt.Sprintf("%s.%s", timestamp, string(payload))
    mac := hmac.New(sha256.New, []byte(secret))
    mac.Write([]byte(signedPayload))
    expected := fmt.Sprintf("v1=%x", mac.Sum(nil))
    
    return hmac.Equal([]byte(expected), []byte(signature))
}
```

**Node.js:**

```javascript
const crypto = require('crypto');

function verifyWebhook(payload, headers, secret, toleranceSeconds = 300) {
    const timestamp = headers['x-relay-timestamp'];
    const signature = headers['x-relay-signature'];
    
    if (!timestamp || !signature) return false;
    
    // Check timestamp
    if (Math.abs(Date.now() / 1000 - parseInt(timestamp)) > toleranceSeconds) {
        return false;
    }
    
    // Compute signature
    const signedPayload = `${timestamp}.${payload}`;
    const expected = 'v1=' + crypto
        .createHmac('sha256', secret)
        .update(signedPayload)
        .digest('hex');
    
    return crypto.timingSafeEqual(Buffer.from(expected), Buffer.from(signature));
}
```

## Retry Policy

### Default Exponential Backoff

| Attempt | Delay | Cumulative Time |
|---------|-------|-----------------|
| 1 | 1 second | 1 second |
| 2 | 5 seconds | 6 seconds |
| 3 | 30 seconds | 36 seconds |
| 4 | 2 minutes | ~2.5 minutes |
| 5 | 10 minutes | ~12.5 minutes |
| 6 | 30 minutes | ~42.5 minutes |
| 7 | 1 hour | ~1.7 hours |
| 8 | 2 hours | ~3.7 hours |
| 9 | 6 hours | ~9.7 hours |
| 10 | 24 hours | ~33.7 hours |

After exhausting all attempts, events move to `DEAD` status and require manual replay.

### Per-Endpoint Custom Backoff

Configure custom retry behavior per endpoint:

```graphql
mutation {
  createEndpoint(input: {
    url: "https://example.com/webhook"
    eventTypes: ["order.*"]
    
    # Aggressive retry for critical endpoint
    maxRetries: 20
    retryBackoffMs: 100        # Start at 100ms
    retryBackoffMax: 3600000   # Cap at 1 hour
    retryBackoffMult: 1.5      # Gentler exponential growth
  }) { id }
}
```

Backoff formula: `min(retryBackoffMs * (retryBackoffMult ^ attempt), retryBackoffMax) + jitter`

### Retryable vs Non-Retryable Errors

**Retryable (will retry):**
- Connection refused, reset, timeout
- DNS resolution failures
- HTTP 408, 429, 500, 502, 503, 504
- TLS handshake errors

**Non-Retryable (immediate failure):**
- HTTP 400, 401, 403, 404, 405, 410
- Invalid URL
- Certificate verification failure (permanent)

## Circuit Breaker

Per-destination circuit breakers prevent cascading failures:

```
┌───────────────────────────────────────────────────────────────┐
│                      CIRCUIT STATES                           │
│                                                               │
│   ┌──────────┐     5 failures      ┌──────────┐               │
│   │  CLOSED  │ ─────────────────►  │   OPEN   │               │
│   │ (normal) │                     │ (reject) │               │
│   └──────────┘                     └──────────┘               │
│        ▲                                 │                    │
│        │                                 │ 5 min timeout      │
│        │ 3 successes                     ▼                    │
│        │                           ┌───────────┐              │
│        └─────────────────────────  │ HALF-OPEN │              │
│                                    │  (probe)  │              │
│                                    └───────────┘              │
│                                          │                    │
│                                          │ failure            │
│                                          ▼                    │
│                                    ┌──────────┐               │
│                                    │   OPEN   │               │
│                                    └──────────┘               │
└───────────────────────────────────────────────────────────────┘
```

**Behavior:**
- **Closed**: Normal operation, requests flow through
- **Open**: All requests rejected immediately, returned to queue with delay
- **Half-Open**: Single probe request allowed; success closes circuit, failure reopens

**Configuration:**

```graphql
mutation {
  createEndpoint(input: {
    url: "https://example.com/webhook"
    eventTypes: ["*"]
    circuitThreshold: 10     # Open after 10 consecutive failures
    circuitResetMs: 600000   # Try half-open after 10 minutes
  }) { id }
}
```

## Rate Limiting

Per-endpoint rate limiting using Redis token bucket:

```graphql
mutation {
  createEndpoint(input: {
    url: "https://example.com/webhook"
    eventTypes: ["*"]
    rateLimitPerSec: 50   # Max 50 requests per second
  }) { id }
}
```

When rate limited, events are returned to the queue with a short delay (100ms) rather than consuming retry attempts.

## Fair Scheduling

Deficit Round-Robin (DRR) ensures fair bandwidth allocation across clients:

```
┌──────────────────────────────────────────────────────────────┐
│                   DRR SCHEDULER                              │
│                                                              │
│  Client A (deficit: 1000)  ──┐                               │
│  Client B (deficit: 500)   ──┼──► Select highest deficit     │
│  Client C (deficit: 0)     ──┘    Process event              │
│                                   Subtract payload size      │
│                                   Add quantum when exhausted │
│                                                              │
│  Quantum = 65536 bytes (64KB)                                │
│  Each client gets equal bandwidth over time                  │
└──────────────────────────────────────────────────────────────┘
```

This prevents a single high-volume client from starving others.

## Configuration Reference

### Required Variables

| Variable | Description |
|----------|-------------|
| `DATABASE_URL` | PostgreSQL connection string |
| `SIGNING_KEY` | HMAC signing key (min 32 characters) |

### Optional Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_URL` | `localhost:6379` | Redis connection string |
| `API_ADDR` | `:8080` | API server listen address |
| `WORKER_CONCURRENCY` | `10` | Concurrent delivery workers |
| `AUTH_ENABLED` | `true` | Enable API key authentication |
| `ENABLE_PLAYGROUND` | `false` | Enable GraphQL playground |

### Database Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_MAX_CONNS` | `100` | Maximum pool connections |
| `DATABASE_MIN_CONNS` | `10` | Minimum pool connections |
| `DATABASE_MAX_CONN_LIFETIME` | `1h` | Connection max lifetime |
| `DATABASE_MAX_CONN_IDLE_TIME` | `30m` | Connection max idle time |

### Redis Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_POOL_SIZE` | `100` | Connection pool size |
| `REDIS_READ_TIMEOUT` | `3s` | Read operation timeout |
| `REDIS_WRITE_TIMEOUT` | `3s` | Write operation timeout |

### Worker Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKER_VISIBILITY_TIMEOUT` | `30s` | Queue message visibility |
| `WORKER_SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |

### Outbox Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `OUTBOX_POLL_INTERVAL` | `1s` | Outbox polling frequency |
| `OUTBOX_BATCH_SIZE` | `100` | Events per poll batch |
| `OUTBOX_CLEANUP_INTERVAL` | `1h` | Cleanup job frequency |
| `OUTBOX_RETENTION_PERIOD` | `24h` | Processed entry retention |

### Observability

| Variable | Default | Description |
|----------|---------|-------------|
| `METRICS_PROVIDER` | `""` | `otel`, `prometheus`, or empty |
| `METRICS_ENDPOINT` | `""` | Provider endpoint |
| `SERVICE_NAME` | `relay` | Service name for metrics |
| `SERVICE_VERSION` | `1.0.0` | Service version tag |
| `ENVIRONMENT` | `development` | Environment tag |

## Security

### SSRF Protection

Relay blocks requests to internal/private addresses:

- Localhost (`127.0.0.0/8`, `::1`)
- Private networks (`10.0.0.0/8`, `172.16.0.0/12`, `192.168.0.0/16`)
- Link-local (`169.254.0.0/16`, `fe80::/10`)
- Cloud metadata endpoints (`169.254.169.254`)

### Input Validation

- Maximum payload size: 1MB
- Maximum URL length: 2048 characters
- URL scheme must be `http` or `https`
- Idempotency keys must be non-empty

### Authentication

When `AUTH_ENABLED=true`, all API requests require an API key:

```bash
curl -H "X-API-Key: your-api-key" http://localhost:8080/graphql
```

## Development

### Prerequisites

- Go 1.22+
- PostgreSQL 15+
- Redis 7+
- Make

### Setup

```bash
# Install dependencies
go mod download

# Install dev tools
make dev-deps

# Start dependencies
docker compose up -d postgres redis

# Run migrations
export DATABASE_URL="postgres://relay:relay@localhost:5432/relay?sslmode=disable"
make migrate-up

# Generate GraphQL code (after schema changes)
make generate
```

### Running Locally

```bash
# Terminal 1: API server
export DATABASE_URL="postgres://relay:relay@localhost:5432/relay?sslmode=disable"
export REDIS_URL="localhost:6379"
export SIGNING_KEY="your-32-character-signing-key-here"
export ENABLE_PLAYGROUND="true"
make run-api

# Terminal 2: Worker
make run-worker
```

### Testing

```bash
# Run all tests
make test

# Run with coverage
make test-coverage

# Run benchmarks
go test -bench=. -benchmem ./...

# Run specific benchmark
go test -bench=BenchmarkCircuitBreaker -benchmem ./internal/delivery/...
```

### Linting

```bash
make lint
```

## Deployment

### Docker

```bash
# Build images
docker compose build

# Start all services
docker compose up -d

# View logs
docker compose logs -f api worker

# Scale workers
docker compose up -d --scale worker=5
```

### Kubernetes (Kustomize)

Relay uses [Kustomize](https://kustomize.io/) for Kubernetes deployments with environment-specific overlays.

```
deploy/k8s/
├── base/                    # Shared base manifests
│   ├── kustomization.yaml
│   ├── namespace.yaml
│   ├── serviceaccount.yaml
│   ├── configmap.yaml
│   ├── secret.yaml
│   ├── api-deployment.yaml
│   ├── api-service.yaml
│   ├── api-hpa.yaml
│   ├── worker-deployment.yaml
│   ├── worker-hpa.yaml
│   └── ingress.yaml
└── overlays/
    ├── dev/                 # Development settings
    ├── staging/             # Staging settings
    └── production/          # Production settings
```

**Deploy to an environment:**

```bash
# Preview what will be deployed
kubectl kustomize deploy/k8s/overlays/dev

# Deploy to dev
kubectl apply -k deploy/k8s/overlays/dev

# Deploy to staging
kubectl apply -k deploy/k8s/overlays/staging

# Deploy to production
kubectl apply -k deploy/k8s/overlays/production
```

**Check deployment:**

```bash
# Dev environment
kubectl -n relay-dev get pods
kubectl -n relay-dev get hpa
kubectl -n relay-dev logs -f deployment/dev-relay-api

# Production environment
kubectl -n relay-production get pods
kubectl -n relay-production logs -f deployment/prod-relay-api
```

**Environment differences:**

| Setting | Dev | Staging | Production |
|---------|-----|---------|------------|
| Namespace | `relay-dev` | `relay-staging` | `relay-production` |
| Replicas | 1 | 2 | 3 |
| HPA Max (API) | 2 | 10 | 50 |
| HPA Max (Worker) | 2 | 20 | 100 |
| Log Level | debug | info | warn |
| CPU Request | 100m | 100m | 500m |
| Memory Request | 128Mi | 128Mi | 512Mi |

**Customizing an overlay:**

Edit `deploy/k8s/overlays/<env>/kustomization.yaml` to:
- Change replica counts via patches
- Override ConfigMap values via `configMapGenerator`
- Override Secrets via `secretGenerator`
- Add environment-specific resources
```
```

### Production Checklist

- [ ] Set strong `SIGNING_KEY` (32+ random characters)
- [ ] Configure `DATABASE_URL` with SSL (`sslmode=require`)
- [ ] Disable GraphQL playground (`ENABLE_PLAYGROUND=false`)
- [ ] Enable authentication (`AUTH_ENABLED=true`)
- [ ] Configure metrics (`METRICS_PROVIDER=otel`)
- [ ] Set appropriate resource limits
- [ ] Configure HPA for workers based on queue depth
- [ ] Set up monitoring alerts for dead letter queue growth
- [ ] Configure backup for PostgreSQL

## Project Structure

```
relay/
├── cmd/
│   ├── api/main.go              # API server entrypoint
│   └── worker/main.go           # Worker entrypoint
├── internal/
│   ├── api/
│   │   ├── server.go            # HTTP server setup
│   │   └── graphql/             # GraphQL schema and resolvers
│   ├── auth/
│   │   ├── apikey.go            # API key middleware
│   │   └── store.go             # Client/key storage
│   ├── config/
│   │   └── config.go            # Configuration loading
│   ├── dedup/
│   │   └── redis.go             # Idempotency checking
│   ├── delivery/
│   │   ├── worker.go            # Delivery worker pool
│   │   ├── sender.go            # HTTP delivery with signing
│   │   ├── circuit.go           # Circuit breaker
│   │   ├── retry.go             # Retry policy
│   │   ├── ratelimit.go         # Token bucket rate limiter
│   │   └── scheduler.go         # DRR fair scheduler
│   ├── domain/
│   │   ├── event.go             # Event entity
│   │   ├── endpoint.go          # Endpoint entity
│   │   ├── delivery.go          # DeliveryAttempt entity
│   │   └── errors.go            # Domain errors
│   ├── event/
│   │   └── store.go             # PostgreSQL event store
│   ├── observability/
│   │   ├── metrics.go           # Metrics interface
│   │   ├── tracing.go           # Tracing interface
│   │   └── otel/                # OpenTelemetry implementation
│   ├── outbox/
│   │   └── processor.go         # Transactional outbox
│   └── queue/
│       └── redis.go             # Redis queue with visibility timeout
├── pkg/
│   ├── backoff/
│   │   └── exponential.go       # Backoff with jitter
│   └── signature/
│       └── hmac.go              # HMAC-SHA256 signing
├── migrations/                   # Database migrations
├── deploy/
│   ├── docker/                   # Dockerfiles
│   └── k8s/                      # Kubernetes manifests (Kustomize)
│       ├── base/                # Shared base manifests
│       └── overlays/            # Environment overlays (dev, staging, prod)
├── docker-compose.yaml
├── Makefile
└── README.md
```

## License

MIT
