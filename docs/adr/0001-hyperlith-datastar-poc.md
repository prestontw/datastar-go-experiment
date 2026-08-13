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
- Patient selection and task-status links are progressively enhanced in place. Markup opts in with one `data-patient-navigation` attribute; an embedded, checked, dependency-free event-delegation utility centralizes modifier/button/target checks. Plain primary navigation starts on `mousedown` for lower perceived latency, suppresses the subsequent native click, and retains click as keyboard/synthetic fallback. Modified/non-primary actions remain native. A stable Datastar controller outside `<main>` aborts the prior patient render stream, calls `history.pushState`, clears the current create-patient/task draft signals, and opens the same route with the new query string. The previous view remains visible until the first new full-main morph; `popstate` opens the corresponding stream for back/forward. Real `href` values retain no-JavaScript fallback. The utility is one embedded same-origin asset and adds no package or build infrastructure.
- Do not globally intercept every link. Crossing between `/patients` and `/tasks/due` remains native navigation because it changes page renderer, stylesheet, title, and primary navigation state; making that persistent would require a broader document/head navigation system. Explicit same-page enhancement is less magic and prevents unrelated links, downloads, external URLs, modified clicks, and future controls from being captured accidentally.

### 3. Apply CQRS and functional-core/imperative-shell boundaries

Commands are accepted at `/commands?command=...`. Domain preparation and validation functions consume supplied values/coeffects and return descriptions of desired records. They do not access a database, generate IDs, or send responses. The shell owns random IDs, current dates, persistence, logging, and Datastar effects.

Page handlers query repeatable-read PostgreSQL snapshots, then pass those values to templates. Commands do not patch page elements. They may patch ephemeral signals for validation messages, success messages, and form clearing; committed database changes drive page rendering.

This is deliberately pragmatic FC/IS rather than an effect-description engine. Introduce explicit effect data only if command orchestration becomes difficult to test or reason about.

### 4. Use homogeneous committed-database notifications

Statement-level PostgreSQL triggers call `pg_notify('practice_changed', ...)` after patient/task writes. A dedicated listener converts every notification into the same in-process `DatabaseEvent`. Payload details are ignored.

Each SSE connection has a one-slot dropping buffer and renders no more often than once per 100 ms. A dropped event loses no domain information because the retained event causes a query of the latest committed state. On PostgreSQL listener reconnect, all clients are invalidated once to cover potentially missed notifications.

The server always sends the complete page `<main>`. It does not compute an HTML diff or maintain a server-side prior view. Datastar performs the fine-grained browser DOM morph. Stable IDs on editable controls help the morph retain the existing DOM nodes; live input values, focus, and selection therefore survive unrelated renders. Server markup deliberately does not restate draft input values. Non-value UI attributes that the browser mutates, such as a `<details>` element’s `open`, use `data-preserve-attr`. This behavior is covered by a cross-client Playwright test. If a view intentionally removes or replaces an editor, preserving focus is not expected.

SSE streams prefer Brotli and fall back to gzip, making repeated HTML highly compressible.

**Consequences:**

- All views may recompute after unrelated writes. That cost is accepted for simplicity and measured before adding culling.
- PostgreSQL is the cross-process event source, so multiple app replicas can observe writes without Redis pub/sub.
- `NOTIFY` is an invalidation hint, not a durable queue. Reconnect invalidation is safe because views derive from current database state.
- Slow clients drop frames rather than back-pressuring all clients.

Revisit with metrics if render CPU, database reads, or fan-out become material. Optimize in this order: shared render caching for identical views, smarter event culling, then a different delivery model only if needed.

### 5. Keep durable state in PostgreSQL; reserve Redis

Patients, tasks, and future authenticated session records belong in PostgreSQL 18.4, the latest stable release available in the locked Nixpkgs and official Docker image when adopted. Current browser signals contain only ephemeral form values, flash/errors, CSRF, and a per-tab random identifier.

