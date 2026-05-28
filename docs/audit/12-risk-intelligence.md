# Audit 12 — Risk, Fraud & Intelligence (Phase 10)

Read-only, atomic-level audit of the FuelGrid OS risk/fraud/intelligence domain.
Repo root: `C:\projects\Actual Projects\fuelGrid os`. No source was modified.

## Scope (files + LOC)

Data layer (`internal/risk/`):

| File | LOC | Purpose |
|------|-----|---------|
| `internal/risk/repo.go` | 28 | Repo struct, sentinel errors, `nullableMoney` |
| `internal/risk/signals_rules.go` | 114 | Signal backfill, rule registry (create/list/status) |
| `internal/risk/alerts_detect.go` | 129 | Detection packs, alert list/get/transition |
| `internal/risk/scoring.go` | 115 | Station score recompute, list, dashboard overview |
| `internal/risk/investigations.go` | 155 | Cases, alert attach, comments, actions, timeline |
| `internal/risk/governance.go` | 121 | Rule tune, suppressions, feedback, pause, summary |

API layer (`services/api/internal/server/`):

| File | LOC |
|------|-----|
| `risk_handlers.go` | 271 |
| `risk_dashboard_handlers.go` | 88 |
| `risk_investigation_handlers.go` | 318 |
| `risk_governance_handlers.go` | 150 |
| `phase10_integration_test.go` | 227 |

Migrations: `0060_risk_foundation` (signals, rules, alerts + perms), `0061_risk_scores`,
`0062_investigations` (cases, case_alerts, comments, actions), `0063_risk_governance`
(suppressions, feedback). Routing reviewed in `services/api/internal/server/server.go:557-613`.
Total in-scope ≈ 2,029 LOC.

Source tables the domain reads: `tank_reconciliations` (0027), `cash_reconciliations`
(0042), `procurement_discrepancies` (0031). RLS design from `0005_rls.up.sql`. Tenant
context helper `internal/database/tenant.go`. Transaction/audit helper `txAudit`
(`services/api/internal/server/accounting_handlers.go:102`).

---

## Flow-by-flow analysis

### 1. Signal backfill (`signals_rules.go:14-39`)

`BackfillSignals` runs three `INSERT ... SELECT ... ON CONFLICT DO NOTHING` statements,
one per source table, summing `RowsAffected()`.

**Source-column correctness (the historical bug-class).** The prior project hit a bug
referencing a nonexistent `created_at` on `procurement_discrepancies`. Here the code
correctly uses `raised_at` (`signals_rules.go:27`), which exists
(`0031_supplier_invoices.up.sql:96`). The other two sources reference real columns:
`tank_reconciliations.created_at` / `.variance_litres` / `.tank_id` (0027:30,37,22) and
`cash_reconciliations.variance` / `.station_id` / `.created_at` (0042:18,14,24). **All
column references resolve** — the regression is not present. (Info, RISK-013.)

**Idempotency.** Keyed on the unique index
`uq_risk_signal_source(tenant_id, signal_type, source_event_id)` (0060:23) with
`ON CONFLICT DO NOTHING`. Re-running does not duplicate signals. Correct.

**Tenant scoping.** Every statement carries `WHERE ... tenant_id = $1`; the tank join is
tenant-pinned (`t.tenant_id = tr.tenant_id`). Good.

**No time window / unbounded scan (RISK-005, Medium).** Backfill ingests the *entire*
history of all three tables on every call. There is no `occurred_at > now() - interval`
or high-water mark. For a long-lived tenant this scans every reconciliation and
discrepancy ever posted on each invocation. The dedup index means no rows are written,
but the SELECT cost grows unbounded.

**`delivery_discrepancy` signals have no station (RISK-006, Medium).** The procurement
statement omits `station_id` because `procurement_discrepancies` has no `station_id`
column (0031:83-114). Signals are inserted with `station_id = NULL`. The signal table's
`actor_id`, `customer_id`, `supplier_id` columns (0060:13-15) are **never populated by any
backfill path** — they are dead schema. Combined with scoring being station-only (§5),
delivery discrepancies can never contribute to any risk score.

