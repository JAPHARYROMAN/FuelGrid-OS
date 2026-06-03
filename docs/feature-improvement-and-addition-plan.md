# FuelGrid OS Feature Improvement and Addition Plan

**Product:** FuelGrid OS  
**Document purpose:** Summarize the feature improvements and additions required to build FuelGrid OS in clear phases.  
**Build approach:** Implement one phase at a time. Do not move to the next phase until the current phase has working backend logic, frontend screens, permissions, audit events, tests, and documentation.

---

## 1. Product vision

FuelGrid OS should become a complete operating system for modern fuel businesses.

It should support:

- single fuel stations
- multi-station operators
- fuel retail chains
- depots
- distributors
- fleet operators
- finance teams
- auditors
- station managers
- attendants
- executives

The guiding principle is:

> Every liter, every shilling, every transaction, every approval, and every user action must be traceable.

FuelGrid OS should not behave like a simple dashboard or CRUD system. It should behave like a financial-grade command platform for fuel operations.

---

## 2. Core implementation rules

### 2.1 Build backend first

Every major feature should start with:

- database design
- Go domain logic
- repository methods
- service layer
- HTTP handlers
- OpenAPI contract
- SDK methods
- backend tests

Frontend pages should come after real backend contracts exist.

### 2.2 Do not build fake production pages

A page should not be considered complete if it only uses mock data.

Every production page must support:

- real API data
- loading state
- empty state
- forbidden state
- validation error state
- server error state
- success state
- permission checks

### 2.3 Audit sensitive actions

Every sensitive action must create an audit record.

Sensitive actions include:

- price changes
- stock adjustments
- sale voids
- shift approvals
- credit-limit changes
- supplier invoice approvals
- expense approvals
- user role changes
- station setup changes
- financial reversals
- closed-period changes

### 2.4 Preserve financial traceability

The system must preserve source records. Avoid deleting business records that affect stock, cash, revenue, receivables, payables, or reports.

Use reversals, corrections, approvals, and audit trails instead of destructive edits.

### 2.5 Reuse shared platform services

The following should be reusable across all modules:

- permissions
- approval workflow
- audit logging
- notifications
- exports
- report filters
- attachments
- data-quality warnings
- idempotency handling

---

## 3. Recommended build phases

| Phase | Name | Main outcome |
|---:|---|---|
| 0 | Planning and control | Create the full feature matrix and implementation checklist |
| 1 | Setup and master data | A tenant can be configured from zero |
| 2 | Identity, roles, and access | Users can only access permitted stations and actions |
| 3 | Shift operations | Stations can open, operate, close, and approve shifts |
| 4 | POS, sales, and payments | Sales and payments are captured and reconciled |
| 5 | Inventory and reconciliation | Every liter movement is traceable |
| 6 | Credit and receivables | Customer credit is controlled and aged |
| 7 | Payables and procurement | Supplier obligations are tracked and controlled |
| 8 | Expenses and petty cash | Operating expenses and cash movements are governed |
| 9 | Governance and audit | Sensitive actions follow approval and audit rules |
| 10 | Reports and exports | Management gets trusted operational and financial reports |
| 11 | Notifications, risk, and intelligence | The system surfaces risks and required actions |
| 12 | Mobile, offline, and hardware readiness | Field workflows and integrations become production-ready |
| 13 | Enterprise readiness | The platform is ready for larger operators and scaling |

---

# Phase 0 — Planning and control

## Goal

Create the control documents needed to build FuelGrid OS in a disciplined way.

## Files to create

```text
docs/feature-improvement-and-addition-plan.md
docs/feature-build-matrix.md
docs/implementation-checklist.md
docs/permissions-matrix.md
docs/audit-events-matrix.md
```

## Required work

Create a feature build matrix with:

* feature name
* phase
* priority
* backend domain
* frontend route
* required database tables
* required API endpoints
* required SDK methods
* required permissions
* required audit events
* required tests
* implementation status

## Acceptance criteria

* Every planned feature belongs to a phase.
* Every feature has clear acceptance criteria.
* Every feature has a backend target and frontend target.
* Development can proceed phase by phase without guessing.

---

# Phase 1 — Setup and master data

## Goal

Allow a new FuelGrid tenant to configure the structure required for operations.

## Target backend domains

