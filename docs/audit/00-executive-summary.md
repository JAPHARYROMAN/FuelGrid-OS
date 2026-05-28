# FuelGrid OS — Codebase Audit: Executive Summary

**Audit date:** 2026-05-29
**Scope:** Full codebase, atomic-level. ~59,000 LOC: 182 Go files (~39k LOC) across 33 `internal/` domain packages + 75 server handler files; 63 migrations (~5.5k SQL LOC, 105 tables); 69 TS/TSX files (~14k LOC); the `@fuelgrid/sdk` (~4.4k LOC) and `@fuelgrid/ui` packages.
**Method:** 18 independent read-only audit agents, one per domain, each opening every file in scope and citing `file:line`. This document synthesizes their findings.

> **Status:** 14 of 18 section reports are complete (sections 01–14). Sections **15 (frontend foundation), 16 (frontend pages), 17 (SDK/UI), 18 (testing & coverage)** were interrupted by a session limit and will be completed separately. Frontend/test findings below are drawn from preliminary signals and the architecture pass; treat them as provisional until those sections land.

---

## 1. Verdict

FuelGrid OS is an **ambitious, well-organized skeleton with a genuinely excellent database schema and clean Go layering — wrapped around business logic that is, in its money-handling core, not yet trustworthy.** The codebase *looks* far more finished than it behaves. It compiles, boots, serves ~237 routes, and passes its own tests — but those tests are happy-path smoke tests that assert almost none of the invariants that matter for a financial system.

The headline problem is a credibility gap between **presentation and substance**:

- The **data model earns an A−** (near-perfect type discipline, composite tenant FKs, UUID PKs everywhere). This is the strongest part of the system and a real asset.
- The **accounting engine, the part a fuel business would actually be audited on, does not hold together**: the balance sheet does not balance, sales revenue is never recognized end-to-end, two parallel AR ledgers disagree, and "immutability" of posted journals is a code comment, not a constraint.
- **Tenant isolation works today only by luck of consistent app-layer `WHERE` clauses.** Row-Level Security is defined on every table and *inert at runtime* — the API connects as the table owner and never sets the tenant GUC, so RLS provides zero defense-in-depth.
- **The "intelligence," "governance," and "enterprise control" layers are substantially cosmetic** — risk rules are disconnected from detection, the approval engine is wired into nothing, enterprise scope grants enforce nothing, and several flagship functions have zero callers.

This is **not** a system to put real money through yet. It is, however, a recoverable one: the bones (schema, layering, transactional outbox pattern, auth primitives) are sound, and most defects are concentrated, nameable, and fixable.

**Overall grade: C− / "advanced prototype."** Backend architecture B+, data model A−, security posture C, financial correctness D, feature authenticity C−, test safety net F.

---

## 2. Findings at a glance (sections 01–14)

| Severity | Count |
|---|---|
| **Critical** | 6 |
| **High** | 65 |
| **Medium** | 98 |
| **Low** | 122 |
| **Info** | 39 |
| **Total (14 of 18 sections)** | **330** |

The 6 Criticals:

1. **Reconciliation & inventory run on `float64` end-to-end** — variance, tolerance, and seal math in floating point (`internal/inventory/repo.go:80`, `reconciliation_handlers.go:45`). The `0.0005` seal threshold is a fingerprint of float residue in a money/litre path. *(INV-001)*
2. **Procurement divide-by-zero on the cost path** — `landed_cost_per_litre / volume_litres` with no guard in the repo (`internal/inventory/deliveries.go:228`). *(PROC-12)*
3. **Weighted-average COGS is permanently corrupted by reversed deliveries** — the cost average omits the `status='posted'`/`supersedes_id IS NULL` filter, so a reversed delivery's original litres stay in the basis forever (`internal/revenue/repo.go:104`). Corrupts COGS, margin, stock value, and the below-cost price guard. *(REV-01)*
4. **Risk engine "pause" is a no-op and rules are disconnected from detection** — `RunDetection` ignores rule status and runs three hardcoded SQL packs regardless of configured thresholds/lookback/tuning (`internal/risk/governance.go:77`, `alerts_detect.go:38`). *(RISK criticals)*
5. **The event/outbox bus silently drops failed events** — `InProcessBus.Publish` always returns `nil`, so failed handlers are marked published and lost; the only consumer is a log line (`internal/events/bus.go:70`). *(INFRA-03)*
6. **RLS is inert at runtime** — defined on ~51 tables but never activated because `database.WithTenant` is never called in the request path and the API connects as the owning role (`internal/database/tenant.go:29`, `migrations/0005_rls.up.sql:12`). Counted as Critical by the infra pass. *(INFRA-01 / AUTH-25)*

---

## 3. Systemic, cross-cutting themes

These patterns recur across many domains and matter more than any single finding. Fixing them is higher-leverage than chasing individual line items.