**`ListSignals` discards `occurred_at` (RISK-014, Low).** `signals_rules.go:43-62` selects
and orders by `occurred_at`, scans it into `var occurred any`, but never adds it to the
output map. The field the query exists to order by is invisible to the client. Also the
list is hard-capped `LIMIT 500` with no pagination/offset (silent truncation).

### 2. Rule engine (`signals_rules.go:66-114`, `governance.go:12-27,77-83`)

`CreateRule` inserts into `risk_rules` defaulting `rule_type='threshold'`,
`severity='medium'`, `lookback_days=30` when ≤0. `ListRules`, `SetRuleStatus`
(draft/active/paused/retired), `TuneRule`, `PauseAllRules` round it out. The status enum
is enforced by `chk_risk_rule_status` (0060:44) and re-validated in the handler
(`risk_handlers.go:131`).

**Rules are inert — detection never reads them (RISK-001, Critical).** A grep of
`internal/risk` for `risk_rules` shows it is touched only by CRUD (`CreateRule`,
`ListRules`, `SetRuleStatus`, `TuneRule`, `PauseAllRules`, `GovernanceSummary`). It is
**never joined or read by `RunDetection`** (`alerts_detect.go:38-77`). Detection is three
hardcoded SQL packs with literal thresholds, severities, and `rule_code` strings. Therefore:

- `threshold` and `lookback_days` are stored but never evaluated. "Tuning" a rule
  (`TuneRule`, `governance.go:12`) has **zero effect** on what alerts fire — pure theatre.
- `rule_type` ('threshold' vs others) is decorative; there is no rule interpreter.
- Detection's hardcoded `rule_code` values (`'fuel_loss'`, `'cash_shortage'`,
  `'delivery_discrepancy'`) need not correspond to any row in `risk_rules`. The Phase-10
  test even creates a rule with code `cash_short` (`phase10_integration_test.go:44`) while
  the pack hardcodes `cash_shortage` — they are unrelated.

The "rule registry" is a metadata table disconnected from the engine; the documented
"explainable detection packs" run regardless of rule configuration.

**Engine "pause" kill switch is a no-op (RISK-002, Critical).** `PauseAllRules`
(`governance.go:77-83`) sets `risk_rules.status='paused'` for active rules.
`RunDetection` never checks rule status. So after `POST /risk/engine/pause` succeeds,
the very next `POST /risk/detect` raises alerts exactly as before — the incident-response
kill switch does nothing. The test (`phase10_integration_test.go:224`) only asserts the
pause endpoint returns 200; it never re-runs detection to prove silence. This is the most
dangerous gap: an operator believes they have stopped the engine when they have not.

### 3. Detection (`alerts_detect.go:38-77`)

Three `INSERT ... SELECT` packs inside one tx, summing `RowsAffected()`:

1. **fuel_loss** — `tank_reconciliations` with `status='exception'`, joined to `tanks`
   for `station_id`, severity `high`, score 70.
2. **cash_shortage** — `cash_reconciliations` `status='posted' AND variance < 0`,
   severity `medium`, score 55, `amount = abs(variance)`.
3. **delivery_discrepancy** — `procurement_discrepancies` `status='open'`, severity
   `medium`, score 50.

**Idempotency / dedup.** Partial unique index `uq_risk_alert_open` on
`(tenant_id, alert_type, subject_id) WHERE status IN
('open','acknowledged','investigating','escalated')` (0060:78-79) + `ON CONFLICT ... DO
NOTHING`. While an alert for a subject is open, re-detection inserts 0. Correct, and
verified by the test (`phase10_integration_test.go:65`).

**Re-detection after close re-raises duplicates (RISK-015, Low / by-design).** Once an
alert is `resolved`/`dismissed`, the partial index no longer covers it, so the next
`RunDetection` creates a *new* alert for the same still-`exception`/`posted`/`open`
source row. If an operator resolves a cash-shortage alert but the underlying
reconciliation row is never changed (risk never rewrites source data — by design,
`0060` header), the alert resurrects on every detect run. There is no "won't-fix /
acknowledged-forever" state on the subject; suppression (§7) is the only mute.