```text
internal/companies
internal/regions
internal/stations
internal/products
internal/tanks
internal/pumps
internal/nozzles
internal/calibration
internal/workforce
internal/identity
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/setup
/apps/web/src/app/(dashboard)/setup/company
/apps/web/src/app/(dashboard)/setup/regions
/apps/web/src/app/(dashboard)/setup/stations
/apps/web/src/app/(dashboard)/setup/products
/apps/web/src/app/(dashboard)/setup/tanks
/apps/web/src/app/(dashboard)/setup/pumps
/apps/web/src/app/(dashboard)/setup/nozzles
/apps/web/src/app/(dashboard)/setup/opening-stock
```

## Features to build

### 1.1 Guided setup checklist

The setup checklist should guide the tenant through:

* company profile
* region setup
* station setup
* product setup
* tank setup
* pump setup
* nozzle setup
* opening stock
* first users
* station access assignment

Acceptance criteria:

* New tenants see setup progress.
* Completed setup steps are saved.
* Users cannot open a shift until minimum setup is complete.
* Command center shows setup warnings when required setup is missing.

### 1.2 Company and region management

Required capabilities:

* create company
* edit company
* suspend company
* create region
* edit region
* assign stations to regions
* deactivate region

Acceptance criteria:

* Company and region records are tenant-scoped.
* Duplicate active names are blocked where appropriate.
* All writes are audited.

### 1.3 Station management

Required capabilities:

* create station
* edit station
* assign station code
* assign timezone
* assign manager
* configure operating rules
* suspend station
* close station

Acceptance criteria:

* Station code is unique inside a tenant.
* Closed stations cannot open new shifts.
* Station status changes are audited.

### 1.4 Product management

Required capabilities:

* create fuel product
* create non-fuel product
* configure unit of measure
* configure price
* configure tax category
* activate or deactivate product

Acceptance criteria:

* Inactive products cannot be sold.
* Price changes are audited.
* Price changes can require approval.

### 1.5 Tank, pump, and nozzle setup

Required capabilities:

* create tank
* assign product to tank
* define tank capacity
* configure minimum level
* configure maximum level
* create pump
* create nozzle
* map nozzle to pump, tank, and product
* deactivate hardware components

Acceptance criteria:

* Nozzle cannot be active without pump, tank, and product mapping.
* Tank capacity is validated.
* Hardware mapping changes are audited.

### 1.6 Opening stock setup

Required capabilities:

* record initial book stock
* record initial physical stock
* assign stock to tank
* require reason and actor
* lock opening stock after approval

Acceptance criteria:

* Opening stock creates inventory ledger entries.
* Opening stock changes are audited.
* Opening stock cannot be silently overwritten.

---

# Phase 2 — Identity, roles, permissions, and station access

## Goal

Ensure every user can only access the correct tenant, company, region, station, and workflow actions.

## Target backend domains

```text
internal/identity
internal/identity/policy
internal/workforce
internal/stations
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/admin/users
/apps/web/src/app/(dashboard)/admin/users/[id]
/apps/web/src/app/(dashboard)/admin/roles
/apps/web/src/app/(dashboard)/admin/permissions
/apps/web/src/app/(dashboard)/admin/station-access
```

## Features to build

### 2.1 User administration

Required capabilities:

* invite user
* activate user
* deactivate user
* assign role
* assign company access
* assign region access
* assign station access
* revoke sessions
* reset MFA
* view user activity

Acceptance criteria:

* User management requires permission.
* Deactivated users cannot authenticate.
* Role and access changes are audited.

### 2.2 Role management

Required capabilities:

* create custom role
* edit custom role
* clone role
* assign permissions
* deactivate role
* view effective permissions

Acceptance criteria:

* System roles are protected.
* Role changes are audited.
* Users cannot grant permissions they do not control.

### 2.3 Permission gates

Required capabilities:

* backend permission enforcement
* frontend permission gates
* forbidden UI states
* station-scoped permission checks
* workflow-state permission checks

Acceptance criteria:

* A station manager cannot access another station unless assigned.
* A user cannot approve an action without the correct permission.
* Forbidden access returns a clear API error.

---

# Phase 3 — Shift operations and readings

## Goal

Make shifts the operational backbone of FuelGrid OS.

## Target backend domains

