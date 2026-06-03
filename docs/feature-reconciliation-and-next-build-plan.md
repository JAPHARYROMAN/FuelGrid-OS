# FuelGrid OS Feature Reconciliation and Next Build Plan

**Product:** FuelGrid OS
**Document purpose:** Convert the feature improvement roadmap into an accurate, code-aware implementation backlog.
**Important rule:** Do not rebuild features that already exist. Verify the current implementation, mark the real status, and build only missing, partial, or quality-gap items.

---

## 1. Why this document exists

The feature improvement plan defines the desired final product standard for FuelGrid OS.

However, FuelGrid OS already contains many core platform and business capabilities. Therefore, the improvement roadmap must be reconciled against the live codebase before implementation begins.

This document prevents the team from accidentally planning or rebuilding features that are already implemented.

The goal is to create a practical backlog that separates:

- features already implemented
- features implemented but needing polish
- features partially implemented
- features missing entirely
- features requiring architectural decisions

---

## 2. Status labels

Use these labels across the feature matrix and implementation checklist.

| Status | Meaning |
|---|---|
| `DONE` | Feature exists, works, is connected to real backend data, and has acceptable tests |
| `PARTIAL` | Feature exists but is incomplete, missing UI, missing edge cases, or missing workflow steps |
| `MISSING` | Feature does not exist yet |
| `NEEDS QUALITY PASS` | Feature exists but does not meet the new definition of done |
| `DECISION REQUIRED` | Product or architecture decision is needed before implementation |
| `VERIFY` | Feature appears to exist, but needs confirmation from code, tests, and UI |

---

## 3. Immediate reconciliation tasks

Before building new features, complete this reconciliation sweep.

### 3.1 Update the feature build matrix

Update:

```text
docs/feature-build-matrix.md
```

For each feature, verify:

- backend domain exists
- database tables exist
- migrations exist
- API endpoints exist
- OpenAPI contract exists
- SDK method exists
- frontend page exists
- frontend page uses real API data
- permission checks exist
- audit events exist
- tests exist
- CI covers it

Then assign one of:

```text
DONE
PARTIAL
MISSING
NEEDS QUALITY PASS
DECISION REQUIRED
VERIFY
```

### 3.2 Update the implementation checklist

Update:

```text
docs/implementation-checklist.md
```

Do not leave generic checklist items as if everything is greenfield.

Split checklist items into:

- already satisfied
- needs verification
- needs implementation
- needs refactor
- not applicable

### 3.3 Update permissions matrix

Update:

```text
docs/permissions-matrix.md
```

The permission matrix must match the permission codes currently used in FuelGrid OS.

Do not introduce a new permission vocabulary unless a deliberate migration is approved.

### 3.4 Update audit events matrix

Update:

```text
docs/audit-events-matrix.md
```

Audit events should match real domain actions and existing audit/event patterns.

Separate:

- existing audit events
- missing audit events
- desired future audit events
- events requiring sensitive data redaction

---

## 4. Key architecture decisions

The following decisions must be made before continuing with implementation.

### Decision 1 — Permission vocabulary

**Current issue**

The improvement plan may define new permission names that do not match the current FuelGrid OS codebase.

Examples of possible new-style permissions:

```text
setup.company.manage
sale.void.approve
inventory.adjust.post
reports.export
```

But the current codebase may already use different permission codes, such as domain-oriented names.

**Recommendation**

Do not migrate permission codes immediately.

Instead:

1. Keep the current code permission vocabulary.
2. Update the documentation to match the code.
3. Add missing permissions only where a real feature requires them.
4. Avoid mass renaming unless there is a dedicated permission migration plan.

**Decision**

```text
Decision: Use current FuelGrid permission codes as the source of truth.
Status: Recommended
```

**Follow-up work**

- Review all current permissions in code and seed data.
- Update `docs/permissions-matrix.md`.
- Add missing permission codes only for new features.
- Add compatibility aliases only if absolutely necessary.

### Decision 2 — `/setup/*` routes vs `/settings/*` routes

**Current issue**

The feature improvement plan proposes routes such as:

```text
/setup/company
/setup/stations
/setup/products
/setup/tanks
/setup/opening-stock
```

But the application may already implement these workflows under:

```text
/settings/*
```

**Recommendation**

Do not duplicate existing settings pages.

Use this model:

```text
/settings/* = permanent administration and configuration pages
/setup      = guided onboarding checklist and setup progress shell
```

This means:

- keep existing `/settings/*` pages
- add `/setup` only as an onboarding checklist/dashboard
- link each setup checklist item to the existing settings page where possible
- create new setup-specific pages only where no equivalent settings page exists

