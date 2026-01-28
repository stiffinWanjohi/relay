# Architecture

## Overview
```
Client → API → PostgreSQL → Outbox Processor → Redis Queue → Workers → Destination
```

## Detailed Flow

```
┌──────────────────────────────────────────────────────────────────────────┐
│                                   CLIENT                                 │
└──────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌──────────────────────────────────────────────────────────────────────────┐
│                              API SERVER                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐      │
│  │   GraphQL   │  │    Auth     │  │  Validation │  │   Dedup     │      │
│  │   Handler   │──│  Middleware │──│  (SSRF,etc) │──│   Check     │      │
│  └─────────────┘  └─────────────┘  └─────────────┘  └─────────────┘      │
└──────────────────────────────────────────────────────────────────────────┘
                                      │
                    ┌─────────────────┼─────────────────┐
                    ▼                 ▼                 ▼
            ┌─────────────┐   ┌─────────────┐   ┌─────────────┐
            │  PostgreSQL │   │  PostgreSQL │   │    Redis    │
            │   (Events)  │   │   (Outbox)  │   │   (Dedup)   │
            └─────────────┘   └─────────────┘   └─────────────┘
                                      │
                                      ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                           OUTBOX PROCESSOR                                │
│  Polls outbox table, enqueues to Redis, deletes processed entries         │
└───────────────────────────────────────────────────────────────────────────┘
                                      │
                                      ▼
┌───────────────────────────────────────────────────────────────────────────┐
│                              REDIS QUEUE                                  │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────┐                        │
│  │ Main Queue  │  │  Processing │  │   Delayed   │  BRPOPLPUSH for        │
│  │   (LPUSH)   │  │    Queue    │  │   (ZSET)    │  visibility timeout    │
│  └─────────────┘  └─────────────┘  └─────────────┘                        │
└───────────────────────────────────────────────────────────────────────────┘
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

## Components

### API Server

Handles incoming requests through a pipeline:

1. **GraphQL Handler** - Parses and validates GraphQL operations
2. **Auth Middleware** - Validates API keys
3. **Validation** - SSRF protection, payload size limits, URL validation
4. **Dedup Check** - Redis lookup for idempotency key

### Storage

| Store | Purpose |
|-------|---------|
| PostgreSQL (Events) | Durable event storage, delivery history |
| PostgreSQL (Outbox) | Transactional outbox for guaranteed delivery |
| Redis (Dedup) | Fast idempotency key lookup (24h TTL) |

### Outbox Processor

Background process that:
1. Polls outbox table every 1s
2. Batch fetches pending entries (100 at a time)
3. Enqueues to Redis
4. Deletes processed entries

This ensures events are never lost even if Redis is temporarily unavailable.

### Redis Queue

Three-queue system for reliability:

| Queue | Purpose |
|-------|---------|
| Main | Pending events (LPUSH/RPOP) |
| Processing | Currently being delivered (visibility timeout) |
| Delayed | Scheduled retries (ZSET sorted by timestamp) |

Uses `BRPOPLPUSH` for atomic dequeue with visibility timeout.

### Worker Pool

Concurrent workers that:
1. Dequeue with 30s visibility timeout
2. Check circuit breaker (fail fast if open)
3. Check rate limiter (delay if exceeded)
4. Send HTTP POST with HMAC signature
5. Record attempt in PostgreSQL
6. Ack (success) or Nack with delay (failure)

### Resilience Components

| Component | Scope | Purpose |
|-----------|-------|---------|
| Circuit Breaker | Per-destination | Stop hammering failing endpoints |
| Rate Limiter | Per-endpoint | Respect endpoint rate limits |
| Retry Policy | Per-endpoint | Configurable backoff strategy |
| DRR Scheduler | Per-client | Fair bandwidth allocation |

## Data Flow

### Happy Path
```
1. Client sends createEvent mutation
2. API validates, checks dedup, stores event + outbox entry (single transaction)
3. Returns event ID immediately
4. Outbox processor picks up entry, enqueues to Redis
5. Worker dequeues, delivers to destination
6. Destination returns 2xx
7. Worker marks delivered, acks message
```

### Failure Path
```
1. Worker delivers to destination
2. Destination returns 5xx (or timeout)
3. Worker records failure, calculates next retry time
4. Worker nacks with delay (message goes to delayed queue)
5. After delay, message moves back to main queue
6. Repeat until max attempts
7. After max attempts, mark as dead (dead letter queue)
```

### Circuit Breaker Flow
```
1. Destination fails 5 times consecutively
2. Circuit opens for that destination
3. New events for that destination skip delivery, nack with 5min delay
4. After 5 minutes, circuit half-opens
5. Single probe request sent
6. Success → circuit closes, resume normal
7. Failure → circuit re-opens for another 5 minutes
```
