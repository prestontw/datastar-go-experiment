set shell := ["bash", "-euo", "pipefail", "-c"]
set dotenv-load := true

app_port := env_var_or_default("APP_PORT", "8443")
database_name := env_var_or_default("DATABASE_NAME", "patient_dashboard")
app_url := "https://localhost:" + app_port

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

# Stop infrastructure shared by every worktree; requires an explicit acknowledgement.
shared-infra-down acknowledgement:
  @[[ {{quote(acknowledgement)}} == "all-agents" ]] || { echo "Refusing: run 'just shared-infra-down all-agents'" >&2; exit 2; }
  docker compose stop postgres redis

# Reset the configured logical database after typing its exact name.
infra-reset database:
  @[[ {{quote(database)}} == {{quote(database_name)}} ]] || { echo "Refusing: name does not match this worktree's DATABASE_NAME" >&2; exit 2; }
  ./scripts/database.sh reset {{quote(database)}}

# Ensure the configured worktree database exists.
db-ensure: infra-up
  ./scripts/database.sh ensure {{quote(database_name)}}

# Run the Go server locally over HTTPS/2 using this worktree's database and port.
dev: certs infra-up
  ./scripts/dev.sh

# Build and run the Nix-independent Docker stack using the shared mkcert certificate.
docker-up:
  ./scripts/docker-up.sh

# Follow logs from the containerized application.
docker-logs:
  docker compose --profile app logs --follow app

# Stop only the containerized application; shared PostgreSQL and Redis stay up.
docker-down:
  docker compose --profile app stop app

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

# Run fast Go unit tests; these do not require PostgreSQL.
test:
  go test ./...

# Create a fresh migrated PostgreSQL database per backend integration test.
test-integration: infra-up
  TEST_DATABASE_URL="postgres://dashboard:dashboard@localhost:5432/postgres?sslmode=disable" go test -count=1 -tags=integration ./internal/postgres

# Run static checks and unit tests.
check: fmt-check typecheck test
  go vet ./...

# Run unit checks plus database integration tests.
check-all: check test-integration

# Run Playwright against a fresh database migrated by the real server.
e2e: certs infra-up deps
  ./scripts/e2e.sh

# Open Playwright's interactive UI against its own fresh migrated database.
e2e-ui: certs infra-up deps
  ./scripts/e2e.sh --ui

# Confirm the running development server negotiated HTTP/2.
protocol:
  @curl --silent --show-error --insecure --http2 --head {{app_url}}/patients | head -n 1

# Download modules and build the server binary.
build:
  go build -o patient-dashboard ./cmd/server
