# Enterprise / Multi-Site Governance Domain Audit — FuelGrid OS (Phase 9)

**Scope:** Read-only, atomic-level audit of the Enterprise / Chain Command domain (Phase 9): optional station groups, delegated enterprise scope grants and effective-station resolution, the generic policy-driven approval engine, enterprise read-model projections (station daily KPIs + freshness state), the executive overview / station ranking, central price rollouts, central procurement plans, inter-station stock transfers, consolidated finance, the station-KPI CSV export, and the enterprise exception command queue. Findings cite `file:line`, name functions, and quote code. Uncertainty is marked explicitly. No source was modified.

## Files in scope (with LOC)

| File | LOC | Role |
|------|-----|------|
| `internal/enterprise/repo.go` | 36 | Repo struct, sentinel errors (`ErrNotFound`/`ErrConflict`/`ErrBadState`), `isUniqueViolation`, `nullableMoney` |
| `internal/enterprise/governance.go` | 315 | Station groups, scope grants, `EffectiveStations`, approval policies/requests/`Decide` |
| `internal/enterprise/projections.go` | 115 | `RebuildStationKPIs`, `EnterpriseOverview`, `StationRanking` |
| `internal/enterprise/central_commercial.go` | 300 | Price rollouts, procurement plans, stock transfers |
| `services/api/internal/server/enterprise_governance_handlers.go` | 330 | Groups, scopes, approval HTTP handlers |
| `services/api/internal/server/enterprise_commercial_handlers.go` | 371 | Rollout / plan / transfer HTTP handlers |
| `services/api/internal/server/enterprise_dashboard_handlers.go` | 99 | Projection rebuild, overview, station ranking |
| `services/api/internal/server/enterprise_finance_handlers.go` | 86 | Consolidated finance, station-KPI CSV export |
| `services/api/internal/server/enterprise_queue_handlers.go` | 44 | Exception command-queue aggregate |
| `services/api/internal/server/phase9_integration_test.go` | 306 | The five Phase-9 integration tests |
| `services/api/migrations/0057_enterprise_governance.{up,down}.sql` | 151 / 12 | groups, memberships, scope grants, approval engine, perms |
| `services/api/migrations/0058_enterprise_projections.{up,down}.sql` | 47 / — | station_daily_kpis, projection state, perm |
| `services/api/migrations/0059_central_commercial.{up,down}.sql` | 125 / 11 | rollouts, plans + lines, transfer orders, perms |

Route wiring lives in `services/api/internal/server/server.go:494–555`. The stock-ledger source-of-truth (`stock_movements`, `0024`) and the canonical posting routine (`internal/inventory/repo.go:PostMovement`) are referenced for comparison but owned by the inventory agent.

---

## Authorization posture (all mutating routes)

Every Phase-9 route is gated by a permission middleware (`server.go:494–555`). Mutating routes use `requirePermission(perm, nil)` (tenant-wide, no station extractor); reads use `requirePermissionHeld(perm)`. The permissions exist and are seeded to `system_admin`/`regional_manager`/`executive` (`0057:137–151`, `0059:108–125`). So authZ is *enforced, not assumed*, at the coarse tenant level:

| Route | Middleware |
|-------|-----------|
| `POST /enterprise/station-groups` (+members) | `enterprise_structure.manage` |
| `POST /enterprise/scope-grants` | `enterprise_access.manage` |
| `POST /approval-policies` | `approval_policy.manage` |
| `POST /approval-requests/{id}/decide` | `approval_request.decide` |
| `POST /enterprise/projections/rebuild` | `enterprise_projection.admin` |
| `POST /central-price-rollouts` / `/approve` / `/activate` | `central_pricing.manage` / `.approve` / `.publish` |
| `POST /central-procurement-plans` / `/release` | `central_procurement.manage` / `.release` |
| `POST /stock-transfers` / `/approve` / `/receive` | `stock_transfer.manage` / `.approve` / `.receive` |
| `POST /approval-requests` (raise) | `enterprise.read` *(read perm gates a write — see ENT-08)* |
| `GET /enterprise/finance/consolidated` | `finance.read` |
| `GET /enterprise/reports/station-kpis` | `finance.export` |

The **critical gap is not at the route gate but below it**: none of these mutating routes constrain the actor to the *stations they are scoped to*. The `extract` argument is `nil` everywhere, so `requirePermission` builds `policy.Resource{}` with no station (`policy_middleware.go:172–181`). A user holding `central_pricing.publish` can activate a rollout across *every* station in the tenant; a user holding `stock_transfer.receive` can post movements into *any* tank in the tenant. The delegated-scope system that supposedly bounds this is inert (ENT-01).

---

## Flow 1 — Station groups & membership (Stage 1)

**Code:** `governance.go:14–74`; handlers `handleCreateStationGroup`/`handleListStationGroups`/`handleAddGroupMember` (`enterprise_governance_handlers.go:19–107`); table `station_groups` + `station_group_memberships` (`0057:6–40`).

`CreateGroup` inserts within the tx and re-reads via `RETURNING` (`governance.go:23–28`) — correct. `AddGroupMember` does `INSERT … ON CONFLICT DO NOTHING` (`governance.go:51–57`), idempotent on re-add. Cross-tenant membership is structurally impossible: both `sgm_group_fk (tenant_id, station_group_id)` and `sgm_station_fk (tenant_id, station_id)` are composite tenant FKs (`0057:32–33`), and the unique `uq_sgm_member` prevents duplicates. Good — the house convention is honoured.

