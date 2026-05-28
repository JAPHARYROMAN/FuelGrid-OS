# Audit 13 — Cross-Cutting Infrastructure & Shared Helpers

**Scope:** the transactional outbox + event plumbing, audit-log writes, Redis cache helper,
Postgres pool + tenant helper, observability (metrics/tracing/Sentry), config & logging, the
shared HTTP middleware stack / helpers / lifecycle, and the three `cmd` entry points
(`api`, `migrate`, `seed`). This is a **read-only** audit; the only artifact written is this file.

Method: every file below was read in full. Findings cite `file:line`, name the function, and
quote code. Uncertainty is marked explicitly.

### Files covered (LOC)

| File | LOC |
|---|---|
| `internal/events/event.go` | 40 |
| `internal/events/outbox.go` | 63 |
| `internal/events/bus.go` | 71 |
| `internal/events/publisher.go` | 199 |
| `internal/audit/audit.go` | 92 |
| `internal/audit/tx.go` | 85 |
| `internal/cache/redis.go` | 38 |
| `internal/database/postgres.go` | 76 |
| `internal/database/tenant.go` | 58 |
| `internal/database/util.go` | 19 |
| `internal/observability/metrics.go` | 113 |
| `internal/observability/sentry.go` | 46 |
| `internal/observability/tracing.go` | 83 |
| `services/api/internal/config/config.go` | 79 |
| `services/api/internal/logging/logging.go` | 43 |
| `services/api/internal/server/server.go` | 865 (middleware/lifecycle subset) |
| `services/api/internal/server/middleware.go` | 112 |
| `services/api/internal/server/handlers.go` | 76 |
| `services/api/internal/server/close_handlers.go` | 72 |
| `services/api/internal/server/metrics_handler.go` | 21 |
| `services/api/internal/server/auth_middleware.go` (shared helper `extractBearer`) | 74 |
| `services/api/internal/server/platform_handlers.go` (middleware `requirePlatformAdmin`) | 212 |
| `services/api/internal/server/audit_handlers.go` (`handleListAuditLogs`) | 154 |
| `services/api/internal/server/accounting_handlers.go` (`txAudit` helper @ L102) | — |
| `services/api/cmd/api/main.go` | 277 |
| `services/api/cmd/migrate/main.go` | 150 |
| `services/api/cmd/seed/main.go` | 431 |

---

## 1. Transactional Outbox & Event Publisher (`internal/events/**`)

### 1.1 The write side (`outbox.go`, `audit/tx.go`)

`WriteOutbox` (`outbox.go:24`) inserts a row into `outbox_events` **using the caller's open
`pgx.Tx`** — this is the textbook outbox pattern and is correct: the event row commits or rolls
back atomically with the business change. Defaults are sensibly filled (ID, Version=1,
OccurredAt, Payload→`null`). Required fields (`Type`, `AggregateType`, `AggregateID`) are
validated up front.

`audit.WriteWithOutbox` (`tx.go:35`) writes both the `audit_logs` row and the `outbox_events`
row from one `TxRecord` on the same `tx`. The tx boundary is genuinely shared — confirmed.

**However, the event payload is hard-wired to the audit "new value":**
```go
// tx.go:66
return events.WriteOutbox(ctx, tx, events.Event{
    ...
    Payload:       next,          // == marshalled r.NewValue
    CorrelationID: r.RequestID,
})
```
There is no domain-specific event payload — every event emitted via the helper carries the audit
diff. For sensitive entities (users, credentials, bank accounts) this means whatever a handler
stuffs into `NewValue` becomes the durable, fan-out event body (see **INFRA-09**, PII).

### 1.2 The drain loop (`publisher.go`)

`processOnce` (`publisher.go:114`) is structured correctly for at-least-once delivery:

- One tx wraps `SELECT ... FOR UPDATE SKIP LOCKED` (L122–131) and the
  `UPDATE ... SET published_at = now()` (L185), so multiple publisher instances partition work
  safely. Good.
- Dispatch happens **after** the rows are read, and only successfully-dispatched IDs are marked
  published (L168–191). A failed `bus.Publish` leaves the row unpublished for the next tick. This
  is correct at-least-once semantics *relative to the bus*.

