# Phase 8 - Customer Credit & Fleet Fuel OS

The phase where FuelGrid OS becomes the operating system for customers who buy fuel on account, and for fleets that need controlled fueling by vehicle, driver, credential, station, product, and limit.

Phase 7 gives the platform accounts receivable, customer invoices, customer payments, and aging. Phase 8 builds the operational layer in front of that finance backbone: customer profiles, credit limits, vehicles, drivers, fuel authorizations, fuel cards, QR/RFID workflows, odometer capture, fleet consumption reports, customer statements, and credit risk alerts.

Phase 8 does not replace the Phase-6 sales engine or the Phase-7 receivables ledger. It decides whether a credit/fleet sale is allowed, captures the operational evidence around that sale, and hands clean billable facts to the revenue and finance layers.

## Stack decisions carried forward

All Phase-8 work continues the patterns locked in earlier phases:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps the business change, audit entry, and outbox event |
| Tenant scoping | Every repo query takes `tenantID` first; RLS remains the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, ...)` composite FKs onto parent unique keys |
| Authorization | `requirePermission(code, scopeExtractor)` for URL-scoped routes, `authorizeStation(...)` for station checks, and `requirePermissionHeld(code)` for tenant-wide customer administration |
| Numeric precision | Money `numeric(14, 2)`; limits and balances never use `float64`/JS `number`; litres remain `numeric(14, 3)` |
| Corrections | Issued authorizations and posted credit sales are corrected by void/reversal workflows, not destructive edits |
| Events | Operational credit/fleet events are emitted through outbox and consumed idempotently by sales, finance, and risk layers |
| Frontend | shadcn-style primitives in `@fuelgrid/ui`; TanStack Query over the hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 8:

| Concern | Convention |
|---|---|
| Credit source of truth | Customer account status, credit limit, available credit, and hold state live in the credit domain. Posted AR balances remain the finance source of truth. |
| Authorization | A fuel authorization is a controlled permission to fuel. It is not a sale until Phase 6 records the sale against it. |
| Credential security | Fuel cards, QR codes, RFID tokens, and PINs store only hashed or tokenized identifiers. Raw credentials are never logged. |
| Fleet dimensions | Credit and fleet sales must carry customer, vehicle, driver, credential, authorization, station, product, nozzle, attendant, odometer, and price agreement dimensions where applicable. |
| Limits | Limits can exist at customer, vehicle, driver, product, station, day, week, month, and per-transaction levels. The strictest applicable limit wins. |
| Risk handoff | Credit risk alerts in this phase are deterministic operational controls. Pattern scoring and fraud intelligence deepen in Phase 10. |

---

## Category A - Customer credit foundation

The customer account and credit model that all fleet fueling depends on.

### Stage 1 - Customer master

**Goal:** A tenant maintains customer accounts with billing, contact, and operational metadata.

- [ ] Evolve or extend Phase-7 minimal customer records into `customers` with code, legal name, trading name, tax ID, billing address, contacts, status, account type, default terms, and notes
- [ ] Add customer contacts with role, email, phone, statement preference, and notification preference
- [ ] Add customer account lifecycle: `prospect -> active -> on_hold -> suspended -> closed`
- [ ] Permission `customer.manage` for tenant-wide customer setup; `customer.read` for read access
- [ ] Repo + handlers + SDK: list, get, create, update, status transition, contacts CRUD
- [ ] Audit + outbox: `customer.created`, `customer.updated`, `customer.status_changed`

**Done when:** A customer can be created, maintained, placed on hold, suspended, and closed without losing billing history.

---

### Stage 2 - Credit limits and terms

**Goal:** A customer has enforceable credit terms and a real-time available credit calculation.

- [ ] Migration `customer_credit_profiles` with credit limit, payment terms, grace days, statement cycle, risk category, hold reason, and review date
- [ ] Compute available credit from Phase-7 AR: open invoices, unapplied payments, credit notes, pending unposted credit sales, and active authorizations
- [ ] Limit checks for hard limit, soft warning threshold, overdue invoice hold, manual hold, and suspended account
- [ ] Permissions: `customer_credit.manage`, `customer_credit.override`, `customer_credit.read`
- [ ] Endpoint `GET /api/v1/customers/{id}/credit-position` returning limit, exposure, available credit, overdue amount, holds, and warnings
- [ ] Audit + outbox: `customer_credit.updated`, `customer_credit.hold_applied`, `customer_credit.hold_released`

**Done when:** A sale authorization is denied when the customer's available credit is exhausted or the account is on hold, unless a permitted override is recorded.

---

### Stage 3 - Customer pricing and contracts

**Goal:** Fleet and credit customers can have controlled pricing agreements without bypassing Phase-6 pricing.

- [ ] Migration `customer_price_agreements` and `customer_price_agreement_lines` with customer, product, station/region scope, price type, fixed price, discount, markup, effective dates, approval state, and version
- [ ] Price agreement lifecycle: `draft -> approved -> active -> expired -> cancelled`
- [ ] Phase-6 pricing integration: credit sale pricing resolves from active customer agreement first, then configured retail price
- [ ] Guard overlapping active agreements for the same customer/product/scope/effective date
- [ ] Permissions: `customer_pricing.manage`, `customer_pricing.approve`
- [ ] Audit + outbox: `customer_price_agreement.created`, `customer_price_agreement.approved`, `customer_price_agreement.activated`

**Done when:** A customer-specific PMS price or discount can be approved, applied to future credit sales, and snapshotted on the sale.

---

## Category B - Fleet identity

The vehicles, drivers, and credentials that identify who is allowed to fuel.

### Stage 4 - Vehicles

**Goal:** Customer fleet vehicles are registered with product, capacity, odometer, and authorization rules.

- [ ] Migration `customer_vehicles` with customer, registration/plate, fleet number, VIN, vehicle type, default product, tank capacity, odometer requirement, status, and metadata
- [ ] Vehicle lifecycle: `active -> on_hold -> retired`
- [ ] Product compatibility guard: vehicle can only authorize products allowed by its profile unless overridden
- [ ] Import/export support for bulk fleet setup from CSV
- [ ] Repo + handlers + SDK: list by customer, get, create, update, status transition, bulk import validation
- [ ] Audit + outbox: `customer_vehicle.created`, `customer_vehicle.updated`, `customer_vehicle.status_changed`

**Done when:** A fleet vehicle can be created, tied to a product rule, held or retired, and included in authorization checks.

---

### Stage 5 - Drivers

**Goal:** Customer drivers are managed separately from vehicles, with identity and status controls.

- [ ] Migration `customer_drivers` with customer, name, phone, license/reference number, PIN hash, status, allowed products, and optional assigned vehicles
- [ ] Driver lifecycle: `active -> on_hold -> inactive`
- [ ] PIN handling: store only salted hashes; never return PIN values in APIs or logs
- [ ] Optional driver-to-vehicle assignment rules: any vehicle, assigned vehicles only, or one primary vehicle
- [ ] Repo + handlers + SDK: list by customer, get, create, update, reset PIN, status transition, assignments
- [ ] Audit + outbox: `customer_driver.created`, `customer_driver.updated`, `customer_driver.pin_reset`, `customer_driver.status_changed`

**Done when:** A driver can be required on a fleet sale, validated by status/PIN, and constrained to allowed vehicles.

---

### Stage 6 - Fuel credentials

**Goal:** Fuel cards, QR tokens, and RFID identifiers can authorize fueling without exposing raw credential values.

- [ ] Migration `fuel_credentials` with customer, optional vehicle, optional driver, credential type (`card`, `qr`, `rfid`, `manual_code`), token hash, masked label, status, issue/expiry dates, and last used metadata
- [ ] Credential lifecycle: `issued -> active -> suspended -> expired -> revoked`
- [ ] Tokenization helper that hashes identifiers and stores display-safe masks only
- [ ] Credential validation endpoint that returns the linked customer/vehicle/driver context plus any holds or warnings
- [ ] Permissions: `fuel_credential.manage`, `fuel_credential.issue`, `fuel_credential.revoke`
- [ ] Audit + outbox: `fuel_credential.issued`, `fuel_credential.activated`, `fuel_credential.suspended`, `fuel_credential.revoked`

**Done when:** A QR/RFID/card token can be issued, validated, suspended, and revoked without storing the raw token.

---

## Category C - Authorization and forecourt workflow

The controlled operational path from customer credential to billable fuel sale.

### Stage 7 - Authorization rules and limits

**Goal:** The system can decide whether a customer, vehicle, driver, or credential is allowed to fuel at this station right now.

- [ ] Migration `fuel_authorization_policies`, `fuel_limits`, and `fuel_authorization_denials`
- [ ] Limit scopes: customer, vehicle, driver, credential, product, station, region, day, week, month, and per-transaction
- [ ] Policy conditions: account status, available credit, overdue balance, product compatibility, odometer required, driver required, credential status, station allowed, time window
- [ ] Denial logging with rule code, context, actor, and whether an override was attempted
- [ ] Permission `fuel_authorization.override` for manual override with reason
- [ ] Audit + outbox: `fuel_authorization.denied`, `fuel_authorization.override_granted`

**Done when:** The authorization service can explain exactly which rule allowed or denied a fueling request.

---

### Stage 8 - Fuel authorization workflow

**Goal:** A station user can authorize a fleet/credit sale before fueling begins.

- [ ] Migration `fuel_authorizations` with customer, vehicle, driver, credential, station, product, requested litres/amount, approved limit, odometer, status, expiry, and source
- [ ] Authorization lifecycle: `requested -> approved -> fulfilled -> expired -> cancelled -> voided`
- [ ] Forecourt endpoint: request authorization by credential/manual customer, select vehicle/driver, capture odometer, product, amount/litre cap, and approval result
- [ ] Idempotent fulfillment link to the Phase-6 sale so one authorization cannot be consumed twice
- [ ] Permissions: `fuel_authorization.create`, `fuel_authorization.cancel`, `fuel_authorization.override`
- [ ] Audit + outbox: `fuel_authorization.requested`, `fuel_authorization.approved`, `fuel_authorization.fulfilled`, `fuel_authorization.cancelled`

**Done when:** An attendant can validate a customer credential, capture vehicle/driver/odometer, receive an approval cap, and later tie the sale to that authorization.

---

### Stage 9 - Credit and fleet sale integration

**Goal:** Phase-6 sales can become customer credit/fleet sales with all required dimensions and finance handoff intact.

- [ ] Extend Phase-6 sale/payment structures to carry `customer_id`, `vehicle_id`, `driver_id`, `credential_id`, `authorization_id`, `odometer`, and customer price agreement snapshot
- [ ] Payment method `credit_account` posts as an AR-bound sale rather than cash/mobile/card
- [ ] Guard: credit sale cannot post unless authorization is approved and unconsumed, or a permitted override is recorded
- [ ] Emit `customer_credit_sale.created` and `customer_invoice_candidate.created` for Phase-7 invoicing
- [ ] Void/correction workflow reverses authorization consumption and sale/AR impact through existing correction patterns
- [ ] Audit + outbox: `credit_sale.posted`, `credit_sale.voided`, `fuel_authorization.consumed`

**Done when:** A fleet sale posts through Phase 6, consumes exactly one authorization, carries vehicle/driver evidence, and becomes billable to the customer.

---

## Category D - Odometer and consumption controls

Fleet customers need consumption evidence, not just invoices.

### Stage 10 - Odometer capture and validation

**Goal:** Odometer readings are captured and validated for fleet consumption analysis.

- [ ] Migration `vehicle_odometer_readings` linked to customer vehicle, sale, authorization, station, actor, reading value, captured_at, and validation status
- [ ] Validation rules: required by vehicle profile, monotonic increase, maximum distance since last fill, minimum distance warning, and manual override reason
- [ ] Optional attachment metadata for odometer photo proof, leaving binary storage/integration to a later file-storage layer if not already available
- [ ] Surface odometer warnings during authorization and sale completion
- [ ] Audit + outbox: `vehicle_odometer.recorded`, `vehicle_odometer.warning_raised`, `vehicle_odometer.corrected`

**Done when:** A vehicle with odometer-required rules cannot complete a fleet sale without a valid reading or an explicit permitted override.

---

### Stage 11 - Fleet consumption reports

**Goal:** Customers and operators can see consumption by vehicle, driver, product, station, and period.

- [ ] Reporting queries over posted fleet sales with vehicle, driver, station, product, litres, amount, odometer, distance, and consumption rate
- [ ] Reports: vehicle consumption, driver consumption, station spend, product spend, fuel economy, exception list, authorization denials
- [ ] Filters: customer, vehicle, driver, station, product, date range, authorization status
- [ ] Export CSV/XLSX for customer statements and fleet managers
- [ ] Permission `fleet_report.read`; customer portal access must be scoped to that customer only
- [ ] Audit export events for sensitive customer reports

**Done when:** A fleet manager can answer which vehicle/driver consumed how much fuel, where, when, at what price, and with what odometer evidence.

---

## Category E - Billing, statements, and risk controls

Customer-facing credit operations.

### Stage 12 - Customer statements

**Goal:** Customer credit activity can be summarized into statement periods with invoices, payments, credits, and fleet sale details.

- [ ] Migration `customer_statements` and `customer_statement_lines` with customer, period, opening balance, invoices, payments, credits, adjustments, closing balance, status, and generated_at
- [ ] Statement lifecycle: `draft -> issued -> superseded`; issued statements are immutable
- [ ] Source lines from Phase-7 AR plus Phase-8 fleet-sale detail appendix
- [ ] Generate statement PDF/CSV-ready data; actual email delivery remains optional unless messaging infrastructure already exists
- [ ] Permission `customer_statement.issue`
- [ ] Audit + outbox: `customer_statement.generated`, `customer_statement.issued`

**Done when:** A customer statement ties AR balances to sale-level fleet detail and can be regenerated as draft until issued.

---

### Stage 13 - Credit risk alerts and holds

**Goal:** Operators get deterministic credit alerts before exposure becomes unmanageable.

- [ ] Alert rules: limit utilization threshold, overdue amount, overdue days, failed payment, abnormal consumption spike, frequent overrides, invalid odometer pattern
- [ ] Migration `customer_credit_alerts` with customer, alert type, severity, source, status, assigned_to, resolution reason, and timestamps
- [ ] Alert lifecycle: `open -> acknowledged -> resolved -> dismissed`
- [ ] Optional automatic hold rules for hard limit breach or severe overdue status
- [ ] Permissions: `customer_credit_alert.manage`, `customer_credit.hold_release`
- [ ] Audit + outbox: `customer_credit_alert.raised`, `customer_credit_alert.resolved`, `customer_credit.auto_hold_applied`

**Done when:** A customer crossing configured risk thresholds produces an alert, can be acknowledged/resolved, and can place the customer on hold when policy requires it.

---

### Stage 14 - Customer and fleet workspace

**Goal:** Staff have one operational workspace for customer credit and fleet fueling.

- [ ] Route `/customers`: list/search customers, status, balance, available credit, holds, overdue amount, recent activity
- [ ] Route `/customers/{id}`: profile, credit position, vehicles, drivers, credentials, limits, authorizations, sales, statements, alerts
- [ ] Route `/fleet/authorize` or station console integration for credential validation and fueling authorization
- [ ] Backend overview endpoints for customer profile and fleet authorization screens
- [ ] Mobile responsive forecourt authorization workflow
- [ ] Permission gates match each sub-workflow; customer portal data must never leak across customer accounts

**Done when:** A user can manage a customer, fleet vehicles/drivers/credentials, authorize a credit sale, and review statements and alerts from the UI.

---

## Phase 8 acceptance criteria

Phase 8 is complete when all of the following are true:

1. Customers have managed profiles, contacts, account status, credit terms, limits, and available credit.
2. Vehicles, drivers, and credentials can be registered, suspended, retired, and validated.
3. Authorization rules explain every approval, denial, warning, and override.
4. A credit/fleet sale cannot post without customer context and either an approved authorization or a permitted override.
5. Customer pricing agreements can be approved, applied, and snapshotted on sales.
6. Odometer capture and validation feed fleet consumption reporting.
7. Customer statements tie Phase-7 AR balances to Phase-8 fleet sale detail.
8. Deterministic credit alerts and holds protect the business from obvious exposure risk.
9. Customer/fleet UI surfaces support admin setup, forecourt authorization, and fleet reporting.
10. Every customer, fleet, credential, authorization, and credit action writes audit + outbox.

---

## Out of scope for Phase 8 intentionally

- Enterprise-wide central customer governance and cross-tenant customer consolidation - Phase 9 or later.
- Machine-learning credit scoring and fraud pattern detection - Phase 10.
- Demand forecasting, automatic limit adjustment, and automated replenishment recommendations - Phase 11.
- AI customer service assistant or natural-language fleet analysis - Phase 12.
- Hardware-reader integrations for physical RFID/card readers - Phase 13. Phase 8 stores and validates credentials; hardware adapters come later.
- Offline mobile authorization and sync conflict handling - Phase 14.

---

## Cross-phase considerations

- Phase 6 remains the sale-posting engine. Phase 8 authorizes and enriches credit/fleet sales; it does not create a parallel sales ledger.
- Phase 7 remains the AR and statement-of-account source for balances. Phase 8 can show credit exposure, but finance postings must still come through Phase 7.
- Phase 10 risk intelligence will use Phase-8 dimensions: customer, vehicle, driver, credential, odometer, denials, overrides, and consumption patterns. Capture them cleanly now.
- Phase 13 hardware integration should plug into the credential validation API without changing the credential storage model.
- Every external customer-facing view must be explicitly scoped to a customer account; tenant staff views remain permission-scoped.

If any of these contracts change, Phase 8 implementation sequencing should be revisited before writing migrations.