### T1 — `float64` has leaked into the money/quantity core (systemic, High→Critical)
The house rule is explicit: money/rate/litre values are decimal **strings**, arithmetic in **SQL**, never float. The data model honors this perfectly. The **Go code does not**, in roughly half the domains:

- Inventory & reconciliation: float end-to-end (Critical INV-001).
- Operations/shift-close: expected-cash, litres-sold, variance thresholds computed in Go float (OPS-001).
- Org/assets: prices, capacities, density scanned to `float64` (ORG-04).
- Procurement: litres flow as `float64` through PO→receipt→invoice (PROC-06).
- Fleet: odometer monotonicity decided in float (FLEET-008).

By contrast, **revenue, accounting, and payments largely kept the discipline** (SQL `numeric`, strings), which is why their *engine* math is mostly right even where their *business logic* is wrong. The float leakage is concentrated in the operational/physical layer — exactly where litre→money conversion happens — so it silently poisons reconciliation, COGS, and cash variance.

### T2 — The accounting/treasury layer is not a coherent ledger (systemic, High)
Individually plausible handlers do not compose into a correct double-entry system:
- **Balance sheet never balances**: net income isn't rolled into equity and there's no close-to-retained-earnings entry (ACCT-016).
- **Sales revenue is never recognized through the live path**: cash-reconciliation approval credits `sales_clearing` but nothing debits it; the clearing liability grows unbounded (PAY-013).
- **Two disjoint AR ledgers**: credit tenders write `ar_entries` but post no journal, so the operational AR and the GL/customer-invoice AR can never reconcile (PAY-003).
- **Balance and immutability of journals are enforced only in Go**, not by DB triggers (ACCT-001); posted entries are mutable in principle.
- **Petty-cash adjustments/transfers move balances with no journal** (ACCT-012).
- Period close checks only the status edge — it never verifies the close checklist's blockers or that the period balances (ACCT-004).