```text
internal/operations
internal/readings
internal/reconciliation
internal/workforce
internal/stations
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/shifts
/apps/web/src/app/(dashboard)/shifts/open
/apps/web/src/app/(dashboard)/shifts/[id]
/apps/web/src/app/(dashboard)/shifts/[id]/close
/apps/web/src/app/(dashboard)/shifts/approvals
```

## Features to build

### 3.1 Open shift

Required capabilities:

* select station
* select operating day
* assign attendants
* assign pumps
* assign nozzles
* capture opening meter readings
* validate no conflicting open shift
* start shift

Acceptance criteria:

* Shift cannot open without station setup.
* Same nozzle cannot be assigned to conflicting active shifts.
* Opening readings are locked after shift opens.

### 3.2 Close shift

Required capabilities:

* capture closing meter readings
* capture cash submissions
* capture card totals
* capture mobile money totals
* summarize credit sales
* compute expected litres sold
* compute expected revenue
* calculate variance
* submit for approval

Acceptance criteria:

* Closing reading cannot be lower than opening reading without correction workflow.
* Variance is calculated server-side.
* Submitted shift becomes read-only.

### 3.3 Shift approval

Required capabilities:

* list submitted shifts
* review readings
* review revenue
* review payment totals
* review variance
* approve shift
* reject shift
* request correction
* lock approved shift

Acceptance criteria:

* Maker cannot approve own shift when maker-checker policy is enabled.
* Approval and rejection are audited.
* Approved shifts feed reconciliation and reports.

### 3.4 Shift timeline

Required capabilities:

* show shift opened
* show readings captured
* show sales posted
* show shift submitted
* show approval or rejection
* show shift locked

Acceptance criteria:

* Timeline is visible on shift detail page.
* Timeline uses real domain or audit events.
* Timeline supports loading and empty states.

---

# Phase 4 — POS, sales, and payments

## Goal

Capture every sale and payment in a way that supports reconciliation, credit control, receipts, reports, and audit.

## Target backend domains

```text
internal/revenue
internal/payments
internal/operations
internal/products
internal/pricing
internal/receivables
internal/banking
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/pos
/apps/web/src/app/(dashboard)/sales
/apps/web/src/app/(dashboard)/sales/[id]
```

## Features to build

### 4.1 POS page

Required capabilities:

* select active station
* detect active shift
* select product
* select nozzle where applicable
* enter quantity
* enter amount
* add non-fuel item
* select customer for credit sale
* split payment by tender type
* submit sale
* print or download receipt

Acceptance criteria:

* Sale requires valid operating context.
* Product price is server-authoritative.
* Sale totals are calculated server-side.
* Sale appears in shift totals.

### 4.2 Sales transaction list

Required capabilities:

* list sales
* filter by station
* filter by shift
* filter by product
* filter by payment method
* filter by customer
* filter by date
* view sale detail
* download receipt
* request void

Acceptance criteria:

* Users see only permitted sales.
* Search and filters use server-side pagination.
* Sale detail is permission-gated.

### 4.3 Sale void workflow

Required capabilities:

* request void
* capture reason
* require approval where policy applies
* reverse sale effect
* reverse payment effect
* preserve original sale
* create reversal record

Acceptance criteria:

* Original sale is never deleted.
* Void requires permission and reason.
* Void creates audit and financial reversal records.
* Reports distinguish sale and reversal.

### 4.4 Payment handling

Required capabilities:

* cash payment
* card payment
* mobile money payment
* voucher payment
* credit sale
* payment reference
* payment status
* payment reconciliation

Acceptance criteria:

* Split payments must sum exactly to sale total.
* Failed external payments do not mark sale as fully paid.
* Payment callbacks are idempotent.

---

# Phase 5 — Inventory, deliveries, transfers, and reconciliation

## Goal

Make every liter movement traceable from receipt to sale, transfer, adjustment, loss, or closing stock.

## Target backend domains

```text
internal/inventory
internal/procurement
internal/reconciliation
internal/readings
internal/tanks
internal/products
internal/risk
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/inventory
/apps/web/src/app/(dashboard)/inventory/dips
/apps/web/src/app/(dashboard)/inventory/reconciliation
/apps/web/src/app/(dashboard)/inventory/adjustments
/apps/web/src/app/(dashboard)/inventory/transfers/tank-to-tank
/apps/web/src/app/(dashboard)/inventory/transfers/station-to-station
/apps/web/src/app/(dashboard)/deliveries
/apps/web/src/app/(dashboard)/deliveries/new
```