Redis is deliberately omitted from both Nix and Compose development infrastructure. Add it only when transient per-tab configuration must survive one process, presence must be shared across replicas, or measured notification/fan-out needs justify it. Avoiding speculative Redis state and process overhead prevents split sources of truth; Redis provisioning and per-agent namespacing get their own decision when there is an actual use.

Patient and task keys use PostgreSQL's native `UUID` type and PostgreSQL 18 `uuidv7()`. Columns have database defaults, and the repository exposes UUIDv7 generation as an explicit command coeffect so the pure command core still receives its ID as a value before insert. This favors time-ordered B-tree locality without a Go UUID dependency. Existing UUID versions could coexist at the type level, but this PoC intentionally resets its PostgreSQL 17 data and uses deterministic v7-shaped fixtures as well as v7 runtime IDs.

The server owns PostgreSQL migrations rather than delegating them to Nix or a Compose-only job. The pinned development environments provide PostgreSQL 18; the schema's use of `uuidv7()` naturally fails if run against an incompatible server, so no redundant application-level version assertion is maintained. Numbered SQL files are embedded in the same application binary in local, Nix-infrastructure, and container execution. Startup acquires a transaction-scoped PostgreSQL advisory lock derived from the current database name, verifies an embedded-file checksum ledger, and applies all pending DDL and ledger writes in one transaction before opening the realtime listener or HTTPS server. This keeps schema behavior independent of how PostgreSQL is provisioned, serializes accidental concurrent startup for one logical database, and allows independent agent databases to migrate concurrently. Applied migrations are immutable; a checksum mismatch or a database migration unknown to an older binary is a startup error rather than an implicit downgrade. This simple migrator is adequate while migrations remain transactional PostgreSQL SQL; long-running or non-transactional operational migrations would require a separate deployment procedure and ADR.

Provision PostgreSQL through Docker Compose on macOS and Nix in the Linux VM. `flake.nix` explicitly exports `INFRA_BACKEND=nix`; Nix lifecycle/database scripts require it. The independent macOS wrapper invokes Docker Compose directly. There is no runtime probing, shared backend dispatcher, or Docker-backed Linux development inside the Nix shell. This turns the environment itself into the infrastructure contract and avoids a configuration matrix that is not needed. In the VM, `nix develop` supplies PostgreSQL 18.4 and `just infra-up` starts one persistent, unprivileged user-level process, avoiding Docker inside an already virtualized environment. The Mac and VM intentionally use separate physical data stores.

For concurrent Go-development agents, share the selected PostgreSQL process and allocate one logical database plus one Go HTTPS port per worktree. This isolates schema, data, migration history, and database-local notification streams with substantially lower VM memory/CPU overhead than a PostgreSQL process or Compose project per worktree. Nix infrastructure data/state/runtime paths follow XDG directories, and `flock` serializes process initialization/start/stop across worktrees. `DATABASE_NAME` and `APP_PORT` are per-worktree configuration; the infrastructure backend and host port are shared VM decisions. Database reset requires the exact configured name and never stops the process or removes its data store. Stopping shared infrastructure requires a separate command with an `all-agents` acknowledgement. Browser cookies remain shared across localhost ports; separate automated browser contexts make that acceptable for agents, while simultaneous human worktree tabs would trigger a revisit using unique `*.localhost` origins.

### 6. Follow Hyperlith/HIFI session and CSRF practices

- Generate session IDs from 160 cryptographically random bits, URL-safe and unpadded.
- Store them in host-only `__Host-sid` cookies with `Secure`, `HttpOnly`, `SameSite=Lax`, and `Path=/`.
- Use an HMAC double-submit CSRF token bound to the session ID. Store it in readable `__Host-csrf`; the static shell reads it into an ephemeral Datastar signal submitted on every unsafe request.
- Verify the submitted token’s session-bound HMAC in constant time and require the delivery cookie to be present. A still-valid signed token is accepted if another tab has since rotated the shared cookie, so one tab cannot break another.
- Reject unsafe requests unless Fetch Metadata says `same-origin` or a matching `Origin` is present. Do not enable CORS.
- Use `html/template` contextual escaping and standard security headers.