### T3 — Tenant isolation has no backstop (systemic, Critical posture / no confirmed live leak)
RLS is comprehensively written and **completely inert** (T-themes 6 above). The saving grace, confirmed by every domain agent, is that app-layer `tenant_id` predicates are present on essentially every query — **no live cross-tenant IDOR was found**. But the entire isolation guarantee is one forgotten `WHERE` clause away from a breach, with no second line of defense. The intra-tenant equivalent (entity A referencing entity B that belongs to a different customer/station within the same tenant) **is** under-enforced in places (FLEET-026, several unFK'd finance columns DB-001/002/003).

### T4 — No separation of duties anywhere (systemic, High)
Every approval workflow can be self-completed by a single actor: open→attend→close→**approve** a shift (OPS-002); create→submit→**approve**→post an expense or supplier payment (ACCT-009); raise and **approve** one's own enterprise approval request (ENT-04); submit and **approve** one's own cash reconciliation (PAY-014). For a fraud-control product this is a foundational gap.

### T5 — Compute-before-transaction TOCTOU (systemic, High)
A repeated shape: validate/compute against the connection pool, then mutate in a *later* transaction with no row lock. Found in reconciliation seal (INV-014), shift close (OPS-008), day close & approval (OPS-007/009), credit-limit check (PAY-001), fuel-authorization limits (FLEET-003). Concurrent requests corrupt frozen snapshots, balances, and overshoot limits.

### T6 — Flagship features are cosmetic or wired to nothing (systemic, High)
A large fraction of the "Phase 8/9/10" surface is scaffolding:
- Risk rules don't drive detection; scoring is fixed constants; pause is a no-op; `RecordFeedback` is dead code (Section 12).
- Enterprise **scope grants enforce nothing** — `EffectiveStations` feeds one read endpoint; every RBAC holder still acts tenant-wide (ENT-01).
- The **approval engine is connected to no workflow** — high-value actions finalize without ever raising a request (ENT-05).
- Central **procurement-plan release creates no purchase orders** (documented gap, PROC).
- **Negotiated fleet pricing (`ResolveCustomerPrice`) has zero callers** — fleet price agreements never affect a sale (FLEET-001).
- Fuel **authorization holds are never enforced on the real credit sale** (`PostCharge` checks only raw `credit_limit`) and are never auto-released (FLEET-002/005/006).

### T7 — Secrets, credentials, and a seed backdoor (High)
- Empty `AUTH_PASSWORD_PEPPER` only *warns* on non-dev boot, then starts (ARCH-02).
- `seed` provisions a **known-password `system_admin` with no non-dev guard** — a production backdoor if ever run there (INFRA-22).
- Raw **password-reset token logged at Info alongside the email** (AUTH-16).
- **TOTP secrets stored plaintext**; **driver PINs and credential tokens hashed with unsalted single-round SHA-256** (AUTH-13, FLEET-007).
- Sessions resolve from Redis only; revocation is best-effort and can lag up to 12h (AUTH-04/10); MFA has no throttle/lockout/replay protection (AUTH-10).

### T8 — Availability / DoS surface (Medium→High)
No global request-body cap (ARCH-11/INFRA-06), no pagination on list endpoints (INFRA-07), no statement/lock/idle timeouts on a 25-connection pool (INFRA-12). Several memory- and connection-exhaustion vectors.

### T9 — The safety net is missing (High)
Tests are happy-path smoke tests that **seed via raw SQL (bypassing the domain layer)** and assert almost no financial invariant — not debits==credits, not A=L+E, not credit-limit enforcement, not tenant isolation. There are **zero frontend and zero SDK tests** (which is exactly why the `fetch` "Illegal invocation" bug shipped and broke every browser call). The OpenAPI spec stops at Phase 3, leaving ~140 routes undocumented (ARCH-01). *(Section 18, pending, will quantify.)*

### T10 — The bright spots (keep these)
Credit where due, because the recovery plan leans on them:
- **Schema (A−):** zero `float`/`money`/`serial`/naive-`timestamp` across 105 tables; money `numeric(14,2)`/rate `(14,4)`/litres `(14,3)` applied consistently; composite `(tenant_id, id)` FKs rigorously propagated through the operational layers; no global index/constraint name collisions.
- **Go layering:** zero HTTP leakage into `internal/<domain>`; clean composition root; textbook transactional-outbox table design with `FOR UPDATE SKIP LOCKED`; correct graceful shutdown.
- **Auth primitives:** argon2id with sound params and constant-time compare; 256-bit session tokens sha256-hashed at rest; generic login errors; constant-time platform-token check.
- **Where the money discipline *was* followed** (revenue/accounting/payments engines), the SQL is correct: debits==credits enforced in SQL, period gating present, solid idempotency backstops (`uq_sales_shift_nozzle`, lock-aware upserts).

---

## 4. Recommended remediation order

Sequenced by risk-reduction per unit effort. Detail and `file:line` for every item are in `99-findings-register.md` and the per-section reports.

**P0 — Do before any real data or pilot:**
1. **Activate RLS** (call `WithTenant` per request; connect as a non-owner role) *or* formally accept app-layer-only isolation and add a CI test that every query is tenant-scoped (T3 / INFRA-01).
2. **Purge `float64` from money/litre paths** — scan-and-replace to decimal strings + SQL arithmetic, starting with inventory/reconciliation/procurement (T1; INV-001, PROC-06/12, OPS-001).
3. **Fix the COGS basis** (add the `status='posted' AND supersedes_id IS NULL` filter; convert the cumulative average to a true moving average that decrements on consumption) (REV-01/02).
4. **Make the ledger actually balance**: roll net income into equity, recognize sales revenue end-to-end, unify the two AR ledgers, add DB balance + immutability triggers, journal every petty-cash movement (T2).
5. **Fix the outbox** so failed handlers aren't marked published (INFRA-03), and **remove/guard the seed backdoor** + reject empty pepper in prod (T7).

**P1 — Before launch:**
6. Add **separation-of-duties** guards (`approved_by != created_by`, role checks) across all approval flows (T4).
7. Close the **TOCTOU** windows with `SELECT … FOR UPDATE` inside the mutating tx (T5).
8. Enforce **fuel-authorization holds and full credit exposure** on the real sale path; add row locks to limit checks (FLEET-002/003, PAY-001).
9. Harden **credential storage** (encrypt TOTP secrets, salt+stretch PINs/tokens), stop logging reset tokens, add MFA throttling (T7).
10. Add **request-body caps, pagination, and DB timeouts** (T8).

**P2 — Honesty & durability:**
11. Either **finish or clearly label** the cosmetic features (risk rules→detection wiring, enterprise scope enforcement, approval-engine wiring, fleet price application, procurement-plan→PO hand-off) (T6).
12. Build a **real test suite**: domain-level fixtures, negative/authz/concurrency/money-invariant assertions, frontend + SDK tests, OpenAPI contract tests, migration up/down tests (T9).
13. Add the **missing finance-layer FKs** (payables/journals bare UUIDs) to lift the schema to a clean A (DB-001/002/003/005).

---

## 5. How to read this report

- **`99-findings-register.md`** — the consolidated, prioritized list of all Critical and High findings with `file:line` and fixes, deduplicated across sections.
- **Sections `01`–`14`** — the full per-domain deep dives (each 3,600–5,500 words) with complete findings tables (Critical→Info). ID prefixes: `ARCH-`, `AUTH-`, `ORG-`, `OPS-`, `INV-`, `PROC-`, `REV-`, `PAY-`, `ACCT-`, `FLEET-`, `ENT-`, `RISK-`, `INFRA-`, `DB-`.
- **Sections `15`–`18`** — frontend foundation, frontend pages, SDK/UI, and testing (`WEB-`, `PAGE-`, `SDK-`, `TEST-`) — pending completion after the session reset.
- **`README.md`** — the index and the running severity tally.