**Suppression application.** Each pack has a correlated `NOT EXISTS` against
`risk_suppressions` filtering `alert_type`, unexpired (`expires_at IS NULL OR > now()`),
and entity scope (`entity_id IS NULL OR entity_id = station_id`). For fuel_loss/cash the
entity is the station; for delivery the entity check is hardcoded `sup.entity_id IS NULL`
(`alerts_detect.go:66`) — i.e. an entity-scoped suppression can **never** mute a delivery
discrepancy, because delivery alerts have no station/entity to match. Functionally
consistent with the NULL-station problem, but means delivery suppression is all-or-nothing.
Suppression is checked at *insert* time only; a suppression created after an alert exists
does not retroactively close it (RISK-016, Low — documented behaviour gap, the test
suppresses *before* detect, `phase10_integration_test.go:198`).

**Severity / score are constants (RISK-009, Medium).** Severity and score are literals
(70/'high', 55/'medium', 50/'medium') independent of the actual variance magnitude. A
1-litre tank variance and a 50,000-litre loss both score 70/high. The "intelligence" does
not weight by amount, frequency, or recency — it is a fixed-label classifier.

### 4. Alert lifecycle (`alerts_detect.go:112-129`, `risk_handlers.go:232-271`)

`TransitionAlert` does an unconditional `UPDATE risk_alerts SET status=$3,
disposition=COALESCE(...), assigned_to=COALESCE(...)`. Routes expose
acknowledge/investigate/resolve/dismiss/escalate, each setting `assigned_to = actor`.

**No state machine — illegal transitions allowed (RISK-003, High).** The update has no
guard on the *current* status. Any target in the enum is accepted from any state:
`resolved → open`, `dismissed → escalated`, re-`resolve` an already-resolved alert,
escalate a dismissed alert, etc. The DB `chk_risk_alert_status` only constrains the value,
not the transition. `ErrBadState` is declared (`repo.go:16`) but **never returned anywhere**
in the package (dead sentinel). Consequences: dispositions can be overwritten silently;
`assigned_to` is reassigned to whoever last clicked; audit shows a transition but the
business rule "you cannot reopen a resolved alert" is unenforced. There is also no
disposition *requirement* on resolve/dismiss — `req.Disposition` is optional
(`risk_handlers.go:245`), so alerts close with `disposition = NULL`, undermining the
governance dismissal-rate metric (§7).

**Tenant isolation / authZ.** Every transition is `WHERE tenant_id=$1 AND id=$2`, returns
`ErrNotFound` (404) otherwise. Routes gated by `requirePermission("risk_alert.manage")`
(`server.go:573-580`). Reads gated by `risk_alert.read`. Sound. All mutations run through
`txAudit` → `audit.WriteWithOutbox` in one tx. Good.

### 5. Scoring (`scoring.go`)

`RecomputeStationScores` upserts `risk_scores` (dimension `station`) from open alerts,
`score = LEAST(SUM(score),100)`, banded low/watch/elevated/high/critical, with a
`jsonb_object_agg(alert_type, cnt)` component breakdown. Then re-counts and returns the
total scored stations. `ListScores`, `Overview` consume it.

**Comment vs reality: not "severity-weighted" (RISK-010, Low).** The header says scores are
"severity-weighted" (`scoring.go:11`, `0061` header). The SQL sums the *hardcoded per-alert
`score` constant* (70/55/50), not anything derived from `severity`. It is a raw sum of
fixed magic numbers capped at 100.