**Decision**

```text
Decision: Keep /settings/* as the main configuration area.
Decision: Add /setup only as a guided checklist and onboarding shell.
Status: Recommended
```

**Follow-up work**

- Update route references in the roadmap.
- Avoid duplicating settings pages.
- Add setup progress state.
- Link setup checklist items to existing configuration pages.

---

## 5. Revised build priority

Based on the reconciliation feedback, do not start by rebuilding Phases 2–11.

Start with the genuinely missing or incomplete work.

### Priority 1 — Persisted setup checklist and onboarding progress

**Status**

```text
PARTIAL / MISSING
```

**Goal**

Create a guided setup experience that helps a new tenant complete the required operating setup before using live station workflows.

**Recommended route model**

```text
/setup
/settings/company
/settings/regions
/settings/stations
/settings/products
/settings/tanks
/settings/pumps
/settings/nozzles
/settings/users
/settings/opening-stock
```

**Features**

**1.1 Setup progress state**

Add persistent setup state that tracks:

- company profile completed
- first station created
- products created
- tanks created
- pumps created
- nozzles mapped
- users invited
- station access assigned
- opening stock entered
- opening stock approved

**Backend requirements**

Possible domain:

```text
internal/setup
```

Possible table:

```text
tenant_setup_progress
```

Suggested fields:

```text
id
tenant_id
company_profile_completed_at
first_station_completed_at
products_completed_at
tanks_completed_at
pumps_completed_at
nozzles_completed_at
users_completed_at
station_access_completed_at
opening_stock_completed_at
opening_stock_approved_at
created_at
updated_at
```

**API endpoints**

```text
GET /api/v1/setup/progress
PATCH /api/v1/setup/progress
POST /api/v1/setup/complete-step
```

**Frontend requirements**

Build:

```text
/apps/web/src/app/(dashboard)/setup/page.tsx
```

The page should show:

- setup checklist
- completion percentage
- missing steps
- links to relevant settings pages
- blocked workflow warnings
- "ready for operations" indicator

**Acceptance criteria**

- Setup progress persists per tenant.
- Setup checklist uses real backend state.
- Setup page links to existing settings/configuration pages.
- Opening a shift is blocked if required setup is incomplete.
- Setup completion events are audited.

### Priority 2 — Opening stock approval and lock workflow

**Status**

```text
PARTIAL
```

**Goal**

Ensure opening stock is controlled, approved, locked, and traceable.

**Required workflow**

1. User enters opening stock by tank.
2. System creates draft opening stock records.
3. Supervisor reviews opening stock.
4. Supervisor approves or rejects.
5. Approved opening stock creates inventory ledger entries.
6. Approved opening stock becomes locked.
7. Any later correction requires controlled adjustment workflow.

**Backend requirements**

Possible domain:

```text
internal/inventory
internal/approvals
internal/audit
```

Required capabilities:

- create opening stock draft
- update draft before submission
- submit opening stock
- approve opening stock
- reject opening stock
- lock approved opening stock
- create inventory ledger entries after approval

**API endpoints**

```text
GET /api/v1/opening-stock
POST /api/v1/opening-stock
PATCH /api/v1/opening-stock/{id}
POST /api/v1/opening-stock/{id}/submit
POST /api/v1/opening-stock/{id}/approve
POST /api/v1/opening-stock/{id}/reject
```

**Frontend requirements**

Possible route:

```text
/settings/opening-stock
```

Required UI states:

- no tanks configured
- draft opening stock
- submitted pending approval
- approved and locked
- rejected with reason
- correction required

**Audit events**

```text
opening_stock.created
opening_stock.updated
opening_stock.submitted
opening_stock.approved
opening_stock.rejected
opening_stock.locked
opening_stock.corrected
```

**Acceptance criteria**

- Opening stock cannot be silently overwritten.
- Approved opening stock creates inventory ledger entries.
- Opening stock approval is audited.
- Rejected opening stock records rejection reason.
- Locked opening stock cannot be edited directly.

### Priority 3 — Dedicated POS page

**Status**

```text
MISSING / PARTIAL
```

**Goal**

Create a real point-of-sale interface for station sales.

**Required route**

```text
/apps/web/src/app/(dashboard)/pos/page.tsx
```

**Required capabilities**

- select station
- detect active operating day
- detect active shift
- select nozzle or product
- enter quantity
- enter amount
- add non-fuel product
- select customer for credit sale
- split payment by tender type
- submit sale
- print or download receipt

**Backend requirements**

Use existing sales, revenue, payment, product, pricing, shift, and receivables domains where available.