## Features to build

### 5.1 Deliveries and goods receipt

Required capabilities:

* create expected delivery
* select supplier
* select product
* capture ordered litres
* receive delivery
* allocate delivery to tank
* capture received litres
* capture variance
* attach documents
* approve receipt

Acceptance criteria:

* Delivery receipt increases stock only after valid receipt.
* Variance outside tolerance creates risk or approval event.
* Delivery documents are linked to receipt.

### 5.2 Tank dips and physical readings

Required capabilities:

* capture tank dip
* convert dip to litres using calibration
* capture physical stock
* compare book and physical stock
* record reading source
* attach evidence where required

Acceptance criteria:

* Tank reading is station-scoped.
* Physical stock is never silently overwritten.
* Reading correction requires reason and audit.

### 5.3 Stock reconciliation

Required capabilities:

* reconcile book vs physical stock
* show variance by tank
* show variance by product
* classify variance
* recommend action
* create adjustment request
* lock reconciliation after approval

Acceptance criteria:

* Variance calculation is server-side.
* Over-tolerance variance is flagged.
* Approved reconciliation feeds reports.

### 5.4 Stock adjustments

Required capabilities:

* create adjustment request
* classify reason
* attach supporting note
* approval workflow
* post adjustment to inventory ledger
* audit adjustment

Acceptance criteria:

* Adjustment cannot be posted without permission.
* Adjustment cannot be edited after posting.
* Ledger records preserve before and after quantities.

### 5.5 Transfers

Required capabilities:

* tank-to-tank transfer
* station-to-station transfer
* dispatch confirmation
* receipt confirmation
* variance capture
* approval requirement
* transfer document

Acceptance criteria:

* Source and destination stock movements are linked.
* Station-to-station transfer supports in-transit state.
* Variance on receipt is traceable.

---

# Phase 6 — Credit, receivables, and customer statements

## Goal

Control customer credit exposure and make all receivables visible, aged, and collectible.

## Target backend domains

```text
internal/receivables
internal/revenue
internal/payments
internal/banking
internal/risk
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/customers
/apps/web/src/app/(dashboard)/customers/[id]
/apps/web/src/app/(dashboard)/customers/[id]/statement
/apps/web/src/app/(dashboard)/credit/invoices
/apps/web/src/app/(dashboard)/credit/aging
/apps/web/src/app/(dashboard)/credit/payments
```

## Features to build

### 6.1 Customer management

Required capabilities:

* create customer
* edit customer
* set credit limit
* assign payment terms
* assign tax information
* activate or deactivate customer
* place customer on hold

Acceptance criteria:

* Credit limit changes are audited.
* Customers on hold cannot make new credit purchases.
* Customer records are tenant or company scoped.

### 6.2 Credit sales

Required capabilities:

* select customer at POS
* validate credit limit
* validate overdue status
* create receivable transaction
* link sale to invoice or statement

Acceptance criteria:

* Credit sale fails if customer exceeds policy.
* Override requires approval.
* Credit sale appears in customer balance.

### 6.3 Invoices and statements

Required capabilities:

* generate invoice
* view invoice detail
* send or download invoice
* generate statement
* allocate payments
* reverse allocation with permission

Acceptance criteria:

* Invoice totals match source transactions.
* Statement shows opening balance, charges, payments, adjustments, and closing balance.
* Payment allocation is audited.

### 6.4 Receivables aging

Required capabilities:

* current bucket
* 1 to 30 days bucket
* 31 to 60 days bucket
* 61 to 90 days bucket
* 90+ days bucket
* customer drilldown
* export aging report

Acceptance criteria:

* Aging buckets are calculated server-side.
* Users see only permitted data.
* Overdue accounts can trigger notifications.

---

# Phase 7 — Payables, procurement, and supplier control

## Goal

Track supplier obligations from purchase, delivery, invoice, payment, and aging.

## Target backend domains

```text
internal/payables
internal/procurement
internal/accounting
internal/banking
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/suppliers
/apps/web/src/app/(dashboard)/procurement/purchase-orders
/apps/web/src/app/(dashboard)/payables/invoices
/apps/web/src/app/(dashboard)/payables/aging
/apps/web/src/app/(dashboard)/payables/payments
```

## Features to build

### 7.1 Supplier management

Required capabilities:

* create supplier
* edit supplier
* assign supplied products
* configure payment terms
* activate supplier
* deactivate supplier

