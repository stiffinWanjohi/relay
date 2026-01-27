.PHONY: build test lint run-api run-worker generate migrate docker-build docker-up docker-down clean

# Build variables
BINARY_API=bin/api
BINARY_WORKER=bin/worker
GO=go
GOFLAGS=-ldflags="-s -w"

# Default target
all: build

# Build binaries
build: build-api build-worker

build-api:
	$(GO) build $(GOFLAGS) -o $(BINARY_API) ./cmd/api

build-worker:
	$(GO) build $(GOFLAGS) -o $(BINARY_WORKER) ./cmd/worker

# Run services
run-api:
	$(GO) run ./cmd/api

run-worker:
	$(GO) run ./cmd/worker

# Generate GraphQL code
generate:
	$(GO) generate ./...

# Run tests
test:
	$(GO) test -v -race -cover ./...

test-coverage:
	$(GO) test -v -race -coverprofile=coverage.out ./...
	$(GO) tool cover -html=coverage.out -o coverage.html

# Lint
lint:
	golangci-lint run ./...

# Database migrations
migrate-up:
	migrate -path migrations -database "$(DATABASE_URL)" up

migrate-down:
	migrate -path migrations -database "$(DATABASE_URL)" down

migrate-create:
	migrate create -ext sql -dir migrations -seq $(name)

# Docker
docker-build:
	docker compose build

docker-up:
	docker compose up -d

docker-down:
	docker compose down

docker-logs:
	docker compose logs -f

# Clean
clean:
	rm -rf bin/
	rm -f coverage.out coverage.html

# Development helpers
dev-deps:
	$(GO) install github.com/99designs/gqlgen@latest
	$(GO) install -tags 'postgres' github.com/golang-migrate/migrate/v4/cmd/migrate@latest
	$(GO) install github.com/golangci/golangci-lint/cmd/golangci-lint@latest

# Help
help:
	@echo "Available targets:"
	@echo "  build        - Build all binaries"
	@echo "  build-api    - Build API server"
	@echo "  build-worker - Build delivery worker"
	@echo "  run-api      - Run API server"
	@echo "  run-worker   - Run delivery worker"
	@echo "  generate     - Generate GraphQL code"
	@echo "  test         - Run tests"
	@echo "  test-coverage- Run tests with coverage"
	@echo "  lint         - Run linter"
	@echo "  migrate-up   - Run database migrations"
	@echo "  migrate-down - Rollback database migrations"
	@echo "  docker-build - Build Docker images"
	@echo "  docker-up    - Start Docker containers"
	@echo "  docker-down  - Stop Docker containers"
	@echo "  clean        - Remove build artifacts"
	@echo "  dev-deps     - Install development dependencies"
