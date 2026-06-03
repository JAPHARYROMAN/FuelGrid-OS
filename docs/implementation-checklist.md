# FuelGrid OS Implementation Checklist

**Purpose:** Provide a phase-gated delivery checklist for the feature improvement plan.

Do not move to the next phase until the current phase has working backend logic, frontend screens, permissions, audit events, tests, documentation, and CI.

## Phase 0 - Planning and control

- [x] Create `docs/feature-improvement-and-addition-plan.md`.
- [x] Create `docs/feature-build-matrix.md`.
- [x] Create `docs/implementation-checklist.md`.
- [x] Create `docs/permissions-matrix.md`.
- [x] Create `docs/audit-events-matrix.md`.
- [x] Review existing Phase 1 implementation against the feature build matrix and update statuses.
- [x] Create `docs/phase-1-readiness-audit.md`.
- [ ] Create GitHub issues only for Phase 1.
- [ ] Confirm Phase 1 acceptance criteria and owners.
- [ ] Confirm Phase 1 database migration sequence.
- [ ] Confirm Phase 1 OpenAPI and SDK scope.

## Universal feature checklist

Every feature must complete this checklist before it can be accepted.

- [ ] Database schema or migration is implemented.
- [ ] Go domain model and validation exist.
- [ ] Repository methods exist.
- [ ] Service layer implements business rules.
- [ ] HTTP handlers exist.
- [ ] OpenAPI contract is updated.
- [ ] SDK method exists.
- [ ] Backend permission checks exist.
- [ ] Frontend permission gates exist.
- [ ] Audit events are emitted for sensitive actions.
- [ ] Idempotency is implemented for retryable write operations.
- [ ] Frontend uses real API data.
- [ ] Loading state exists.
- [ ] Empty state exists.
- [ ] Forbidden state exists.
- [ ] Validation error state exists.
- [ ] Server error state exists.
- [ ] Success state exists.
- [ ] Unit tests exist.
- [ ] Integration tests exist.
- [ ] API contract tests exist where applicable.
- [ ] Frontend tests exist where applicable.
- [ ] End-to-end tests exist for critical workflows.
- [ ] Documentation is updated.
- [ ] CI passes.

## Phase 1 - Setup and master data

- [ ] Guided setup checklist.
- [ ] Company management.
- [ ] Region management.
- [ ] Station management.
- [ ] Product management.
- [ ] Tank setup.
- [ ] Pump setup.
- [ ] Nozzle setup.
- [ ] Opening stock setup.
- [ ] First users setup.
- [ ] Station access assignment.
- [ ] Command center setup warnings.
- [ ] Phase 1 backend tests.
- [ ] Phase 1 frontend tests.
- [ ] Phase 1 documentation.
- [ ] Phase 1 acceptance review.

## Phase 2 - Identity, roles, permissions, and station access

- [ ] User administration.
- [ ] Role management.
- [ ] Effective permissions view.
- [ ] Station-scoped access checks.
- [ ] Workflow-state permission checks.
- [ ] Forbidden API responses.
- [ ] Forbidden UI states.
- [ ] Role and access audit events.
- [ ] Phase 2 tests and documentation.
- [ ] Phase 2 acceptance review.

## Phase 3 - Shift operations and readings

- [ ] Open shift workflow.
- [ ] Closing readings workflow.
- [ ] Cash, card, mobile money, and credit totals capture.
- [ ] Server-side variance calculation.
- [ ] Shift submission.
- [ ] Shift approval, rejection, and correction request.
- [ ] Shift locking.
- [ ] Shift timeline.
- [ ] Phase 3 tests and documentation.
- [ ] Phase 3 acceptance review.

## Phase 4 - POS, sales, and payments

- [ ] POS sale creation.
- [ ] Active station and active shift validation.
- [ ] Server-authoritative price and totals.
- [ ] Split payment handling.
- [ ] Receipt generation.
- [ ] Sales transaction list and filters.
- [ ] Sale detail page.
- [ ] Sale void workflow and reversal records.
- [ ] Payment callback idempotency.
- [ ] Phase 4 tests and documentation.
- [ ] Phase 4 acceptance review.

## Phase 5 - Inventory, deliveries, transfers, and reconciliation

