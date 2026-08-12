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

Choose either workflow. Docker execution does not require Nix, Go, pnpm, `just`, PostgreSQL, or Redis on the host. Both workflows intentionally require `mkcert` and reuse the same `.certs/localhost*.pem` files, so trust and direct TLS/HTTP2 behavior stay as close as possible.

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

The script runs `mkcert -install`, creates the shared localhost certificate if it is missing, builds the Go application in Docker, and starts the app, PostgreSQL, and Redis. After the macOS trust prompt is accepted, Chrome and Safari should open <https://localhost:8443/patients> without a certificate warning. The certificate covers `localhost`, `127.0.0.1`, and `::1`.

Useful Docker-only commands:

```sh
docker compose --profile app logs --follow app
docker compose --profile app down       # retain database data and host certificates
docker compose --profile app down -v    # delete database/Redis data; host certificates remain
```

The wrapper also supplies the host UID/GID so the unprivileged scratch container can read the owner-only private key. The equivalent explicit sequence is:

```sh
./scripts/certs.sh
LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)" \
  docker compose --profile app up -d --wait --build
```

Do not share the `.certs` directory or use this development certificate in production.

### Nix development workflow

You need Nix with flakes enabled and access to either the host Docker daemon or another compatible Docker endpoint:

```sh
nix develop
just setup
just dev
```

`just setup` creates a trusted local `mkcert` certificate, starts PostgreSQL and Redis through Compose, and installs the locked test-only packages. Nix supplies Docker client tools for convenience but does not run a Docker daemon; using Docker Desktop’s/your host’s `docker` command instead is also valid.

Run `just` to discover all commands. Common commands are:

```sh
just dev          # local Go process + containerized infrastructure
just docker-up    # same Docker-only script described above
just check        # formatting, JS/TS typechecking, Go tests, and go vet
just e2e          # Playwright sanity and cross-view realtime tests
just protocol     # print the negotiated HTTP protocol
just infra-reset  # discard and recreate local data
```

The application applies its PostgreSQL schema and synthetic seed data at startup.

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

The implementation is split into a functional core (`internal/domain`) and an imperative shell (`internal/app`, `internal/postgres`). Redis is available in Compose but deliberately unused until ephemeral state must survive one process or be shared by replicas.

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
| `ADDR` | `:8443` | HTTPS listen address |
| `DATABASE_URL` | Local Compose PostgreSQL URL | PostgreSQL DSN |
| `APP_SECRET` | Set by `flake.nix` | At least 32 bytes; replace outside local development |
| `TLS_CERT_FILE` | `.certs/localhost.pem` | TLS certificate |
| `TLS_KEY_FILE` | `.certs/localhost-key.pem` | TLS private key |

Go is pinned to 1.26 in `go.mod`, with toolchain `go1.26.5`; the locked Nix environment currently supplies Go 1.26.5.