**Delivery-guarantee gaps:**

1. **The "bus" is synchronous, in-process, and the only subscriber is a log line.** The single
   `Subscribe("*")` call lives in `cmd/api/main.go:251` and just logs the event; there are **no
   real consumers anywhere in the repo** (grep for `Subscribe(` returns only the bus definition
   and that one call). So "at-least-once delivery" currently means "at-least-once logged." The
   whole retry/SKIP-LOCKED apparatus protects delivery to a no-op. Consumer idempotency cannot be
   evaluated because no consumer exists (**INFRA-02**).

2. **`InProcessBus.Publish` never returns an error** (`bus.go:55–71`): handler errors are logged
   and swallowed, and `Publish` returns `nil` unconditionally (L70). Therefore in `processOnce`
   the `if err := p.bus.Publish(...)` branch (L170) is **dead** — every event is always marked
   `dispatched` even if its handler failed. With the current bus the retry path can never fire; a
   handler that genuinely fails will have its outbox row marked published and **lost** (the
   "durable record" claim in the comment is only true if a handler never reports failure)
   (**INFRA-03**).

3. **No `failed_at` / attempt count / dead-letter.** `outbox_events` only has `published_at IS
   NULL`. A poison event that some future real consumer keeps failing on would be retried every
   tick forever with no backoff, no cap, no quarantine, and (because of point 2) no signal. There
   is no retry/backoff at the outbox layer at all (**INFRA-04**).

4. **Ordering is best-effort only.** The query orders by `occurred_at` (L128) but
   `SKIP LOCKED` + multiple publishers + per-event dispatch means events for the same aggregate
   can be delivered out of order across batches/instances. `occurred_at` is filled with
   `time.Now()` at write time (`outbox.go:42`) at second/microsecond resolution with no
   per-aggregate sequence, so ties and clock skew can reorder. Acceptable for a log sink; a real
   consumer needing per-aggregate order would need a sequence column (**INFRA-10**, Low).

5. **`context.Background()` ignores shutdown.** `run()` calls
   `p.processOnce(context.Background())` (L97, L106), so an in-flight drain batch is **not**
   cancellable by `Stop(ctx)`. `Stop` (L78) closes `stopCh` and waits on `doneCh`, but a
   long-running `processOnce` holding `FOR UPDATE` locks will block shutdown until it finishes or
   the 5s `publisher.Stop` timeout in `main.go:268` elapses — after which the goroutine keeps
   running against `context.Background()` with no deadline, holding row locks and a pool conn
   while the pool is being closed underneath it (**INFRA-05**).

6. **Empty-batch commit churn / immediate first tick.** Minor: every empty poll opens a tx and
   commits it (L158–163) to avoid idle-tx counters — fine. The immediate first tick (L97) before
   the ticker is a dev nicety, harmless.

---

## 2. Audit Log (`internal/audit/**`)

`Write` (`audit.go:48`) inserts `audit_logs` on the caller's tx. Strengths:

- Malformed IPs are stored as `NULL` rather than aborting the business tx — `net.ParseIP`
  gates `ipArg` (L60–63). Sensible defensive choice and well-commented.
- `nullString`/`nullJSON` normalize empties to SQL NULL.

**Weaknesses:**

- **No tamper-resistance.** `audit_logs` is described as "append-only" (package doc, `audit.go:1`)
  but nothing enforces it: the app connects as the table owner (see §4 / **INFRA-01**), so UPDATE
  and DELETE on `audit_logs` are fully permitted. There is no hash chain, no per-row signature, no
  `REVOKE UPDATE/DELETE`, no trigger blocking mutation. "Append-only" is a convention, not a
  guarantee (**INFRA-08**).
- **PII in plaintext.** `previous_value`/`new_value` are opaque JSONB the caller controls; auth and
  fleet handlers put user emails, names, phone numbers, credential identifiers, bank-account
  details into these. There is no redaction layer. Combined with the lack of write protection and
  the fact that the same blob is copied into `outbox_events.payload`, PII has a wide, mutable blast
  radius (**INFRA-09**).
- **No verification that an audited write is ever accompanied by an audit row.** `WriteWithOutbox`
  is opt-in per handler; nothing structurally forces a state-changing handler to call it. (Out of
  strict scope to enumerate which domain handlers skip it, but the helper cannot self-enforce.)

