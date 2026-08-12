# ADR 0001: Hyperlith-inspired Go and Datastar PoC

- **Status:** Accepted for the PoC
- **Date:** 2026-08-11
- **Owners:** Project maintainers
- **Decision scope:** Application shape, rendering, realtime delivery, state, security, styling, tooling, and local/container transport

## Context

This project is an educational patient-dashboard demonstration for therapists and psychiatrists. It should remain small enough to understand as one system while demonstrating server-driven interaction, multiplayer updates, multiple page views, query parameters, secure session/CSRF foundations, PostgreSQL persistence, and HTTP/2 SSE.

The primary architectural references are Anders Murphy’s [Hyperlith](https://github.com/andersmurphy/hyperlith) values and Casey Link’s [HIFI CRUD](https://github.com/Ramblurr/hifi-crud) continuation. The relevant ideas are:

- `view = f(state)` with one top-level render function per page;
- large/main morphs instead of fragment-specific response endpoints;
- immediate updates after state changes;
- commands separated from queries and render functions;
- homogeneous database events, allowing throttling and frame dropping;
- durable state in the database and signals only for ephemeral input/UI state;
- unguessable cookie sessions, CSRF protection, same-origin operation, and strict response headers;
- a small, sovereign deployment unit with few dependencies.

“Homogeneous” is used here in the Hyperlith sense: every database event has the same semantic meaning—“render the latest state”—rather than carrying a required delta.

## Decisions

### 1. Use a Go monolith and standard-library web stack

Use `net/http`, the Go 1.22+ `ServeMux`, `html/template`, `embed`, `crypto/*`, and `database/sql`. There is no application framework, alternate router, template generator, JavaScript framework, CSS compiler, or asset pipeline.

Direct Go dependencies are limited to:

1. `github.com/starfederation/datastar-go` for Datastar’s SSE protocol.
2. `github.com/lib/pq` for PostgreSQL.

The PostgreSQL driver is an explicit exception to the “Datastar-only” dependency goal: `database/sql` intentionally ships without database drivers. `lib/pq` is mature and dependency-free, which keeps this PoC smaller than a feature-rich driver stack. Revisit it if PostgreSQL features, performance, or active driver evolution require `pgx`.

### 2. Use one URL and top-level render function per page

The two pages are:

- `/patients?patient=<uuid>&status=<open|done|all>&q=<search>`
- `/tasks/due?window=<7|14|30>`

A `GET` returns a mostly static shim; Datastar then makes a long-lived `POST` to the same path and query string. The server queries a value snapshot and invokes exactly one page renderer, producing the complete `<main id="morph">`.

The due-task route is intentionally a second page, not a dashboard partial. It forces one homogeneous database change to be interpreted by clients on different routes and query-param views.

**Consequences:**

- Initial and subsequent dynamic content share one rendering path.
- Link previews and clients without JavaScript receive only a shell and no patient data.
- Query parameters describe views without introducing path hierarchy or path-parameter routing.
- Navigation currently reloads a shim. This is simple and can be optimized later without changing page semantics.

### 3. Apply CQRS and functional-core/imperative-shell boundaries

Commands are accepted at `/commands?command=...`. Domain preparation and validation functions consume supplied values/coeffects and return descriptions of desired records. They do not access a database, generate IDs, or send responses. The shell owns random IDs, current dates, persistence, logging, and Datastar effects.

Page handlers query repeatable-read PostgreSQL snapshots, then pass those values to templates. Commands do not patch page elements. They may patch ephemeral signals for validation messages, success messages, and form clearing; committed database changes drive page rendering.

This is deliberately pragmatic FC/IS rather than an effect-description engine. Introduce explicit effect data only if command orchestration becomes difficult to test or reason about.

### 4. Use homogeneous committed-database notifications

Statement-level PostgreSQL triggers call `pg_notify('practice_changed', ...)` after patient/task writes. A dedicated listener converts every notification into the same in-process `DatabaseEvent`. Payload details are ignored.

Each SSE connection has a one-slot dropping buffer and renders no more often than once per 100 ms. A dropped event loses no domain information because the retained event causes a query of the latest committed state. On PostgreSQL listener reconnect, all clients are invalidated once to cover potentially missed notifications.

The server always sends the complete page `<main>`. It does not compute an HTML diff or maintain a server-side prior view. Datastar/Idiomorph performs the fine-grained browser DOM morph. SSE streams prefer Brotli and fall back to gzip, making repeated HTML highly compressible.

**Consequences:**

- All views may recompute after unrelated writes. That cost is accepted for simplicity and measured before adding culling.
- PostgreSQL is the cross-process event source, so multiple app replicas can observe writes without Redis pub/sub.
- `NOTIFY` is an invalidation hint, not a durable queue. Reconnect invalidation is safe because views derive from current database state.
- Slow clients drop frames rather than back-pressuring all clients.

Revisit with metrics if render CPU, database reads, or fan-out become material. Optimize in this order: shared render caching for identical views, smarter event culling, then a different delivery model only if needed.

### 5. Keep durable state in PostgreSQL; reserve Redis

Patients, tasks, and future authenticated session records belong in PostgreSQL. Current browser signals contain only ephemeral form values, flash/errors, CSRF, and a per-tab random identifier.

Redis runs in the development Compose stack as an explicit integration seam but is not used by application code. Add it only when transient per-tab configuration must survive one process, presence must be shared across replicas, or measured notification/fan-out needs justify it. Avoiding speculative Redis state prevents split sources of truth.

### 6. Follow Hyperlith/HIFI session and CSRF practices

- Generate session IDs from 160 cryptographically random bits, URL-safe and unpadded.
- Store them in host-only `__Host-sid` cookies with `Secure`, `HttpOnly`, `SameSite=Lax`, and `Path=/`.
- Use an HMAC double-submit CSRF token bound to the session ID. Store it in readable `__Host-csrf`; the static shell reads it into an ephemeral Datastar signal submitted on every unsafe request.
- Verify the submitted token’s session-bound HMAC in constant time and require the delivery cookie to be present. A still-valid signed token is accepted if another tab has since rotated the shared cookie, so one tab cannot break another.
- Reject unsafe requests unless Fetch Metadata says `same-origin` or a matching `Origin` is present. Do not enable CORS.
- Use `html/template` contextual escaping and standard security headers.

The CSP permits jsDelivr for the temporarily CDN-hosted Datastar module and includes its SRI hash. Datastar expression evaluation currently needs `'unsafe-eval'`; colocated inline page CSS needs `'unsafe-inline'`. These are known CSP concessions, not defaults to copy blindly. Vendoring Datastar and adopting nonced or external style assets can tighten CSP later.

Authentication, authorization, clinical audit logs, retention rules, secrets management, field encryption, backup/restore validation, and compliance controls are out of scope. Synthetic data is mandatory.

### 7. Use native scoped CSS and one Web Component example

Each page’s content and `@scope` styles live in the same `.gohtml` file. This gives locality and boundaries without Tailwind versions, class translation, generated names, or a Node build step. Shared shell styles remain in the shell template.

`<patient-avatar>` is a standards-only custom element. Its DOM and Shadow DOM CSS are colocated in one JavaScript file. It uses no runtime package, is served from the Go binary, and is checked with TypeScript’s `allowJs`/`checkJs` mode. Server-rendered patient data remains in normal HTML; the component is progressive presentation, not client-side application state.

`@scope` and Web Components target modern browsers, consistent with the Playwright/Datastar baseline. Revisit CSS Modules or another strategy if browser support or stylesheet scale requires it.

### 8. Terminate trusted TLS directly in the Go process

Local and container execution both call `http.Server.ListenAndServeTLS` with HTTP/1.1 and HTTP/2 explicitly enabled. There is no development-only plaintext path or Docker reverse proxy. `mkcert` produces a trusted localhost certificate mounted into the container. SSE has no server write timeout; shutdown cancellation closes streams.

**Consequences:** local behavior exercises the same transport semantics as the container, reducing SSE connection-limit and buffering surprises. A production deployment may terminate TLS at a proxy, but that proxy must preserve streaming, avoid response buffering, and negotiate HTTP/2 with clients.

### 9. Pin tools and test at multiple levels

- `go.mod` declares Go 1.26 and toolchain 1.26.5.
- `flake.lock` pins Nixpkgs; the shell supplies Go 1.26.5, pnpm, Node, mkcert, Docker clients, and Playwright browsers. PostgreSQL and Redis run only through Docker Compose.
- `pnpm-lock.yaml` pins test-only Node packages.
- Docker images pin Go, PostgreSQL, and Redis patch versions.
- Go tests cover pure commands, security tokens, event dropping, templates, and HTTP command boundaries.
- Playwright checks HTTP/2, Web Component upgrade, patient creation, CSRF rejection, query-param routing, and one change appearing on two different pages.

`just` is the discoverable command interface; Nix is the tool environment; Docker Compose is the PostgreSQL/Redis runtime environment.

## Alternatives considered

- **HTMX or fragment endpoints:** rejected because fragment routes work against the one-view/continuous-signal experiment.
- **Tailwind:** deferred to avoid a build step and version migration while the UI is small.
- **CSS Modules:** no natural payoff without a bundler and JS/TS component tree; native `@scope` provides the desired middle ground.
- **WebSockets:** unnecessary because traffic is primarily server-to-client rendering and Datastar supports POST-based SSE.
- **Redis pub/sub immediately:** redundant with PostgreSQL commit notifications and would add an operational state path without a demonstrated need.
- **In-memory data:** rejected because it would hide transaction, event, and multi-process tradeoffs central to the experiment.
- **Element-level server patches:** rejected for database-driven changes because they can conflict with the authoritative top-level page render.
- **Server-side HTML diffing:** rejected until measurements show compressed full renders are inadequate.

## Validation and revisit triggers

Record measurements before changing the architecture:

- render duration and PostgreSQL query count by route/view;
- connected streams and dropped/coalesced invalidations;
- compressed bytes per full render;
- notification reconnects and page convergence;
- input/focus preservation during morphs;
- number and complexity of page-scoped styles/components.

Create a superseding ADR if any of the following occurs: real authentication/PHI scope, more than one deployment region, persistent per-tab state, renders consistently exceeding the 100 ms resolution window, PostgreSQL notification reliability proving insufficient, unsupported-browser requirements, or a frontend build pipeline becoming worthwhile.

## Report maintenance log

- **2026-08-11:** Initial report. Added the second page, PostgreSQL notifications, Redis reservation, HMAC double-submit CSRF, direct HTTP/2 TLS, native scoped CSS, and the Web Component/typechecking example.
- **2026-08-12:** Removed PostgreSQL from the Nix shell. `lib/pq` is pure Go and needs no PostgreSQL headers; PostgreSQL remains a Docker Compose runtime service.
