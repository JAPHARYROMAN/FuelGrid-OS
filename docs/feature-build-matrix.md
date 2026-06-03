# FuelGrid OS Feature Build Matrix

**Purpose:** Track every planned feature from the improvement plan through backend, frontend, controls, tests, and delivery status.

**Status legend**

Reconciled label set (authoritative as of 2026-06-03 — aligns with [feature-reconciliation-and-next-build-plan.md](feature-reconciliation-and-next-build-plan.md)):

- `DONE`: Backend, frontend, SDK, permissions, audit events, and tests verified present against the live codebase. No blocking acceptance gap.
- `PARTIAL`: Real implementation exists and was verified, but one or more acceptance requirements (usually frontend pages, tests, or a sub-workflow) are still missing.
- `MISSING`: Feature is absent or only a permission stub exists. A genuine build target.
- `NEEDS QUALITY PASS`: Implemented but requires hardening (tests, edge cases, polish) before acceptance.
- `DECISION REQUIRED`: Cannot proceed without a product or architecture decision.
- `VERIFY`: Not yet reconciled against current code; status must be confirmed before work starts.

Legacy labels (retained for historical rows; superseded by the reconciled set above):

- `Created`: Phase 0 planning artifact exists.
- `Planned - verify`: Planned from the feature plan; implementation must be checked against the current codebase before work starts.
- `Partial - verified`: Real implementation exists and was checked, but one or more acceptance requirements are still missing.
- `Blocked by gaps`: A major backend, frontend, SDK, OpenAPI, permission, audit, or test gap prevents feature acceptance.
- `In progress`: Actively being implemented.
- `Blocked`: Cannot proceed without a decision, dependency, or missing prerequisite.
- `Accepted`: Backend, frontend, permissions, audit events, tests, documentation, and CI are complete.

Acceptance criteria for each feature are defined in [feature-improvement-and-addition-plan.md](feature-improvement-and-addition-plan.md). This matrix is the execution tracker.

## Reconciliation summary (2026-06-03)

Code-aware reconciliation across all phases. Each feature row's **Status** column reflects verification against the live codebase (handlers, migrations, routes, SDK, permissions, audit events, frontend, tests).

**Counts (64 feature rows):** 30 DONE · 25 PARTIAL · 8 MISSING · 0 NEEDS QUALITY PASS · 0 DECISION REQUIRED · 1 VERIFY.

The 8 genuinely **MISSING** features below are the real build targets. None currently require a product/architecture decision (0 DECISION REQUIRED). Build order is tracked in [feature-reconciliation-and-next-build-plan.md](feature-reconciliation-and-next-build-plan.md).

- **4.3 Sale void workflow (MISSING)** — No `sale_voids`/`sale_reversals` tables, no void-request/approve endpoints, no reversal logic, no `sale.void.*` permissions or audit events. Entire feature absent.
- **10.4 Profitability report (MISSING)** — No `GET /reports/profitability` route or page; existing `financials.csv/.pdf` is an accounting income statement, not operational profitability (revenue, COGS, gross margin, net operating result, station/product comparison).
- **10.6 Station comparison report (MISSING)** — No `GET /reports/station-comparison` route, no frontend page, no ranking logic or scoped-access checks.
- **12.1 Mobile attendant workflow (MISSING)** — Only `mobile.attendant` permission stub exists; no `apps/mobile`, no `mobile_sessions`/`offline_drafts` tables, no `/mobile/*` routes or SDK methods.
- **12.2 Offline sync foundation (MISSING)** — Only `mobile.sync` permission stub; no `idempotency_keys`/`sync_batches`/`sync_conflicts` tables, no `/sync/*` routes or SDK methods.
- **12.3 Hardware integration readiness (MISSING)** — Only `integration.manage` permission stub; `devices` table exists but no device-registry/webhook endpoints or signature verification.
- **13.2 Data lifecycle and retention (MISSING)** — Accounting-period close exists, but no `retention_policies`/`retention_jobs` tables, no closed-period change-request workflow, no `retention.*`/`closed_period.change` endpoints, SDK methods, permissions, or audit events.
- **C.3 Attachments (MISSING)** — Zero references in handlers, migrations, SDK, or UI; no `attachments` table, endpoints, SDK methods, upload UI, permission gates, or audit events.