The `txAudit` server helper (`accounting_handlers.go:102`) is the canonical wrapper:
```go
tx := s.deps.DB.Begin(ctx)
defer tx.Rollback(ctx)
entityID, err := fn(tx)
if err != nil { return false }            // fn already wrote the HTTP response
...
audit.WriteWithOutbox(ctx, tx, rec)       // 500 on failure
tx.Commit(ctx)                            // 500 on failure
```
**The earlier-conversation hint that `txAudit` double-writes errors is NOT confirmed.** The paths
are mutually exclusive: a business error in `fn` writes the response inside `fn` and returns
`false` *before* `txAudit` writes anything; an infra failure (audit insert or commit) happens only
when `fn` succeeded and wrote nothing, so `txAudit`'s single `writeError(500)` is the only write.
Callers correctly do `if !ok { return }` then `writeJSON(success)` (e.g.
`handleCreateAccount` L229–232). The helper is correct on commit/rollback and status codes.

Two real issues in `txAudit` itself:

- **It binds the tx to `r.Context()`** (L103). When `chimiddleware.Timeout(30s)` (§3) cancels the
  request context, `tx.Commit(ctx)` fails and the whole business action rolls back even if the
  work itself was instant — a slow audit/outbox insert near the 30s boundary silently discards a
  committed-in-spirit business change and returns 500. Low-probability but a correctness cliff
  (**INFRA-11**, Low).
- **Default `NewValue`** (`L117–119`) sets `{"id": entityID}` when the handler left it nil, so the
  emitted event payload for many actions is just the id — thin but harmless.

---

## 3. Shared HTTP Middleware, Helpers & Lifecycle (`server.go`, `middleware.go`, `handlers.go`)

### 3.1 Middleware stack (`server.go:149–162`)

Order (outermost → innermost): `RequestID` → `echoRequestID` → `logRequests` → `recordMetrics`
→ `Recoverer` → `Timeout(30s)` → `cors`.

- **Panic recovery is present** (`chimiddleware.Recoverer`, L153) and sits *inside*
  `logRequests`/`recordMetrics`, so a recovered panic is correctly observed as a `5xx` by both. ✔
- **CORS is the innermost middleware** (L155), *inside* `Recoverer` and `Timeout`. Functionally OK
  for same-handler requests, but it means a panic in CORS handling (unlikely) is the only thing
  Recoverer guards there; more importantly the ordering is unusual — CORS preflight responses still
  pass through `Timeout` and auth-less paths, which is fine here but worth noting.
- **No request body size limit on the global stack.** There is no
  `http.MaxBytesReader`/`http.MaxBytesHandler` in the middleware chain. Body limits exist only in
  two domain handlers (`calibration_handlers.go:202` `MaxBytesReader`, `banking_handlers.go:712`
  `io.LimitReader(r.Body, 1<<20)`). Every JSON `POST/PATCH` goes through `decodeJSON`/
  `json.NewDecoder(r.Body)` with **no cap**, so a malicious client can stream an arbitrarily large
  body and force unbounded allocation. `ReadTimeout: 30s` (L845) bounds *time* not *size*
  (**INFRA-06**, High).
- **No global rate limiting / concurrency cap** on the shared stack (login has its own limiter in
  the identity service; everything else is uncapped). Out-of-scope to fully assess but relevant to
  pool exhaustion (§4).

### 3.2 `logRequests` (`middleware.go:36`)

Emits one structured line per request with `request_id`, `tenant_id`, `user_id`, `method`, `path`,
`status`, `bytes`, `latency_ms`, `remote`, `user_agent`.

- **`path` is the raw `r.URL.Path`** (L52), not the route template. This logs full real paths
  including any IDs/slugs — fine for logs, but `correlation_id` is just `request_id` duplicated
  (L47), so the "correlation" field carries no extra signal yet (acknowledged in the comment).
- **No obvious secret leakage**: it does not log the Authorization header, body, or query string.
  `user_agent` and `remote` are logged — acceptable. ✔
- `routePattern` is read inside the `defer` (after `next` returns), so the chi route context is
  populated — correct (L49 via `routePattern`).