**Redundant window function (RISK-011, Low / not a correctness bug).** The inner subquery
computes `count(*) OVER (PARTITION BY station_id, alert_type) AS cnt` then the outer
`GROUP BY station_id` does `jsonb_object_agg(alert_type, cnt)`. For N alerts of one type
this emits N identical `(alert_type, cnt=N)` pairs; `jsonb_object_agg` keeps the last, so
the component map is *coincidentally correct* (`{type: N}`). `SUM(score)` and `count(*)`
are computed over the duplicated rows and remain correct (sum/count are insensitive to the
extra `cnt` column). Net: the window machinery is unnecessary complexity that happens to
produce the right answer; a plain `GROUP BY station_id, alert_type` rolled up would be
clearer and unambiguous.

**Only the station dimension is ever computed (RISK-007, Medium).** `risk_scores.dimension`
and `ListScores`/`Overview` are generalized to any dimension, but `RecomputeStationScores`
is the only writer and only emits `dimension='station'`. Alerts with `station_id IS NULL`
(all delivery_discrepancy alerts — §3) are excluded by `WHERE station_id IS NOT NULL`
(`scoring.go:32`), so procurement risk is invisible to scoring/overview. No actor,
customer, or supplier scoring exists despite the schema and the package doc promising
entity scoring.

**Stale scores never decay (RISK-017, Low).** `risk_scores` is upserted only for stations
that currently have open alerts. A station whose alerts all close keeps its last stored
score until someone runs recompute again — and even then, a station with zero open alerts
is not in the `SELECT`, so the `ON CONFLICT` branch never fires to zero it. Its old
`high`/`critical` row persists indefinitely. `Overview.TopStations` will surface a station
as high-risk long after it is clean.

**Overview SQL aggregates.** `Overview` (`scoring.go:84-115`) groups open alerts by
severity (correct, tenant-filtered), sums to `OpenTotal`, takes top-10 station scores, and
reads `max(computed_at)`. The open-by-severity counts are correct. `TopStations` inherits
the stale-score problem above. Error from the final `max()` query is swallowed
(`_ = ...Scan`) — acceptable since `ComputedAt` is nullable.

### 6. Investigations (`investigations.go`, `risk_investigation_handlers.go`)

Cases, `AttachAlert`, comments, recommended actions, action status, case status, timeline.

**Case status machine unguarded (RISK-004, High).** Same defect as alerts: `SetCaseStatus`
(`investigations.go:111-125`) does an unconditional `UPDATE ... SET status=$3`. The
documented lifecycle (open→assigned→in_review→action_required→resolved→closed) is not
enforced — a `closed` case can jump back to `open`, `resolved` can skip to `closed`
without review, etc. Handler validates the *value* against the enum
(`risk_investigation_handlers.go:291`) but never the *transition*. `SetActionStatus`
(`investigations.go:99`) is likewise unguarded across suggested/accepted/completed/dismissed.

**`assigned_to` clobbered on every status change (RISK-018, Low).** `handleSetCaseStatus`
hardcodes `assignee := &actor.UserID` (`risk_investigation_handlers.go:301`) and passes it
to the `COALESCE(...)` — so *any* status transition silently reassigns the case to the
acting user. There is no way to change status without stealing ownership, and no way to
explicitly assign to someone else (no `assigned_to` field in the request body). Same
pattern for alerts (`risk_handlers.go:253`).

**`AttachAlert` has no alert referential integrity (RISK-008, Medium).** Migration 0062
gives `investigation_case_alerts` a composite FK on `(tenant_id, case_id)` but **no FK on
`alert_id`** (`0062:31-39`). `risk_alerts` does expose `uq_risk_alerts_tenant_id(tenant_id,
id)` (0060:81) that a composite FK could target, but it was not used. So `AttachAlert`
(`investigations.go:76-85`) will insert a link to a non-existent alert id without error;
the follow-up `UPDATE risk_alerts ... WHERE id=$2` simply affects 0 rows silently. The
handler's `isForeignKeyViolation` catch (`risk_investigation_handlers.go:137`) only ever
triggers on a bad *case* id. A garbage `alert_id` yields a 200 and a dangling link (the
timeline JOIN just hides it). Cross-tenant attach is blocked only because `tenant_id` is
fixed to the actor's tenant in the INSERT — defense by query construction, not by FK.