The CSP permits jsDelivr for the temporarily CDN-hosted Datastar module and includes its SRI hash. Datastar expression evaluation currently compiles declarative attribute expressions with the JavaScript `Function` constructor and therefore needs `'unsafe-eval'`; a nonce or hash cannot authorize runtime string compilation. Datastar does not currently provide a generally available CSP-safe evaluator. Removing this directive would require a distinct CSP-compatible Datastar runtime/precompiled expression scheme or replacing its declarative expression layer with explicit trusted JavaScript while retaining the SSE protocol.

For the PoC, keep Datastar on the pinned CDN URL with subresource integrity because it makes the dependency and update experiment easy to inspect and follows the original no-vendoring constraint. Before a production-oriented deployment, prefer embedding an audited Datastar bundle in the Go binary. Vendoring would make the application independent of CDN availability, keep runtime bytes under the same deployment and review boundary, work offline, eliminate the broad `https://cdn.jsdelivr.net` script source, and allow CSP to narrow to `script-src 'self' 'unsafe-eval'`. It would also make it easier to update the Datastar browser runtime and Go SDK together. It would not remove `'unsafe-eval'`, because the vendored runtime would still compile attribute expressions. Costs include owning update/license-attribution checks, losing shared CDN caching, and slightly increasing repository and binary size.

Colocated inline page `<style>` elements and the Web Component’s generated Shadow DOM `<style>` need `'unsafe-inline'` under `style-src`. We deliberately retain literal `.gohtml` colocation for this PoC: the page structure, selectors, responsive behavior, and scoped boundary can be read and changed as one unit; there is no asset naming/cache-invalidation convention; and the experiment can evaluate whether native `@scope` remains manageable before adding more machinery. This prioritizes development locality over the strongest CSP.

Moving the same vanilla `@scope` rules into adjacent embedded `.css` files remains a straightforward hardening option, not the current plan. It would permit `style-src 'self'` without `'unsafe-inline'`, allow independent browser caching, improve stylesheet-specific tooling, and avoid reparsing unchanged CSS in each HTML document. Its costs are weaker literal colocation, an additional asset request and route, cache/version management, and the possibility that template structure and styles drift across files. Exact CSP hashes or per-response nonces could preserve same-file CSS, but require automated hash regeneration or nonce propagation, including into Shadow DOM styles. These are known CSP concessions, not defaults to copy blindly.

Authentication, authorization, clinical audit logs, retention rules, secrets management, field encryption, backup/restore validation, and compliance controls are out of scope. Synthetic data is mandatory.

### 7. Use native scoped CSS and one Web Component example

Each page’s content and `@scope` styles live in the same `.gohtml` file. This gives locality and boundaries without Tailwind versions, class translation, generated names, or a Node build step. Shared shell styles remain in the shell template.

`<patient-avatar>` is a standards-only custom element. Its DOM and Shadow DOM CSS are colocated in one JavaScript file. It uses no runtime package, is served from the Go binary, and is checked with TypeScript’s `allowJs`/`checkJs` mode. Server-rendered patient data remains in normal HTML; the component is progressive presentation, not client-side application state.

`@scope` and Web Components target modern browsers, consistent with the Playwright/Datastar baseline. Revisit CSS Modules or another strategy if browser support or stylesheet scale requires it.

### 8. Treat drafts and committed records as separate concurrent states

Datastar’s DOM morphing can retain an existing input node, its live value, focus, and selection, but it cannot decide whether a new committed value semantically conflicts with a person’s draft. Hyperlith similarly provides an architectural warning rather than a conflict-resolution primitive: pages can change underneath users, signals are for ephemeral state, and the UI must be designed accordingly.

If task editing is added, use explicit optimistic concurrency rather than last-write-wins:

1. Store a monotonically increasing `revision` plus `updated_at` and `updated_by` on each editable task.
2. When editing begins, retain the draft, the baseline field values, and `baseRevision` as per-tab ephemeral signals. The database remains the source of the current committed value.
3. Continue rendering the latest committed task outside or alongside the editor. A full-main update must not write the committed title into the draft input.
4. If the database revision changes while the local draft is dirty, keep the draft intact and show an explicit conflict state: who changed it, when, the value the draft started from, the current committed value, and the local draft.
5. Save with a compare-and-swap statement such as `UPDATE ... SET ..., revision = revision + 1 WHERE id = $1 AND revision = $expected`. Zero changed rows is a conflict, not success. Re-read the row and patch conflict signals; never silently overwrite.
6. Offer explicit choices appropriate to the field: discard the draft and accept the current value, continue comparing/merging, or deliberately overwrite after authorization and a fresh version check. For long clinical notes, use a three-way merge or a purpose-built collaborative model rather than pretending last-write-wins is collaboration.

Attribution and timestamps make the UI honest but do not prevent lost updates; the conditional database write is the correctness boundary. Presence indicators or advisory locks can reduce collisions but are hints, not substitutes for revision checking, because clients disconnect and locks become stale. Datastar supplies useful mechanisms—persistent signals, reactive conflict banners, `PatchSignals`, and authoritative full-page morphs—but no automatic CRDT, merge algorithm, or domain conflict policy.

This design keeps Hyperlith’s grain: commands remain distinct, the database is authoritative, a commit triggers a fresh render, and drafts remain ephemeral. If server rendering itself must reason about current draft state, promote only the necessary per-tab editing metadata to the existing per-tab state seam (potentially Redis when replicated), not into shared task state.

For the PoC, intentional same-page patient navigation clears incomplete create-patient and create-task signals so a task draft cannot silently move to another patient. Future work should persist drafts per user/tab and form subject: a new-patient draft belongs to its author, while a new-task draft should be keyed by patient and author. Restore drafts when returning, show their saved age/ownership, remove them after successful submission, and define retention/cleanup. PostgreSQL is the default durable home if drafts must survive restarts; Redis is justified only if measured transient-state requirements make it preferable.

### 9. Terminate trusted TLS directly in the Go process

Local and container execution both call `http.Server.ListenAndServeTLS` with HTTP/1.1 and HTTP/2 explicitly enabled. There is no development-only plaintext path or Docker reverse proxy. Both development workflows use the same host-generated `mkcert` files in `.certs/`: the local Go process reads them directly and Compose bind-mounts them read-only into the unprivileged application container. `scripts/certs.sh` is the common idempotent generation/trust step. Nix supplies `mkcert` on Linux; a non-Nix Mac installs it independently. SSE has no server write timeout; shutdown cancellation closes streams.

**Consequences:** local and container behavior share the issuing CA, leaf certificate, trust procedure, server implementation, and HTTP/2 semantics, reducing transport-specific differences and SSE surprises. Docker Desktop users need no Nix, Go, pnpm, or `just`, but do need host `mkcert`, because trust belongs to the browser’s host OS and should not be delegated to a container. The Docker wrapper supplies the host UID/GID so the scratch container can read the owner-only key without weakening its permissions. A production deployment must use a real certificate or may terminate TLS at a proxy that preserves streaming, avoids response buffering, and negotiates HTTP/2 with clients.

### 10. Pin tools and test at multiple levels

- `go.mod` declares Go 1.26 and toolchain 1.26.5.
- `flake.lock` pins Nixpkgs; the shell supplies Go 1.26.5, PostgreSQL 18.4, `flock`, pnpm, Node, mkcert, and Playwright browsers. It does not include Docker tooling.
- `pnpm-lock.yaml` pins test-only Node packages.
- Docker images pin Go and PostgreSQL 18.4 patch versions.
- Fast Go tests cover pure commands, security tokens, event dropping, templates, and HTTP command boundaries through a fake repository.
- Tagged PostgreSQL integration tests create a unique empty database per test, invoke the production embedded migrator, exercise the real store, and drop the fixture. They are explicit (`just test-integration`) rather than hidden inside fast unit tests.
- Each Playwright run gets a unique empty database. The real server startup applies production migrations before its health check, then tests verify HTTP/2, Web Component upgrade, patient creation, CSRF rejection, query-param routing, and one change appearing on two different pages. The fixture is dropped afterward.