### 3.3 `recordMetrics` (`middleware.go:73`) — see also §5

Uses the chi route **template** for the `path` label and buckets status to `Nxx` — good cardinality
hygiene. Correctly computes `routePattern` *after* `next.ServeHTTP` (L85→87). It increments/
decrements `HTTPInflight` directly on the gauge (L80–81), which is correct.

### 3.4 Shared response helpers (`handlers.go`, `auth_handlers.go`)

- `writeJSON` (`handlers.go:55`) sets `Content-Type`, writes status, encodes — and **ignores the
  encode error** (`_ = json.NewEncoder...`). After `WriteHeader` the status is already committed, so
  a mid-stream encode failure yields a truncated body with a 200 — only loggable, but it isn't
  logged (**INFRA-13**, Low).
- `writeError` (`auth_handlers.go:236`) returns a uniform `{"error","status"}` body. **No internal
  detail leakage** — handlers log the real error via `s.logger.Error(...)` and return a generic
  "internal error" to the client. This is consistently applied across the files read. ✔ Good.
- `decodeJSON` (`handlers.go:63`) sets `DisallowUnknownFields()` and collapses all parse errors to a
  single opaque `errInvalidJSON` — no leakage, but also no body-size cap (see INFRA-06). Note: many
  handlers bypass this helper and call `json.NewDecoder(r.Body).Decode` directly
  (`accounting_handlers.go:202`), so `DisallowUnknownFields` is inconsistently applied
  (**INFRA-14**, Info).

### 3.5 Pagination

There is **no shared pagination helper.** Only `handleListAuditLogs` (`audit_handlers.go:47`)
implements a cap (`limit` 1..200, default 50) and it is hand-rolled with manual `$n` arg threading
(L73–76) — correct and injection-safe, but bespoke. The dozens of other list endpoints
(`handleListAccounts` L134, `handleListExpenses`, `handleListSuppliers`, risk/fleet/banking lists,
etc.) return **all rows** as `{"items":[...],"count":N}` with no `LIMIT`/`OFFSET`. A tenant with
large history can force the API to load an entire table into memory and serialize it in one
response — an availability and memory-exhaustion risk that grows with data
(**INFRA-07**, High).

### 3.6 Lifecycle (`server.go:840–865`)

`http.Server` sets `ReadHeaderTimeout: 10s`, `ReadTimeout: 30s`, `WriteTimeout: 30s`,
`IdleTimeout: 120s` — solid Slowloris protection. ✔ `Start` correctly treats `ErrServerClosed` as
clean (L855). `Shutdown` delegates to `http.Server.Shutdown` which drains in-flight requests
(L862–865). Graceful-shutdown wiring in `main.go` is analyzed in §6.

---

## 4. Database Pool & Tenant Helper (`internal/database/**`)

`Connect` (`postgres.go:40`) parses the URL, applies `MaxConns/MinConns/MaxConnLifetime/
MaxConnIdleTime` from config, and pings with a 5s timeout before returning. The `Querier`
interface (L32) cleanly lets repos run either auto-committed or inside a tx — good design.

**Critical / High issues:**

- **No statement / lock / idle-in-transaction timeout anywhere.** Grep for `statement_timeout`,
  `lock_timeout`, `idle_in_transaction_session_timeout` across the repo returns **zero** hits, and
  the CI `DATABASE_URL` (`.github/workflows/ci.yml:123`) sets none. A single slow query or a tx that
  hangs (e.g. the publisher running on `context.Background()`, §1.5) can pin a connection
  indefinitely. With `MaxOpenConns: 25` (config default, `config.go:30`) and no per-statement
  timeout, a handful of stuck queries exhausts the pool and the whole API stalls. Add
  `statement_timeout`/`lock_timeout`/`idle_in_transaction_session_timeout` via the connection
  string or an `AfterConnect` hook (**INFRA-12**, High).