**`AttachAlert` force-escalates without state check (interaction with RISK-003).** It sets
the alert to `escalated` when status ∈ (open,acknowledged,investigating). A `resolved` or
`dismissed` alert attached to a case stays closed (the WHERE excludes them) yet is still
linked — so a case can cite a dismissed alert as evidence with no flag.

**Case-scoped timeline (documented simplification) — impact assessment.** `CaseTimeline`
(`investigations.go:129-155`) UNION-ALLs linked-alert events, comments, and actions, all
filtered to the one case, ordered by `at`. This is correct and tenant-scoped (the alert
JOIN is `a.tenant_id = ica.tenant_id`). The acknowledged simplification — the timeline is
*case-scoped*, not a full cross-source evidence reconstruction — is reasonable for an MVP:
it shows what an investigator did inside the case. **Impact: Low-Medium.** What it omits is
the *upstream* evidence chain — the alert's own lifecycle transitions (acknowledged→
investigating timestamps), the source reconciliation/discrepancy facts, and any related
alerts on the same subject/station. So "alert_linked at T" appears but not "why the alert
fired" or "what happened to it before/after linking". For an audit-grade investigation
trail this is thin but not incorrect. The `action:<status>` kind label
(`investigations.go:138`) reflects the action's *current* status at read time, not the
status when the event occurred — so a completed-then-dismissed action shows only the latest
label with the *creation* timestamp, slightly misrepresenting chronology (RISK-019, Low).

**Comments/actions store author but expose neither.** `caseMap` and the comment/action
responses never return `author_id`/timestamps, and there is no list endpoint for raw
comments/actions outside the timeline. Minor (Info).

### 7. Governance & tuning (`governance.go`, `risk_governance_handlers.go`)

`TuneRule`, `CreateSuppression`, `ListSuppressions`, `RecordFeedback`, `PauseAllRules`,
`GovernanceSummary`.

**Suppression expiry silently dropped on bad/datetime input (RISK-012, Medium).**
`handleCreateSuppression` parses `expires_at` with `dateLayout = "2006-01-02"`
(`tanks_handlers.go:19`) and **swallows the parse error**:
`if t, perr := time.Parse(dateLayout, req.ExpiresAt); perr == nil { expires = &t }`
(`risk_governance_handlers.go:75-77`) — no `else`. So a caller sending an RFC3339 datetime
(`2026-06-01T00:00:00Z`), or any malformed value, gets `expires = nil`, i.e. a
**permanent, never-expiring suppression** with no error returned (201). Given suppression
permanently blinds a fraud-detection signal, a silently-permanent suppression from a typo
is a real safety problem. The date is also parsed as midnight UTC with no timezone
handling.

**The "feedback loop" is entirely absent (RISK-020, Medium).** `RecordFeedback`
(`governance.go:67-74`) writing `risk_feedback` is **dead code**: no handler calls it, no
route maps to it (confirmed by grep — only references are the function itself and a test
teardown `DELETE FROM risk_feedback`). The `0063` header claims "Feedback from dispositions
feeds rule tuning," and alert dispositions are captured on `risk_alerts.disposition`, but
nothing ever transfers them into `risk_feedback`, and tuning reads no feedback. The
self-improving-intelligence narrative is cosmetic; `risk_feedback` is a write-never table.

**`GovernanceSummary` metrics (`governance.go:86-121`).** Rule active/paused counts, alert
open/resolved/dismissed counts (correct `FILTER` aggregates, tenant-scoped), signal count,
active-suppression count (`expires_at IS NULL OR > now()` — correct), and
`dismissal_rate = dismissed/(resolved+dismissed)` guarded against divide-by-zero. The math
is correct. But because dispositions aren't required (§4), and because "open" includes
escalated, the dismissal-rate is a weak signal. Two `Scan` errors are deliberately ignored
(`_ =`) for signals/suppressions — those keys are simply absent on DB error rather than
failing the request (Info).