Acceptance criteria:

* Supplier changes are audited.
* Inactive suppliers cannot be used for new purchase orders.

### 7.2 Purchase orders

Required capabilities:

* create purchase order
* add product line items
* set expected delivery date
* submit for approval
* approve purchase order
* convert to delivery receipt
* close purchase order
* cancel purchase order

Acceptance criteria:

* Purchase order approval follows policy.
* Closed purchase orders cannot be edited.
* Purchase orders link to delivery and supplier invoice.

### 7.3 Supplier invoices

Required capabilities:

* record supplier invoice
* link invoice to delivery or purchase order
* validate price and quantity
* approve invoice
* schedule payment
* post payable

Acceptance criteria:

* Supplier invoice cannot be paid before approval where policy requires approval.
* Invoice variance is visible.
* Supplier invoice feeds payables aging.

### 7.4 Payables aging

Required capabilities:

* current bucket
* 1 to 30 days bucket
* 31 to 60 days bucket
* 61 to 90 days bucket
* 90+ days bucket
* supplier drilldown
* payment due alerts
* export report

Acceptance criteria:

* Aging buckets are calculated server-side.
* Paid invoices do not appear as outstanding.
* Overdue supplier balances can trigger notifications.

---

# Phase 8 — Expenses, petty cash, and cash controls

## Goal

Control operating expenses and cash movement at station and company level.

## Target backend domains

```text
internal/expenses
internal/accounting
internal/banking
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/expenses
/apps/web/src/app/(dashboard)/expenses/new
/apps/web/src/app/(dashboard)/expenses/categories
/apps/web/src/app/(dashboard)/expenses/petty-cash
```

## Features to build

### 8.1 Expense categories

Required capabilities:

* create category
* set approval threshold
* set accounting mapping
* activate category
* deactivate category

Acceptance criteria:

* Category changes are audited.
* Inactive categories cannot be used for new expenses.

### 8.2 Expense entry

Required capabilities:

* select station or company
* select category
* enter amount
* enter vendor or payee
* attach receipt
* submit for approval where required
* post expense

Acceptance criteria:

* Expense over threshold requires approval.
* Expense with required receipt cannot submit without attachment.
* Posted expense affects reports.

### 8.3 Petty cash ledger

Required capabilities:

* create petty cash float
* record cash issue
* record cash return
* record petty cash expense
* reconcile petty cash
* close petty cash period

Acceptance criteria:

* Petty cash balance cannot go negative unless policy allows.
* All petty cash movements are ledgered.
* Reconciliation variance requires reason.

---

# Phase 9 — Governance, approvals, and audit

## Goal

Create a reusable governance layer for all sensitive actions.

## Target backend domains

```text
internal/approvals
internal/audit
internal/identity/policy
internal/events
internal/notifications
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/approvals
/apps/web/src/app/(dashboard)/governance/policies
/apps/web/src/app/(dashboard)/audit-log
```

## Features to build

### 9.1 Approval engine

Required entities:

* approval policies
* approval requests
* approval steps
* approval comments
* approval assignments
* approval SLA state

Required capabilities:

* submit request
* approve
* reject
* request changes
* escalate
* cancel
* view approval timeline

Acceptance criteria:

* One approval engine supports multiple entity types.
* Maker-checker rules are enforceable.
* Approval decisions are immutable.
* Final approval can trigger the target domain action.

### 9.2 Governance policy page

Required capabilities:

* configure policy by entity
* configure policy by action
* configure amount threshold
* configure variance threshold
* configure approver role
* configure station or company scope
* enable or disable policy

Acceptance criteria:

* Policy changes are audited.
* Policy simulation shows whether an action requires approval.

### 9.3 Approvals queue

Required capabilities:

* list pending approvals
* filter by entity
* filter by action
* filter by station
* filter by requester
* filter by date
* approve or reject
* view linked record
* show SLA state

Acceptance criteria:

* Users see only approval items they can act on.
* Decision requires comment where policy requires it.
* Approved action updates the target domain.

### 9.4 Audit log page

Required capabilities:

* list audit events
* filter by actor
* filter by date
* filter by station
* filter by entity
* filter by action
* view before and after changes
* export audit log

Acceptance criteria:

* Audit log is read-only.
* Audit export is itself audited.
* Sensitive before and after values are redacted where required.

---