Required server-side validations:

- station is active
- user has station access
- shift is open
- product is active
- price is server-authoritative
- credit customer is active
- credit limit is not exceeded unless approval is granted
- payment split equals sale total
- idempotency key prevents duplicate sale submission

**API endpoints**

Verify existing endpoints first. Add only what is missing.

Possible endpoints:

```text
GET /api/v1/pos/context
POST /api/v1/sales
GET /api/v1/sales/{id}
GET /api/v1/sales/{id}/receipt
```

**SDK methods**

```text
getPOSContext()
createSale()
getSale()
getSaleReceipt()
```

**Acceptance criteria**

- POS uses real backend data.
- Sale cannot be submitted without active shift.
- Sale total is calculated server-side.
- Split payments reconcile exactly.
- Credit sale updates receivables.
- Sale appears in shift totals and reports.
- Duplicate submission is prevented through idempotency.

### Priority 4 — Sale void request, approval, and reversal workflow

**Status**

```text
MISSING
```

**Goal**

Allow sales to be voided safely without deleting the original transaction.

**Required workflow**

1. User requests sale void.
2. User provides reason.
3. System checks policy.
4. If approval is required, approval request is created.
5. Approver approves or rejects.
6. Approved void creates reversal records.
7. Reports show original sale and reversal clearly.
8. Audit log preserves the full trail.

**Backend requirements**

Possible domains:

```text
internal/revenue
internal/payments
internal/approvals
internal/audit
internal/accounting
internal/reconciliation
```

Required capabilities:

- request void
- approve void
- reject void
- create sale reversal
- create payment reversal
- update shift/revenue summaries
- preserve original sale
- prevent duplicate void

**API endpoints**

```text
POST /api/v1/sales/{id}/void-request
POST /api/v1/sales/{id}/void-approve
POST /api/v1/sales/{id}/void-reject
GET /api/v1/sales/{id}/void-status
```

**Audit events**

```text
sale.void_requested
sale.void_approved
sale.void_rejected
sale.void_posted
sale.reversal_created
payment.reversal_created
```

**Acceptance criteria**

- Original sale is never deleted.
- Void requires permission and reason.
- Void approval follows policy.
- Approved void creates reversal records.
- Reports distinguish sale from reversal.
- Void operation is idempotent.
- All actions are audited.

### Priority 5 — Quality pass against the new definition of done

**Status**

```text
NEEDS QUALITY PASS
```

**Goal**

Bring existing pages and features up to the new quality standard.

**Pages to review**

Review all existing pages under:

```text
/apps/web/src/app/(dashboard)
```

**Required checks per page**

Each page should have:

- real backend data
- loading state
- empty state
- forbidden state
- validation error state
- server error state
- success state
- permission gate
- station/company scope enforcement
- audit behavior for sensitive actions
- tests where appropriate

**Backend checks**

Each sensitive endpoint should have:

- permission check
- tenant scope check
- station/company scope check
- validation
- audit event
- idempotency where needed
- test coverage

**Output file**

Create:

```text
docs/quality-pass-register.md
```

Suggested columns:

```text
Feature
Page
Backend endpoint
Status
Missing loading state
Missing empty state
Missing forbidden state
Missing permission gate
Missing audit
Missing tests
Owner
Priority
```

**Acceptance criteria**

- Each existing feature is reviewed.
- Missing quality items become GitHub issues.
- High-risk financial and inventory pages are fixed first.

### Priority 6 — Attachments framework

**Status**

```text
MISSING / PARTIAL
```

**Goal**

Support document and image attachments for operational evidence.

**Attachment use cases**

- delivery notes
- supplier invoices
- expense receipts
- tank dip evidence
- incident evidence
- calibration documents
- approval support files
- audit support files

**Backend requirements**

Possible domain:

```text
internal/documents
internal/storage
```

Required capabilities:

- upload attachment
- validate content type
- validate file size
- associate attachment with entity
- list entity attachments
- download attachment
- delete draft attachment
- retain approved/posted attachments
- audit access where required

Suggested table:

```text
attachments
```

Suggested fields:

```text
id
tenant_id
company_id
station_id
entity_type
entity_id
filename
content_type
size_bytes
storage_key
checksum
uploaded_by
created_at
deleted_at
```

**API endpoints**

```text
POST /api/v1/attachments
GET /api/v1/attachments/{id}
GET /api/v1/entities/{entity_type}/{entity_id}/attachments
DELETE /api/v1/attachments/{id}
```

**Acceptance criteria**

