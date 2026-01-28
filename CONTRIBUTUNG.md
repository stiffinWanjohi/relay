# Contributing

Thanks for your interest in Relay.

## Quick Setup

```bash
git clone https://github.com/stiffinWanjohi/relay.git
cd relay

# Start dependencies
docker compose up -d postgres redis

# Run migrations
make migrate-up

# Run services (separate terminals)
make run-api
make run-worker
```

## Making Changes

1. Fork the repo
2. Create a branch (`git checkout -b fix/something`)
3. Make changes
4. Run tests (`make test`)
5. Run linter (`make lint`)
6. Commit with [conventional commits](https://www.conventionalcommits.org/)
7. Open PR

## Commit Messages

```
feat(worker): add retry jitter
fix(queue): handle redis timeout
docs: update deployment guide
test(circuit): add half-open tests
```

## Code Style

- Run `make lint` before committing
- Keep functions small and focused
- Add tests for new features
- Update docs if behavior changes

## Issues

Bug reports and feature requests welcome. Please include:

- What you expected
- What happened
- Steps to reproduce
- Go version, OS

## Questions?

Open an issue or reach out on [Twitter/X](https://twitter.com/stiffinWanjohi).