# Phase 10 — Reports, exports, and executive dashboards

## Goal

Provide trustworthy operational, financial, inventory, credit, supplier, and executive reports.

## Target backend domains

```text
internal/reports
internal/revenue
internal/reconciliation
internal/inventory
internal/receivables
internal/payables
internal/expenses
internal/enterprise
internal/accounting
internal/audit
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/reports
/apps/web/src/app/(dashboard)/reports/daily-operations
/apps/web/src/app/(dashboard)/reports/stock-loss
/apps/web/src/app/(dashboard)/reports/profitability
/apps/web/src/app/(dashboard)/reports/credit-cashflow
/apps/web/src/app/(dashboard)/reports/station-comparison
/apps/web/src/app/(dashboard)/reports/exports
```

## Features to build

### 10.1 Reports overview

Required capabilities:

* report catalog
* favorite reports
* recent reports
* scheduled reports
* export history
* permission-aware report list

Acceptance criteria:

* Users only see reports they can access.
* Reports explain data freshness.

### 10.2 Daily operations report

Required content:

* opening stock
* deliveries
* sales litres
* sales value
* payments by tender
* credit sales
* expenses
* closing stock
* variances
* shift status
* approval status

Acceptance criteria:

* Report can run by station and date.
* Report exports to PDF and Excel.
* Report links back to source transactions.

### 10.3 Stock loss and variance report

Required content:

* tank
* product
* opening book stock
* deliveries
* sales
* transfers
* adjustments
* expected closing stock
* physical closing stock
* variance
* tolerance
* classification

Acceptance criteria:

* Variance formula is documented.
* Over-tolerance rows are highlighted.
* Report supports drilldown to readings and adjustments.

### 10.4 Profitability report

Required content:

* revenue
* cost of goods
* gross margin
* expenses
* net operating result
* station comparison
* product comparison

Acceptance criteria:

* Cost assumptions are documented.
* Report supports station, date, and product filters.
* Export matches on-screen totals.

### 10.5 Credit and cashflow report

Required content:

* cash sales
* mobile payments
* card payments
* credit sales
* collections
* outstanding receivables
* overdue receivables
* supplier payments
* cash variance
* projected cash position

Acceptance criteria:

* Report reconciles to receivables and payments.
* Overdue balances match aging report.

### 10.6 Station comparison report

Required content:

* revenue
* litres sold
* margin
* stock variance
* expenses
* stockout risk
* risk alerts
* collections
* approval delays

Acceptance criteria:

* Comparison respects station access.
* Ranking logic is server-side.
* Report can export.

### 10.7 Export service

Required formats:

* PDF
* Excel
* CSV

Acceptance criteria:

* Export jobs are permission-checked.
* Exports are audited.
* Exported files include generated timestamp and filters.

---

# Phase 11 — Notifications, risk, and intelligence

## Goal

Make FuelGrid OS proactive by surfacing problems before they become losses.

## Target backend domains

```text
internal/notifications
internal/risk
internal/events
internal/scheduler
internal/enterprise
```

## Target frontend routes

```text
/apps/web/src/app/(dashboard)/notifications
/apps/web/src/app/(dashboard)/notifications/settings
/apps/web/src/app/(dashboard)/risk
```

## Features to build

### 11.1 Notification center

Required capabilities:

* list notifications
* mark read
* mark unread
* filter by severity
* filter by station
* open linked record
* configure channels
* configure quiet hours
* configure notification categories

Acceptance criteria:

* Notifications are tenant-scoped.
* Notification preferences are user-specific.
* Critical events remain visible until acknowledged where policy requires.

### 11.2 Risk alerts

Required alert types:

* stock variance over tolerance
* cash variance over tolerance
* low stock
* stockout risk
* unusual sale voids
* delayed shift approval
* overdue credit account
* overdue supplier payment
* missing tank dip
* suspicious stock adjustment pattern

Acceptance criteria:

* Risk alert links to source data.
* Risk alert has severity and score.
* Risk alert lifecycle supports open, investigating, resolved, and dismissed.
* Resolution requires comment.

### 11.3 Deterministic insights

Required capabilities:

* compute rule-based operational insights
* show recommended action
* show source data
* show confidence or data-quality warning

Acceptance criteria:

* Every insight is traceable to source data.
* Insight text is deterministic for the same data.
* User can navigate from insight to source record.

---