- **RLS is defined but inert at runtime — confirmed.** This corroborates and extends the prior
  audit. `database.WithTenant` (`tenant.go:29`) is the **only** code that issues
  `SET LOCAL app.current_tenant`, and grep confirms it is **never called in the request path** —
  every domain repo takes the raw `*Pool`/`tx` and filters by `tenant_id` in Go-built SQL.
  Migration `0005_rls.up.sql:12–16` states RLS is *ENABLED but NOT FORCED*, the app connects as the
  **table owner** (the CI DSN connects as `fuelgrid` superuser, not the `fuelgrid_app` role created
  at `0005:29`), and owners bypass RLS. Net effect: **RLS provides the running API zero protection**;
  tenant isolation rests entirely on hand-written `WHERE tenant_id = $1` clauses with no DB backstop.
  Run the API as `fuelgrid_app`, call `WithTenant` per request (or set the GUC in a tx-scoped
  middleware), and `FORCE ROW LEVEL SECURITY` (**INFRA-01**, Critical).

- `WithTenant` itself (when used) is correct: zero-tenant guard (L31), `SET LOCAL` so the GUC
  cannot leak back to the pooled conn (L48), rollback-on-error/panic via deferred `Rollback`
  (L38–43), commit on success (L57). The UUID interpolation is safe (fixed-format `uuid.String()`).
  The helper is sound — it's simply orphaned.

- `util.UUIDStrings` (`util.go`) is a tidy, correct helper; no issues.

---

## 5. Observability (`metrics.go`, `tracing.go`, `sentry.go`)

### Metrics

- Label sets are `{method, path, status}` with `path`=route template and `status`=`Nxx` bucket —
  **cardinality is bounded correctly** (`middleware.go:88–92`). Latency histogram uses
  `ExponentialBuckets(0.005, 2, 12)` (~5ms..10s) — reasonable. ✔
- `OutboxBacklog`/`OutboxLag` gauges are populated by `ObserveOutbox` (`metrics.go:82`) on a 15s
  ticker from `main.go:224`. The query is tenant-agnostic (`WHERE published_at IS NULL`) — fine.
- **Dead/misleading code:** `Metrics.Inflight()` (`metrics.go:108`) returns a **brand-new**
  `&inflight{}` on *every call* (L112), so its counter is always reset and never wired to the
  `HTTPInflight` gauge. Grep confirms it is never called — the middleware uses
  `HTTPInflight.Inc()/.Dec()` directly. The `inflight` type (L99–113) is unused. Harmless today but
  a trap: anyone "fixing inflight" via this method would get a no-op (**INFRA-15**, Low).
- **`/metrics` is unauthenticated** (`metrics_handler.go:15`), by design ("gate via network policy").
  Acceptable but must be enforced at ingress in prod; the code itself does nothing (**INFRA-16**, Info).

### Tracing (`tracing.go`)

- Default exporter is `none` (config default `OTEL_EXPORTER=none`) → no-op tracer provider (L52–61);
  `stdout` writes pretty JSON spans to stderr (L62–66); `otlp` is **not implemented** — it falls
  into `default:` and returns an error (L67–68), so setting `OTEL_EXPORTER=otlp` in prod is a hard
  boot failure unless caught. In `main.go` tracing-init failure is *non-fatal* (logged + no-op,
  L81–84), so a typo'd exporter silently disables tracing. Propagators are set even in `none` mode —
  good for context propagation (**INFRA-17**, Low).

### Sentry (`sentry.go`)

- Blank DSN disables cleanly (L26–29). DSN-on path sets Environment/Release/TracesSampleRate.
- **No `BeforeSend`/PII scrubbing.** The comment (L36–37) admits breadcrumbs aren't scrubbed "yet."
  Once errors carry request data, emails/tokens could ship to Sentry unredacted. Wire a `BeforeSend`
  scrubber before enabling in prod (**INFRA-18**, Medium).
- Flush timeout is a fixed 2s (L44) regardless of `ShutdownTimeout` config — minor.

---

## 6. Config, Logging & Entry Points

### Config (`config.go`)

- Env surface is comprehensive and defaults are dev-tuned. `DATABASE_URL`/`REDIS_URL` optional
  (thin smoke-test mode).