**Defects:**

- **ENT-12 (Low) — No membership removal / group archival path.** There is `AddGroupMember` and `ListGroupMembers` (`governance.go:51,59`) but no remove-member, no group archive/rename handler, and `station_groups.status` (`active|archived`, `0057:15`) can never become `archived` through any route. A group, once populated, is immutable except by adding more members. `ListGroupMembers` itself is not exposed by any route — group contents are write-only from the API surface.
- **ENT-13 (Info) — `kind` is free-text, unvalidated.** `station_groups.kind` (`0057:10`) has no CHECK and the handler passes it through verbatim (`enterprise_governance_handlers.go:38`). Cosmetic; acceptable for a label.

---

## Flow 2 — Delegated enterprise scopes & effective-station resolution (Stage 2)

**Code:** `GrantScope` (`governance.go:78–85`), `EffectiveStations` (`governance.go:87–152`); handlers `handleGrantScope`/`handleEffectiveStations` (`enterprise_governance_handlers.go:111–167`); table `enterprise_scope_grants` (`0057:45–61`).

`EffectiveStations` resolves a user's grants into a station set: a `tenant` grant short-circuits to `(nil, tenantWide=true)` (`governance.go:107–109`); `station` is itself; `company`/`region` expand against `stations` filtered by `tenant_id`; `group` expands via memberships (`governance.go:121–133`). Resolution queries are all tenant-scoped, so the *resolution* cannot leak cross-tenant stations even given a forged `scope_id`.

**This is where the documented "simplification" bites.** The brief asks whether scopes that "resolve to an effective-station set without rewiring the policy engine" actually enforce anything. The answer, traced through the whole tree:

> **`EffectiveStations` is referenced in exactly one place: the read-only endpoint `GET /enterprise/users/{id}/effective-stations` (`server.go:507`).** A repo-wide grep for `EffectiveStations` finds only its definition, that one handler, and the TS SDK wrapper. **No mutating route, no policy check, and no workflow consults it.**

Therefore the entire delegated-scope subsystem is **advisory/cosmetic**: granting a user a `region` scope confers *zero* additional authority and imposes *zero* restriction. Authority is decided solely by the tenant-wide RBAC permissions on the routes. A user with `stock_transfer.receive` but a `station`-scoped grant for Station A can still receive a transfer into a tank at Station B — nothing checks. Conversely, a scope grant does not *unlock* anything either. This is the single largest "is it real or cosmetic?" finding of the phase.

**Defects:**

- **ENT-01 (High) — Delegated scopes enforce nothing; the feature is decorative.** `EffectiveStations` (`governance.go:87`) is never consulted by any authorization or mutation path. Grants neither grant nor restrict. Any RBAC holder operates tenant-wide. This is a governance illusion: an operator may believe "scope-grant Station A to user X" limits X to A, but it does not. Fix: thread effective-station enforcement into the mutating handlers (e.g., transfer create/receive, rollout activate with `scope_type='station'`) by intersecting the target station(s) with `EffectiveStations`, or document loudly that grants are informational only.
- **ENT-02 (Medium) — `scope_id` is an unconstrained, unvalidated UUID.** `enterprise_scope_grants.scope_id` (`0057:50`) has no FK and `GrantScope` does not validate it against `companies`/`regions`/`station_groups`/`stations`. You may grant `scope_type='region'` with a random or another tenant's region UUID; the row persists. Resolution then yields an empty set (tenant filter saves correctness), so the only harm is silent dead grants and a misleading "access" UI. Combined with ENT-01 the impact is muted, but if ENT-01 is ever fixed this becomes a correctness landmine.
- **ENT-03 (Low) — Invalid `scope_type` returns 500, not 400.** `GrantScope` relies on the DB CHECK `chk_scope_type` (`0057:53`). A bad `scope_type` raises SQLSTATE `23514`, which `isForeignKeyViolation` (only matches `23503`, `platform_handlers.go:206`) does not map, so `handleGrantScope` falls through to `writeError(…, 500, "internal error")` (`enterprise_governance_handlers.go:138`). Client-supplied bad input should be a 400.
- **ENT-14 (Low) — No de-duplication or revocation of grants.** No unique index on `(tenant_id, user_id, scope_type, scope_id)`; the same grant can be inserted repeatedly. There is no revoke endpoint, so an over-grant cannot be withdrawn via the API.

---

## Flow 3 — Approval engine: policies, requests, decisions (Stage 3)

**Code:** `CreatePolicy`/`ListPolicies` (`governance.go:179–212`), `RaiseRequest` (`governance.go:216–234`), `Decide`/`requestForUpdate` (`governance.go:270–315`); handlers `enterprise_governance_handlers.go:171–330`; tables `approval_policies`/`approval_requests`/`approval_decisions` (`0057:64–132`).

**Policy matching.** `RaiseRequest` selects `COALESCE(MAX(required_approvals), 1)` over `approval_policies WHERE status='active' AND workflow_type=$2 AND min_amount <= COALESCE($3::numeric,0)` (`governance.go:218–222`). So the strictest threshold-matching policy's approval count is snapshotted onto the request. Reasonable. But:

- `required_role` is written by `CreatePolicy` (`governance.go:185`) and read by `ListPolicies` (`governance.go:206`) yet **never enforced** — `Decide` does not check that the decider holds the role. Segregation-of-duties by role is non-functional.
- `approval_policies.scope_type`/`scope_id` columns exist (`0057:71–72`) but `CreatePolicy` never sets them (always NULL) and nothing reads them. Dead columns.
- The handler `handleCreateApprovalPolicy` accepts `min_amount` as a string but does **not** validate it is a non-negative decimal (contrast the rollout/transfer handlers which call `parseDecimal`). A garbage `min_amount` is passed to `nullableMoney` → `COALESCE($3::numeric,0)`; a non-numeric string raises a cast error → 500.

**Raising.** `RaiseRequest` snapshots `required_approvals` and inserts within the tx with `RETURNING` (`governance.go:225–232`) — correct re-read discipline. `approval_requests.station_id` is stored (`0057:98`) but never used for authorization or routing.

**Decide flow (the prior stale-status bug).** The brief flags a prior defect where the final read used a separate connection. **Verified fixed.** `Decide` (`governance.go:270`) locks the row `FOR UPDATE` via `requestForUpdate` (`governance.go:305–307`), inserts the decision, then issues the status `UPDATE … RETURNING approvalReqColumns` **on the same `tx`** (`governance.go:289–298`), and `handleDecideApproval` serializes that returned object (`enterprise_governance_handlers.go:323,329`) without any post-commit re-read. No separate connection is used. Multi-approval counting is correct and concurrency-safe: the `FOR UPDATE` lock serializes concurrent deciders, and the `CASE WHEN approvals_count + 1 >= required_approvals` arithmetic operates on the locked row (`governance.go:294–295`).

**Idempotency / one-vote.** `uq_approval_decision_one (tenant_id, approval_request_id, decided_by)` (`0057:127`) enforces one decision per decider; a duplicate raises `23505` → `Decide` maps to `ErrConflict` → 409 "you have already decided" (`governance.go:282–284`, `enterprise_governance_handlers.go:315`). Illegal transitions: a non-`requested` request returns `ErrBadState` → 409 (`governance.go:275`). Both correct and tested (`phase9_integration_test.go:64–79`).

**Defects:**

- **ENT-04 (High) — No self-approval guard; `required_role` unenforced.** `Decide` never compares `deciderID` against the request's `requested_by`, so a requester can approve their own request (the integration test does exactly this, `phase9_integration_test.go:57–66`). Combined with single-approval default policies, one user single-handedly raises *and* approves a high-value action — defeating the engine's purpose. And because `required_role` is ignored (`governance.go` never reads it), a policy demanding e.g. "executive" approval is not honoured. Fix: reject `decided_by == requested_by` (configurable), and verify the decider holds `required_role` before counting the vote.
- **ENT-05 (Medium) — The approval engine is not wired into any workflow it claims to gate.** The migration header calls it "a generic policy-driven approval engine that workflows call before finalizing high-value actions" (`0057:3–4`). No Phase-9 workflow calls it: `ActivatePriceRollout`, `ReleasePlan`, `ApproveTransfer`/`ReceiveTransfer` do **not** raise or check approval requests. The `central_price_rollouts.status` includes `'pending_approval'` (`0059:22`) but no code ever sets it. So approvals are a free-standing CRUD island; "high-value actions" finalize without ever consulting it. Reference IDs (`reference_type`/`reference_id`) are caller-supplied free text with no FK and no back-link enforcement.
- **ENT-06 (Low) — `min_amount` not validated in handler; bad value → 500.** `handleCreateApprovalPolicy` (`enterprise_governance_handlers.go:177–204`) does not `parseDecimal(req.MinAmount)`. A non-numeric string reaches the `::numeric` cast and produces a 500 rather than a 400.
- **ENT-07 (Low) — No request cancel/expire path.** Statuses `cancelled`/`expired` exist (`0057:103`) but no handler ever sets them; stale requests accumulate as `requested` forever and inflate the exception queue (`approvals_waiting`).
- **ENT-08 (Low) — Raising a request is gated by a *read* permission.** `POST /approval-requests` sits in the `requirePermissionHeld("enterprise.read")` group (`server.go:496–501`), so anyone who can *view* enterprise dashboards can *create* approval requests. Minor, but a write under a read gate is a smell.

---

## Flow 4 — Projections & read models (Stages 4–6)

**Code:** `RebuildStationKPIs` (`projections.go:14–37`), `EnterpriseOverview` (`projections.go:51–81`), `StationRanking` (`projections.go:93–115`); handlers `enterprise_dashboard_handlers.go`; tables `station_daily_kpis` + `enterprise_projection_state` (`0058`).

**Rebuild.** `RebuildStationKPIs` does an `INSERT … SELECT … FROM revenue_days … ON CONFLICT (tenant_id, station_id, business_date) DO UPDATE` (`projections.go:15–22`), genuinely idempotent (re-running upserts the same rows; the test verifies no double-count, `phase9_integration_test.go:123–129`). It then records freshness in `enterprise_projection_state` (`projections.go:29–35`). Within the tx. Good.