# Phase 12 — Mobile, offline, and hardware readiness

## Goal

Prepare FuelGrid OS for real field operations and hardware integration.

## Target backend domains

```text
internal/integrations
internal/events
internal/readings
internal/inventory
internal/operations
```

## Target frontend/mobile areas

```text
apps/mobile
apps/web/src/app/(dashboard)/integrations
apps/web/src/app/(dashboard)/devices
```

## Features to build

### 12.1 Mobile attendant workflow

Required capabilities:

* attendant login
* assigned station
* assigned shift
* record meter readings
* record simple sales where allowed
* capture tank dip
* capture delivery receipt
* offline draft queue

Acceptance criteria:

* Mobile user sees only assigned station and shift.
* Offline entries sync with idempotency keys.
* Conflicts require supervisor review.

### 12.2 Offline sync foundation

Required capabilities:

* local queue
* idempotency keys
* sync status
* retry and backoff
* conflict detection
* server-side duplicate protection

Acceptance criteria:

* Repeated sync does not duplicate transactions.
* Conflicts are visible and resolvable.
* Offline-created records retain original device timestamp.

### 12.3 Hardware integration readiness

Required capabilities:

* device registry
* pump controller adapter interface
* tank gauge adapter interface
* webhook receiver
* signed integration requests
* integration event log
* retry handling

Acceptance criteria:

* Hardware events are idempotent.
* Bad signatures are rejected.
* Integration failures are visible in operations dashboard.

---

# Phase 13 — Enterprise readiness and scaling

## Goal

Prepare FuelGrid OS for larger deployments, multiple teams, and enterprise operations.

## Target backend domains

```text
internal/enterprise
internal/observability
internal/scheduler
internal/events
internal/audit
internal/database
```

## Features to build

### 13.1 Enterprise hierarchy improvements

Required capabilities:

* tenant hierarchy
* company hierarchy
* region hierarchy
* station hierarchy
* depot support
* fleet account support
* role scope
* reporting scope

Acceptance criteria:

* Enterprise user can switch context safely.
* Reports aggregate only permitted scope.
* Cross-scope data leakage tests exist.

### 13.2 Data lifecycle and retention

Required capabilities:

* audit retention policy
* session retention policy
* export retention policy
* archived station data handling
* closed financial period lock

Acceptance criteria:

* Closed periods cannot be changed without controlled workflow.
* Retention jobs are observable.
* Deletion is soft or archived unless explicitly safe.

### 13.3 Observability dashboard

Required capabilities:

* API health
* database health
* Redis health
* outbox backlog
* scheduler status
* failed jobs
* export job status
* notification delivery status

Acceptance criteria:

* Operators can see system health from one place.
* Failed background jobs are visible.
* Alerts exist for outbox backlog and scheduler failure.

---

# Cross-cutting feature additions

## A. Permissions matrix

Create:

```text
docs/permissions-matrix.md
```

Each permission should define:

* permission code
* description
* allowed roles
* backend enforcement point
* frontend gate
* audit requirement

Example permission codes:

```text
setup.company.manage
setup.station.manage
setup.product.manage
shift.open
shift.close
shift.approve
sale.create
sale.view
sale.void
inventory.adjust
inventory.transfer
credit.customer.manage
credit.payment.allocate
payables.invoice.approve
expenses.approve
governance.policy.manage
audit.view
reports.export
```

## B. Audit events matrix

Create:

```text
docs/audit-events-matrix.md
```

Each audit event should define:

* event code
* entity type
* action
* reason required
* before snapshot required
* after snapshot required
* severity
* retention requirement

Example audit events:

```text
company.created
station.updated
product.price_changed
tank.mapping_changed
shift.opened
shift.closed
shift.approved
sale.created
sale.void_requested
sale.void_approved
stock.adjustment_requested
stock.adjustment_posted
customer.credit_limit_changed
supplier.invoice_approved
expense.approved
approval.policy_changed
user.role_changed
report.exported
```

## C. Data-quality warnings

Dashboards and reports should warn users when:

* operating day is not closed
* shifts are not approved
* latest tank dips are missing
* reconciliation is not completed
* report projection is stale
* data source failed
* user is viewing partial data

## D. Idempotency

Use idempotency keys for:

* sales
* payments
* mobile sync
* payment callbacks
* delivery receipts
* stock adjustments
* transfers
* exports
* notification dispatch

