set shell := ["bash", "-euo", "pipefail", "-c"]

app_url := "https://localhost:8443"

# List the available project commands.
default:
  @just --list

# Prepare certificates, containers, and browser-test dependencies.
setup: certs infra-up deps

# Generate and trust a localhost development certificate.
certs:
  @mkdir -p .certs
  @if [[ ! -s .certs/localhost.pem || ! -s .certs/localhost-key.pem ]]; then \
    mkcert -install; \
    mkcert -cert-file .certs/localhost.pem -key-file .certs/localhost-key.pem localhost 127.0.0.1 ::1; \
    chmod 0600 .certs/localhost-key.pem; \
  fi

# Install pnpm-managed test and typecheck dependencies.
deps:
  pnpm install --frozen-lockfile

# Start PostgreSQL and Redis, waiting until both are healthy.
infra-up:
  docker compose up -d --wait postgres redis

# Stop infrastructure without deleting its data.
infra-down:
  docker compose stop postgres redis

# Delete and recreate the development databases.
infra-reset:
  docker compose down --volumes
  docker compose up -d --wait postgres redis

# Run the Go server locally over HTTPS/2.
dev: certs infra-up
  go run ./cmd/server

# Build and run the complete stack in Docker over HTTPS/2.
docker-up: certs
  LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)" docker compose --profile app up -d --wait --build

# Follow logs from the containerized application.
docker-logs:
  docker compose --profile app logs --follow app

# Stop the complete Docker stack.
docker-down:
  docker compose --profile app down

# Format all Go source files.
fmt:
  go fmt ./...
  nixfmt flake.nix

# Fail if Go or Nix files need formatting.
fmt-check:
  @unformatted="$(gofmt -l $(find . -name '*.go' -not -path './vendor/*'))"; [[ -z "$unformatted" ]] || { echo "Go files need formatting:"; echo "$unformatted"; exit 1; }
  @nixfmt --check flake.nix

# Typecheck the standards-only Web Component and Playwright tests.
typecheck: deps
  pnpm run typecheck

# Run Go unit tests.
test:
  go test ./...

# Run static checks and unit tests.
check: fmt-check typecheck test
  go vet ./...

# Run Playwright sanity and multiplayer tests.
e2e: certs infra-up deps
  pnpm exec playwright test

# Open Playwright's interactive test UI.
e2e-ui: certs infra-up deps
  pnpm exec playwright test --ui

# Confirm the running development server negotiated HTTP/2.
protocol:
  @curl --silent --show-error --insecure --http2 --head {{app_url}}/patients | head -n 1

# Download modules and build the server binary.
build:
  go build -o patient-dashboard ./cmd/server