**Staleness / consistency.** The projection is a *snapshot* of `revenue_days`; it is only refreshed on the explicit `POST /enterprise/projections/rebuild`. There is no trigger, outbox consumer, or cron driving it. Between a new `revenue_days` row and the next manual rebuild, the overview and ranking are stale. `enterprise_projection_state.last_rebuilt_at` is surfaced to the UI (`enterprise_dashboard_handlers.go:55–59`) so lag is at least *visible* — but consumers have no guard against acting on stale KPIs.

**Defects:**

- **ENT-09 (Medium) — KPI projection never prunes; deleting/relocking a revenue day leaves an orphan.** `RebuildStationKPIs` only upserts `revenue_days` rows that currently exist; it never `DELETE`s `station_daily_kpis` rows whose source `revenue_days` row was removed or whose figures were *reduced to omission*. If a revenue day is deleted or a station is decommissioned, its KPI row persists and keeps inflating `EnterpriseOverview` sums and station ranking. A true rebuild should be a full replace (truncate-by-tenant then insert, or anti-join delete) rather than insert-only-upsert. (Note: `sdk_station_fk … ON DELETE CASCADE`, `0058:17`, cleans up only the station-deleted case, not the revenue-day-deleted case.)
- **ENT-10 (Low) — `EnterpriseOverview` ignores the requested date window for AP/AR/incidents.** Only `gross/net/margin` honour `from`/`to` (`projections.go:53–56`); AP outstanding, AR outstanding, open incidents, and approvals-waiting are all-time tenant totals (`projections.go:59–78`), regardless of the `?from=&to=` the handler passes. The response shape implies a period view; mixing period revenue with lifetime balances is misleading on a dashboard.
- **ENT-15 (Info) — AR "outstanding" is a net ledger sum, which is broadly correct but unfiltered.** `EnterpriseOverview` computes `SUM(amount) FROM ar_entries` (`projections.go:64–66`). Verified against `internal/receivables/repo.go:212,237`: charges are positive, payments negative, so the sum nets to true outstanding AR. However it includes customers in credit (negative balances offset others) and has no "written-off" exclusion, so it is an approximation, not a strict dunnable-AR figure. Acceptable for an executive glance; flagged for transparency.
- **ENT-16 (Low) — `StationRanking` / ranking aggregates are unbounded.** `StationRanking` (`projections.go:93`) `LEFT JOIN`s every station against the KPI projection and groups — no pagination, no limit. For a large chain this is an unbounded result set returned in full to the dashboard, the consolidated-finance `by_station` array, and the CSV export. N×days is bounded by the projection, but the station fan-out is unpaginated everywhere it is used.

---

## Flow 5 — Central price rollouts (Stage 7)

**Code:** `CreatePriceRollout`/`ApprovePriceRollout`/`ActivatePriceRollout`/`lockedRollout` (`central_commercial.go:33–124`); handlers `enterprise_commercial_handlers.go:28–147`; table `central_price_rollouts` (`0059:7–32`).

**Does activation actually push prices?** **Yes — this is REAL, not a no-op.** `ActivatePriceRollout` locks the rollout `FOR UPDATE`, guards `status ∈ {approved, scheduled}`, then `INSERT INTO price_changes (… station_id, product_id, unit_price, effective_from, reason='central rollout', set_by, previous_price) SELECT … FROM stations s WHERE … (scope match)` (`central_commercial.go:88–99`), and finally `UPDATE … status='active', stations_applied=<rows>` with `RETURNING` (`central_commercial.go:105–110`) — all in one tx. Cross-checked against the price resolver: the active selling price for `(station, product)` is the latest `price_changes` row with `effective_from <= now()` (`internal/pricing/repo.go:87–88`, also consumed by `internal/revenue/repo.go:101`). So an activated rollout genuinely becomes the live price once its date arrives. State machine and locking are sound; the test confirms a tenant rollout applies to both stations (`phase9_integration_test.go:152–155`).

**Defects:**

- **ENT-11 (Medium) — `scope_type ∈ {region, station}` with NULL `scope_id` silently applies to zero stations and still marks `active`.** Nothing requires a non-NULL `scope_id` for non-tenant scopes (`central_price_rollouts` has no such CHECK, `0059:7–32`; neither `CreatePriceRollout` nor the handler validates it). The activation predicate `($6='region' AND s.region_id=$7)` / `($6='station' AND s.id=$7)` with `$7=NULL` matches nothing (`s.id = NULL` is never true), so `stations_applied=0`, yet `status` becomes `'active'` (`central_commercial.go:96–110`). The operator gets a green "active" rollout that changed no prices.
- **ENT-17 (Low) — Rollout writes `price_changes` for stations that do not carry the product.** The activation `SELECT … FROM stations s` (`central_commercial.go:94–98`) is not joined to tanks/products; it writes a price row for product P at *every* in-scope station, including ones with no tank for P. The price board only surfaces priced products with tanks, so the rows are inert noise, but they pollute the append-only price history and the `previous_price` lineage.
- **ENT-18 (Low) — Malformed `effective_from` silently defaults to "today".** `handleCreatePriceRollout` parses `effective_from` and on parse error keeps `from = time.Now()` (`enterprise_commercial_handlers.go:49–54`) with no error. A typo'd date becomes an *immediate* price change rather than a rejected request. Date is `numeric(14,4)` price aside, the column is a `date`; precision is fine.
- **ENT-19 (Info) — No supersede linkage.** Status `'superseded'`/`'scheduled'` exist (`0059:22`) but no code transitions a prior active rollout to `superseded` when a new one activates for overlapping scope. Two active rollouts for the same product/scope are allowed; resolution falls back to `price_changes` recency, which is correct, but the rollout table's own status is unreliable as a record of what is live.

