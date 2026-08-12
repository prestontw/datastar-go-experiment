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

You need Nix with flakes enabled and a running Docker daemon.

```sh
nix develop
just setup
just dev
```

`just setup` will:

1. Create and trust a localhost certificate with `mkcert`.
2. Start PostgreSQL and Redis with Docker Compose.
3. Install test-only packages from `pnpm-lock.yaml`.

Open <https://localhost:8443/patients>. The application applies its PostgreSQL schema and synthetic seed data at startup.

Run `just` to discover all commands. Common commands are:

```sh
just dev          # local Go process + containerized infrastructure
just check        # formatting, JS/TS typechecking, Go tests, and go vet
just e2e          # Playwright sanity and cross-view realtime tests
just protocol     # print the negotiated HTTP protocol
just infra-reset  # discard and recreate local data
```

### Complete Docker stack

Generate the trusted certificate once, then build and run the app with the same direct TLS/HTTP2 setup:

```sh
nix develop
just docker-up
just docker-logs
```

The Compose recipe runs the container as your local UID/GID so it can read the owner-only mkcert private key mounted from `.certs/`.

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

Datastar expressions currently require CSP `unsafe-eval`; inline colocated page CSS requires `unsafe-inline`. The ADR records both exceptions. Security headers and direct TLS are useful foundations, but they do not make this a production clinical system.

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