## E. Attachments

Support attachments for:

* delivery notes
* expense receipts
* tank dip evidence
* incident evidence
* supplier invoices
* audit support documents
* calibration documents

## F. Shared UI components to add

Recommended shared components:

```text
packages/ui/src/approval-timeline.tsx
packages/ui/src/audit-diff-viewer.tsx
packages/ui/src/data-quality-card.tsx
packages/ui/src/export-button.tsx
packages/ui/src/money-field.tsx
packages/ui/src/permission-denied-state.tsx
packages/ui/src/report-shell.tsx
packages/ui/src/risk-score-badge.tsx
packages/ui/src/shift-timeline.tsx
packages/ui/src/variance-badge.tsx
```

## G. SDK modules to add or improve

Recommended SDK modules:

```text
packages/sdk/src/setup.ts
packages/sdk/src/users.ts
packages/sdk/src/roles.ts
packages/sdk/src/shifts.ts
packages/sdk/src/sales.ts
packages/sdk/src/payments.ts
packages/sdk/src/inventory.ts
packages/sdk/src/deliveries.ts
packages/sdk/src/reconciliation.ts
packages/sdk/src/customers.ts
packages/sdk/src/receivables.ts
packages/sdk/src/suppliers.ts
packages/sdk/src/payables.ts
packages/sdk/src/expenses.ts
packages/sdk/src/approvals.ts
packages/sdk/src/audit.ts
packages/sdk/src/reports.ts
packages/sdk/src/notifications.ts
packages/sdk/src/risk.ts
```

---

# Suggested issue template

Use this template for every GitHub issue.

```md
## Goal

## User story

As a ...
I want ...
So that ...

## Phase

## Backend domain

## Frontend route

## Database changes

## API endpoints

## SDK methods

## Permissions

## Audit events

## UI states

- loading
- empty
- forbidden
- validation error
- server error
- success

## Tests

- unit
- integration
- API contract
- frontend
- e2e where required

## Acceptance criteria
```

---

# Definition of done for every feature

A feature is complete only when:

* database migrations are complete
* Go code compiles
* backend tests pass
* OpenAPI is updated
* SDK method exists
* frontend page uses real API data
* permission checks are implemented
* audit events are implemented
* validation is handled
* loading state exists
* empty state exists
* forbidden state exists
* error state exists
* documentation is updated
* CI passes

---

# First build order

Build in this exact order:

1. Phase 0 — Planning and control
2. Phase 1 — Setup and master data
3. Phase 2 — Identity, roles, permissions, and station access
4. Phase 3 — Shift operations and readings
5. Phase 4 — POS, sales, and payments
6. Phase 5 — Inventory, deliveries, transfers, and reconciliation
7. Phase 6 — Credit, receivables, and customer statements
8. Phase 7 — Payables, procurement, and supplier control
9. Phase 8 — Expenses, petty cash, and cash controls
10. Phase 9 — Governance, approvals, and audit
11. Phase 10 — Reports, exports, and executive dashboards
12. Phase 11 — Notifications, risk, and intelligence
13. Phase 12 — Mobile, offline, and hardware readiness
14. Phase 13 — Enterprise readiness and scaling

---

# Immediate next actions

Start with these actions:

1. Add this file to `docs/feature-improvement-and-addition-plan.md`.
2. Create `docs/feature-build-matrix.md`.
3. Create `docs/implementation-checklist.md`.
4. Create `docs/permissions-matrix.md`.
5. Create `docs/audit-events-matrix.md`.
6. Create GitHub issues only for Phase 1.
7. Complete Phase 1 backend gaps.
8. Complete Phase 1 setup UI.
9. Add tests for tenant setup.
10. Run full CI.
11. Move to Phase 2 only after Phase 1 is accepted.

---

# Final product outcome

When all phases are complete, FuelGrid OS should support:

* full tenant, company, region, and station setup
* secure user and station access control
* complete shift lifecycle
* sales and split-payment workflows
* inventory ledger and reconciliation
* deliveries and stock transfers
* customer credit and receivables
* supplier payables and procurement
* expenses and petty cash
* approvals and governance
* immutable audit visibility
* executive reports and exports
* notifications and operational risk alerts
* mobile and offline field workflows
* integration readiness
* enterprise-scale observability

The final system should be a financial-grade operating system for fuel operations, where every physical liter and every monetary movement is accountable.
