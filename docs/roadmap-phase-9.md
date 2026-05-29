# Phase 9 - Chain & Enterprise Command

The phase where FuelGrid OS moves from single-station excellence to multi-station command. Earlier phases build reliable station operations, procurement, sales, finance, customer credit, and fleet workflows. Phase 9 gives owners and enterprise operators the controls they need across the whole network: executive command center, regional dashboards, station ranking, central pricing, central procurement, network inventory, consolidated finance, multi-station reports, and enterprise approvals.

Phase 9 does not rebuild station workflows. It coordinates and governs them across companies, regions, and stations. The goal is one operational picture, one approval model, and one control plane over many stations.

## Stack decisions carried forward

All Phase-9 work continues the patterns locked in earlier phases:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps the business change, audit entry, and outbox event |
| Tenant scoping | Every repo query takes `tenantID` first; RLS remains the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, ...)` composite FKs onto parent unique keys |
| Authorization | Station, region, company, and tenant scopes must be explicit; tenant-wide permission does not imply bypassing workflow approval |
| Numeric precision | Money, litres, and rates retain decimal discipline from Phases 4-8 |
| Read models | Enterprise dashboards use materialized/read-model tables fed idempotently from source events where direct OLTP queries would be too expensive |
| Corrections | Enterprise actions issue controlled changes or approvals; they do not mutate posted station history destructively |
| Frontend | shadcn-style primitives in `@fuelgrid/ui`; TanStack Query over the hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 9:

| Concern | Convention |
|---|---|
| Scope hierarchy | Tenant -> company -> region -> station is the primary command hierarchy. Optional station groups can overlay it, but must not replace tenant scoping. |
| Central control | Central policies produce station-scoped effective records. A station's runtime workflow reads effective policy, not draft enterprise policy. |
| Approvals | Enterprise approvals are policy-driven, auditable, and source-linked. No approval should be hidden inside a status update. |
| Consolidation | Consolidated reports aggregate posted source-of-truth data. They never recalculate station facts differently from station reports. |
| Drill-down | Every enterprise metric must drill down to region, station, document, and source event where permissions allow. |

---

## Category A - Enterprise hierarchy and governance

The structure that allows one tenant to govern many stations.

### Stage 1 - Enterprise hierarchy hardening

**Goal:** Companies, regions, stations, and optional groups are formal enterprise dimensions for reporting and control.

- [ ] Review and harden existing company/region/station models for Phase-9 use: parent links, status, ordering, timezone, operating currency, and reporting labels
- [ ] Add optional `station_groups` and `station_group_memberships` for overlays such as highway corridor, brand, dealer, or performance cohort
- [ ] Guard group membership so a station cannot be assigned outside its tenant
- [ ] Repo + handlers + SDK for station groups and hierarchy summary
- [ ] Permission `enterprise_structure.manage`
- [ ] Audit + outbox: `station_group.created`, `station_group.updated`, `station_group.membership_changed`

**Done when:** Enterprise users can organize stations by company, region, and optional group without changing station operational ownership.

---

### Stage 2 - Enterprise scopes and delegated roles

**Goal:** Users can be granted authority at tenant, company, region, station, and group levels.

- [ ] Extend role assignment/scope model to support company, region, and group scopes in addition to station and tenant scopes
- [ ] Effective permission resolver returns allowed station IDs and scope reason for any enterprise permission
- [ ] Guard all Phase-9 reads and writes with explicit scope resolution
- [ ] Admin UI for assigning enterprise roles and reviewing effective access
- [ ] Permissions: `enterprise_access.manage`, `enterprise_access.read`
- [ ] Audit + outbox: `enterprise_role.assigned`, `enterprise_role.revoked`, `enterprise_scope.changed`

**Done when:** A regional manager can see and approve only stations in their region, while a tenant CFO can see consolidated finance across all stations.

---

### Stage 3 - Enterprise approval policies

**Goal:** Cross-station approvals are policy-driven instead of hard-coded per workflow.

- [ ] Migration `approval_policies`, `approval_steps`, `approval_requests`, and `approval_decisions`
- [ ] Policy dimensions: workflow type, amount/litre thresholds, station/region/company scope, role required, sequential vs parallel approval, expiry, escalation
- [ ] Generic approval service that workflows can call before finalizing central price changes, procurement releases, finance payments, credit overrides, and period locks
- [ ] Approval lifecycle: `requested -> approved -> rejected -> cancelled -> expired`
- [ ] Permissions: `approval_policy.manage`, `approval_request.decide`
- [ ] Audit + outbox: `approval.requested`, `approval.approved`, `approval.rejected`, `approval.escalated`

**Done when:** A high-value supplier payment, central price change, or bulk procurement order can require enterprise approval based on policy.

---

## Category B - Enterprise data and command read models

Fast, consistent cross-station views.

### Stage 4 - Enterprise event projections

**Goal:** Source events from operations, inventory, procurement, sales, finance, credit, and fleet feed enterprise read models idempotently.

- [ ] Create projection tables for daily station KPIs, product KPIs, inventory KPIs, finance KPIs, customer credit KPIs, and risk-ready dimensions
- [ ] Projection consumers keyed by source event ID, tenant, station, business date, and projection type
- [ ] Backfill command to rebuild projections from posted source tables and audit/outbox history
- [ ] Projection freshness metadata with lag and failure status
- [ ] Permission `enterprise_projection.admin` for rebuild/replay tools
- [ ] Audit + outbox: `enterprise_projection.rebuilt`, `enterprise_projection.failed`

**Done when:** Enterprise dashboards load from read models and can be rebuilt without double-counting historical events.

---

### Stage 5 - Executive command center

**Goal:** Owners get a single first screen for the network: sales, margin, inventory, cash, exceptions, and risk indicators.

- [ ] Route `/enterprise`: KPI cards for revenue, litres sold, gross margin, inventory on hand, stockout risk, cash variance, overdue AP/AR, open incidents, and approvals waiting
- [ ] Backend `GET /api/v1/enterprise/overview` with date range, company, region, group, station, and product filters
- [ ] Drill-down from tenant to company, region, station, product, and source report
- [ ] Data freshness indicator for each projection domain
- [ ] Permission gate `enterprise.read`
- [ ] Mobile responsive owner view

**Done when:** An owner can open `/enterprise` and understand the current state of the network with drill-down to source workflows.

---

### Stage 6 - Regional dashboards and station ranking

**Goal:** Managers can compare stations fairly and find outliers.

- [ ] Regional dashboard route `/enterprise/regions/{id}` with station KPIs, product mix, inventory health, cash variance, incidents, and approval queue
- [ ] Station ranking model with configurable metrics: litres, revenue, margin, stock variance, cash variance, uptime, open exceptions, overdue close, customer credit exposure
- [ ] Normalize rankings by operating days, active nozzles, product availability, and station status where practical
- [ ] Trend and benchmark views by week/month
- [ ] Permission gates follow region/company scope resolution
- [ ] Audit export events for ranking/report downloads

**Done when:** A regional manager can rank stations, identify top/bottom performers, and drill into the operational reason behind a score.

---

## Category C - Central commercial control

Central policies that become station-effective operations.

### Stage 7 - Central pricing

**Goal:** Enterprise users can define, approve, schedule, and roll out product pricing across stations.

- [ ] Migration `central_price_books`, `central_price_rules`, `price_rollouts`, and `station_price_effective_records`
- [ ] Pricing scope: tenant, company, region, group, station, product, customer segment where supported
- [ ] Rollout lifecycle: `draft -> pending_approval -> approved -> scheduled -> active -> superseded -> cancelled`
- [ ] Approval integration for threshold changes or emergency overrides
- [ ] Phase-6 pricing engine reads station-effective price records and snapshots the applied price on sale
- [ ] Permissions: `central_pricing.manage`, `central_pricing.approve`, `central_pricing.publish`
- [ ] Audit + outbox: `central_price_rollout.created`, `central_price_rollout.approved`, `central_price_rollout.published`, `station_price.activated`

**Done when:** A central PMS price can be scheduled for selected regions/stations, approved, activated at the right time, and used by Phase-6 sales.

---

### Stage 8 - Central procurement planning

**Goal:** Procurement teams can coordinate replenishment across stations using network inventory and supplier constraints.

- [ ] Migration `central_procurement_plans`, `central_procurement_plan_lines`, and `station_procurement_allocations`
- [ ] Plan inputs: station inventory, expected sales run-rate, open POs, supplier availability, truck capacity, lead time, product, and target stock cover
- [ ] Plan lifecycle: `draft -> reviewed -> approved -> released -> closed`
- [ ] Release creates station-scoped Phase-5 purchase orders or allocation requests, idempotently linked to the plan line
- [ ] Approval integration for high-value or bulk supplier commitments
- [ ] Permissions: `central_procurement.manage`, `central_procurement.approve`, `central_procurement.release`
- [ ] Audit + outbox: `central_procurement_plan.created`, `central_procurement_plan.approved`, `central_procurement_plan.released`

**Done when:** A central procurement plan can allocate PMS/AGO deliveries to multiple stations and release station POs without duplicate orders.

---

### Stage 9 - Network inventory and transfers

**Goal:** Enterprise users can see inventory across the network and manage controlled inter-station transfers where allowed.

- [ ] Network inventory read model by product, station, tank, available litres, ullage, days cover, stockout risk, and overstock risk
- [ ] Migration `stock_transfer_orders` and `stock_transfer_lines` using the Phase-4 `transfer` movement type for posted stock movements
- [ ] Transfer lifecycle: `draft -> approved -> dispatched -> received -> closed`; `cancelled` terminal before dispatch
- [ ] Guard source station availability and destination tank compatibility
- [ ] Receiving posts paired transfer-out and transfer-in stock movements with source links and audit
- [ ] Permissions: `stock_transfer.manage`, `stock_transfer.approve`, `stock_transfer.receive`
- [ ] Audit + outbox: `stock_transfer.approved`, `stock_transfer.dispatched`, `stock_transfer.received`

**Done when:** A manager can identify overstock/stockout imbalance and move product between stations through an audited transfer workflow.

---

## Category D - Consolidated finance and reporting

Multi-station financial control without corrupting station-level truth.

### Stage 10 - Consolidated finance views

**Goal:** Finance teams can see consolidated P&L, balance sheet, cash, AP, AR, and margin across the network.

- [ ] Consolidated finance read models over Phase-7 posted journal lines and Phase-6/5/8 source dimensions
- [ ] Filters by company, region, group, station, product, supplier, customer, and accounting period
- [ ] Reports: consolidated P&L, balance sheet, trial balance, cash position, AP aging, AR aging, margin by station/product
- [ ] Drill-down to station report, journal entry, source document, and source event
- [ ] Permission `enterprise_finance.read`; export requires `enterprise_finance.export`
- [ ] Audit + outbox: `enterprise_finance_export.generated`

**Done when:** Tenant finance can reconcile consolidated totals to station finance reports and then to journal lines.

---

### Stage 11 - Multi-station report builder

**Goal:** Operators can create reusable reports across stations without writing SQL.

- [ ] Report definition model with dataset, dimensions, measures, filters, sort, grouping, sharing scope, and owner
- [ ] Supported datasets: sales, inventory, procurement, finance, customer credit, fleet, cash, incidents, approvals
- [ ] Saved views and scheduled export metadata, with actual scheduled automation deferred unless Phase-11 automation exists
- [ ] CSV/XLSX export with audited filter and row count metadata
- [ ] Permission `enterprise_report.manage`, `enterprise_report.export`
- [ ] UI route `/enterprise/reports`

**Done when:** A regional manager can save a report for station cash variance by week and export it for their scoped stations only.

---

## Category E - Enterprise operations UX

The control-room surfaces for multi-station work.

### Stage 12 - Approval and exception command queue

**Goal:** Enterprise users can see and act on approvals and exceptions across their scope.

- [ ] Route `/enterprise/approvals`: approvals grouped by workflow, severity, due date, scope, and requester
- [ ] Route `/enterprise/exceptions`: unresolved operational exceptions from shifts, inventory, procurement, finance, credit, and fleet
- [ ] Bulk approve/reject only where policy allows; each item still writes an individual audit decision
- [ ] Escalation metadata: overdue, reassigned, delegated, and comment history
- [ ] Permission gates from approval policy and effective scope
- [ ] Audit + outbox: `enterprise_queue.actioned`, `enterprise_exception.escalated`

**Done when:** A regional or tenant approver can clear their queue without opening every station workflow one by one.

---

### Stage 13 - Enterprise settings and rollout console

**Goal:** Central policies can be configured, previewed, and rolled out safely.

- [ ] Route `/enterprise/settings`: hierarchy, groups, central pricing, approval policies, procurement planning defaults, report sharing, and projection health
- [ ] Rollout preview showing affected stations, products, customers, and effective dates before publish
- [ ] Dry-run endpoint for pricing/procurement/approval policy changes
- [ ] Rollback/cancel rules for scheduled but not-yet-active rollouts
- [ ] Permission gates per policy domain
- [ ] Audit + outbox for every rollout preview, publish, cancel, and rollback

**Done when:** A central operator can preview and safely publish a policy change with clear impact before it affects station workflows.

---

## Phase 9 acceptance criteria

Phase 9 is complete when all of the following are true:

1. Enterprise hierarchy supports company, region, station, and optional group reporting/control.
2. Users can be scoped at tenant, company, region, group, and station levels.
3. Approval policies can be defined and reused by central workflows.
4. Enterprise read models are idempotent, rebuildable, and show freshness/lag.
5. Executive and regional dashboards show network health with drill-down to source facts.
6. Stations can be ranked by configurable metrics with fair normalization where available.
7. Central pricing can be approved, scheduled, published, and consumed by Phase-6 sales.
8. Central procurement can plan and release station-scoped Phase-5 purchase orders.
9. Network inventory supports cross-station visibility and controlled stock transfers.
10. Consolidated finance ties to Phase-7 posted journal lines and station-level reports.
11. Enterprise approvals/exceptions can be worked from a scoped command queue.
12. Every enterprise policy, rollout, approval, and export writes audit + outbox.

---

## Out of scope for Phase 9 intentionally

- Fraud scoring, anomaly detection, and investigations - Phase 10.
- Forecasting and automated replenishment suggestions - Phase 11.
- AI summaries and natural-language analytics - Phase 12.
- Hardware, pump controller, tank gauge, bank, POS, or ERP integrations - Phase 13.
- Offline mobile enterprise workflows - Phase 14.
- Cross-tenant benchmarking unless explicitly productized with privacy controls.

---

## Cross-phase considerations

- Phase 9 central pricing must feed Phase-6 effective pricing without changing historical sale price snapshots.
- Central procurement must release Phase-5 station POs instead of creating a parallel procurement ledger.
- Network inventory must read and post through the Phase-4 stock ledger. Transfers use the reserved `transfer` movement type with paired source links.
- Consolidated finance must aggregate Phase-7 posted journal lines and never recalculate finance differently from station finance.
- Phase 10 risk scoring will consume Phase-9 hierarchy, station ranking, approvals, and rollout history. Preserve these dimensions in audit/outbox events.

If any of these contracts change, Phase 9 migration and projection design must be revisited before implementation.