**AuthZ.** `risk_governance.admin` gates governance read/list/pause (`server.go:605-609`);
`risk_rule.tune` gates tune; `risk_alert.suppress` gates suppression create
(`server.go:610-613`). The three perms are seeded to system_admin/executive/auditor
(0063:44-49). Enforcement is real. Note `/risk/engine/pause` (a POST mutation) lives in the
`requirePermissionHeld("risk_governance.admin")` group alongside GET reads — functionally
fine (the perm is the right one) but stylistically it mixes a mutation into a read group.

### 8. Cross-cutting: tenant isolation, tx boundaries, RLS

**Every risk query carries `WHERE tenant_id = $1`** (verified across all six data files).
No IDOR was found via query construction: list/get/transition all filter by tenant and
return `ErrNotFound`/404 on miss. URL-param ids are `uuid.Parse`-validated before use.

**RLS is not active for this domain (RISK-021, Info / house-wide).** Risk tables enable an
RLS `tenant_isolation` policy (e.g. 0060:25-28) keyed on `current_setting('app.current_tenant')`.
But `internal/database/WithTenant` — the only code that runs `SET LOCAL app.current_tenant`
— is **never called anywhere** in the Go codebase. The risk repo uses raw `r.pool.Query`,
and `txAudit` begins a raw `s.deps.DB.Begin(ctx)` tx; neither sets the GUC. Per
`0005_rls.up.sql:12-16` the API connects as the table-owning superuser, which *bypasses*
non-FORCED RLS, so requests still work and isolation falls entirely to the `WHERE` clauses.
This is the documented platform posture (RLS as future belt-and-braces), not a Phase-10
regression — but it means the risk tables' RLS policies provide zero runtime protection
today, and a single forgotten `tenant_id` clause anywhere would leak. Flagged Info because
it is consistent with the rest of the app and the WHERE coverage here is complete.

**Tx boundaries.** All mutations use `txAudit`, which wraps the business change +
`audit.WriteWithOutbox` in one tx and rolls back on any handler-written error. Reads use the
pool directly (no tx) — fine. Detection/backfill/recompute correctly receive the `tx` and
run inside it. No partial-commit paths found.

**Money/number handling.** `amount`/`threshold` are `numeric(14,2)`, `litres`
`numeric(14,3)`. Arithmetic (`abs(variance)`, `SUM(score)`) is in SQL; values are surfaced
as text via `::text` casts (`alertColumns`, `ListScores`, etc.). `nullableMoney`
(`repo.go:23`) maps `""`→NULL before a `$N::numeric` cast, avoiding a cast error on empty
threshold. Consistent with house convention. No float arithmetic on money. Good.

**Error handling / status codes.** Unique violations → 409 (`risk_handlers.go:87`), FK
violations on case ops → 400, not-found → 404, auth → 401/403, validation → 400, else 500
with a generic message and server-side log on the two dashboard/governance reads. Generally
correct. The notable gap is the *silently swallowed* suppression-expiry parse error
(RISK-012) — the only place a bad input returns success.

