# Audit 10 — Customer-Credit & Fleet Domain (Phase 8)

**Repository:** `C:\projects\Actual Projects\fuelGrid os`
**Domain:** Customer master, credit profiles/positions, price agreements, fleet
identity (vehicles/drivers/credentials), fuel authorizations + limits, odometer
& consumption, statements & credit alerts.
**Method:** Read-only, atomic-level trace of every `.go` in `internal/fleet/`,
the six Phase-8 `server` handler files, `customers_handlers.go`, migrations
0050–0056, and `phase8_integration_test.go`. Nothing was modified.

## Scope & Sizes (LOC)

| File | LOC |
| --- | --- |
| `internal/fleet/repo.go` | 46 |
| `internal/fleet/contacts.go` | 85 |
| `internal/fleet/credit_profiles.go` | 159 |
| `internal/fleet/price_agreements.go` | 186 |
| `internal/fleet/vehicles.go` | 119 |
| `internal/fleet/drivers.go` | 168 |
| `internal/fleet/credentials.go` | 160 |
| `internal/fleet/authorizations.go` | 271 |
| `internal/fleet/odometer.go` | 133 |
| `internal/fleet/statements.go` | 188 |
| `services/api/internal/server/customers_handlers.go` | 324 |
| `services/api/internal/server/fleet_credit_handlers.go` | 473 |
| `services/api/internal/server/fleet_identity_handlers.go` | 413 |
| `services/api/internal/server/fleet_authorization_handlers.go` | 280 |
| `services/api/internal/server/fleet_odometer_handlers.go` | 128 |
| `services/api/internal/server/fleet_statement_handlers.go` | 219 |
| `services/api/internal/server/phase8_integration_test.go` | 349 |
| Migrations 0050–0056 (`.up.sql`) | 555 |

Supporting reads: `0034_customers_ar.up.sql`, `0045_customer_invoices.up.sql`,
`internal/receivables/repo.go`, `accounting_handlers.go` (`txAudit`),
`payments_handlers.go` (`actorHolds`), `pricing_handlers.go` (`parseDecimal`),
`banking_handlers.go` (`parseOptDate`), `server.go` (route table).

---

## Flow-by-flow analysis

### 1. Customer master & account lifecycle (Stage 1)

The Phase-6 `customers` table is widened additively by `0050_customer_master`
(`legal_name`, `trading_name`, `tax_id`, `billing_address`, `account_type`,
`default_terms_days`, `notes`) and the status CHECK is broadened to
`prospect|active|on_hold|suspended|closed` plus legacy `inactive|deleted`
(0050:18-20). Create/update flow through `customers_handlers.go`
`handleCreateCustomer` / `handleUpdateCustomer` — both correctly wrap the
business change + `audit.WriteWithOutbox` + commit in one tx (lines 133-164,
189-219). Tenant scoping is via `actor.TenantID`, never a client field.

`handleSetCustomerStatus` (fleet_credit_handlers.go:25-61) validates against
`validCustomerStatuses` (excludes `inactive`/`deleted`, good) and delegates to
`receivables.SetCustomerStatus` (receivables/repo.go:188). **There is no state
machine** — `SetCustomerStatus` updates `status = $3` for any row not `deleted`,
so any status may jump to any other (e.g. `closed` → `active`, or `suspended` →
`active` skipping review). Phase-8 introduces a richer lifecycle but enforces no
transition guard (cf. price-agreement and authorization transitions which *do*
guard `WHERE status = $from`). **FLEET-010 (Medium).**

`account_type` and `default_terms_days` accept arbitrary values — no CHECK and
no Go validation. `account_type` defaults `'standard'` but a caller may send any
string; `default_terms_days` is never used by any credit/authorization code path
(the enforced terms live on `customer_credit_profiles.payment_terms_days`),
making it dead/misleading config. **FLEET-019 (Low/Info).**

### 2. Credit profile, position & holds (Stage 2)

