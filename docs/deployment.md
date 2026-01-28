# Deployment

## Docker Compose (Development)

```bash
docker compose up -d
```

Services:
- API: `http://localhost:8080`
- GraphQL Playground: `http://localhost:8080/playground`
- PostgreSQL: `localhost:5432`
- Redis: `localhost:6379`

Scale workers:
```bash
docker compose up -d --scale worker=5
```

## Kubernetes

Relay uses [Kustomize](https://kustomize.io/) with environment overlays.

### Structure

```
deploy/k8s/
├── base/                 # Shared manifests
└── overlays/
    ├── dev/              # Development
    ├── staging/          # Staging
    └── production/       # Production
```

### Deploy

```bash
# Preview
kubectl kustomize deploy/k8s/overlays/production

# Apply
kubectl apply -k deploy/k8s/overlays/production

# Check status
kubectl -n relay-production get pods
kubectl -n relay-production get hpa
```

### Environment Differences

| Setting | Dev | Staging | Production |
|---------|-----|---------|------------|
| Namespace | `relay-dev` | `relay-staging` | `relay-production` |
| API Replicas | 1 | 2 | 3 |
| Worker Replicas | 1 | 2 | 3 |
| HPA Max (API) | 2 | 10 | 50 |
| HPA Max (Worker) | 2 | 20 | 100 |
| Log Level | debug | info | warn |

### Secrets

Create secrets before deploying:

```bash
kubectl -n relay-production create secret generic relay-secrets \
  --from-literal=database-url='postgres://...' \
  --from-literal=signing-key='your-32-char-key'
```

Or use sealed-secrets, external-secrets, etc.

## Production Checklist

### Security

- [ ] Strong `SIGNING_KEY` (32+ random chars)
- [ ] `DATABASE_URL` with SSL (`sslmode=require`)
- [ ] `ENABLE_PLAYGROUND=false`
- [ ] `AUTH_ENABLED=true`
- [ ] Network policies restricting pod communication
- [ ] Secrets in secret manager (not env vars)

### Reliability

- [ ] Multiple API replicas (≥2)
- [ ] Multiple worker replicas (≥3)
- [ ] HPA configured for both
- [ ] PodDisruptionBudget set
- [ ] Liveness/readiness probes configured
- [ ] Resource limits set

### Observability

- [ ] `METRICS_PROVIDER=otel` configured
- [ ] Metrics collector deployed
- [ ] Dashboards created
- [ ] Alerts configured:
  - Dead letter queue growth
  - Circuit breakers opening
  - High queue depth
  - Error rate spikes

### Database

- [ ] PostgreSQL HA (Cloud SQL, RDS, etc.)
- [ ] Connection pooling (PgBouncer)
- [ ] Automated backups
- [ ] Point-in-time recovery tested

### Redis

- [ ] Redis HA (Sentinel or Cluster)
- [ ] Persistence enabled (AOF)
- [ ] Memory limits set
- [ ] Eviction policy: `noeviction`

## Scaling

### Workers

Workers are stateless. Scale based on queue depth:

```yaml
# HPA scales on custom metric
metrics:
  - type: External
    external:
      metric:
        name: relay_queue_depth
      target:
        type: AverageValue
        averageValue: "100"
```

Or use CPU-based scaling as a proxy.

### Database

For high throughput:
1. Add read replicas for queries
2. Use connection pooling
3. Partition old events to archive tables

### Redis

For high throughput:
1. Redis Cluster for sharding
2. Increase `REDIS_POOL_SIZE`
3. Separate Redis instances for queue vs dedup

## Monitoring Queries

### Dead Letters (should be ~0)

```graphql
{ queueStats { dead } }
```

### Queue Backlog

```graphql
{ queueStats { queued pending } }
```

### Endpoint Health

```graphql
{
  endpoint(id: "...") {
    stats { successRate avgLatencyMs }
  }
}
```

## Rollback

```bash
# Kubernetes
kubectl -n relay-production rollout undo deployment/relay-api
kubectl -n relay-production rollout undo deployment/relay-worker

# Docker
docker compose down
docker compose up -d --build  # with previous image tag
```
