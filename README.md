# [Datastar](https://data-star.dev/) Proof of Concept

After watching [*Put State in the Right Place*](https://www.youtube.com/watch?v=W7Ki3aXgmZU), I wanted to try playing around with Datastar.
I had read a little bit about it on the Rust subreddit, on a blog post (<https://hamy.xyz/blog/2026-03_datastar-rust-todo>), and as an [alternative](https://htmx.org/essays/alternatives/#datastar) to [HTMX](https://htmx.org/) (though I was initially more interested in [`htmz`](https://leanrada.com/htmz/)).
After checking out <https://www.youtube.com/watch?v=2ECucq-mTGg>, referenced in the *Software Should Work* talk, I wanted to hone in specifically on [`Hyperlith`](https://github.com/andersmurphy/hyperlith).
I haven't gotten around to actually using Clojure, though, so instead I wanted to try building off of the [rational](https://github.com/andersmurphy/hyperlith#rational-more-like-a-collection-of-opinions) in my own proof of concept.

I wanted something a little more advanced than the typical Todo demo, so I came up with a task list per "patient" (which is really multiple Todo lists on each other's shoulders in a trench coat).
Some of the things I was interested in trying out were:
- I wanted the app to "really make you feel like SPA-der-Man" (I wanted it to feel like a single-page application). This meant limiting screen changes when changing between patients, and customizing links to open a new SSE stream instead of opening a new page and then a new SSE, in the style of [Inertia.js](https://inertiajs.com/). Since I was following the Hyperlith approach, opening a new page would show an empty shell until we got the new patient content, which ruined the SPA illusion.

  I also used the trick of navigating on mouse down rather than click to make the app feel even snappier.
- Maintaining `view = f(state)`. When I was going through the [*Hypermedia Systems*](https://hypermedia.systems/) book, it felt tougher to debug the state of the application if I ended up in a wonky state after several actions: the "formula" was more like `view = f(state) + i_1(interaction, state) + i_2(interaction, state) + ... + i_n(interaction, state)`. Initially, I wanted to architect the frontend more like a reactive app based on signals, but I liked trying out the Hyperlith approach!
- Persistent drafts: I wanted this app to feel like a local app or like a notebook after watching some of my doctors enter in information during an appointment. I didn't want operators to lose information.
- Maintain focus and input elements across new HTML sent over SSE.
- Minimal external dependencies. I am trying to prioritize this in my main development, but I wanted to see how far I could push this in this PoC. This decision led to implementing this PoC in Go with its web-friendly standard library---I'm really happy with only two direct dependencies (Datastar and the postgres client library)!
- Colocated styles and content. I like Svelte's and Astro's approach to styling and wanted something that felt similar. Tailwind is a nice first approximation, but introduces an asset build step (which I'm not ready for yet!) and also isn't vanilla CSS, which could incur a cost later when updating versions.
- Easy multi-agent developing. I went with one Postgres instance running with each agent/worktree having an individual logical database because I'm trying to do my development within a VM, which is a constrained environment, so I wanted to trade a little isolation for reduced resources.

  This also had some effect on picking Nix for reproducible dev environments.
- Local development over HTTPS and HTTP/2, which should help with production equivalency and local usage of SSE while developing.
- Simple web-component for my own understanding as well.
- UUIDv7's! When the agent bootstrapped Postgres, it used an older version that didn't support v7 out of the box!
- Multiple levels of testing, from unit tests to curl-style integration tests to Playwright tests.
  Test databases are created in the style of [*Zero to Production in Rust](https://80a8f3c0.lpalmieri.pages.dev/posts/2020-08-31-zero-to-production-3-5-html-forms-databases-integration-tests/#3-7-1-test-isolation).

Overall, I'm really happy with and energized by how this project turned out. Programming in Go does not spark joy, but I am very pleased with the iteration speed and minimal external dependencies!

---

Below is what the agent spewed out.
It might be helpful in terms of more specific technical direction and details.

---

# Northstar patient dashboard

A small patient-practice dashboard built with Go’s standard HTTP server, router, and templates, plus [Datastar](https://data-star.dev/) for realtime hypermedia updates.

> **Demo only:** every patient is synthetic. This project does not yet provide authentication, authorization, audit logging, or the other controls required for real clinical/PHI use.

## What the demo shows

- A two-pane patient/task workspace at `/patients`.
- A genuinely separate practice-wide page at `/tasks/due?window=14`.
- Debounced interactive patient search and selected-patient/status query parameters.
- No-flash, history-aware in-place morphs beginning on mouse down between patient/status views, with native and keyboard fallback.
- Patient creation, task creation/completion, and durable per-tab/per-patient task drafts.
- Multiplayer updates between clients—even when they are on different routes or query-param views.
- Full `<main>` morphs over throttled, compressed SSE rather than endpoint-specific fragments.
- PostgreSQL `NOTIFY` events emitted after commits.
- Trusted local HTTPS with HTTP/2 in both local and Docker execution.
- Native `@scope` CSS colocated with each page template.
- A standards-only `<patient-avatar>` Web Component with colocated Shadow DOM styles and checked JavaScript.

## Quick start

Choose either workflow. Docker execution does not require Nix, Go, pnpm, `just`, or PostgreSQL on the host. Nix development does not require Docker in the Linux VM. Both workflows intentionally require `mkcert` and reuse the same `.certs/localhost*.pem` layout, so direct TLS/HTTP2 behavior stays as close as possible. Each environment has its own mkcert CA; `scripts/certs.sh` replaces an existing leaf certificate when the current environment's CA did not issue it.

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

The script runs `mkcert -install`, creates the localhost certificate when it is missing or was issued by another environment's mkcert CA, builds the Go application in Docker, and starts the app and PostgreSQL. Run it from the macOS host—not from the Linux Nix shell—so the certificate is issued by the CA trusted by the host browser. After the macOS trust prompt is accepted, Chrome and Safari should open <https://localhost:8443/patients> without a certificate warning. The certificate covers `localhost`, `127.0.0.1`, and `::1`.

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
  docker compose --profile app up -d --wait --build --force-recreate app
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

Inside a Nix shell, `just infra-up` automatically starts PostgreSQL 18.4 as a shared, unprivileged user process supplied by Nix. Its default data directory is `$XDG_DATA_HOME/go-datastar-patient-dashboard/postgres-18` (falling back to `~/.local/share/...`), its logs use `$XDG_STATE_HOME`, and its socket/coordination files use `$XDG_RUNTIME_DIR` or `/tmp`. Calls from multiple worktrees are serialized with `flock` and reuse that one process. `just setup` also creates the trusted `mkcert` certificate and installs locked test-only packages.

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

Migration SQL lives in `internal/postgres/migrations/` and is embedded in the server binary. The schema targets PostgreSQL 18 and uses native `uuidv7()`. The migrator:

1. Acquires a PostgreSQL transaction-scoped advisory lock, so two local/container server processes cannot migrate concurrently.
2. Creates a `schema_migrations` ledger if needed.
3. Reads numbered `.sql` files in lexical order.
4. Verifies the SHA-256 checksum of every previously applied file and refuses edited history or a database newer than the binary.
5. Applies all pending migrations and ledger entries in one transaction. A failure rolls the entire migration attempt back and prevents the HTTPS server from starting.

`001_init.sql` defines native `UUID` primary keys with `DEFAULT uuidv7()`. Application commands request IDs from PostgreSQL as explicit coeffects before preparing inserts, so all newly created patient/task IDs are v7 while command functions remain deterministic. Deterministic synthetic fixtures are also valid v7 values. `002_task_drafts.sql` adds private per-session/tab/patient drafts. Applied files are recorded and skipped; add future changes as the next numbered migration and never edit an applied file.

The PostgreSQL 18 upgrade intentionally starts clean rather than performing `pg_upgrade`: Nix uses a new `postgres-18` data directory and Compose uses a new `postgres-18-data` volume. The obsolete Nix 17 cluster in the primary VM was stopped and removed during this change. On a Mac that previously ran this project, the old unreferenced volume may be deleted with `docker volume rm patient-dashboard_postgres-data` after stopping the old stack. During local development, an intentional rewrite of one database’s migration history requires naming it explicitly, for example `just infra-reset patient_dashboard_agent_a`. This force-drops and recreates only that logical database; it never stops the selected PostgreSQL process or removes its data directory/volume.

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

Fast `just test` checks the table-driven JavaScript view-transition policy with Node's built-in test runner and runs database-free Go tests: domain and security tests are pure, HTTP handler tests use the repository interface with a fake, PostgreSQL selection policy is a pure unit, and migration discovery/checksum tests inspect the embedded files. `TestBrowserlessHTTPProtocol` additionally drives that fake-backed application through a real loopback TLS/HTTP2 connection using a stateful Go `http.Client` and cookie jar. It verifies the shell, secure session/CSRF cookies, initial Datastar SSE render, and an authenticated command at the wire level without launching a browser.

That protocol test deliberately sits between direct `httptest.ResponseRecorder` tests and Playwright. It is fast and deterministic and catches request/header/cookie/SSE integration mistakes, but it is not a replacement for the real PostgreSQL integration tests or for Playwright's DOM, focus, history, event, and Web Component coverage. Database behavior is tested separately and explicitly:

```sh
just test-integration
```

Integration tests use the real `lib/pq` store and follow the “Zero to Production” isolation pattern. For every test, the harness connects to the selected shared PostgreSQL administrative database, creates a uniquely named empty database, invokes the production `Store.Migrate`, exercises behavior, and force-drops the database during cleanup. This ensures tests cannot accidentally rely on a developer’s schema or seed state. Override the administrative connection with `TEST_DATABASE_URL`; its database component is replaced for each fixture.

`TestDatabaseBackedHTTPProtocol` composes those real migrations and store operations with the browserless TLS/HTTP2 client. It creates a patient, saves its task draft, and creates the task through authenticated HTTP commands; reads each state back through Datastar SSE views; and compares a normalized wire transcript at `internal/app/testdata/database_http_protocol.golden`. Dynamic UUID and security-token values are reduced to stable contract facts such as UUID version, cookie names, event types, statuses, and persisted labels. After intentionally reviewing a protocol change, update it with:

```sh
UPDATE_SNAPSHOTS=1 just test-integration
```

This gives regression flows a faster Go-native home while Playwright remains responsible for browser execution. The snapshot flow opens a fresh stream after each write; PostgreSQL `LISTEN`/`NOTIFY` propagation across already-connected views remains covered by Playwright.

`just e2e` similarly creates one fresh database for the Playwright run. Playwright starts the real Go server against it, so server startup—not test setup—applies the embedded production migrations before `/healthz` becomes ready. The database is dropped after the run, and UI tests do not pollute the long-lived development database. It uses the worktree’s `APP_PORT`; stop that worktree server before E2E. Concurrent worktrees can run E2E on different ports, and Playwright deliberately refuses to reuse an existing server because it may point at the wrong database.

`just check` remains fast and does not require live services beyond installing locked tools. `just check-all` adds backend database integration tests; `just e2e` remains a separate browser suite.

## Routes

| Method | Route | Role |
| --- | --- | --- |
| `GET` | `/patients?patient=<uuid>&status=open&q=maya` | Static page shim |
| `POST` | `/patients?...` | Long-lived Datastar render stream for that exact view |
| `GET` | `/tasks/due?window=7` | Static second-page shim |
| `POST` | `/tasks/due?window=7` | Long-lived render stream for the due-task view |
| `POST` | `/commands?command=...` | CQRS command boundary for records and private drafts |
| `GET` | `/healthz` | PostgreSQL readiness |

There is one URL per page. The GET/POST pair avoids separate “initial page” and “updates” URLs while preserving HTTP semantics for the static shim and Datastar stream.

Patient and status links keep canonical query-string URLs but are progressively enhanced. A small checked, dependency-free utility (`internal/web/assets/patient-view-navigation.js`) delegates `mousedown` and `click` handling for links marked only with `data-patient-navigation`. Plain primary-button navigation begins on mouse down; the later click is suppressed. Keyboard/synthetic clicks use the click fallback, while Command/Ctrl/Shift/Alt clicks, non-primary buttons, downloads, and alternate targets retain native behavior. The utility emits one event to the stable Datastar stream controller, which keeps the current dashboard visible and updates browser history. A single reactive stream effect owns every patient-page request: a revision signal only tells it when to reopen, while it always reads the destination from `window.location`. The browser URL is therefore the sole canonical route state rather than being duplicated in a `_pageUrl` signal. On every run, the effect synchronously aborts an explicitly owned `AbortController`, creates its replacement, and passes that controller to Datastar. This avoids relying on action-cleanup bookkeeping to cancel a different-URL long-lived request.

The server independently enforces the same ownership rule. Each shell creates a random `pageStreamId` for that active document and sends a monotonically increasing `pageStreamRevision`. An in-process registry keyed by hashed session plus document ID cancels the previous transport, accepts same-revision network reconnects, and answers a lower-revision late retry with `204 No Content` so it cannot displace or patch the current route. Short-lived revision tombstones cover Datastar's retry window and then expire. The registry is intentionally process-local for this single-server PoC; multiple application replicas would require affinity by stream identity or shared cancellation/ordering coordination. This identity is deliberately separate from the durable draft `tabId`: committed tasks are shared records, drafts are tab-scoped working slips, and page-stream ownership is scoped only to one live document. Back/forward repeats the stream transition; without JavaScript, the same `href` performs ordinary navigation.

The same utility progressively enhances the GET search form. Input is debounced for 300 ms, so no search request starts while keystrokes continue inside that quiet window; pausing opens one replacement stream, keeps the input focused, filters the patient picker, clears the task pane until a result is chosen, and uses `history.replaceState` so incremental queries do not flood browser history. An explicit patient choice cancels any pending search timer so an older query cannot overwrite the newer selection; clearing the search after choosing a result retains that patient while restoring the full picker. Explicit form submission runs immediately, and the ordinary GET form remains the no-JavaScript fallback.

The transition policy is explicit and executable:

| Search transition | `q` | `patient` |
| --- | --- | --- |
| Start or refine search | non-empty | removed |
| Choose a result | retained | chosen patient |
| Clear after choosing | removed | retained |
| Clear without choosing | removed | absent |

`patientSearchURL` in `internal/web/assets/patient-view-policy.js` owns the client URL rules and is table-tested in `tests/patient-view-policy.test.js`. `selectDashboardPatient` in `internal/postgres/store.go` owns server interpretation of filtered results and is table-tested in `internal/postgres/store_test.go`. The rendered hidden patient field is derived from the selected snapshot; it transports that canonical selection through the GET form rather than introducing another state store.

Command success and error messages render in one fixed bottom-right shell notification layer outside `<main>`. Showing a message therefore does not move the page heading, patient picker, task form, or list; each message has an accessible dismiss button, and navigation can also clear it without waiting for a main morph. The checked controller and its pure policy are embedded same-origin JavaScript modules and add no package, build step, or framework infrastructure. The `/assets/{name}` handler serves any existing `.js` file embedded from `internal/web/assets/*.js`, while rejecting other extensions and missing files; adding another checked JavaScript asset does not require editing a server allowlist. Links between Patients and Due tasks remain native because they cross page renderers and scoped styles. Explicit opt-in is safer and smaller than globally hijacking all links.

New-task drafts are durable, private working state rather than shared practice records. The browser keeps a random tab identity in `sessionStorage`; PostgreSQL keys each draft by a domain-separated hash of the secure session ID, that tab UUID, and patient UUID. Input changes advance a revision and are saved after a 500 ms quiet period. Patient/search/history navigation flushes a pending save before changing context, and hiding the tab attempts the same on a best-effort basis. The draft table intentionally emits no practice-wide `NOTIFY` event.

On return, the server includes the selected patient's draft in the authoritative render. A patient guard hydrates Datastar task signals only when task context changes, so an unrelated realtime morph for the same patient cannot replace actively typed local values with an older database snapshot. Navigating still clears the old task signals immediately, preventing one patient's title from appearing under another while the replacement stream arrives.

Creating a task and clearing its draft happen in one PostgreSQL transaction. Draft saves carry monotonically increasing per-tab revisions; task creation writes an empty tombstone at the next revision, so an autosave already in flight cannot resurrect submitted text. After the command commits, Datastar clears title, date, and priority signals. This is deliberately paper-like best effort, not a claim of synchronous keystroke durability: a browser or network can disappear before the debounce/flush request completes. Session loss can also leave an unreachable draft row; retention cleanup and saved-age UI are future operational refinements.

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

A patient render obtains a repeatable-read shared-practice snapshot plus the selected tab's private draft and passes those values to one top-level template. Every committed patient/task write emits the same kind of invalidation event. Each connection can therefore drop intermediate events, throttle to at most one render per 100 ms, and always query the latest state without replaying deltas.

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