`customer_credit_profiles` (0051) holds the soft terms: `payment_terms_days`,
`grace_days`, `statement_cycle_days`, `risk_category`, `warning_threshold_pct`
(CHECK 0..100), `hold`, `hold_reason`, `review_date`. One profile per customer
(`uq_ccp_customer`). `UpsertCreditProfile` (credit_profiles.go:66-88) is a clean
`INSERT … ON CONFLICT DO UPDATE` with `COALESCE` partial-update semantics. The
handler runs it in an audited tx (fleet_credit_handlers.go:234-253). Note: the
input `RiskCategory` is **never validated** against an enum — any free string is
stored (e.g. `"watch"` in the test is fine, but `"$%^"` is equally accepted).
**FLEET-018 (Low).**

`CreditPosition` (credit_profiles.go:116-159) is the heart of the read model.
Exposure = `SUM(ar_entries.amount)` + `SUM(approved_amount WHERE status =
'approved')` (open authorization holds). `available = credit_limit − ar − held`.
`over_limit` and `warning` are derived in SQL using
`COALESCE(p.warning_threshold_pct, 80)`. `overdue` reads `customer_invoices`
(`outstanding_amount`, `due_date < CURRENT_DATE`, status in
`issued|partially_paid`) — those columns exist (0045). The `hold` flag is
`profile.hold OR status IN ('on_hold','suspended')`. This is correct and matches
the documented design.

**The documented "simplification" is now correct in the read model but broken in
the write model.** The comment says CreditPosition "once referenced
fuel_authorizations" and the current query does include `auth.held`. The bug is
that the *enforcement* of that exposure happens only inside
`RequestAuthorization` — see §4 — while the **actual money-moving credit sale
(`receivables.PostCharge`) ignores both the authorization holds and the profile
hold entirely** (receivables/repo.go:207-229 checks `balance + amount <=
credit_limit` and nothing else). So a customer flagged `hold=true` in their
profile, or `on_hold`, can still be charged on a credit sale; and the
authorization "hold" is invisible to the sale-time limit check. The two systems
are parallel and only loosely coupled. **FLEET-002 (High).**

`SetHold` (credit_profiles.go:104-111) upserts the profile and is audited via
`handleSetCreditHold` (fleet_credit_handlers.go:287-329). `handleCreditPosition`
reads via `s.deps.DB` (pool, not tx) — fine for a read. Routes for read ride
`customer_credit.read`, writes ride `customer_credit.manage` (server.go:414-420).
AuthZ is correct.

### 3. Customer price agreements (Stage 3)

`customer_price_agreements` (0052) supports `fixed|discount|markup`, a draft →
approved → active → expired/cancelled lifecycle, and partial unique indexes
guaranteeing one `active` agreement per `(customer, product, station)` scope and
one tenant-wide. `CreatePriceAgreement`, `TransitionPriceAgreement`,
`ResolveCustomerPrice` (price_agreements.go) are well-formed; the transition
guards `WHERE status = $from` (good state machine) and maps unique-violation on
activate → `ErrConflict` → HTTP 409.

**However `ResolveCustomerPrice` has zero callers in the entire repository.** A
repo-wide grep finds the function defined (price_agreements.go:157) but never
invoked from any sale, authorization, or handler path. The whole point of Stage 3
— "a credit sale resolves its unit price from the customer's active agreement
first, then falls back to retail" (0052 header comment) — is **unimplemented**.
Customers with negotiated fleet prices are billed at retail. The agreement CRUD
and lifecycle exist and are tested, but the resolution is dead code.
**FLEET-001 (High).**

`approved_by` is set only on the `approve` transition (fleet_credit_handlers.go:
448-451); `activate` does not stamp an activator and `expire` is reachable via a
manual route despite the comment implying time-based expiry. There is **no
job/scan that auto-expires agreements** past `effective_to` — `ResolveCustomerPrice`
already date-filters, but since it is never called this is moot; still, the
`status='active'` rows linger forever, polluting the partial-unique index and
blocking a legitimate new active agreement for the same scope. **FLEET-013 (Medium).**

### 4. Fuel authorization decisioning (Stages 7-9) — the critical flow