- Attachments are tenant-scoped.
- Attachments cannot leak across tenants.
- File type and size are validated.
- Approved/posted business documents are retained.
- Attachment access is permission-checked.

### Priority 7 — Notification center UI

**Status**

```text
PARTIAL / MISSING UI
```

**Goal**

Expose notifications clearly in the web app.

**Required routes**

```text
/apps/web/src/app/(dashboard)/notifications
/apps/web/src/app/(dashboard)/notifications/settings
```

**Required capabilities**

- list notifications
- filter by severity
- filter by station
- mark as read
- mark as unread
- open linked record
- notification preferences
- channel settings
- category settings

**Notification categories**

```text
risk
approval
shift
inventory
credit
payables
expenses
system
```

**Acceptance criteria**

- Notification feed uses real backend data.
- Users see only their permitted notifications.
- Critical notifications remain visible until acknowledged where required.
- Notification preferences are saved per user.
- Notification events are generated from real domain events.

### Priority 8 — Missing report types and report polish

**Status**

```text
PARTIAL
```

**Goal**

Complete any missing high-value report types and make report behavior consistent.

**Reports to verify**

- daily operations report
- stock loss and variance report
- profitability report
- credit and cashflow report
- station comparison report
- receivables aging
- payables aging
- expense report
- audit activity report
- export history

**Required report standards**

Each report should have:

- date filters
- station/company filters
- permission-aware data
- data freshness warning
- export support
- source transaction drilldown
- empty state
- forbidden state
- server-side calculations

**Acceptance criteria**

- Report totals are calculated server-side.
- Reports respect station access.
- Exports match on-screen totals.
- Export events are audited.
- Stale or incomplete data is clearly indicated.

### Priority 9 — Mobile, offline, and hardware readiness

**Status**

```text
MISSING
```

**Goal**

Prepare FuelGrid OS for field operations and hardware integrations.

This is a later phase and should not block POS, setup, void workflow, or quality-pass work.

**9.1 Mobile attendant workflow**

Required capabilities:

- attendant login
- assigned station
- assigned shift
- record meter readings
- capture tank dip
- capture delivery receipt
- view assigned tasks
- offline draft queue

**9.2 Offline sync foundation**

Required capabilities:

- local queue
- idempotency keys
- sync status
- retry and backoff
- conflict detection
- server-side duplicate protection
- supervisor conflict review

**9.3 Device and hardware registry**

Required capabilities:

- device registry
- pump controller adapter interface
- tank gauge adapter interface
- webhook receiver
- signed integration requests
- integration event log
- retry handling

**Acceptance criteria**

- Offline sync does not duplicate records.
- Hardware events are idempotent.
- Bad signatures are rejected.
- Integration failures are visible to operators.
- Mobile and hardware work is documented separately before implementation.

---

## 6. Revised build order

Use this order instead of blindly following the full roadmap.

```text
1. Reconcile feature-build-matrix statuses against live code
2. Decide permission-code strategy
3. Decide /setup vs /settings route strategy
4. Build persisted setup checklist
5. Build opening stock approval and lock UI
6. Build dedicated POS page
7. Build sale void request/approval/reversal workflow
8. Run quality pass on existing pages
9. Add attachments framework
10. Add notification center UI
11. Complete missing reports and export polish
12. Plan mobile/offline/hardware as a separate major phase
```

---

## 7. What not to do

Do not:

- rebuild features that already exist
- create duplicate `/setup/*` pages if `/settings/*` already handles them
- rename permission codes without a migration plan
- create mock-only production pages
- introduce separate approval systems per module
- implement sale void by deleting or mutating the original sale
- build mobile/offline before core POS and setup gaps are closed

---

## 8. Recommended source-of-truth hierarchy

Use this order when documents and code disagree:

```text
1. Current working code
2. Current database migrations
3. Current tests
4. Current OpenAPI contract
5. Current SDK methods
6. Current frontend routes
7. Feature improvement docs
```

The documentation should be updated to match the live system unless a deliberate product decision changes the system.

---

## 9. Final recommendation

FuelGrid OS should proceed as an incremental product hardening and completion effort, not a greenfield rebuild.

The genuinely important next work is:

1. persisted setup checklist
2. opening stock approval and lock workflow
3. dedicated POS page
4. sale void approval and reversal workflow
5. quality pass across existing pages
6. attachments framework
7. notification center UI
8. missing report polish
9. mobile/offline/hardware readiness

This keeps the roadmap realistic and prevents wasted work.

---

> **Working agreement:** Use this reconciliation file as the practical working backlog. Keep `docs/feature-improvement-and-addition-plan.md` as the long-term target standard.
