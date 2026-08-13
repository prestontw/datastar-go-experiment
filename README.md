# Northstar patient dashboard

A small patient-practice dashboard built with Go’s standard HTTP server, router, and templates, plus [Datastar](https://data-star.dev/) for realtime hypermedia updates.

> **Demo only:** every patient is synthetic. This project does not yet provide authentication, authorization, audit logging, or the other controls required for real clinical/PHI use.

## What the demo shows

- A two-pane patient/task workspace at `/patients`.
- A genuinely separate practice-wide page at `/tasks/due?window=14`.
- Patient search and selected-patient/status query parameters.
- Patient creation, task creation, and task completion commands.
- Multiplayer updates between clients—even when they are on different routes or query-param views.
- Full `<main>` morphs over throttled, compressed SSE rather than endpoint-specific fragments.
- PostgreSQL `NOTIFY` events emitted after commits.
- Trusted local HTTPS with HTTP/2 in both local and Docker execution.
- Native `@scope` CSS colocated with each page template.
- A standards-only `<patient-avatar>` Web Component with colocated Shadow DOM styles and checked JavaScript.

## Quick start

Choose either workflow. Docker execution does not require Nix, Go, pnpm, `just`, or PostgreSQL on the host. Nix development does not require Docker in the Linux VM. Both workflows intentionally require `mkcert` and reuse the same `.certs/localhost*.pem` layout, so trust and direct TLS/HTTP2 behavior stay as close as possible.

### Docker workflow on macOS without Nix

Install Docker Desktop and `mkcert` with Homebrew. Install `nss` as well if Firefox should use the generated CA:

```sh
brew install mkcert
# Optional for Firefox:
brew install nss
```

Then run:

```sh
./scripts/docker-up.sh
```

The script runs `mkcert -install`, creates the shared localhost certificate if it is missing, builds the Go application in Docker, and starts the app and PostgreSQL. After the macOS trust prompt is accepted, Chrome and Safari should open <https://localhost:8443/patients> without a certificate warning. The certificate covers `localhost`, `127.0.0.1`, and `::1`.

Useful Docker-only commands:

```sh
docker compose --profile app logs --follow app
docker compose --profile app down       # retain database data and host certificates
docker compose --profile app down -v    # delete PostgreSQL data; host certificates remain
```

The wrapper also selects the Docker infrastructure backend, ensures the configured logical database exists, and supplies the host UID/GID so the unprivileged scratch container can read the owner-only private key. The equivalent explicit sequence is:

```sh
./scripts/certs.sh
docker compose up -d --wait postgres
# scripts/docker-up.sh also creates DATABASE_NAME when it is not present.
LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)" \
  docker compose --profile app up -d --wait --build app
```

Do not share the `.certs` directory or use this development certificate in production.

### Nix development workflow on Linux

Only Nix with flakes enabled is required; no Docker daemon is needed:

```sh
nix develop
cp .env.example .env  # choose a unique database name/port for an agent worktree
just setup
just dev
```

Inside a Nix shell, `just infra-up` automatically starts PostgreSQL 17 as a shared, unprivileged user process supplied by Nix. Its default data directory is `$XDG_DATA_HOME/go-datastar-patient-dashboard/postgres-17` (falling back to `~/.local/share/...`), its logs use `$XDG_STATE_HOME`, and its socket/coordination files use `$XDG_RUNTIME_DIR` or `/tmp`. Calls from multiple worktrees are serialized with `flock` and reuse that one process. `just setup` also creates the trusted `mkcert` certificate and installs locked test-only packages.

The Nix shell explicitly exports `INFRA_BACKEND=nix`; the Nix lifecycle/database scripts reject calls outside that environment and do not probe for Docker. The independent macOS wrapper talks to Docker Compose directly. This makes entering `nix develop` the environment boundary instead of another runtime choice each agent must understand. All VM worktrees share the Nix PostgreSQL process and port.

Run `just` to discover all commands. Common commands are:

```sh
just dev          # local Go process + shared Nix PostgreSQL
just check        # formatting, typechecking, fast Go unit tests, and go vet
just check-all    # check plus fresh-database backend integration tests
just e2e          # Playwright with a fresh database migrated by the real server
just protocol     # print the negotiated HTTP protocol
just infra-reset patient_dashboard  # reset exactly this logical database
```

The application applies its PostgreSQL schema and synthetic seed data at startup.

## Database startup and migrations

The Go server owns schema migration in every execution mode. After connecting to PostgreSQL—and before starting the notification listener or accepting HTTPS requests—it calls `Store.Migrate`. In Nix, `just infra-up` waits for the user-level PostgreSQL process. On macOS, the Docker wrapper waits for the Compose health check before starting the application.

Migration SQL lives in `internal/postgres/migrations/` and is embedded in the server binary. The migrator:

1. Acquires a PostgreSQL transaction-scoped advisory lock, so two local/container server processes cannot migrate concurrently.
2. Creates a `schema_migrations` ledger if needed.
3. Reads numbered `.sql` files in lexical order.
4. Verifies the SHA-256 checksum of every previously applied file and refuses edited history or a database newer than the binary.
5. Applies all pending migrations and ledger entries in one transaction. A failure rolls the entire migration attempt back and prevents the HTTPS server from starting.

`001_init.sql` is idempotent to adopt databases created before the ledger was introduced; after that one-time adoption it is recorded and skipped. Add future changes as `002_description.sql`, `003_description.sql`, and so on—never edit an applied file. During local development, an intentional rewrite of one database’s migration history requires naming it explicitly, for example `just infra-reset patient_dashboard_agent_a`. This force-drops and recreates only that logical database; it never stops the selected PostgreSQL process or removes its data directory/volume.

The Linux Go process connects to Nix PostgreSQL through `localhost:5432` by default. The macOS application container connects to Compose PostgreSQL through `postgres:5432`. These environments intentionally have separate physical data stores; they share migration behavior and logical naming, not rows.

## Concurrent agents and worktrees

Agents run the Go server directly and share one selected PostgreSQL process—normally the Nix-managed user process in the Linux VM. Each worktree gets a separate logical database and HTTPS port, avoiding a PostgreSQL process or Compose project per agent. Copy `.env.example` to the ignored `.env` in each worktree and choose unique values:

```dotenv
# Worktree A
DATABASE_NAME=patient_dashboard_agent_a
APP_PORT=18443

# Worktree B uses, for example:
# DATABASE_NAME=patient_dashboard_agent_b
# APP_PORT=18444
```

`just` loads `.env`. `just dev` idempotently starts/reuses the selected shared PostgreSQL process, creates the configured logical database, exports its derived `DATABASE_URL` and `ADDR`, runs the production migrator, and starts Go. The PostgreSQL host port remains intentionally shared at 5432. Per-database migration locks do not block migrations in other agent databases, and PostgreSQL `LISTEN`/`NOTIFY` traffic is scoped to the connected database.

Only the two agent-owned values need parameterization for this model: `DATABASE_NAME` and `APP_PORT`. A PostgreSQL process or Compose project per worktree would additionally require unique process/container names, PostgreSQL host ports, data directories/volumes, networks, and images. It provides stronger process-level isolation, but the shared-process/logical-database model is simpler and substantially lighter for the VM.

Destructive helpers are deliberately scoped:

```sh
just infra-reset patient_dashboard_agent_a  # only this database
just shared-infra-down all-agents            # intentionally affects everyone
```

Raw `pg_ctl` against the shared data directory—and `docker compose down`/`down -v` when Docker is selected—bypass these safeguards and must not be used by agents against shared VM infrastructure. Resetting an agent database force-closes that database’s connections, so stop/restart that agent’s Go process; other logical databases and agents remain available.

This isolates server ports, schema, rows, migration history, and notifications. It does not fully isolate browser cookies because cookies for `localhost` are not port-scoped. Automated agents use separate Playwright browser contexts/processes, so that is acceptable for now. If humans need simultaneous tabs for several worktrees, introduce per-agent `*.localhost` hostnames and a wildcard mkcert certificate.

## Test database strategy

Fast `go test ./...` tests stay database-free: domain and security tests are pure, HTTP handler tests use the repository interface with a fake, and migration discovery/checksum tests inspect the embedded files. Database behavior is tested separately and explicitly:

```sh
just test-integration
```

Integration tests use the real `lib/pq` store and follow the “Zero to Production” isolation pattern. For every test, the harness connects to the selected shared PostgreSQL administrative database, creates a uniquely named empty database, invokes the production `Store.Migrate`, exercises repository behavior, and force-drops the database during cleanup. This ensures tests cannot accidentally rely on a developer’s schema or seed state. Override the administrative connection with `TEST_DATABASE_URL`; its database component is replaced for each fixture.

`just e2e` similarly creates one fresh database for the Playwright run. Playwright starts the real Go server against it, so server startup—not test setup—applies the embedded production migrations before `/healthz` becomes ready. The database is dropped after the run, and UI tests do not pollute the long-lived development database. It uses the worktree’s `APP_PORT`; stop that worktree server before E2E. Concurrent worktrees can run E2E on different ports, and Playwright deliberately refuses to reuse an existing server because it may point at the wrong database.

`just check` remains fast and does not require live services beyond installing locked tools. `just check-all` adds backend database integration tests; `just e2e` remains a separate browser suite.

## Routes

| Method | Route | Role |
| --- | --- | --- |
| `GET` | `/patients?patient=<uuid>&status=open&q=maya` | Static page shim |
| `POST` | `/patients?...` | Long-lived Datastar render stream for that exact view |
| `GET` | `/tasks/due?window=7` | Static second-page shim |
| `POST` | `/tasks/due?window=7` | Long-lived render stream for the due-task view |
| `POST` | `/commands?command=...` | CQRS command boundary; may patch ephemeral signals only |
| `GET` | `/healthz` | PostgreSQL readiness |

There is one URL per page. The GET/POST pair avoids separate “initial page” and “updates” URLs while preserving HTTP semantics for the static shim and Datastar stream.

## Architecture in brief

```text
browser route + query + ephemeral signals
                  │
                  │ POST, HTTPS/2
                  ▼
       stdlib net/http imperative shell
          │ query             │ command
          ▼                   ▼
 PostgreSQL snapshot     pure command preparation
          │                   │
          │                   ▼
          │             PostgreSQL transaction
          │                   │
          │            trigger → NOTIFY
          │                   │
          └──── full render ◀─┘
                    │
       throttled Datastar SSE main morph
```

A page render obtains a repeatable-read value snapshot and passes it to one top-level template. Every committed patient/task write emits the same kind of invalidation event. Each connection can therefore drop intermediate events, throttle to at most one render per 100 ms, and always query the latest state without replaying deltas.

The implementation is split into a functional core (`internal/domain`) and an imperative shell (`internal/app`, `internal/postgres`). Redis is deliberately omitted until ephemeral state must survive one process or be shared by replicas.

See [ADR 0001](docs/adr/0001-hyperlith-datastar-poc.md) for decisions, tradeoffs, and revisit triggers.

## Security model

The PoC follows Hyperlith/HIFI practices where applicable:

- Session IDs contain 160 random bits and are stored in `__Host-sid` (`Secure`, `HttpOnly`, `SameSite=Lax`, `Path=/`).
- CSRF uses a session-bound HMAC double-submit token in `__Host-csrf` plus the Datastar `csrf` signal.
- Unsafe requests also require same-origin `Origin` or Fetch Metadata.
- No CORS is enabled.
- Responses include HSTS, a restrictive CSP, frame denial, no-referrer, MIME-sniffing prevention, and a restrictive permissions policy.
- HTML is rendered by `html/template`, which contextually escapes patient content.

Datastar expressions currently require CSP `unsafe-eval`; inline colocated page CSS requires `unsafe-inline`. The ADR records both exceptions. We intentionally keep native scoped CSS literally in each `.gohtml` file during the PoC so markup and styles can be evaluated as one unit. Adjacent embedded CSS could later remove `unsafe-inline`, improve independent caching, and enable `style-src 'self'`, at the cost of weaker literal colocation and asset/cache management.

Datastar is currently pinned on jsDelivr with SRI. Vendoring its audited browser bundle into the Go binary would remove CDN availability and supply-chain runtime dependencies and narrow the source policy to `'self'`; it would not remove `unsafe-eval`, which Datastar needs to compile declarative expressions. See the ADR for the complete hardening tradeoff. Security headers and direct TLS are useful foundations, but they do not make this a production clinical system.

## Concurrent editing policy

Task editing is not implemented yet. When it is, live draft signals and morph-time focus preservation will prevent another client’s render from mechanically erasing typed text, but that alone does not prevent a lost update. Editable rows should gain an explicit revision, update time, and editor identity. Save commands will conditionally update the expected revision; a mismatch will retain the local draft while showing the baseline and latest committed value, attribution, and explicit discard/merge/overwrite choices. Datastar provides signals, reactive UI, and server patches for this flow; conflict detection and merge policy remain application/database responsibilities.

## Dependencies

Application code uses the standard library for HTTP, routing, templates, embedding, cryptography, and database abstractions. Direct Go dependencies are:

- `github.com/starfederation/datastar-go` — Datastar SSE protocol.
- `github.com/lib/pq` — the unavoidable PostgreSQL wire/database driver; Go’s standard library defines `database/sql` but ships no PostgreSQL driver.

Node packages are test/development-only: Playwright, TypeScript, and Node type declarations. Datastar itself is pinned to `v1.0.2` on jsDelivr with subresource integrity; no frontend package build is needed.

## Configuration

| Variable | Development default | Description |
| --- | --- | --- |
| `DATABASE_NAME` | `patient_dashboard` | Logical database selected by `just dev`; unique per agent worktree |
| `APP_PORT` | `8443` | Host HTTPS port selected by development/E2E scripts; unique per agent worktree |
| `INFRA_BACKEND` | Environment-owned | Set to `nix` by `flake.nix`; intentionally unset in the Docker workflow |
| `POSTGRES_PORT` | `5432` | Shared infrastructure port; all worktrees must agree |
| `ADDR` | `:8443` | Low-level server listen address; derived from `APP_PORT` by scripts |
| `DATABASE_URL` | Selected local PostgreSQL URL | Low-level PostgreSQL DSN; derived from `DATABASE_NAME` by scripts |
| `APP_SECRET` | Set by `flake.nix` | At least 32 bytes; replace outside local development |
| `TLS_CERT_FILE` | `.certs/localhost.pem` | TLS certificate |
| `TLS_KEY_FILE` | `.certs/localhost-key.pem` | TLS private key |

Go is pinned to 1.26 in `go.mod`, with toolchain `go1.26.5`; the locked Nix environment currently supplies Go 1.26.5.