`just` is the discoverable command interface for Nix development. `scripts/infra.sh`, `scripts/database.sh`, and test helpers are Nix-only. `scripts/docker-up.sh` forms the separate, Nix-independent macOS entry point and handles Compose/database provisioning directly. No Docker client or daemon is part of the Linux VM contract.

Keep the small orchestration layer as portable POSIX `sh`. Rewriting it in Node would make Node a macOS host prerequisite for an otherwise Docker-plus-mkcert workflow; Deno would add a new tool to both environments. The concurrency-sensitive Nix lifecycle also delegates directly to `flock` and PostgreSQL utilities, so JavaScript would mostly wrap the same subprocesses rather than remove complexity. Revisit if orchestration acquires enough parsing/state logic to benefit from a typed program. The reversed `.gitignore` intentionally permits only `scripts/*.sh` today. A future JavaScript or TypeScript transition must add explicit `scripts/*.js` or `scripts/*.ts` allow rules (and directory traversal rules if scripts become nested).

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
- **2026-08-12:** Clarified morph-time focus retention and added stable editor IDs, preserved `<details open>`, and a cross-client focus/value/selection test. Expanded the CSP tradeoff and hardening paths.
- **2026-08-12:** Chose to retain literal `.gohtml` CSS colocation for the PoC, documented adjacent external CSS and vendored-Datastar hardening tradeoffs, and established the intended optimistic-concurrency/conflict-UI policy for future task editing.
- **2026-08-12:** Made full-stack Docker execution independent of Nix while requiring host `mkcert`. Local Go and Docker runs now reuse the same owner-only certificate files and host trust, favoring parity over an additional container-only development CA.
- **2026-08-12:** Replaced rerunning one idempotent schema file with an embedded, checksummed migration ledger protected by a PostgreSQL advisory lock and transaction. Local and Docker server startup now share one explicit migration lifecycle.
- **2026-08-12:** Added Zero-to-Production-style database test isolation: tagged backend tests migrate a new database per test, and Playwright runs against a fresh database migrated by real server startup.
- **2026-08-12:** Selected one shared Compose PostgreSQL with a logical database and Go HTTPS port per agent worktree. Scoped reset to an explicit database name, guarded shared shutdown, parameterized dev/E2E, and made migration locks database-specific.
- **2026-08-12:** Reintroduced PostgreSQL as a Nix runtime tool—not a `lib/pq` build dependency—so the Linux VM needs no Docker daemon. Added one shared user-level Nix cluster for all worktrees and removed unused Redis provisioning.
- **2026-08-12:** Simplified infrastructure selection: `flake.nix` fixes Nix development to Nix PostgreSQL; scripts outside Nix default to Docker for macOS. Removed Docker tooling and the unsupported Docker-on-Linux/Nix configuration path.
- **2026-08-12:** Added progressive same-page patient/status navigation with URL history and stream replacement, while retaining native cross-page navigation. Current drafts clear on navigation; per-author/per-patient persisted drafts are recorded as future work.
- **2026-08-12:** Moved patient-link eligibility logic into one checked event-delegation utility and begin plain pointer navigation on `mousedown`; click remains the keyboard/synthetic fallback.
- **2026-08-12:** Upgraded Nix and Docker to PostgreSQL 18.4, intentionally discarded the PostgreSQL 17 development data, and moved patient/task generation plus deterministic fixtures from UUIDv4 to native PostgreSQL UUIDv7.
- **2026-08-12:** Debounced interactive patient search by 300 ms, replacing the canonical query-string URL and one owned page stream only after typing pauses while retaining immediate form submission and native GET fallback.