- [ ] Expected deliveries.
- [ ] Goods receipt.
- [ ] Delivery document attachments.
- [ ] Tank dips and calibration conversion.
- [ ] Physical stock readings.
- [ ] Stock reconciliation.
- [ ] Stock adjustment workflow.
- [ ] Tank-to-tank transfers.
- [ ] Station-to-station transfers.
- [ ] Inventory ledger posting.
- [ ] Phase 5 tests and documentation.
- [ ] Phase 5 acceptance review.

## Phase 6 - Credit, receivables, and customer statements

- [ ] Customer management.
- [ ] Credit limits and payment terms.
- [ ] Customer hold workflow.
- [ ] Credit sale validation.
- [ ] Credit override approval.
- [ ] Invoice generation.
- [ ] Customer statements.
- [ ] Payment allocation and reversal.
- [ ] Receivables aging.
- [ ] Phase 6 tests and documentation.
- [ ] Phase 6 acceptance review.

## Phase 7 - Payables, procurement, and supplier control

- [ ] Supplier management.
- [ ] Purchase order workflow.
- [ ] Purchase order approval.
- [ ] Purchase order link to delivery and supplier invoice.
- [ ] Supplier invoice recording.
- [ ] Supplier invoice approval.
- [ ] Supplier payment scheduling.
- [ ] Payables aging.
- [ ] Phase 7 tests and documentation.
- [ ] Phase 7 acceptance review.

## Phase 8 - Expenses, petty cash, and cash controls

- [ ] Expense categories.
- [ ] Approval thresholds.
- [ ] Expense entry.
- [ ] Expense receipt attachments.
- [ ] Expense approval.
- [ ] Petty cash float.
- [ ] Petty cash movements.
- [ ] Petty cash reconciliation.
- [ ] Phase 8 tests and documentation.
- [ ] Phase 8 acceptance review.

## Phase 9 - Governance, approvals, and audit

- [ ] Reusable approval engine.
- [ ] Approval policies.
- [ ] Approval requests and steps.
- [ ] Approval queue.
- [ ] Maker-checker enforcement.
- [ ] Governance policy simulation.
- [ ] Audit log list.
- [ ] Audit before and after view.
- [ ] Audit export.
- [ ] Phase 9 tests and documentation.
- [ ] Phase 9 acceptance review.

## Phase 10 - Reports, exports, and executive dashboards

- [ ] Reports overview.
- [ ] Daily operations report.
- [ ] Stock loss and variance report.
- [ ] Profitability report.
- [ ] Credit and cashflow report.
- [ ] Station comparison report.
- [ ] Export service for PDF, Excel, and CSV.
- [ ] Export audit events.
- [ ] Report data-quality warnings.
- [ ] Phase 10 tests and documentation.
- [ ] Phase 10 acceptance review.

## Phase 11 - Notifications, risk, and intelligence

- [ ] Notification center.
- [ ] Notification settings.
- [ ] Critical notification acknowledgement.
- [ ] Risk alert rules.
- [ ] Risk alert lifecycle.
- [ ] Deterministic insights.
- [ ] Source-data links.
- [ ] Phase 11 tests and documentation.
- [ ] Phase 11 acceptance review.

## Phase 12 - Mobile, offline, and hardware readiness

- [ ] Mobile attendant login and assignment.
- [ ] Mobile meter readings.
- [ ] Mobile tank dips.
- [ ] Mobile delivery receipt.
- [ ] Offline draft queue.
- [ ] Sync idempotency keys.
- [ ] Conflict detection and supervisor review.
- [ ] Device registry.
- [ ] Signed webhook receiver.
- [ ] Integration event log.
- [ ] Phase 12 tests and documentation.
- [ ] Phase 12 acceptance review.

## Phase 13 - Enterprise readiness and scaling

- [ ] Tenant hierarchy.
- [ ] Company hierarchy.
- [ ] Region hierarchy.
- [ ] Station hierarchy.
- [ ] Depot support.
- [ ] Fleet account support.
- [ ] Role and reporting scope.
- [ ] Data lifecycle and retention policies.
- [ ] Closed financial period lock.
- [ ] Observability dashboard.
- [ ] Outbox and scheduler alerts.
- [ ] Phase 13 tests and documentation.
- [ ] Phase 13 acceptance review.
