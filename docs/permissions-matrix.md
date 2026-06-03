# FuelGrid OS Permissions Matrix

**Purpose:** Define permission codes, allowed roles, backend enforcement points, frontend gates, and audit expectations.

Permission checks must be tenant-scoped by default. Station, region, company, and workflow-state scope must be applied where relevant.

## Role labels

- `Owner`: Tenant owner or equivalent super-admin for the tenant.
- `Executive`: Cross-company or cross-station executive user.
- `Ops Manager`: Operations manager with multi-station responsibility.
- `Station Manager`: Manager scoped to assigned stations.
- `Finance`: Finance or accounting user.
- `Auditor`: Read-only audit and reporting user.
- `Attendant`: Station attendant with assigned shift access.
- `Admin`: System administration role inside the tenant.

## Matrix

| Permission code | Description | Allowed roles | Backend enforcement point | Frontend gate | Audit requirement |
|---|---|---|---|---|---|
| setup.view | View setup progress and setup warnings. | Owner, Executive, Ops Manager, Station Manager, Admin | setup handlers | /setup | No |
| setup.company.manage | Create, edit, suspend, or close company records. | Owner, Admin | company write handlers | /setup/company | Yes |
| setup.region.manage | Create, edit, assign stations to, or deactivate regions. | Owner, Admin, Ops Manager | region write handlers | /setup/regions | Yes |
| setup.station.manage | Create, edit, configure, suspend, or close stations. | Owner, Admin, Ops Manager | station write handlers | /setup/stations | Yes |
| setup.product.manage | Create, edit, price, activate, or deactivate products. | Owner, Admin, Ops Manager, Finance | product and pricing write handlers | /setup/products | Yes |
| setup.hardware.manage | Create or edit tanks, pumps, nozzles, and mappings. | Owner, Admin, Ops Manager, Station Manager | tank, pump, nozzle write handlers | /setup/tanks, /setup/pumps, /setup/nozzles | Yes |
| setup.opening_stock.manage | Record opening stock. | Owner, Ops Manager, Station Manager, Finance | opening stock write handlers | /setup/opening-stock | Yes |
| setup.opening_stock.approve | Approve and lock opening stock. | Owner, Ops Manager, Finance | opening stock approval handlers | /setup/opening-stock | Yes |
| user.manage | Invite, activate, deactivate, update, or revoke user sessions. | Owner, Admin | user write handlers | /admin/users | Yes |
| role.manage | Create, edit, clone, or deactivate roles. | Owner, Admin | role write handlers | /admin/roles | Yes |
| permission.manage | Assign permissions to roles. | Owner, Admin | role permission handlers | /admin/permissions | Yes |
| permission.view | View effective permissions. | Owner, Admin, Auditor | permission read handlers | /admin/permissions | No |
| station_access.manage | Assign company, region, or station access. | Owner, Admin, Ops Manager | station access handlers | /admin/station-access | Yes |
| shift.view | View permitted shifts. | Owner, Executive, Ops Manager, Station Manager, Finance, Auditor, Attendant | shift read handlers | /shifts, /shifts/[id] | No |
| shift.open | Open a shift for an assigned station. | Ops Manager, Station Manager, Attendant | shift open handler | /shifts/open | Yes |
| shift.close | Close or submit an assigned shift. | Station Manager, Attendant | shift close handler | /shifts/[id]/close | Yes |
| shift.approve | Approve, reject, or request correction for submitted shifts. | Owner, Ops Manager, Station Manager, Finance | shift approval handlers | /shifts/approvals | Yes |
| sale.create | Create sales in a valid active shift context. | Station Manager, Attendant | sale create handler | /pos | Yes |
| sale.view | View permitted sales and sale details. | Owner, Executive, Ops Manager, Station Manager, Finance, Auditor, Attendant | sale read handlers | /sales, /sales/[id] | No |
| sale.void.request | Request a sale void. | Station Manager, Finance | sale void request handler | /sales/[id] | Yes |
| sale.void.approve | Approve or reject a sale void. | Owner, Ops Manager, Finance | sale void approval handler | /sales/[id] | Yes |
| payment.reconcile | Reconcile card, mobile money, voucher, and external payments. | Finance, Ops Manager | payment reconciliation handlers | /sales/[id], reports | Yes |
| inventory.delivery.manage | Create expected deliveries and receive deliveries. | Ops Manager, Station Manager, Finance | delivery write handlers | /deliveries, /deliveries/new | Yes |
| inventory.delivery.approve | Approve delivery receipts and over-tolerance variances. | Owner, Ops Manager, Finance | delivery approval handlers | /deliveries | Yes |
| inventory.dip.capture | Capture tank dips and physical stock readings. | Station Manager, Attendant, Ops Manager | tank dip handlers | /inventory/dips | Yes |
| inventory.reconcile | Submit and approve stock reconciliation. | Owner, Ops Manager, Station Manager, Finance | reconciliation handlers | /inventory/reconciliation | Yes |
| inventory.adjust.request | Request a stock adjustment. | Ops Manager, Station Manager, Finance | stock adjustment request handler | /inventory/adjustments | Yes |
| inventory.adjust.approve | Approve or reject a stock adjustment. | Owner, Ops Manager, Finance | stock adjustment approval handler | /inventory/adjustments | Yes |
| inventory.adjust.post | Post approved stock adjustment to the inventory ledger. | Finance, Ops Manager | stock adjustment post handler | /inventory/adjustments | Yes |
| inventory.transfer.request | Request tank-to-tank or station-to-station stock transfer. | Ops Manager, Station Manager | transfer request handler | /inventory/transfers/* | Yes |
| inventory.transfer.dispatch | Confirm dispatch from source stock. | Ops Manager, Station Manager | transfer dispatch handler | /inventory/transfers/* | Yes |
| inventory.transfer.receive | Confirm receipt into destination stock. | Ops Manager, Station Manager | transfer receipt handler | /inventory/transfers/* | Yes |
| credit.customer.manage | Create, edit, activate, deactivate, or hold customers. | Owner, Finance, Ops Manager | customer write handlers | /customers, /customers/[id] | Yes |
| credit.limit.change | Change customer credit limits or payment terms. | Owner, Finance | customer credit handlers | /customers/[id] | Yes |
| credit.sale.override | Override credit policy where approval allows. | Owner, Finance, Ops Manager | credit sale override handler | /pos | Yes |
| credit.invoice.manage | Generate, send, download, or reverse invoices where allowed. | Finance | invoice handlers | /credit/invoices | Yes |
| credit.payment.allocate | Allocate or reverse customer payments. | Finance | payment allocation handlers | /credit/payments | Yes |
| credit.aging.view | View receivables aging. | Owner, Executive, Finance, Auditor | receivables aging handler | /credit/aging | No |
| supplier.manage | Create, edit, activate, or deactivate suppliers. | Owner, Finance, Ops Manager | supplier write handlers | /suppliers | Yes |
| procurement.po.manage | Create, submit, close, or cancel purchase orders. | Finance, Ops Manager | purchase order handlers | /procurement/purchase-orders | Yes |
| procurement.po.approve | Approve purchase orders. | Owner, Finance, Ops Manager | purchase order approval handler | /procurement/purchase-orders | Yes |
| payables.invoice.manage | Record and edit supplier invoices before approval. | Finance | supplier invoice handlers | /payables/invoices | Yes |
| payables.invoice.approve | Approve supplier invoices. | Owner, Finance | supplier invoice approval handler | /payables/invoices | Yes |
| payables.aging.view | View payables aging. | Owner, Executive, Finance, Auditor | payables aging handler | /payables/aging | No |
| expenses.category.manage | Manage expense categories and approval thresholds. | Owner, Finance, Admin | expense category handlers | /expenses/categories | Yes |
| expenses.create | Create and submit expenses. | Ops Manager, Station Manager, Finance | expense write handlers | /expenses/new | Yes |
| expenses.approve | Approve or reject expenses. | Owner, Finance, Ops Manager | expense approval handler | /expenses | Yes |
| petty_cash.manage | Create floats and record petty cash movements. | Finance, Station Manager | petty cash handlers | /expenses/petty-cash | Yes |
| petty_cash.reconcile | Reconcile petty cash. | Finance, Ops Manager | petty cash reconciliation handler | /expenses/petty-cash | Yes |
| governance.policy.manage | Configure approval and governance policies. | Owner, Admin, Finance | approval policy handlers | /governance/policies | Yes |
| approval.queue.view | View approval queue items assigned to the user. | Owner, Ops Manager, Station Manager, Finance | approval queue handler | /approvals | No |
| approval.decision | Approve, reject, request changes, escalate, or cancel assigned approvals. | Owner, Ops Manager, Station Manager, Finance | approval decision handlers | /approvals | Yes |
| audit.view | View audit log events. | Owner, Executive, Auditor, Admin | audit read handlers | /audit-log | No |
| audit.export | Export audit logs. | Owner, Auditor | audit export handler | /audit-log | Yes |
| reports.view | View permitted reports. | Owner, Executive, Ops Manager, Station Manager, Finance, Auditor | report handlers | /reports/* | No |
| reports.export | Export reports. | Owner, Executive, Finance, Auditor | export handlers | /reports/exports | Yes |
| reports.schedule | Schedule recurring reports. | Owner, Executive, Finance | scheduled report handlers | /reports | Yes |
| notifications.view | View notification center. | Owner, Executive, Ops Manager, Station Manager, Finance, Auditor, Attendant | notification read handlers | /notifications | No |
| notifications.manage | Configure notification settings. | Owner, Admin, user self | notification preference handlers | /notifications/settings | Yes |
| risk.view | View risk alerts and insights. | Owner, Executive, Ops Manager, Station Manager, Finance, Auditor | risk read handlers | /risk | No |
| risk.manage | Resolve, dismiss, or update risk alert status. | Owner, Ops Manager, Finance | risk status handlers | /risk | Yes |
| mobile.attendant | Use mobile attendant workflows for assigned station and shift. | Attendant, Station Manager | mobile API handlers | apps/mobile | Yes |
| mobile.sync | Sync offline drafts with idempotency keys. | Attendant, Station Manager | sync handlers | apps/mobile | Yes |
| integration.manage | Manage devices, integrations, webhooks, and signing keys. | Owner, Admin, Ops Manager | integration handlers | /integrations, /devices | Yes |
| enterprise.manage | Manage enterprise hierarchy and reporting scope. | Owner, Admin, Executive | enterprise handlers | enterprise settings | Yes |
| enterprise.scope.switch | Switch active enterprise context. | Owner, Executive, Ops Manager | context switch handler | context switcher | Yes |
| retention.manage | Configure retention and closed-period policies. | Owner, Admin, Finance | retention handlers | governance settings | Yes |
| closed_period.change | Request or approve closed-period changes. | Owner, Finance | closed period handlers | governance settings | Yes |
| observability.view | View operational health, jobs, outbox, and scheduler state. | Owner, Admin, Ops Manager | observability handlers | observability dashboard | No |