`fuel_limits` (0054) defines caps by `scope` and `period IN ('transaction',
'day','week','month')` with `max_amount`/`max_litres`, optionally scoped to
customer/vehicle/product. `fuel_authorizations` records the request → approved →
fulfilled/cancelled/voided/expired lifecycle; `uq_fuel_auth_consumed` enforces
one sale per authorization.

`RequestAuthorization` (authorizations.go:75-143) decision order (skipped wholly
when `override`):
1. `CreditPosition` lookup; unknown customer → `unknown_customer` / `ErrDenied`.
2. `account_status` if `suspended|closed`; `account_hold` if `pos.Hold`.
3. credential status must be `active` if a credential id is supplied.
4. `requested_amount <= available` else `insufficient_credit`.
5. per-transaction limit: `EXISTS(... period = 'transaction' AND max_amount IS
   NOT NULL AND … $amt > max_amount)` else `transaction_limit`.

**Only `period = 'transaction'` limits are enforced.** The migration defines and
the `CreateLimit` API accepts `day`/`week`/`month` periods (authorizations.go:233,
fleet_authorization_handlers.go:240-280), but `RequestAuthorization` never sums
prior fulfilled/approved authorizations over a day/week/month window. A daily or
monthly cap is silently inert — fuel can be drawn without bound across many
transactions. This is a core advertised control that does not work.
**FLEET-003 (High).**

`max_litres` limits are **never enforced anywhere** — the per-transaction check
tests `max_amount` only, and there is no litre dimension on the request (the
request carries `requested_amount` money, no litres). Any litre cap is dead.
**FLEET-011 (Medium).**

The per-transaction `EXISTS` only filters on `customer_id`/`vehicle_id`; it
**ignores `product_id`** even though `fuel_limits.product_id` exists and the
request carries `ProductID`. A product-scoped transaction limit applies to *all*
products (or none, since `product_id = $x OR product_id IS NULL` is absent from
the WHERE). **FLEET-014 (Medium).**