- **No validation.** `Load` (L68) just calls `envconfig.Process` and returns. There is no check that
  in non-dev `AUTH_PASSWORD_PEPPER`, `DATABASE_URL`, `PLATFORM_ADMIN_TOKEN`, etc. are set — the
  *only* guard is a `Warn` (not a hard fail) in `main.go:181` for an empty pepper outside
  development. A prod deploy missing `DATABASE_URL` boots happily into a degraded
  no-database API (`main.go:160` "postgres skipped"), serving 401/404s rather than failing loudly
  (**INFRA-19**, Medium).
- **Secrets are plain strings** read straight from env (`AuthPasswordPepper`, `PlatformAdminToken`,
  `SentryDSN`). No secret-store integration, no `String()` redaction wrapper — if `Config` is ever
  logged/dumped these leak. (`main.go:55` logs only env/addr, so no leak *today*.) (**INFRA-20**, Low.)
- `NODE_ENV` is the env var name for a Go service (`config.go:18`) — cosmetic, but a footgun for
  ops expecting `APP_ENV`.

### Logging (`logging.go`)

- Clean slog wrapper; JSON default, level/format configurable, unknown→info/JSON. No PII/secret
  handling of its own (it's a transport). The leakage risk lives at call sites, and the files read
  do not log secrets. ✔

### `cmd/api/main.go`

- Graceful shutdown is mostly correct: SIGINT/SIGTERM → `srv.Shutdown(ctx)` with `ShutdownTimeout`
  (L114–118), then waits for `Start`'s error (L119). Defers unwind: tracing-shutdown, then
  `cleanup()` (stops publisher + closes pool + closes redis in reverse), then `cancel()`. Order is
  acceptable.
- **Boot-time dependency failures are fatal (good)** for DB/Redis *when their URL is set*
  (L154, L167) — but *absence* of the URL is a silent warn (see INFRA-19).
- **`errCh` is buffered(1)** (L101) so the `Start` goroutine never leaks if shutdown wins the race. ✔
- The outbox metrics observer goroutine (L224) and publisher (L266) are started in `wireDeps` and
  torn down via `cleanups`. The observer's `obsCtx` is cancelled by cleanup (L222); the publisher's
  `Stop` has a 5s deadline (L268) but, as noted in §1.5, the loop runs on `context.Background()` so
  it cannot actually honor that deadline mid-batch (**INFRA-05**).
- Sentry/tracing init failures are non-fatal (L69–72, L81–84) — reasonable for telemetry.

### `cmd/migrate/main.go`

- Solid: clear subcommands, `ErrNoChange`→success (L121), `version` handles `ErrNilVersion`.
- **`down-all` and `force` are unguarded.** `down-all` (L84) drops every migration with no
  confirmation/flag, and `force <n>` (L95) blindly stamps a version — both are documented as
  "dangerous"/"recovery" but a fat-fingered CI invocation wipes the schema. Consider an explicit
  `--yes`/`CONFIRM=` gate, especially since the same binary runs against prod (**INFRA-21**, Medium).
- `resolveMigrationsPath` (L130) silently falls back to `./migrations` — if the expected dir is
  missing it could apply the *wrong* migration set; it does error if none found (L149). Low.

### `cmd/seed/main.go`

- **Idempotency is shallow.** The only guard is "does a tenant with this slug exist?" (L99–106);
  if it does, the whole seed is skipped. If a prior run committed *partially* (it shouldn't — it's
  one tx, L92/L392 — so this is mostly safe) the slug check is adequate. The single-tx wrap is good. ✔
- **Hard-coded default passwords** for demo + admin users (`defaultUserPassword`,
  `defaultAdminPassword`, L41/L46) and a default `fuelgrid_app` role password in
  `0005_rls.up.sql:29`. The seed creates a **`system_admin`** account (`admin@fuelgrid.local`,
  L276–288) with a known password. If `seed` is ever run against a shared/staging/prod database
  (it only requires `DATABASE_URL`, with no env guard refusing non-development), it provisions a
  **full-admin backdoor with publicly-known credentials**. The `//nolint:gosec` comments
  acknowledge the smell but nothing *prevents* prod execution. Gate the seed on
  `NODE_ENV=development` or a `SEED_ALLOW_NONDEV` flag (**INFRA-22**, High).
- The "this should 403" second station (no `user_station_access`) is a clean CI probe — good.

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| INFRA-01 | Critical | `migrations/0005_rls.up.sql:12`; `database/tenant.go:29`; `cmd/api/main.go:146` | RLS is ENABLED-not-FORCED, API connects as table owner, and `WithTenant` (the only `SET LOCAL app.current_tenant`) is never called in the request path → RLS gives the running API **zero** tenant isolation; only hand-written WHERE clauses protect tenants. | Connect API as `fuelgrid_app`, set `app.current_tenant` per request tx (middleware or `WithTenant`), and `FORCE ROW LEVEL SECURITY` on all tenant tables. |
| INFRA-02 | High | `cmd/api/main.go:251`; `events/bus.go:55` | The outbox has **no real consumers** — the sole subscriber logs events. Delivery guarantees, idempotency, and retry all protect a no-op; events are effectively a write-only log table. | Wire actual consumers (or document the outbox as audit-only). Define consumer idempotency keys before adding any. |
| INFRA-03 | High | `events/bus.go:70`; `events/publisher.go:170` | `InProcessBus.Publish` always returns `nil`, so the publisher's failure branch is dead — every event is marked `published_at` even when its handler errored, so a failing handler **silently loses** the event despite the "durable record" claim. | Make `Publish` return an aggregated handler error (or per-event status) so `processOnce` can leave failed rows unpublished. |
| INFRA-06 | High | `server.go:149-162`; `handlers.go:63` | No global request-body size limit; `decodeJSON`/`json.NewDecoder(r.Body)` read unbounded bodies. A large/streamed body forces unbounded allocation. | Add `http.MaxBytesReader` (or a `MaxBytesHandler`) in the shared middleware stack with a sane default (e.g. 1 MiB). |
| INFRA-07 | High | `accounting_handlers.go:134`; (most list handlers) | No shared pagination; list endpoints (except audit-logs) return **all** tenant rows with no LIMIT/OFFSET → memory/availability risk that scales with data. | Add a shared `parsePage(r)` helper enforcing a max limit + offset/cursor; thread `LIMIT/OFFSET` into every list query. |
| INFRA-12 | High | `database/postgres.go:40`; `config.go:29` | No `statement_timeout`/`lock_timeout`/`idle_in_transaction_session_timeout` anywhere; with `MaxOpenConns:25`, a few stuck queries/txs exhaust the pool and stall the API. | Set timeouts via DSN `options=-c statement_timeout=...` or an `AfterConnect` hook in `pgxpool.Config`. |
| INFRA-22 | High | `cmd/seed/main.go:276`; `:41`/`:46` | Seed provisions a `system_admin` user with a hard-coded, public default password and has **no guard** against running outside development → full-admin backdoor if run on shared/prod DB. | Refuse to run unless `NODE_ENV=development` or an explicit `SEED_ALLOW_NONDEV=1`; require passwords be supplied for any admin account. |
| INFRA-04 | Medium | `events/publisher.go`; `migrations` (no column) | No `failed_at`/attempt-count/dead-letter on `outbox_events`; a poison event would retry forever with no backoff or quarantine (once a real consumer that can fail exists). | Add `attempt_count`, `last_error`, `next_attempt_at`; implement exponential backoff and a dead-letter threshold. |
| INFRA-08 | Medium | `audit/audit.go:1` | `audit_logs` is "append-only" by convention only; app owns the table so UPDATE/DELETE are allowed, no hash chain/signature/trigger. | `REVOKE UPDATE,DELETE` from the app role + add an immutability trigger or a hash chain for tamper-evidence. |
| INFRA-09 | Medium | `audit/tx.go:66`; `audit/audit.go:71` | PII (emails, names, phones, credential/bank data) stored unredacted in `audit_logs` *and* copied verbatim into `outbox_events.payload`; mutable and fanned out. | Define per-action event payloads (not the audit diff); add a redaction/allow-list layer for sensitive entities. |
| INFRA-18 | Medium | `observability/sentry.go:31` | No `BeforeSend`/breadcrumb scrubbing; once errors carry request context, secrets/PII can ship to Sentry. | Add a `BeforeSend` that strips Authorization headers, tokens, emails, and request bodies. |
| INFRA-19 | Medium | `config.go:68`; `cmd/api/main.go:160` | No config validation; missing `DATABASE_URL`/pepper in prod only warns and boots a degraded API instead of failing fast. | Add a `Config.Validate()` that hard-fails in non-dev when required secrets/URLs are unset. |
| INFRA-21 | Medium | `cmd/migrate/main.go:84`/`:95` | `down-all` and `force` are unguarded destructive ops in the same binary used against prod. | Require an explicit confirmation flag/env for `down-all` and `force`. |
| INFRA-05 | Medium | `events/publisher.go:97`/`:106` | Publisher drains on `context.Background()`, so `Stop(ctx)`'s deadline (5s in `main.go:268`) can't cancel an in-flight batch; loop keeps running against a closing pool, holding row locks. | Thread a cancellable context from `Start`/`Stop` into `processOnce`. |
| INFRA-10 | Low | `events/publisher.go:128`; `outbox.go:42` | Ordering is best-effort only (no per-aggregate sequence; `SKIP LOCKED` + multi-instance reorders). | Add a monotonic per-aggregate sequence if any consumer requires ordering. |
| INFRA-11 | Low | `accounting_handlers.go:103` | `txAudit` binds the tx to `r.Context()`; a request hitting the 30s `Timeout` near commit rolls back an otherwise-complete business action and returns 500. | Use a fresh `context.WithTimeout(context.Background(), ...)` for the commit, or a dedicated DB-op deadline. |
| INFRA-13 | Low | `handlers.go:58` | `writeJSON` ignores `json.Encode` errors after `WriteHeader` → silent truncated 200s, not even logged. | Log encode failures (status already sent, but the operator needs the signal). |
| INFRA-15 | Low | `observability/metrics.go:108` | `Metrics.Inflight()` returns a fresh, never-wired counter each call (dead code); a "fix" via it would be a no-op. | Delete `Inflight()` + the `inflight` type; the gauge is used directly. |
| INFRA-17 | Low | `observability/tracing.go:67`; `cmd/api/main.go:81` | `otlp` exporter unimplemented (errors); tracing-init failure is non-fatal, so a typo silently disables tracing. | Implement OTLP or remove it from the documented enum; consider failing fast on an explicitly-requested-but-broken exporter. |
| INFRA-20 | Low | `config.go:40`/`:53` | Secrets are plain strings with no redaction wrapper; safe today but leak if `Config` is ever logged. | Wrap secrets in a type with a redacting `String()`/`LogValue()`. |
| INFRA-14 | Info | `accounting_handlers.go:202` | Many handlers bypass `decodeJSON` and decode directly, so `DisallowUnknownFields` is inconsistently applied. | Standardize on `decodeJSON` everywhere. |
| INFRA-16 | Info | `metrics_handler.go:15` | `/metrics` is unauthenticated by design (network-policy gated); code enforces nothing. | Ensure ingress/network policy actually restricts it in prod. |

### Severity counts

- **Critical:** 1 (INFRA-01)
- **High:** 6 (INFRA-02, 03, 06, 07, 12, 22)
- **Medium:** 6 (INFRA-04, 05, 08, 09, 18, 19, 21) — *7 items*
- **Low:** 6 (INFRA-10, 11, 13, 15, 17, 20)
- **Info:** 2 (INFRA-14, 16)

*(Total: 22 findings. Note: Medium count is 7 — INFRA-04, 05, 08, 09, 18, 19, 21.)*

### Top 5 risks

1. **INFRA-01 (Critical)** — RLS is inert at runtime; the API has no DB-level tenant isolation,
   only hand-written WHERE clauses. One forgotten clause = cross-tenant data exposure.
2. **INFRA-22 (High)** — `seed` creates a known-password `system_admin` with no non-dev guard:
   a full-admin backdoor if ever pointed at a shared/prod database.
3. **INFRA-12 (High)** — No statement/lock/idle timeouts + 25-conn pool: a few stuck queries
   exhaust the pool and stall the entire API.
4. **INFRA-03 + INFRA-02 (High)** — The outbox's delivery guarantees are illusory: the bus always
   reports success and the only consumer is a log line, so handler failures silently lose events.
5. **INFRA-06 + INFRA-07 (High)** — No global request-body cap and no list pagination: two
   independent, easily-triggered memory-exhaustion / availability vectors.
