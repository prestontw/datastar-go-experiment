set shell := ["bash", "-euo", "pipefail", "-c"]

app_url := "https://localhost:8443"

# List the available project commands.
default:
  @just --list

# Prepare certificates, containers, and browser-test dependencies.
setup: certs infra-up deps

# Generate and trust the localhost certificate shared by local and Docker runs.
certs:
  ./scripts/certs.sh

# Install pnpm-managed test and typecheck dependencies.
deps:
  pnpm install --frozen-lockfile

# Start PostgreSQL and Redis, waiting until both are healthy.
infra-up:
  docker compose up -d --wait postgres redis

# Stop infrastructure without deleting its data.
infra-down:
  docker compose stop postgres redis

# Delete and recreate database data while retaining the Docker development CA.
infra-reset:
  docker compose down
  docker volume rm patient-dashboard_postgres-data patient-dashboard_redis-data 2>/dev/null || true
  docker compose up -d --wait postgres redis

# Run the Go server locally over HTTPS/2.
dev: certs infra-up
  go run ./cmd/server

# Build and run the Nix-independent Docker stack using the shared mkcert certificate.
docker-up:
  ./scripts/docker-up.sh

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
