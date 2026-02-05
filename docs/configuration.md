# Configuration

All configuration is via environment variables.

## Required

| Variable | Description |
|----------|-------------|
| `DATABASE_URL` | PostgreSQL connection string |
| `SIGNING_KEY` | HMAC signing key (min 32 characters) |

## Optional

### API Server

| Variable | Default | Description |
|----------|---------|-------------|
| `API_ADDR` | `:8080` | Listen address |
| `ENABLE_PLAYGROUND` | `false` | Enable GraphQL playground |
| `AUTH_ENABLED` | `true` | Require API key auth |
| `GLOBAL_RATE_LIMIT_RPS` | `0` | Global requests per second (0 = unlimited) |
| `CLIENT_RATE_LIMIT_RPS` | `0` | Per-client requests per second (0 = unlimited) |

### Redis

| Variable | Default | Description |
|----------|---------|-------------|
| `REDIS_URL` | `localhost:6379` | Redis address |
| `REDIS_POOL_SIZE` | `100` | Connection pool size |
| `REDIS_READ_TIMEOUT` | `3s` | Read timeout |
| `REDIS_WRITE_TIMEOUT` | `3s` | Write timeout |

### Database

| Variable | Default | Description |
|----------|---------|-------------|
| `DATABASE_MAX_CONNS` | `100` | Max pool connections |
| `DATABASE_MIN_CONNS` | `10` | Min pool connections |
| `DATABASE_MAX_CONN_LIFETIME` | `1h` | Connection max lifetime |
| `DATABASE_MAX_CONN_IDLE_TIME` | `30m` | Connection max idle time |

### Worker

| Variable | Default | Description |
|----------|---------|-------------|
| `WORKER_CONCURRENCY` | `10` | Concurrent delivery workers |
| `WORKER_VISIBILITY_TIMEOUT` | `30s` | Queue message visibility |
| `WORKER_SHUTDOWN_TIMEOUT` | `30s` | Graceful shutdown timeout |

### Outbox

| Variable | Default | Description |
|----------|---------|-------------|
| `OUTBOX_POLL_INTERVAL` | `1s` | Polling frequency |
| `OUTBOX_BATCH_SIZE` | `100` | Events per batch |
| `OUTBOX_CLEANUP_INTERVAL` | `1h` | Cleanup job frequency |
| `OUTBOX_RETENTION_PERIOD` | `24h` | Processed entry retention |

### Notifications

| Variable | Default | Description |
|----------|---------|-------------|
| `NOTIFICATION_ENABLED` | `false` | Enable failure notifications |
| `NOTIFICATION_SLACK_WEBHOOK_URL` | `` | Slack webhook URL for alerts |
| `NOTIFICATION_SMTP_HOST` | `` | SMTP server host |
| `NOTIFICATION_SMTP_PORT` | `587` | SMTP server port |
| `NOTIFICATION_SMTP_USERNAME` | `` | SMTP username |
| `NOTIFICATION_SMTP_PASSWORD` | `` | SMTP password |
| `NOTIFICATION_EMAIL_FROM` | `` | From address for emails |
| `NOTIFICATION_EMAIL_TO` | `` | Comma-separated recipient emails |
| `NOTIFICATION_NOTIFY_ON_TRIP` | `true` | Notify on circuit breaker trip |
| `NOTIFICATION_NOTIFY_ON_RECOVER` | `true` | Notify on circuit breaker recovery |

### Observability

| Variable | Default | Description |
|----------|---------|-------------|
| `METRICS_PROVIDER` | `` | `otel`, `prometheus`, or empty |
| `METRICS_ENDPOINT` | `` | Provider endpoint |
| `SERVICE_NAME` | `relay` | Service name for metrics |
| `SERVICE_VERSION` | `1.0.0` | Service version tag |
| `ENVIRONMENT` | `development` | Environment tag |

## Example .env

```bash
DATABASE_URL=postgres://relay:relay@localhost:5432/relay?sslmode=disable
REDIS_URL=localhost:6379
SIGNING_KEY=your-32-character-minimum-secret-key

# Development
ENABLE_PLAYGROUND=true
AUTH_ENABLED=false
WORKER_CONCURRENCY=5

# Production
# ENABLE_PLAYGROUND=false
# AUTH_ENABLED=true
# WORKER_CONCURRENCY=20
# METRICS_PROVIDER=otel
# METRICS_ENDPOINT=otel-collector:4317

# Rate Limiting (optional)
# GLOBAL_RATE_LIMIT_RPS=1000
# CLIENT_RATE_LIMIT_RPS=100

# Notifications (optional)
# NOTIFICATION_ENABLED=true
# NOTIFICATION_SLACK_WEBHOOK_URL=https://hooks.slack.com/services/...
# NOTIFICATION_SMTP_HOST=smtp.example.com
# NOTIFICATION_SMTP_PORT=587
# NOTIFICATION_EMAIL_FROM=relay@example.com
# NOTIFICATION_EMAIL_TO=ops@example.com,oncall@example.com
```