---

## Flow 6 — Central procurement plans (Stage 8)

**Code:** `CreatePlan`/`AddPlanLine`/`ReleasePlan`/`ListPlans` (`central_commercial.go:128–180`); handlers `enterprise_commercial_handlers.go:151–245`; tables `central_procurement_plans` + `_lines` (`0059:34–73`).

`CreatePlan` + per-line `AddPlanLine` run in one tx; FK violations on a line map to 400 "unknown station or product" (`enterprise_commercial_handlers.go:181–183`). `ReleasePlan` guards `status ∈ {draft, reviewed, approved}` → `released` and flips all lines `released=true`, returning the line count (`central_commercial.go:146–161`).

**The documented gap — release does NOT auto-create Phase-5 POs.** Confirmed. `ReleasePlan` only sets boolean flags; a repo-wide check shows no procurement/PO code reads `central_procurement_plan_lines.released`. The comment "the station-scoped allocation hand-off to Phase-5 procurement" (`central_commercial.go:144–145`) is aspirational — there is no hand-off. **Integrity/usefulness impact:** a released plan is a dead-end flag. The feature produces no procurement artifact, no PO, no reservation, no notification; downstream Phase-5 has no awareness a plan was released. As shipped it is a glorified to-do list with no actionable output — effectively cosmetic beyond record-keeping.

**Defects:**

- **ENT-20 (Medium) — `target_litres` is unvalidated and uncapped (zero/negative accepted).** `central_procurement_plan_lines.target_litres numeric(14,3)` has **no CHECK** (`0059:60`), `AddPlanLine` passes the string straight to `$5::numeric` (`central_commercial.go:136–139`), and the handler does not `parseDecimal` it (contrast transfers/rollouts). A line with `target_litres = "0"` or `"-5000"` persists. Contrast `stock_transfer_orders` which has `chk_sto_litres CHECK (litres > 0)` (`0059:89`).
- **ENT-21 (Medium) — Plan lines are write-only; no read path.** `ListPlans` returns only `id, name, status` (`central_commercial.go:165`); there is no endpoint to read a plan's lines. After creation, the plan's substance (which station, which product, how much) is invisible through the API — undermining "coordinate station-scoped replenishment."
- **ENT-22 (Low) — Empty plans can be released; no line-count guard.** `ReleasePlan` returns `ErrBadState` only on a bad status, never on zero lines. `POST /…/release` on a line-less plan returns 200 with `released_lines: 0`.
- **ENT-23 (Info) — No status-advance handlers for `reviewed`/`approved`/`closed`.** Only `release` mutates status; `reviewed`/`approved`/`closed` (`0059:43`) are unreachable, so the documented review/approval workflow is absent.

---

## Flow 7 — Inter-station stock transfers (Stage 9)

**Code:** `CreateTransfer`/`ApproveTransfer`/`ReceiveTransfer`/`lockedTransfer` (`central_commercial.go:199–300`); handlers `enterprise_commercial_handlers.go:256–371`; table `stock_transfer_orders` (`0059:75–103`).

`CreateTransfer` validates `litres>0` in the handler (`enterprise_commercial_handlers.go:272`) and the DB (`chk_sto_litres`). `ApproveTransfer` guards `status='draft' → 'approved'` (`central_commercial.go:230–232`). `ReceiveTransfer` locks the order `FOR UPDATE`, guards `status ∈ {approved, dispatched}`, reads source balance, refuses overdraw (`ErrInsufficientStock` → 422), posts the paired out/in movements, and marks `received` with movement IDs — all in one tx (`central_commercial.go:245–288`). The happy path and overdraw refusal are tested (`phase9_integration_test.go:171–213`).

**Does it post real, atomic stock movements on both sides?** Yes — both `INSERT INTO stock_movements` and the order `UPDATE` are in the same tx and `txAudit` commits once (`enterprise_commercial_handlers.go:337–366`). The two movements are `movement_type='transfer'` with `source_ref_type='transfer'`, `source_ref_id=<order id>` (`central_commercial.go:268–278`), traceable. **But the balance math diverges dangerously from the canonical ledger discipline (ENT-24).**

**Defects:**

- **ENT-24 (High) — `ReceiveTransfer` computes `balance_after` from a stale per-row snapshot instead of the authoritative ledger SUM, and bypasses `inventory.PostMovement` entirely.** The canonical posting routine computes `balance_after = (SELECT COALESCE(SUM(litres),0) FROM stock_movements WHERE tank_id=…) + litres` in-statement (`internal/inventory/repo.go:152–153`), and the ledger doc (`0024:5–14`) is explicit that *book stock is the SUM of all rows; `balance_after` is only a display snapshot*. `ReceiveTransfer` instead reads `balance_after FROM stock_movements WHERE … status='posted' ORDER BY seq DESC LIMIT 1` (`central_commercial.go:254,264`) and computes `(snapshot ± litres)` in Go (`central_commercial.go:270,276`). Two concrete bugs flow from this:
  1. **Reversal blindness.** The `status='posted'` filter skips `reversed` rows. The canonical SUM nets a reversal against its contra to zero; the snapshot approach reads the *latest posted row's* `balance_after`, which after a reversal sequence can be a value that no longer reflects the true SUM. The overdraw guard (`fromBal < litres`, `central_commercial.go:261`) therefore checks against a possibly-wrong balance, and the posted `balance_after` snapshots drift from `CurrentBalance`.
  2. **Opening-balance bypass.** `inventory.PostMovement` refuses a flow movement on a tank with no opening balance (`ErrNoOpeningBalance`, `repo.go:135–142`); `ReceiveTransfer` raw-inserts and skips that guard, so a transfer can post *into* a destination tank that was never opened, establishing a ledger out of band of the inventory module's invariants. Fix: route both legs through `inventory.PostMovement` (or replicate its SUM-based `balance_after` and opening check).