**Dead code.** `ErrBadState` (`repo.go:16`) never returned; `RecordFeedback` /
`risk_feedback` never wired (RISK-020); `risk_signals.actor_id`/`customer_id`/`supplier_id`
columns never written (RISK-006); the multi-dimension `risk_scores`/`ListScores` generality
exercised only for `station` (RISK-007). `nullableMoney` is used (live).

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| RISK-001 | Critical | `internal/risk/alerts_detect.go:38-77`; `signals_rules.go:66-114` | Detection is hardcoded SQL packs; `risk_rules` (threshold, lookback, rule_type, status) is never read by the engine. Rule CRUD and tuning have no effect on detection. | Drive detection from `risk_rules`: have `RunDetection` join active rules and apply their `threshold`/`lookback_days`, or document that rules are advisory metadata only and stop exposing "tune". |
| RISK-002 | Critical | `internal/risk/governance.go:77-83`; `alerts_detect.go:38-77` | "Engine pause" sets `risk_rules.status='paused'` but `RunDetection` ignores rule status, so detection keeps firing after pause. The kill switch is a no-op. | Make `RunDetection` skip packs whose corresponding rule is not `active`, or gate the whole run on a tenant-level engine flag that pause actually toggles. Add a test that pauses then detects → 0. |
| RISK-003 | High | `internal/risk/alerts_detect.go:114-129`; `risk_handlers.go:232-271` | Alert transitions are unconditional `UPDATE status`; no current-state guard. Illegal transitions (resolved→open, dismissed→escalated, re-resolve) allowed; disposition overwritable; `ErrBadState` is dead. Resolve/dismiss don't require a disposition. | Add `WHERE ... AND status = <allowed-from>` (or a transition map) returning `ErrBadState`→409; require disposition on resolve/dismiss. |
| RISK-004 | High | `internal/risk/investigations.go:99-125`; `risk_investigation_handlers.go:237-318` | Case and action status transitions are unconditional; the documented case lifecycle is unenforced (closed→open, skip review, etc.). | Enforce a transition table for case and action status; reject illegal moves with 409. |
| RISK-005 | Medium | `internal/risk/signals_rules.go:14-39` | Backfill rescans the entire history of all three source tables on every call; no time window / high-water mark. Unbounded SELECT growth. | Add an `occurred_at`/`raised_at > since` filter or persist a per-type watermark; expose a `since` param. |
| RISK-006 | Medium | `internal/risk/signals_rules.go:26-29`; `0060:13-15` | Delivery signals/alerts have no `station_id`; `risk_signals.actor_id/customer_id/supplier_id` are never populated (dead schema). | Capture station/supplier on procurement signals (via PO/delivery join) so they can be scored and entity-suppressed; populate or drop the unused columns. |
| RISK-007 | Medium | `internal/risk/scoring.go:14-46,32` | Only `dimension='station'` is ever computed and `WHERE station_id IS NOT NULL` excludes delivery alerts. No actor/customer/supplier scoring despite schema + doc claims. | Add scoring for non-station dimensions, or down-scope the schema/docs; ensure procurement risk surfaces somewhere. |
| RISK-008 | Medium | `internal/risk/investigations.go:76-85`; `0062:31-39` | `investigation_case_alerts` has no FK on `alert_id`; attaching a bogus/non-existent alert succeeds silently (200, dangling link). | Add composite FK `(tenant_id, alert_id) → risk_alerts(tenant_id, id)`, or existence-check the alert before insert and 400 on miss. |
| RISK-009 | Medium | `internal/risk/alerts_detect.go:44-67` | Severity and score are fixed literals independent of variance magnitude/frequency/recency — a 1 L and a 50,000 L loss score identically. | Derive severity/score from the variance amount (and rule thresholds per RISK-001). |
| RISK-012 | Medium | `services/api/internal/server/risk_governance_handlers.go:73-78` | `expires_at` parsed with date-only layout and the parse error is swallowed; a datetime or typo yields a silently *permanent* suppression (201, no error). | Return 400 on parse failure; accept RFC3339; reject past expiries. |
| RISK-020 | Medium | `internal/risk/governance.go:67-74` | `RecordFeedback`/`risk_feedback` is dead code — no handler/route; dispositions never feed tuning, contradicting the `0063` "feedback feeds tuning" claim. | Wire a feedback endpoint and a tuning input, or remove the table and correct the docs. |
| RISK-010 | Low | `internal/risk/scoring.go:11,16-28` | Doc says "severity-weighted" but score sums hardcoded per-alert constants, not severity. | Weight by severity or fix the comment. |
| RISK-011 | Low | `internal/risk/scoring.go:29-34` | Redundant `count(*) OVER (...)` + `jsonb_object_agg` over duplicated rows; coincidentally correct but confusing. | Use `GROUP BY station_id, alert_type` rolled up, or `jsonb_object_agg` over a pre-aggregated subquery. |
| RISK-013 | Info | `internal/risk/signals_rules.go:27` (vs `0031:96`) | Verified: backfill correctly uses `procurement_discrepancies.raised_at`; the prior `created_at` regression is NOT present. | None — confirmation. |
| RISK-014 | Low | `internal/risk/signals_rules.go:41-64` | `ListSignals` selects/orders by `occurred_at` but never returns it; hard `LIMIT 500` with no pagination. | Include `occurred_at` in output; add pagination. |
| RISK-015 | Low | `internal/risk/alerts_detect.go:51,59,67` | After an alert is closed, re-detection re-raises a new alert for the same still-flagged source row (no won't-fix state). | Track per-subject acknowledgement, or rely on suppression; document the behaviour. |
| RISK-016 | Low | `internal/risk/alerts_detect.go:49-66` | Suppression applies only at insert time and not retroactively; delivery suppression is forced entity-less (`entity_id IS NULL`). | Optionally close existing matching open alerts on suppression create; give procurement an entity to scope on. |
| RISK-017 | Low | `internal/risk/scoring.go:14-46` | Scores for stations whose alerts all closed are never zeroed; stale high-risk rows persist and surface in Overview. | Recompute should also reset/delete scores for stations with no open alerts. |
| RISK-018 | Low | `risk_handlers.go:253`; `risk_investigation_handlers.go:301` | Any status transition reassigns the alert/case to the acting user; no explicit assignee field. | Accept optional `assigned_to`; don't auto-clobber ownership on status change. |
| RISK-019 | Low | `internal/risk/investigations.go:138` | Timeline `action:<status>` shows the action's *current* status at the *creation* timestamp, slightly misrepresenting chronology. | Emit action status-change events with their own timestamps, or label as creation only. |
| RISK-021 | Info | `internal/database/tenant.go`; `0005_rls.up.sql:12-16` | RLS policies on risk tables are inert at runtime — `WithTenant`/`SET LOCAL app.current_tenant` is never invoked and the API connects as a superuser that bypasses non-FORCED RLS. Isolation rests entirely on `WHERE tenant_id`. House-wide, not Phase-10-specific. | Track platform-wide: migrate API onto the `fuelgrid_app` role and set the tenant GUC per request, or FORCE RLS in tests. |

### Severity counts

- **Critical: 2** (RISK-001, RISK-002)
- **High: 2** (RISK-003, RISK-004)
- **Medium: 6** (RISK-005, RISK-006, RISK-007, RISK-008, RISK-009, RISK-012, RISK-020) — *(7 entries; see note)*
- **Low: 8** (RISK-010, RISK-011, RISK-014, RISK-015, RISK-016, RISK-017, RISK-018, RISK-019)
- **Info: 2** (RISK-013, RISK-021)

*Note: Medium is 7 findings (RISK-005/006/007/008/009/012/020). Total findings: 21.*

### Top 5 risks

1. **RISK-002 (Critical)** — The engine "pause" kill switch does nothing; detection keeps
   firing after pause because `RunDetection` ignores rule status. Operators get false
   assurance during an incident. `internal/risk/governance.go:77-83`.
2. **RISK-001 (Critical)** — `risk_rules` is disconnected from detection; thresholds,
   lookbacks, and "tuning" have zero effect. The configurable rule engine is an illusion
   over three hardcoded SQL packs. `internal/risk/alerts_detect.go:38-77`.
3. **RISK-003 (High)** — No alert state machine; any transition (e.g. reopening a resolved
   alert, overwriting a disposition) is permitted, and `ErrBadState` is dead. Undermines
   audit integrity. `internal/risk/alerts_detect.go:114-129`.
4. **RISK-012 (Medium)** — A malformed or datetime `expires_at` silently creates a
   *permanent* suppression (error swallowed, 201 returned), permanently blinding a fraud
   signal from a typo. `services/api/internal/server/risk_governance_handlers.go:73-78`.
5. **RISK-008 (Medium)** — No FK on `investigation_case_alerts.alert_id`; bogus alert ids
   can be attached to cases without error, producing dangling evidence links.
   `internal/risk/investigations.go:76-85`, `0062:31-39`.