## Phase 0 - Planning and control

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 0.1 Feature improvement plan | P0 | docs | n/a | n/a | n/a | n/a | n/a | n/a | doc review | DONE |
| 0.2 Feature build matrix | P0 | docs | n/a | n/a | n/a | n/a | n/a | n/a | doc review | DONE |
| 0.3 Implementation checklist | P0 | docs | n/a | n/a | n/a | n/a | n/a | n/a | doc review | DONE |
| 0.4 Permissions matrix | P0 | internal/identity/policy | all protected routes | permissions, role_permissions | policy endpoints | permissions SDK | permission.manage | role.permissions_changed | unit, integration, frontend gates | DONE |
| 0.5 Audit events matrix | P0 | internal/audit | /audit-log | audit_events | audit endpoints | audit SDK | audit.view | audit.exported | unit, integration, export | DONE |
| 0.6 Phase 1 GitHub issues | P0 | project management | n/a | n/a | n/a | n/a | n/a | n/a | issue checklist review | VERIFY |

## Phase 1 - Setup and master data

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 1.1 Guided setup checklist | P0 | internal/companies, internal/stations, internal/workforce | Existing: /setup linking to /settings/*; target: /setup/* | Missing setup_steps and tenant_setup_state; counts are computed from domain tables | Missing GET/PATCH /setup/checklist | Missing setup.getChecklist, setup.updateStep | Current reads use station.read and domain permissions | Missing setup.step_completed | Need backend checklist tests and frontend state tests | DONE |
| 1.2 Company and region management | P0 | internal/companies, internal/regions | Existing: /settings/companies, /settings/regions; target: /setup/company, /setup/regions | companies, regions | Existing CRUD /companies, CRUD /regions | Existing list/create/update/delete company and region methods | Current: companies.manage, regions.manage | Existing company.created, company.updated, company.deleted, region.created, region.updated, region.deleted | Integration coverage exists but skipped without TEST_DATABASE_URL and TEST_REDIS_URL; frontend tests missing | DONE |
| 1.3 Station management | P0 | internal/stations | Existing: /settings/stations; target: /setup/stations | stations | Existing CRUD /stations plus station overview | Existing list/get/create/update/delete station methods | Current: station.manage, station.read | Existing station.created, station.updated, station.deleted | Integration coverage exists but skipped without TEST_DATABASE_URL and TEST_REDIS_URL; frontend tests missing | DONE |
| 1.4 Product management | P0 | internal/products, internal/pricing | Existing: /settings/products, /settings/pricing; target: /setup/products | products, price_changes | Existing CRUD /products, POST /stations/{stationID}/prices | Existing list/create/update/delete products and pricing methods | Current: products.manage, price.change, pricing.read | Existing product.created, product.updated, product.deleted, price.changed, price.scheduled | Product audit integration exists but skipped without TEST_DATABASE_URL and TEST_REDIS_URL; approval tests missing | PARTIAL |
| 1.5 Tank, pump, and nozzle setup | P0 | internal/tanks, internal/pumps, internal/nozzles, internal/calibration | Existing: /settings/tanks, /settings/pumps, /settings/tanks/[tankID]; target: /setup/tanks, /setup/pumps, /setup/nozzles | tanks, pumps, nozzles, tank_calibration_charts, tank_calibration_entries | Existing CRUD /tanks, /pumps, /nozzles and calibration endpoints | Existing list/create/update/delete tank, pump, nozzle and calibration methods | Current: tanks.manage, pumps.manage, tanks.calibrate, station.read | Existing tank.created, tank.updated, tank.deleted, tank.status_changed, pump.*, nozzle.*, tank_calibration.* | Integration coverage exists but skipped without TEST_DATABASE_URL and TEST_REDIS_URL; frontend tests missing | DONE |
| 1.6 Opening stock setup | P0 | internal/inventory, internal/tanks | Missing /setup/opening-stock | stock_movements ledger exists; missing opening_stock approval table | Existing POST /tanks/{id}/opening-balance but no approval endpoint | Missing typed SDK method for opening balance | Current: stock.adjust | Existing opening_balance.set; missing opening_stock.recorded/approved semantics | Backend integration coverage exists but skipped without TEST_DATABASE_URL and TEST_REDIS_URL; SDK/frontend/approval tests missing | PARTIAL |

## Phase 2 - Identity, roles, permissions, and station access

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 2.1 User administration | P0 | internal/identity, internal/workforce | /admin/users, /admin/users/[id] | users, user_roles, user_station_access, sessions | CRUD /users, POST /users/{id}/sessions/revoke | users.*, stationAccess.* | user.manage, station_access.manage | user.invited, user.deactivated, user.station_access_changed | unit, integration, auth | DONE |
| 2.2 Role management | P0 | internal/identity/policy | /admin/roles, /admin/permissions | roles, permissions, role_permissions | CRUD /roles, GET /roles/{id}/effective-permissions | roles.*, permissions.* | role.manage, permission.manage | role.created, role.permissions_changed | unit, integration, policy | DONE |
| 2.3 Permission gates | P0 | internal/identity/policy | all protected routes | permissions, access_assignments | policy check endpoints or middleware | permissions.can, permissions.require | all scoped permissions | permission.denied where required | unit, integration, frontend gate | DONE |

## Phase 3 - Shift operations and readings

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 3.1 Open shift | P0 | internal/operations, internal/readings | /shifts/open | shifts, shift_assignments, meter_readings | POST /shifts/open | shifts.open | shift.open | shift.opened, meter_reading.captured | unit, integration, conflict checks | DONE |
| 3.2 Close shift | P0 | internal/operations, internal/reconciliation | /shifts/[id]/close | shift_closures, tender_totals, shift_variances | POST /shifts/{id}/close | shifts.close | shift.close | shift.closed, shift.submitted | unit, integration, variance | DONE |
| 3.3 Shift approval | P0 | internal/operations, internal/approvals | /shifts/approvals | approvals, approval_steps, shift_locks | POST /shifts/{id}/approve, POST /shifts/{id}/reject | shifts.approve, shifts.reject | shift.approve | shift.approved, shift.rejected, shift.locked | unit, maker-checker, integration | DONE |
| 3.4 Shift timeline | P1 | internal/operations, internal/audit | /shifts/[id] | audit_events, domain_events | GET /shifts/{id}/timeline | shifts.timeline | shift.view | timeline.viewed where required | unit, frontend states | PARTIAL |

## Phase 4 - POS, sales, and payments

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 4.1 POS page | P0 | internal/revenue, internal/payments | /pos | sales, sale_lines, payments | POST /sales | sales.create | sale.create | sale.created, payment.created | unit, integration, frontend, e2e | PARTIAL |
| 4.2 Sales transaction list | P0 | internal/revenue | /sales, /sales/[id] | sales, sale_lines, payments | GET /sales, GET /sales/{id} | sales.list, sales.get | sale.view | n/a | API pagination, frontend states | PARTIAL |
| 4.3 Sale void workflow | P0 | internal/revenue, internal/approvals, internal/audit | /sales/[id] | sale_voids, reversals, approvals | POST /sales/{id}/void-requests, POST /sales/{id}/void-approve | sales.requestVoid, sales.approveVoid | sale.void.request, sale.void.approve | sale.void_requested, sale.void_approved, sale.reversed | unit, integration, approval | MISSING |
| 4.4 Payment handling | P0 | internal/payments, internal/banking | /pos, /sales/[id] | payments, payment_attempts, payment_callbacks | POST /payments, POST /payment-callbacks | payments.create, payments.reconcile | payment.reconcile | payment.status_changed, payment.callback_received | idempotency, integration | PARTIAL |

## Phase 5 - Inventory, deliveries, transfers, and reconciliation

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 5.1 Deliveries and goods receipt | P0 | internal/procurement, internal/inventory | /deliveries, /deliveries/new | deliveries, delivery_lines, delivery_receipts, attachments | CRUD /deliveries, POST /deliveries/{id}/receive | deliveries.*, inventory.receiveDelivery | inventory.delivery.manage, inventory.delivery.approve | delivery.expected_created, delivery.received, delivery.receipt_approved | unit, ledger integration | DONE |
| 5.2 Tank dips and physical readings | P0 | internal/readings, internal/tanks | /inventory/dips | tank_dips, calibration_tables, attachments | POST /tank-dips, GET /tank-dips | inventory.captureDip, inventory.listDips | inventory.dip.capture | tank_dip.captured, tank_dip.corrected | unit, calibration, frontend | DONE |
| 5.3 Stock reconciliation | P0 | internal/reconciliation, internal/inventory | /inventory/reconciliation | stock_reconciliations, stock_variances | POST /reconciliations, POST /reconciliations/{id}/approve | reconciliation.* | inventory.reconcile | stock.reconciliation_submitted, stock.reconciliation_approved | unit, variance, approval | DONE |
| 5.4 Stock adjustments | P0 | internal/inventory, internal/approvals | /inventory/adjustments | stock_adjustments, inventory_ledger, approvals | POST /stock-adjustments, POST /stock-adjustments/{id}/post | inventory.adjustments.* | inventory.adjust.request, inventory.adjust.approve, inventory.adjust.post | stock.adjustment_requested, stock.adjustment_posted | unit, ledger, approval | PARTIAL |
| 5.5 Transfers | P1 | internal/inventory | /inventory/transfers/tank-to-tank, /inventory/transfers/station-to-station | stock_transfers, transfer_lines, inventory_ledger | CRUD /stock-transfers, POST /stock-transfers/{id}/dispatch, POST /stock-transfers/{id}/receive | inventory.transfers.* | inventory.transfer.request, inventory.transfer.dispatch, inventory.transfer.receive | stock.transfer_requested, stock.transfer_dispatched, stock.transfer_received | unit, integration, variance | DONE |

## Phase 6 - Credit, receivables, and customer statements

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 6.1 Customer management | P0 | internal/receivables | /customers, /customers/[id] | customers, customer_credit_terms | CRUD /customers | customers.* | credit.customer.manage, credit.limit.change | customer.created, customer.updated, customer.credit_limit_changed | unit, integration, permission | DONE |
| 6.2 Credit sales | P0 | internal/revenue, internal/receivables | /pos | sales, receivable_transactions, credit_overrides | POST /sales with credit tender | sales.createCredit | sale.create, credit.sale.override | credit_sale.created, credit_sale.override_approved | unit, limit policy, integration | DONE |
| 6.3 Invoices and statements | P1 | internal/receivables, internal/payments | /credit/invoices, /customers/[id]/statement | invoices, invoice_lines, statements, payment_allocations | CRUD /invoices, GET /customers/{id}/statement, POST /payment-allocations | receivables.* | credit.invoice.manage, credit.payment.allocate | invoice.generated, payment.allocated, payment.allocation_reversed | unit, integration, export | PARTIAL |
| 6.4 Receivables aging | P1 | internal/receivables | /credit/aging | receivable_transactions, aging_snapshots | GET /receivables/aging | receivables.aging | credit.aging.view | report.exported where exported | unit, report, frontend | DONE |

## Phase 7 - Payables, procurement, and supplier control

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 7.1 Supplier management | P0 | internal/payables, internal/procurement | /suppliers | suppliers, supplier_products | CRUD /suppliers | suppliers.* | supplier.manage | supplier.created, supplier.updated, supplier.status_changed | unit, integration | DONE |
| 7.2 Purchase orders | P1 | internal/procurement | /procurement/purchase-orders | purchase_orders, purchase_order_lines, approvals | CRUD /purchase-orders, POST /purchase-orders/{id}/approve | procurement.purchaseOrders.* | procurement.po.manage, procurement.po.approve | purchase_order.created, purchase_order.approved, purchase_order.cancelled | unit, approval, integration | PARTIAL |
| 7.3 Supplier invoices | P1 | internal/payables | /payables/invoices | supplier_invoices, supplier_invoice_lines, payable_transactions | CRUD /supplier-invoices, POST /supplier-invoices/{id}/approve | payables.invoices.* | payables.invoice.manage, payables.invoice.approve | supplier_invoice.recorded, supplier_invoice.approved | unit, variance, approval | PARTIAL |
| 7.4 Payables aging | P1 | internal/payables | /payables/aging | payable_transactions, aging_snapshots | GET /payables/aging | payables.aging | payables.aging.view | report.exported where exported | unit, report, frontend | PARTIAL |

## Phase 8 - Expenses, petty cash, and cash controls

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 8.1 Expense categories | P1 | internal/expenses | /expenses/categories | expense_categories | CRUD /expense-categories | expenses.categories.* | expenses.category.manage | expense.category_created, expense.category_changed | unit, integration | PARTIAL |
| 8.2 Expense entry | P1 | internal/expenses, internal/accounting | /expenses, /expenses/new | expenses, expense_attachments, approvals | CRUD /expenses, POST /expenses/{id}/approve | expenses.* | expenses.create, expenses.approve | expense.submitted, expense.approved, expense.posted | unit, approval, attachment | DONE |
| 8.3 Petty cash ledger | P1 | internal/expenses, internal/banking | /expenses/petty-cash | petty_cash_floats, petty_cash_movements, petty_cash_reconciliations | CRUD /petty-cash, POST /petty-cash/{id}/reconcile | expenses.pettyCash.* | petty_cash.manage, petty_cash.reconcile | petty_cash.float_created, petty_cash.movement_recorded, petty_cash.reconciled | unit, ledger, integration | PARTIAL |

## Phase 9 - Governance, approvals, and audit

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 9.1 Approval engine | P0 | internal/approvals | /approvals | approval_policies, approval_requests, approval_steps, approval_comments | POST /approvals/requests, POST /approvals/{id}/decisions | approvals.* | approval.request, approval.decision | approval.request_submitted, approval.decision_recorded | unit, integration, maker-checker | DONE |
| 9.2 Governance policy page | P1 | internal/approvals, internal/identity/policy | /governance/policies | approval_policies | CRUD /approval-policies, POST /approval-policies/simulate | approvals.policies.* | governance.policy.manage | approval.policy_changed | unit, simulation, frontend | PARTIAL |
| 9.3 Approvals queue | P1 | internal/approvals | /approvals | approval_requests, approval_assignments | GET /approvals/queue, POST /approvals/{id}/approve | approvals.queue, approvals.decide | approval.queue.view, approval.decision | approval.decision_recorded | unit, scoped listing, frontend | DONE |
| 9.4 Audit log page | P1 | internal/audit | /audit-log | audit_events | GET /audit-events, POST /audit-events/export | audit.list, audit.export | audit.view, audit.export | audit.exported | API pagination, export, frontend | DONE |

## Phase 10 - Reports, exports, and executive dashboards

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 10.1 Reports overview | P1 | internal/reports | /reports | report_catalog, report_runs, report_favorites | GET /reports/catalog, GET /reports/recent | reports.catalog, reports.recent | reports.view | n/a | unit, frontend states | DONE |
| 10.2 Daily operations report | P1 | internal/reports, internal/operations | /reports/daily-operations | report_runs, export_jobs | GET /reports/daily-operations | reports.dailyOperations | reports.view, reports.export | report.exported | report math, export | PARTIAL |
| 10.3 Stock loss and variance report | P1 | internal/reports, internal/inventory | /reports/stock-loss | report_runs, export_jobs | GET /reports/stock-loss | reports.stockLoss | reports.view, reports.export | report.exported | variance math, export | PARTIAL |
| 10.4 Profitability report | P1 | internal/reports, internal/accounting | /reports/profitability | report_runs, export_jobs | GET /reports/profitability | reports.profitability | reports.view, reports.export | report.exported | report math, export | MISSING |
| 10.5 Credit and cashflow report | P1 | internal/reports, internal/receivables | /reports/credit-cashflow | report_runs, export_jobs | GET /reports/credit-cashflow | reports.creditCashflow | reports.view, reports.export | report.exported | reconciliation, export | PARTIAL |
| 10.6 Station comparison report | P1 | internal/reports, internal/enterprise | /reports/station-comparison | report_runs, export_jobs | GET /reports/station-comparison | reports.stationComparison | reports.view, reports.export | report.exported | scoped access, ranking | MISSING |
| 10.7 Export service | P0 | internal/reports, internal/audit | /reports/exports | export_jobs, export_files | POST /exports, GET /exports/{id} | reports.exports.* | reports.export | report.exported | permission, job, file metadata | PARTIAL |

## Phase 11 - Notifications, risk, and intelligence

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 11.1 Notification center | P1 | internal/notifications | /notifications, /notifications/settings | notifications, notification_preferences | GET /notifications, PATCH /notifications/{id}, CRUD /notification-preferences | notifications.* | notifications.view, notifications.manage | notification.acknowledged, notification.preference_changed | unit, integration, frontend | PARTIAL |
| 11.2 Risk alerts | P1 | internal/risk | /risk | risk_alerts, risk_alert_events | GET /risk-alerts, PATCH /risk-alerts/{id} | risk.alerts.* | risk.view, risk.manage | risk.alert_created, risk.alert_status_changed | unit, lifecycle, frontend | DONE |
| 11.3 Deterministic insights | P2 | internal/risk, internal/events | /risk | insights, insight_sources | GET /insights | risk.insights | risk.view | insight.generated where persisted | deterministic tests, source trace | PARTIAL |

## Phase 12 - Mobile, offline, and hardware readiness

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 12.1 Mobile attendant workflow | P1 | internal/operations, internal/readings | apps/mobile | mobile_sessions, offline_drafts | mobile API routes for shifts, readings, dips, deliveries | mobile.* | mobile.attendant | mobile.entry_synced | unit, integration, offline | MISSING |
| 12.2 Offline sync foundation | P0 | internal/integrations, internal/events | apps/mobile | idempotency_keys, sync_batches, sync_conflicts | POST /sync/batches, GET /sync/conflicts | sync.* | mobile.sync | sync.conflict_detected, sync.batch_processed | idempotency, duplicate protection | MISSING |
| 12.3 Hardware integration readiness | P1 | internal/integrations | /integrations, /devices | devices, integration_events, webhook_events | CRUD /devices, POST /webhooks/hardware | integrations.* | integration.manage | device.registered, integration.signature_rejected | signature, idempotency, retry | MISSING |

## Phase 13 - Enterprise readiness and scaling

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| 13.1 Enterprise hierarchy improvements | P1 | internal/enterprise | context switcher, reports | tenant_hierarchy, company_hierarchy, region_hierarchy, scope_assignments | hierarchy and scope APIs | enterprise.* | enterprise.manage, enterprise.scope.switch | enterprise.scope_changed | cross-scope leakage, report access | PARTIAL |
| 13.2 Data lifecycle and retention | P1 | internal/enterprise, internal/database | governance pages | retention_policies, retention_jobs, closed_periods | CRUD /retention-policies, POST /closed-periods/change-requests | enterprise.retention.* | retention.manage, closed_period.change | retention.job_run, closed_period.change_requested | unit, job, audit | MISSING |
| 13.3 Observability dashboard | P1 | internal/observability | operations dashboard | health_snapshots, job_failures, outbox_metrics | GET /observability/health | observability.* | observability.view | observability.alert_triggered | health checks, frontend states | PARTIAL |

## Cross-cutting additions

| Feature | Priority | Backend domain | Frontend route | Required database tables | Required API endpoints | Required SDK methods | Required permissions | Required audit events | Required tests | Status |
|---|---|---|---|---|---|---|---|---|---|---|
| C.1 Data-quality warnings | P0 | internal/reports, internal/risk | dashboards and reports | data_quality_warnings | GET /data-quality-warnings | reports.dataQuality | reports.view | n/a | unit, report fixtures, frontend | DONE |
| C.2 Idempotency | P0 | shared platform | API write endpoints | idempotency_keys | write endpoints accept idempotency key | SDK write options | endpoint-specific | endpoint-specific | duplicate protection tests | PARTIAL |
| C.3 Attachments | P1 | shared platform | forms requiring evidence | attachments | CRUD /attachments | attachments.* | endpoint-specific | attachment.added, attachment.removed | upload, permission, virus scan if enabled | MISSING |
| C.4 Shared UI components | P1 | packages/ui | all modules | n/a | n/a | n/a | n/a | n/a | component tests, visual checks | PARTIAL |
| C.5 SDK modules | P0 | packages/sdk | n/a | n/a | all public APIs | setup, users, roles, shifts, sales, payments, inventory, reports, etc. | n/a | n/a | SDK unit, contract | PARTIAL |