- **ENT-25 (High) — No product alignment: a transfer can mix products across tanks.** `stock_transfer_orders.product_id` is an independent FK to `products` (`0059:80,94`) with **no constraint** that it equals `from_tank.product_id`, equals `to_tank.product_id`, or that the two tanks even hold the same product. Neither `CreateTransfer` nor `ReceiveTransfer` checks. A tank stores exactly one product (`0010:13`, `chk`-enforced by `uq_tanks_tenant_station_product`), so this lets you move "5000 L of PMS" out of a *diesel* tank into a *kerosene* tank — corrupting both ledgers and the physical model. Fix: validate `from_tank.product_id = to_tank.product_id = product_id` (ideally a composite FK `(tenant_id, from_tank_id, product_id) REFERENCES tanks(tenant_id, id, product_id)` for both tanks, mirroring the `uq_tanks_tenant_station_product` key).
- **ENT-26 (Medium) — `balance_after` read uses `float64`, risking precision loss on litres.** `ReceiveTransfer` scans balances into `float64` (`central_commercial.go:253`) and casts litres via `SELECT $1::numeric` into a `float64` (`central_commercial.go:257–259`), then does float arithmetic for both the guard and the posted `balance_after` (`-$4::numeric`, `$5::numeric - $4::numeric`, `central_commercial.go:270,276`). `litres` is `numeric(14,3)`; doing the comparison and the stored balance in float64 reintroduces the rounding the "money/quantities as decimals, arithmetic in SQL" convention exists to avoid. The posted `balance_after` is `($5 - $4)` where `$5` is the float-derived prior balance — compounding drift. Keep the arithmetic in SQL against the ledger SUM.
- **ENT-27 (Low) — "Inter-station" is not enforced; same-station transfers allowed.** Only `from_tank_id <> to_tank_id` is required (`chk_sto_distinct`, `0059:91`). Two tanks at the *same* station can transfer between each other — arguably fine, but the "inter-station" framing is not enforced, and there is no station-mismatch authZ tie-in (see ENT-01).
- **ENT-28 (Low) — `dispatched` status is unreachable; the lifecycle skips it.** `ReceiveTransfer` accepts `approved` *or* `dispatched` (`central_commercial.go:250`) but no handler ever sets `dispatched`. The documented draft→approved→dispatched→received flow collapses to draft→approved→received. Dead state.

---

## Flow 8 — Consolidated finance & station-KPI export (Stages 10–11)

**Code:** `handleConsolidatedFinance`/`handleStationKPIExport` (`enterprise_finance_handlers.go`).

`handleConsolidatedFinance` calls `accounting.IncomeStatement`, `accounting.BalanceSheet`, and `enterprise.StationRanking`, and emits all three side by side (`enterprise_finance_handlers.go:29–56`). The KPI export builds a 4-column CSV from `StationRanking` and returns it with a SHA-256 checksum (`enterprise_finance_handlers.go:74–85`). Money values flow through as decimal strings end-to-end — discipline preserved here.

**Defects:**

- **ENT-29 (Medium) — "Consolidated" finance performs no reconciliation; the tie-out is claimed but absent.** The doc says it "reconciles to station reports … one consolidated view that reconciles" (`enterprise_finance_handlers.go:14–17`). In fact it merely places the GL income statement next to the projection-derived `by_station` array. There is no assertion, variance, or tie that `SUM(by_station.gross_revenue)` equals `income_statement.revenue` — and they come from *different* sources (posted journal lines vs. `revenue_days` projection) that need not agree. The integration test (`phase9_integration_test.go:266–277`) checks each independently and never compares them. The feature is a juxtaposition, not a reconciliation.
- **ENT-30 (Low) — KPI export is unpaginated and fully buffered.** `handleStationKPIExport` builds the entire CSV in a `bytes.Buffer` and returns it inside a JSON field plus checksum (`enterprise_finance_handlers.go:74–85`). For a large chain this is an unbounded in-memory + JSON-encoded payload; the checksum over `buf.Bytes()` is sensible but the whole-result-in-RAM pattern does not scale. (Inherits ENT-16.)
- **ENT-31 (Info) — Export Content-Type is JSON, not CSV.** The "CSV export" is delivered as a JSON string field, not a `text/csv` download. Functional, but the naming oversells it as a file export.

---

## Flow 9 — Enterprise exception command queue (Stage 12)

**Code:** `handleEnterpriseExceptions` (`enterprise_queue_handlers.go:13–44`).