**Concurrency race (the prompt's "two concurrent authorizations").** There is no
`SELECT … FOR UPDATE`, advisory lock, or serializable isolation anywhere in
`internal/fleet` (grep confirms zero `FOR UPDATE`). Two concurrent
`RequestAuthorization` calls each run `CreditPosition` in their own
READ-COMMITTED tx, both read the same `available`, both pass the
`amt <= available` check, and both insert an `approved` hold. Combined holds can
exceed the credit limit. The integration test (lines 180-200) only verifies the
*sequential* path. There is no DB constraint catching the overrun (exposure is
not materialized). **FLEET-004 (High).**

**Denial is committed but not audited.** On `ErrDenied` the handler commits the
tx (fleet_authorization_handlers.go:70-77) so the `fuel_authorization_denials`
row persists — good for the risk pipeline — but **no `audit.WriteWithOutbox`**
is written for the denial event, and no outbox event is emitted. Approvals,
fulfillments, cancels are audited; denials (the security-relevant decision,
including *override-attempt* denials) are not in the audit log. **FLEET-009
(Medium).**

**Override gate.** `req.Override` requires `actorHolds(…, "fuel_authorization.
override")` (fleet_authorization_handlers.go:52-55), evaluated *in addition* to
the route's `fuel_authorization.create`. This is correct. But override **skips
every check including `account_status` suspended/closed and credential status** —
an override can authorize fuel for a `closed` account. Whether that is intended
is undocumented; at minimum overriding a hard account-closed state is dangerous.
**FLEET-015 (Low).**

**Fulfillment & the sale-id linkage (the documented integrity risk).**
`FulfillAuthorization` (authorizations.go:184-202) atomically flips
`approved → fulfilled` and stamps `consumed_by` with single-use via the
`WHERE status='approved' AND consumed_by IS NULL` guard + `uq_fuel_auth_consumed`
— the single-use semantics are sound (double-fulfill → `ErrConsumed` → 409,
verified by test 209-216).

The risk is the `consumed_by` value itself. `handleFulfillAuthorization` (lines
145-185) accepts a **raw client-supplied `consumed_by` UUID with no validation**:
there is no FK from `fuel_authorizations.consumed_by` to `sales` (0054 has no
such constraint), the handler never checks the sale exists, belongs to the
tenant, belongs to the same customer/station, or matches the authorized amount.
The test literally fulfills with `uuid.New()` — a sale id that does not exist
(test line 211, 269). Consequences: (a) holds can be "consumed" against phantom
sales, releasing exposure-reservation semantics with no real sale; (b) a sale can
never be reconciled back to its authorization because nothing guarantees the link
points at a real sale; (c) `FleetConsumption` (§6) and any downstream billing
that trusts `consumed_by` are corruptible. **FLEET-005 (High).**

**Fulfillment is never invoked automatically by the sale engine.** Grep shows
`FulfillAuthorization` is reachable *only* from the manual `/fulfill` endpoint.
The Phase-6 credit-sale path (`receivables.PostCharge`) does not fulfill the
authorization. Therefore in normal operation an `approved` hold is **never
released** — exposure permanently inflates by every authorization ever requested,
progressively starving available credit and tripping `over_limit`/warning alerts
falsely. Authorizations also do not auto-expire: `expiry_at` is set to `now()+1h`
(authorizations.go:129) but **no scan transitions expired holds to `expired`**, so
the 1-hour window is cosmetic and stale holds count against exposure forever.
**FLEET-006 (High).**

`cancel`/`void` (authorizations.go:205-221): cancel allowed from
`requested|approved`; void from `approved|fulfilled` and nulls `consumed_by`.
Void of a fulfilled authorization reverses the consumption flag but **does not
reverse the underlying AR charge** (since fulfillment never posted one) — benign
today, but if FLEET-005/006 are fixed by auto-posting, void must also reverse the
charge or it will silently leak money. Noted for the fix. State guards are
correct; bad transition → `ErrBadState` → 409.

### 5. Credentials, PINs (Stages 5-6)

`fuel_credentials` (0053) stores `token_hash`, `masked_label` (never the raw
token), unique per `(tenant, token_hash)`. `customer_drivers.pin_hash` likewise.

**The hash is a single unsalted SHA-256.** `hashSecret` (drivers.go:41-44) is
`sha256(tenantID + ":" + raw)` — one round, no per-record salt, no work factor.
For driver PINs (typically 4 digits, ≤10⁴ space) this is **trivially
brute-forceable**: an attacker with DB read access (or the dump) precomputes
10⁴ hashes per tenant and recovers every PIN instantly. Credential tokens share
the weakness. The convention bar (bcrypt/argon2 for secrets) is not met. This is
the most serious security defect in the domain. **FLEET-007 (High).**

`VerifyDriverPIN` (drivers.go:155-168) compares with `*stored ==
hashSecret(…)` — a **non-constant-time string comparison**, leaking timing.
Lower priority than FLEET-007 (hashes are equal length and SHA-256 dominates) but
still a deviation from `subtle.ConstantTimeCompare`. **FLEET-016 (Low).**
`VerifyDriverPIN` is also itself **dead code** — no handler or authorization path
calls it; the driver PIN is captured and reset but never actually checked at
authorization time (`RequestAuthorization` does not verify a driver PIN). So the
PIN provides no security benefit today. **FLEET-012 (Medium).**

`handleResetDriverPIN` / `handleCreateDriver` accept any PIN string with **no
length/format validation** — empty clears the PIN, but `"1"` or a 100-char string
is accepted (fleet_identity_handlers.go:191-227, drivers.go:138-152). No minimum
entropy is enforced. **FLEET-020 (Low).**

`ValidateCredential` (credentials.go:132-159): resolves token→context, sets
`Usable = status=='active' && !expired`, and best-effort updates `last_used_at`
*only when usable*. Two issues:
- **Enumeration oracle.** An unknown token returns `ErrNotFound` → HTTP 404
  "credential not recognized" (fleet_identity_handlers.go:340-342), while a known
  token returns 200. Combined with the unsalted hash and the single-round lookup,
  an authenticated operator can enumerate which tokens are valid. The lookup also
  isn't constant-time (DB index hit vs miss). The endpoint *is* behind
  `fuel_credential.manage`, limiting exposure to staff, but it is still an oracle.
  **FLEET-017 (Low).**
- The raw token travels in the JSON request body to `/validate` and is hashed
  server-side — acceptable, but there is no rate limiting on the endpoint, so
  brute-forcing a manual_code credential type is feasible. **FLEET-021 (Info).**

`IssueCredential` does not validate `credential_type` in Go (defaults `"card"`,
otherwise relies on the DB CHECK → would surface as a 500 on a bad value rather
than 400). Minor. Binding to vehicle/driver is via nullable FKs with composite
tenant constraints — tenant-safe.

### 6. Vehicles, drivers, odometer & consumption (Stages 4, 10-11)

Vehicle/driver CRUD (vehicles.go, drivers.go) is standard, tenant-scoped, audited
via `fleetStatusTransition`. `uq_vehicles_registration` is partial on
`status <> 'retired'` (good — retired regs can be reused). `allowed_product_ids`
and `assignment_rule` are stored on drivers but, like the PIN, are **never
consulted by `RequestAuthorization`** — product-allow-listing and driver
assignment rules are inert. **FLEET-012 (Medium)** (same root cause: the
authorization decision ignores driver/vehicle policy).

**`RecordOdometer` violates two core conventions** (odometer.go:25-61):
1. **No transaction / no audit atomicity.** It runs three *separate*
   non-transactional `r.pool` queries (read `max(reading)`, cast `$1::numeric`,
   then INSERT). The handler then opens a *second, separate* "best-effort" tx for
   audit and **ignores its error** (`_ = audit.WriteWithOutbox(...)`,
   fleet_odometer_handlers.go:54-62). So a reading can be persisted with **no
   audit/outbox record at all** if the audit tx fails — directly contrary to "one
   tx wraps business change + audit + outbox." **FLEET-008 (High).**
2. **`float64` arithmetic on a measured quantity.** It scans `max(reading)` and
   the cast reading into Go `float64` and compares `cur <= *last`
   (odometer.go:26-45). Readings are `numeric(14,1)`; pulling them through
   `float64` for the monotonicity decision is exactly the float discipline the
   house rules forbid. (The stored `distance_since` is computed in SQL, which is
   fine, but the *validation decision* is float-based and can misclassify near
   ties.) **FLEET-008 (High)** (same finding, second facet).
3. **Monotonicity race.** `max(reading)` is read outside any lock, so two
   concurrent readings can both pass/fail validation inconsistently and the
   `distance_since` subquery re-reads `max` independently of the validation read.
   Subsumed under FLEET-008.

`FleetConsumption` (odometer.go:97-133) joins fulfilled authorizations (by
`created_at::date BETWEEN`) and odometer min/max per vehicle. It reports
`amount_total` (money) but **labels itself "consumption (km/litre)" while never
computing km/litre** — distance is `max−min` odometer, amount is money, and there
is no litres column anywhere (authorizations track money, not litres), so the
advertised "consumption km/litre" metric (Stage 11) **cannot be computed** and is
absent from the output. The DTO has `Distance` and `AmountTotal` but no
efficiency ratio. **FLEET-022 (Low/Info).** Also, fulfilled-authorization amount
is summed by `created_at::date` of the authorization, not the fulfillment date,
slightly mis-bucketing fuelings authorized late in a period but fulfilled later.

### 7. Statements & credit alerts (Stages 12-13)

`GenerateStatement` (statements.go:39-60) computes opening = `SUM(amount) WHERE
d < start`, charges = `SUM(amount) WHERE between AND type='charge'`, payments =
`−SUM(amount) WHERE between AND type='payment'`, closing = `SUM(amount) WHERE
d <= end`. Period math is correct and balances tie out (test: closing 1500 with a
1500 charge). Two gaps:
- **No `period_start <= period_end` validation** in the handler
  (fleet_statement_handlers.go:46-51) — an inverted range yields a meaningless
  statement (opening uses `< start`, charges use empty `BETWEEN start AND end`),
  silently producing a zero-charge statement. **FLEET-023 (Low).**
- **No supersede logic.** The migration's status enum includes `superseded`
  (0056:20) and the header comment implies regeneration supersedes the prior
  statement, but `GenerateStatement` only ever inserts `draft`; re-generating the
  same period creates **duplicate overlapping statements** with no uniqueness
  constraint and never marks an older one `superseded`. `IssueStatement` flips
  draft→issued with a guard (good) but two issued statements for the same period
  can coexist. **FLEET-013 (Medium)** (grouped with the lifecycle-gap finding).

`ScanCreditAlerts` (statements.go:124-154) raises `over_limit` (AR+holds >
limit, severity high) and `overdue` (past-due invoices, medium) idempotently via
`uq_credit_alert_open` partial unique index `ON CONFLICT … DO NOTHING`. Correct
and deterministic; the rescan test confirms no duplication. But:
- The scan **does not raise the `warning`-threshold alert** that the credit
  profile's `warning_threshold_pct` exists to drive — `CreditPosition.Warning`
  is computed for the read API but no alert is generated at the warning band,
  only at full over-limit. The threshold config is half-wired. **FLEET-024 (Low).**
- The header comment says alerts "can place a customer on hold," but
  `ScanCreditAlerts`/`TransitionAlert` **never set `customers.status` or the
  profile `hold`** — there is no auto-hold action. Advertised, not implemented.
  **FLEET-024 (Low)** (same theme).

`TransitionAlert` (statements.go:176-188) updates status/reason/assignee for any
id with no from-state guard — acknowledging an already-resolved alert silently
re-opens-style mutates it (no `WHERE status …`). The unique index only covers
`open|acknowledged`, so a resolve→acknowledge bounce is possible. Minor.
**FLEET-025 (Low).**

### 8. Tenant isolation (IDOR) review

All repo methods take `tenantID` first and every query filters
`WHERE tenant_id = $1`; composite FKs `(tenant_id, …)` are present on all Phase-8
tables. Spot checks: `GetAuthorization`, `GetVehicle`, `GetDriver`,
`GetPriceAgreement`, `ListContacts`, `CreditPosition` all scope by tenant. Status
transitions and deletes scope by `(tenant_id, id)`. **No cross-tenant IDOR found
in the app layer.** One soft gap: handlers like `handleCreateVehicle`,
`handleCreatePriceAgreement`, `handleIssueCredential`, `handleRequestAuthorization`
accept a body `customer_id`/`vehicle_id`/`station_id` and rely on the composite
FK to reject cross-tenant ids — which works (FK violation → 400), but a
*same-tenant* mismatch (e.g. a vehicle belonging to customer A attached to an
authorization for customer B within the same tenant) is **not validated**: the
authorization FKs only enforce tenant, not that the vehicle/driver/credential
belongs to the named customer. So one tenant's operator can cross-link entities
between that tenant's own customers. **FLEET-026 (Low).**

### 9. AuthZ on mutating routes

Every mutating route in server.go:406-491 is wrapped with `requirePermission` (or
`requirePermissionHeld` for reads). Mapping verified: status/contacts →
`customer.manage`; profile/hold → `customer_credit.manage`; pricing →
`customer_pricing.manage|.approve`; vehicles/drivers → `customer.manage`;
credentials → `fuel_credential.issue|.manage`; authorizations →
`fuel_authorization.create|.cancel`; limits → `fuel_limit.manage`; statements →
`customer_statement.issue`; alerts → `customer_credit_alert.manage`; override is
additionally gated in-handler. **No unprotected mutating route found.** One
oddity: `/fleet/credentials/validate` is a POST classified as a forecourt read
but sits under `fuel_credential.manage` (write-ish) — defensible but inconsistent
with its read nature.

### 10. Misc / code quality

- `nullableMoney`, `money0`, `deref` helpers are sane; money is consistently
  passed as decimal strings and cast `::numeric` in SQL **except** the odometer
  float path (FLEET-008) and the handler-level `parseDecimal` validators which
  parse to `float64` *only for validation* (not for storage) — acceptable but the
  validator would reject a perfectly valid 18-digit decimal that overflows
  float64 mantissa (edge). **FLEET-027 (Info).**
- `parseOptDate` (banking_handlers.go:950) **silently returns nil on a malformed
  date**, so `effective_to: "garbage"` becomes "no end date" and a bad
  `review_date` is silently dropped rather than 400'd — silent data corruption in
  price agreements / credit profiles. **FLEET-028 (Low).**
- N+1: none found; list endpoints use single set-based queries.
  `FleetConsumption` is one query with two LEFT JOIN subqueries (good).

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
| --- | --- | --- | --- | --- |
| FLEET-001 | High | price_agreements.go:157 | `ResolveCustomerPrice` has zero callers — negotiated fleet prices never applied to sales (Stage 3 unimplemented) | Wire resolution into the credit-sale price path and snapshot the applied price on the sale |
| FLEET-002 | High | receivables/repo.go:207-229; credit_profiles.go:116 | Credit sale (`PostCharge`) enforces only raw `credit_limit`; ignores authorization holds, profile `hold`, and `on_hold/suspended` status that CreditPosition computes | Have the sale path consume the authorization and/or check `CreditPosition.Hold`/exposure before charging |
| FLEET-003 | High | authorizations.go:113-126 | Only `period='transaction'` fuel limits enforced; `day/week/month` caps are inert | Sum prior approved+fulfilled authorizations over each period window and enforce strictest |
| FLEET-004 | High | authorizations.go:75-143 | No row lock/serializable isolation — two concurrent authorizations both pass the available-credit check and over-reserve | `SELECT … FOR UPDATE` on the customer (or advisory lock per customer) around position+insert |
| FLEET-005 | High | fleet_authorization_handlers.go:145-185 | `consumed_by` (sale id) accepted unvalidated; no FK/tenant/customer/amount check — phantom-sale fulfillment | Validate the sale exists, same tenant+customer, amount matches; add FK |
| FLEET-006 | High | authorizations.go:129,184; (no expiry job) | Holds never auto-released (fulfill is manual/never called by sales) and never auto-expire — exposure inflates permanently | Auto-fulfill on sale + scheduled job to expire `approved` past `expiry_at` |
| FLEET-007 | High | drivers.go:41-44 | PINs & credential tokens hashed with single unsalted SHA-256 — 4-digit PINs trivially brute-forced | Use bcrypt/argon2id with per-record salt + work factor |
| FLEET-008 | High | odometer.go:25-61; fleet_odometer_handlers.go:54-62 | `RecordOdometer` runs outside a tx, audits in a separate best-effort tx (error ignored), and uses `float64` for the monotonicity decision | Wrap read+insert+audit in one tx; do the comparison in SQL (numeric) |
| FLEET-009 | Medium | fleet_authorization_handlers.go:70-77; authorizations.go:65 | Authorization *denials* (incl. override-attempts) are not written to the audit log/outbox, only the denial table | Emit `audit.WriteWithOutbox` for denial decisions |
| FLEET-010 | Medium | receivables/repo.go:188; fleet_credit_handlers.go:25 | No state machine on customer status — any status → any (e.g. closed→active) | Enforce allowed transitions with `WHERE status IN (…)` |
| FLEET-011 | Medium | authorizations.go:113-126 | `max_litres` limits never enforced (no litre dimension on request/check) | Track litres on the request and enforce litre caps, or drop the column |
| FLEET-012 | Medium | drivers.go:155; authorizations.go | Driver PIN verify, `allowed_product_ids`, `assignment_rule` all dead — authorization never checks driver/vehicle policy or PIN | Enforce driver PIN + product-allowlist + assignment rule in `RequestAuthorization` |
| FLEET-013 | Medium | statements.go:39; price_agreements.go | No statement supersede/dedupe; no agreement auto-expiry — stale `active`/duplicate rows accumulate | Mark prior period statement `superseded`; add unique-per-period guard; expire agreements past `effective_to` |
| FLEET-014 | Medium | authorizations.go:113-126 | Per-transaction limit ignores `product_id` despite the column existing | Add `(product_id = $x OR product_id IS NULL)` to the limit predicate |
| FLEET-015 | Low | authorizations.go:89; fleet_authorization_handlers.go:52 | Override bypasses *all* checks including suspended/closed account | Keep hard account-status/credential checks even under override, or document intent |
| FLEET-016 | Low | drivers.go:167 | PIN compare is non-constant-time `==` | Use `subtle.ConstantTimeCompare` (moot if FLEET-012/007 fixed) |
| FLEET-017 | Low | credentials.go:148; fleet_identity_handlers.go:340 | Validate endpoint returns 404 for unknown vs 200 for known token — enumeration oracle, non-constant-time | Return a uniform "not usable" result regardless of existence; rate-limit |
| FLEET-018 | Low | credit_profiles.go:66 | `risk_category` accepts any free string (no enum) | Validate against an allowed set |
| FLEET-019 | Low/Info | customers_handlers.go:83; 0050:11-12 | `account_type` unvalidated; `default_terms_days` is dead config | Validate/enumerate or remove |
| FLEET-020 | Low | fleet_identity_handlers.go:191; drivers.go:138 | No PIN length/format validation | Enforce min length/numeric policy |
| FLEET-021 | Info | fleet_identity_handlers.go:319 | No rate limiting on credential validate (brute-force of manual_code) | Add per-actor/IP throttle |
| FLEET-022 | Low/Info | odometer.go:83-133 | "Consumption" report computes no km/litre (no litre dimension); buckets by auth `created_at` | Add litres to model; bucket by fulfillment date; compute efficiency |
| FLEET-023 | Low | fleet_statement_handlers.go:46 | No `period_start <= period_end` check — inverted range yields silent zero statement | Validate ordering, return 400 |
| FLEET-024 | Low | statements.go:124-154 | Scan never raises a warning-band alert nor auto-holds, despite config/comments | Add warning-threshold alert + optional auto-hold action |
| FLEET-025 | Low | statements.go:176-188 | `TransitionAlert` has no from-state guard | Guard valid transitions |
| FLEET-026 | Low | authorizations.go:131; fleet_identity_handlers.go:45 | FKs enforce tenant but not that vehicle/driver/credential belongs to the named customer (intra-tenant cross-link) | Validate ownership in-handler/repo |
| FLEET-027 | Info | pricing_handlers.go:230 | `parseDecimal` validates via `float64` — could misjudge extreme-precision decimals | Validate with a decimal/big.Rat parser |
| FLEET-028 | Low | banking_handlers.go:950 | `parseOptDate` silently drops malformed dates (becomes NULL) | Distinguish "absent" from "invalid"; 400 on invalid |

### Severity counts

- **Critical:** 0
- **High:** 8 (FLEET-001..008)
- **Medium:** 6 (FLEET-009, 010, 011, 012, 013, 014)
- **Low:** 11 (FLEET-015, 016, 017, 018, 020, 023, 024, 025, 026, 028, plus 019 low/022 low)
- **Info:** ~3 (FLEET-021, 027, and the low/info facets of 019/022)

(Several IDs span a low/info boundary; counted once at their dominant severity.)

### Top-5 risks

1. **FLEET-007** — PINs/tokens under unsalted single-round SHA-256: full PIN
   recovery from any DB read. (drivers.go:41)
2. **FLEET-006 + FLEET-005** — Authorization holds never released/expired and
   `consumed_by` is unvalidated: exposure inflates forever and fulfillment can
   point at phantom sales, corrupting the credit position the whole system relies
   on. (authorizations.go:129/184; fleet_authorization_handlers.go:145)
3. **FLEET-002** — The actual credit *sale* ignores holds, profile hold, and
   on_hold/suspended status; the authorization gate is advisory-only.
   (receivables/repo.go:207)
4. **FLEET-003 + FLEET-004** — Day/week/month limits are inert and there is no
   locking, so concurrent requests over-reserve and periodic caps don't apply.
   (authorizations.go:113-143)
5. **FLEET-001** — Negotiated fleet pricing (`ResolveCustomerPrice`) is never
   called; credit customers are billed at retail. (price_agreements.go:157)
