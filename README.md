<p align="center">
  <h1 align="center">📡 Relay</h1>
  <p align="center">Webhook delivery that doesn't lose events</p>
  <p align="center">
    <a href="https://github.com/stiffinWanjohi/relay/actions"><img src="https://github.com/stiffinWanjohi/relay/workflows/CI/badge.svg" alt="CI"></a>
    <a href="https://goreportcard.com/report/github.com/stiffinWanjohi/relay"><img src="https://goreportcard.com/badge/github.com/stiffinWanjohi/relay" alt="Go Report"></a>
    <img src="https://img.shields.io/badge/go-1.25-blue" alt="Go 1.25">
    <a href="LICENSE"><img src="https://img.shields.io/badge/license-MIT-green" alt="License"></a>
  </p>
</p>

## Why

I've seen webhooks fail silently and cost real money. Orders placed but never fulfilled. Customers complaining before anyone noticed.

Relay fixes that.

## What It Does

- **Durable** - Transactional outbox. If it's in the DB, it ships.
- **Deduplicated** - Idempotency keys. Retry all you want.
- **Resilient** - Circuit breakers + exponential backoff.
- **Observable** - Full delivery history, queue stats, real-time log streaming.
- **Multi-tenant** - Fair scheduling, per-endpoint config.
- **Event Types** - Schema catalog with JSONSchema validation.
- **Smart Routing** - Content-based filtering with JSONPath expressions.
- **Transformable** - JavaScript payload transformations before delivery.
- **Ordered** - FIFO delivery with partition key support.
- **Rate Limited** - Global and per-client rate limits with Redis.
- **Debuggable** - Temporary endpoints for testing webhooks.
- **Alerting** - Custom rules for failure rate, latency, queue depth alerts.
- **Connectors** - Pre-built integrations for Slack, Discord, Teams, Email.
- **Analytics** - Time-series metrics with breakdowns by endpoint, event type.

[All features →](docs/features.md)

## How It Works
```
Client → API → PostgreSQL → Outbox Processor → Redis Queue → Workers → Destination
                  ↑                                             ↓
             (durable)                              circuit breaker
                                                    rate limiter
                                                    retry + backoff
```

[Full architecture →](docs/architecture.md)

## Quick Start

### Docker Compose
```bash
docker compose up -d
# GraphQL Playground: http://localhost:8080/playground
```

### Binary
```bash
# Start API server
relay serve

# Start worker (separate terminal)
relay worker

# Or both together (development)
relay all
```

### Send a Webhook
```bash
# CLI
relay send --dest https://example.com/hook --payload '{"order": 123}' --key order-123

# GraphQL
curl -X POST http://localhost:8080/graphql \
  -H "Content-Type: application/json" \
  -d '{"query": "mutation { createEvent(input: { destination: \"https://example.com/hook\", payload: { order: 123 } }, idempotencyKey: \"order-123\") { id status } }"}'
```

## Verify Signatures
```go
signed := fmt.Sprintf("%s.%s", timestamp, payload)
mac := hmac.New(sha256.New, []byte(secret))
mac.Write([]byte(signed))
valid := hmac.Equal([]byte(fmt.Sprintf("v1=%x", mac.Sum(nil))), []byte(signature))
```

[All languages →](docs/verification.md)

## Config
```bash
DATABASE_URL=postgres://...  # Required
SIGNING_KEY=32-char-min      # Required  
REDIS_URL=localhost:6379     # Default
WORKER_CONCURRENCY=10        # Default
```

[Full config →](docs/configuration.md)

## Deploy
```bash
kubectl apply -k deploy/k8s/overlays/production
```

[Deployment guide →](docs/deployment.md)

## CLI

```bash
relay start      # Start server (API + workers)

relay send       # Send a webhook event
relay get <id>   # Get event details
relay list       # List events
relay replay <id># Replay failed event
relay stats      # Queue statistics
relay health     # Health check
relay debug      # Start webhook debugger session
relay logs -f    # Stream delivery logs in real-time
relay openapi    # View OpenAPI spec
relay apikey     # Manage API keys
```

## Docs

- [Architecture](docs/architecture.md) - System design deep dive
- [Features](docs/features.md) - All capabilities explained
- [API Reference](docs/api.md) - Full GraphQL schema
- [Configuration](docs/configuration.md) - Environment variables
- [Deployment](docs/deployment.md) - Production checklist
- [Verification](docs/verification.md) - Signature examples

## License

MIT - See [LICENSE](LICENSE)