Six fixed `count(*)` queries over incidents, shift exceptions, unmatched bank lines, unposted cash recs, waiting approvals, and open credit alerts, all tenant-scoped, summed into `total` (`enterprise_queue_handlers.go:24–42`). The test asserts a pending approval shows up (`phase9_integration_test.go:294–305`).

**Defects:**

- **ENT-32 (Low) — Six sequential round-trips where one query (or `UNION ALL`) would do.** Each check is a separate `QueryRow` in a loop (`enterprise_queue_handlers.go:33–39`); a single `SELECT (subquery), (subquery), …` would halve latency and remove the per-check error fan-out. Bounded (six), so Low.
- **ENT-33 (Info) — Reads `s.deps.DB` directly rather than going through a repo.** Unlike the rest of Phase 9 which routes through `s.enterprise`, this handler embeds SQL in the server layer (`enterprise_queue_handlers.go:35`), referencing six tables from five other domains. Tight coupling; a schema change in any of those tables silently breaks this handler with no compile-time signal.
- **ENT-34 (Info) — `expired`/`cancelled` approvals and unactionable stale items inflate `total`.** Tied to ENT-07: because requests never expire, `approvals_waiting` grows unboundedly, and the queue total loses signal over time.

---

## Tenant isolation (IDOR) summary

All Phase-9 repo methods take `tenantID` first and filter `WHERE tenant_id = $1` (verified across `governance.go`, `projections.go`, `central_commercial.go`). All composite FKs are tenant-scoped (`sgm_*`, `esg_user_fk`, `cpr_*`, `cpp_*`, `sto_*`, `sdk_station_fk`). Handlers always derive `tenantID` from the authenticated actor (`identity.Require`), never from the request body or URL. The one user-supplied entity id that crosses into a lookup — `handleEffectiveStations`'s `{id}` user param — is still resolved under the actor's `tenant_id` (`enterprise_governance_handlers.go:161`), so no cross-tenant read. **No IDOR found.** The unconstrained `scope_id`/`reference_id` columns (ENT-02) accept arbitrary UUIDs but resolution stays tenant-filtered, so they are integrity smells, not isolation breaches. (RLS policies are present on every table but are inert at runtime per the prior cross-cutting audit; app-layer filtering is the real boundary and it holds.)

## Tx + audit + outbox atomicity

Every mutating handler uses `s.txAudit` (`accounting_handlers.go:102–130`), which begins a tx, runs `fn`, writes `audit.WriteWithOutbox`, and commits once — business change + audit + outbox in one atomic unit. Verified for groups, members, scope grants, policies, requests, decisions, rollout create/approve/activate, plan create/release, and transfer create/approve/receive. The read-only handlers (overview, ranking, consolidated, export, exceptions, effective-stations) correctly do not open a tx. Atomicity discipline is sound across the phase.

## Money / quantity discipline

Money flows as decimal strings (`amount`, `unit_price`, `litres`, KPI sums) with `::text` casts on read and `::numeric` on write — correct, **except** the stock-transfer balance arithmetic, which drops to `float64` (ENT-26) and abandons the SUM-based ledger model (ENT-24).

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| ENT-01 | High | `governance.go:87`; `server.go:494–555` | Delegated scopes enforce nothing — `EffectiveStations` used only by a read endpoint; mutations are tenant-wide | Intersect target stations with effective set in mutating handlers, or document grants as advisory |
| ENT-04 | High | `governance.go:270–303` | No self-approval guard; `required_role` never enforced | Reject `decider==requester`; verify decider holds `required_role` |
| ENT-24 | High | `central_commercial.go:254–278` | Transfer `balance_after` from stale snapshot + `status='posted'` filter; bypasses `inventory.PostMovement` opening-balance guard | Post both legs via `inventory.PostMovement` / SUM-based balance |
| ENT-25 | High | `central_commercial.go:199–288`; `0059:80,94` | No product alignment — transfer can move one product out of a tank holding another | Enforce `from.product = to.product = product_id` via composite FK/check |
| ENT-02 | Medium | `0057:50`; `governance.go:78` | `scope_id` unconstrained, unvalidated UUID (no FK) | Validate `scope_id` against the scoped table per `scope_type` |
| ENT-05 | Medium | `0057:3`; rollout/transfer/plan flows | Approval engine not wired into any workflow it claims to gate | Have high-value transitions raise/await approval requests |
| ENT-09 | Medium | `projections.go:15–22` | Insert-only upsert never prunes orphaned KPI rows on revenue-day deletion | Full replace (truncate-by-tenant or anti-join delete) on rebuild |
| ENT-11 | Medium | `central_commercial.go:80–110`; `0059:7` | `region`/`station` rollout with NULL `scope_id` applies to 0 stations yet marks `active` | Require non-NULL `scope_id` for non-tenant scopes |
| ENT-20 | Medium | `central_commercial.go:134–139`; `0059:60` | `target_litres` unvalidated; zero/negative accepted (no CHECK) | Add `CHECK (target_litres > 0)` + handler validation |
| ENT-21 | Medium | `central_commercial.go:165` | Plan lines write-only — no read endpoint | Add a GET endpoint returning plan lines |
| ENT-26 | Medium | `central_commercial.go:253–278` | Transfer balance/guard arithmetic in `float64`, not SQL numeric | Do comparison + stored balance in SQL against ledger SUM |
| ENT-29 | Medium | `enterprise_finance_handlers.go:14–56` | "Consolidated" finance never reconciles GL to station breakdown | Compute and surface the variance / tie-out |
| ENT-03 | Low | `enterprise_governance_handlers.go:138`; `0057:53` | Invalid `scope_type` → 500 not 400 (`23514` unmapped) | Map CHECK violations to 400 |
| ENT-06 | Low | `enterprise_governance_handlers.go:177` | `min_amount` not validated; bad value → 500 | `parseDecimal(MinAmount)` before insert |
| ENT-07 | Low | `0057:103` | No cancel/expire path; stale requests accumulate | Add cancel/expire transitions + sweeper |
| ENT-08 | Low | `server.go:496–501` | `POST /approval-requests` gated by a read permission | Gate raise under a write permission |
| ENT-10 | Low | `projections.go:59–78` | Overview AP/AR/incidents ignore `from`/`to` window | Either window them or label them lifetime |
| ENT-12 | Low | `governance.go:51–74` | No member removal / group archive; `ListGroupMembers` unexposed | Add remove-member, archive, and a members GET |
| ENT-14 | Low | `0057:45` | No grant de-dup or revocation | Unique index + revoke endpoint |
| ENT-16 | Low | `projections.go:93`; finance/export | Unbounded station fan-out (no pagination) | Paginate ranking/export |
| ENT-17 | Low | `central_commercial.go:94–98` | Rollout writes price rows for stations not carrying the product | Join to tanks/products in the activation SELECT |
| ENT-18 | Low | `enterprise_commercial_handlers.go:49–54` | Malformed `effective_from` silently → today | Reject unparseable date with 400 |
| ENT-22 | Low | `central_commercial.go:146–161` | Empty plan releasable (no line-count guard) | Require ≥1 line to release |
| ENT-27 | Low | `0059:91` | "Inter-station" not enforced; same-station transfer allowed | Optionally require differing stations |
| ENT-28 | Low | `central_commercial.go:250` | `dispatched` status unreachable | Add a dispatch handler or drop the state |
| ENT-30 | Low | `enterprise_finance_handlers.go:74–85` | Export fully buffered in RAM + JSON-wrapped | Stream / paginate |
| ENT-32 | Low | `enterprise_queue_handlers.go:33–39` | Six sequential count queries | Collapse to one query |
| ENT-13 | Info | `0057:10` | `kind` free-text unvalidated | Optional enum |
| ENT-15 | Info | `projections.go:64–66` | AR sum is net ledger (correct) but unfiltered for credits/write-offs | Document or filter |
| ENT-19 | Info | `0059:22` | No supersede linkage between rollouts | Mark prior rollout superseded on activate |
| ENT-23 | Info | `0059:43` | `reviewed`/`approved`/`closed` plan states unreachable | Add transitions |
| ENT-31 | Info | `enterprise_finance_handlers.go:82` | "CSV export" delivered as JSON string | Offer `text/csv` download |
| ENT-33 | Info | `enterprise_queue_handlers.go:35` | Queue handler embeds cross-domain SQL in server layer | Move into a repo |
| ENT-34 | Info | `enterprise_queue_handlers.go` | Never-expiring approvals inflate queue total | Tied to ENT-07 |

### Severity counts

- **Critical:** 0
- **High:** 4 — ENT-01, ENT-04, ENT-24, ENT-25
- **Medium:** 8 — ENT-02, ENT-05, ENT-09, ENT-11, ENT-20, ENT-21, ENT-26, ENT-29
- **Low:** 15 — ENT-03, ENT-06, ENT-07, ENT-08, ENT-10, ENT-12, ENT-14, ENT-16, ENT-17, ENT-18, ENT-22, ENT-27, ENT-28, ENT-30, ENT-32
- **Info:** 7 — ENT-13, ENT-15, ENT-19, ENT-23, ENT-31, ENT-33, ENT-34

**Total: 34 findings.**

### Top-5 risks

1. **ENT-25 (High) — Stock transfers have no product alignment** (`central_commercial.go:199–288`): litres of one product can be moved out of a tank that physically holds another, corrupting both per-tank ledgers and the one-product-per-tank physical model. Worst data-integrity defect in the phase.
2. **ENT-24 (High) — Transfer posting abandons the authoritative ledger model** (`central_commercial.go:254–278`): `balance_after` is taken from a stale `status='posted'` snapshot and computed in Go, bypassing `inventory.PostMovement`'s SUM-based balance and opening-balance guard — book stock drifts from the source-of-truth sum.
3. **ENT-01 (High) — Delegated enterprise scopes enforce nothing** (`governance.go:87`): the entire scope-grant subsystem is advisory; `EffectiveStations` is never consulted by any mutation, so every RBAC holder operates tenant-wide. The headline Phase-9 governance feature is cosmetic.
4. **ENT-04 (High) — No self-approval guard and `required_role` unenforced** (`governance.go:270–303`): a single user can raise and approve their own high-value request, and role-restricted policies are not honoured — defeating segregation of duties.
5. **ENT-05 (Medium) — The approval engine is not wired into any workflow it claims to gate** (`0057:3`; rollout/transfer/plan flows): "high-value actions" (price activation, transfer receive, plan release) finalize without ever raising or checking an approval, leaving the engine a free-standing CRUD island.
