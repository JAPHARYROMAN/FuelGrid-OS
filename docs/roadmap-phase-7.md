# Phase 7 - Finance & Accounting Control

The phase where FuelGrid OS turns operational truth into financial control. Phases 3 and 4 establish trusted shift and inventory facts. Phase 5 creates supplier payables from procurement. Phase 6 prices sales, records payment methods, and produces revenue facts. Phase 7 consumes those facts and gives the business a controlled finance layer: cash reconciliation, bank deposits, expenses, customer invoices, supplier payments, financial reports, aging, and accounting exports.

Phase 7 does not replace station operations, procurement, or sales. It adds the financial system of record around them. Every financial change must be tenant-scoped, auditable, reversible by correction entry, and explainable back to the operational event that produced it.

## Stack decisions carried forward

All Phase-7 work continues the patterns locked in earlier phases:

| Concern | Continued choice |
|---|---|
| Backend transactions | One tx wraps the business change, audit entry, and outbox event |
| Tenant scoping | Every repo query takes `tenantID` first; RLS remains the safety net |
| Tenant-bound FKs | Children carry `(tenant_id, ...)` composite FKs onto parent unique keys |
| Authorization | `requirePermission(code, scopeExtractor)` for URL-scoped routes, `authorizeStation(...)` for row/body station checks, `requirePermissionHeld(code)` for tenant-wide finance reads |
| Migrations | One concern per file; system permissions seeded inline |
| Numeric precision | Money `numeric(14, 2)`; rates `numeric(14, 4)`; quantities continue as `numeric(14, 3)` where litres are involved. No `float64`/JS `number` for accounting values. |
| Corrections | No destructive edits to posted finance facts. Corrections are reversal or adjustment entries with audit trail. |
| Frontend | shadcn-style primitives in `@fuelgrid/ui`; TanStack Query over the hand-written `@fuelgrid/sdk` |

New conventions specific to Phase 7:

| Concern | Convention |
|---|---|
| Finance source of truth | Posted journal entries are the financial source of truth. Operational tables remain the operational source of truth. |
| Double entry | Every posted journal entry must balance: total debits equal total credits per tenant, currency, and entry. |
| Source links | Journal entries and settlement rows link to their source event or document: shift close, sale, payment, payable, expense, deposit, invoice, or correction. |
| Period control | Accounting periods move `open -> closing -> closed -> locked`. Posted entries in closed periods require a permitted adjustment flow. Locked periods are immutable. |
| Station vs tenant scope | Cash/deposits are station-scoped. Chart of accounts, accounting periods, supplier AP, consolidated reports, and exports are tenant-wide, with station dimensions where relevant. |
| Idempotency | Outbox consumers create finance documents idempotently from source event IDs. Replays must not duplicate payables, receivables, or journal entries. |

---

## Category A - Accounting foundation

The accounting rails everything else posts through.

### Stage 1 - Chart of accounts

**Goal:** A tenant has a controlled chart of accounts that every finance posting uses.

- [ ] Migration `accounts` with tenant, code, name, type (`asset`, `liability`, `equity`, `income`, `expense`, `contra_asset`, `contra_income`), parent account, status, and normal balance
- [ ] Seed a default fuel-retail chart: cash on hand, bank clearing, bank accounts, accounts receivable, inventory, accounts payable, sales revenue, discounts, COGS, fuel purchases, freight/duty/levies, operating expenses, cash over/short
- [ ] Permission `account.manage` for tenant-wide account setup; `finance.read` for reads
- [ ] Repo + handlers + SDK: list, get, create, update, deactivate, restore
- [ ] Guard deactivation when an account has posted journal lines or is mapped as a system account
- [ ] Audit + outbox: `account.created`, `account.updated`, `account.deactivated`

**Done when:** A tenant can maintain its chart of accounts, system mappings exist, and blocked accounts cannot receive new postings.

---

### Stage 2 - Accounting periods and posting controls

**Goal:** Finance users can control when entries are allowed and prevent back-dated changes after a period is closed.

- [ ] Migration `accounting_periods` with tenant, start date, end date, status, close metadata, lock metadata, and unique non-overlapping period rules
- [ ] Period state machine: `open -> closing -> closed -> locked`; reopen from `closed` requires `period.reopen`, while `locked` is terminal except platform-admin repair
- [ ] Posting guard that every finance write calls before creating journal entries
- [ ] Permissions: `period.manage`, `period.close`, `period.lock`
- [ ] Repo + handlers + SDK: list periods, create next period, start close, close, reopen, lock
- [ ] Audit + outbox: `accounting_period.created`, `accounting_period.closed`, `accounting_period.reopened`, `accounting_period.locked`

**Done when:** A journal entry cannot be posted into a closed period without the adjustment flow, and cannot be posted into a locked period at all.

---

### Stage 3 - Journal engine

**Goal:** Every financial document posts balanced, traceable double-entry accounting.

- [ ] Migration `journal_entries` and `journal_lines` with tenant, period, entry number, source type/id, station dimension, status, memo, posted_by, posted_at, reversal links, debit, credit, and account
- [ ] Enforce balanced entries at the repository/service boundary and with a deferred database check or trigger where practical
- [ ] Entry lifecycle: `draft -> posted -> reversed`; posted entries are immutable except reversal metadata
- [ ] Build a posting service used by cash reconciliation, deposits, AP, AR, expenses, and adjustments
- [ ] Permission `journal.read` for views and `journal.adjust` for manual adjustments
- [ ] Repo + handlers + SDK: list, get, create draft adjustment, post adjustment, reverse posted entry
- [ ] Audit + outbox: `journal_entry.posted`, `journal_entry.reversed`

**Done when:** Posting a transaction creates balanced journal lines, reversing it creates a new balanced reversal, and the original entry remains unchanged.

---

## Category B - Cash and banking

The daily money movement from stations into the bank.

### Stage 4 - Cash reconciliation

**Goal:** Finance verifies expected cash from shifts against counted cash and records over/short cleanly.

- [ ] Migration `cash_reconciliations` and `cash_reconciliation_lines` linked to operating day, shift close, station, attendant, expected cash, counted cash, variance, status, and reviewer
- [ ] Consume Phase-6 payment breakdowns, not raw meter data, as the expected cash source
- [ ] Reconciliation lifecycle: `draft -> submitted -> approved -> posted`; `rejected` returns to draft with reason
- [ ] Variance rules: within tolerance auto-classifies; above tolerance requires reason and approval
- [ ] Posting: debit cash on hand, credit sales clearing; variance posts to cash over/short
- [ ] Permissions: `cash_reconciliation.manage`, `cash_reconciliation.approve`, `finance.read`
- [ ] Audit + outbox: `cash_reconciliation.submitted`, `cash_reconciliation.approved`, `cash_reconciliation.posted`

**Done when:** A station day's cash is reconciled against Phase-6 expected cash, variances are explicit, and approval posts balanced journal entries.

---

### Stage 5 - Bank deposits

**Goal:** Cash movements from station safe to bank are recorded, tracked, and matched against bank confirmation.

- [ ] Migration `bank_accounts`, `bank_deposits`, and `bank_deposit_lines` with station, source reconciliations, deposit slip number, cash amount, expected bank date, actual bank date, status, and attachments metadata
- [ ] Deposit lifecycle: `draft -> prepared -> in_transit -> confirmed -> posted`; `voided` requires reversal if already posted
- [ ] Posting: move funds from cash on hand to bank clearing at preparation, then from bank clearing to bank account on confirmation
- [ ] Guard against depositing the same approved cash reconciliation twice
- [ ] Permissions: `bank_account.manage`, `bank_deposit.manage`, `bank_deposit.confirm`
- [ ] Audit + outbox: `bank_deposit.prepared`, `bank_deposit.confirmed`, `bank_deposit.posted`, `bank_deposit.voided`

**Done when:** Approved cash can be grouped into a deposit, confirmed by bank date/reference, and posted without double-counting any shift/day cash.

---

### Stage 6 - Bank statement import and matching

**Goal:** Finance can import bank statement lines and match deposits, supplier payments, customer payments, and fees.

- [ ] Migration `bank_statement_imports` and `bank_statement_lines` with account, statement period, import hash, transaction date, value date, amount, reference, description, status, and matched document
- [ ] CSV import validation: header mapping, duplicate detection, amount/date parsing, row-level error report
- [ ] Matching engine: exact reference match first, then amount/date tolerance suggestions
- [ ] Actions: match, split match, unmatch, mark bank fee, mark unknown
- [ ] Posting for bank fees and unresolved adjustments through the journal engine
- [ ] Permission `bank_statement.manage`
- [ ] Audit + outbox: `bank_statement.imported`, `bank_statement_line.matched`, `bank_statement_line.unmatched`

**Done when:** A statement import can match confirmed deposits and supplier/customer payments, with unmatched lines visible for follow-up.

---

## Category C - Payables and supplier settlement

The Finance phase consumes approved supplier invoices from Phase 5 and pays them.

### Stage 7 - Accounts payable ledger

**Goal:** Approved supplier invoices become payable documents with aging and controlled settlement status.

- [ ] Migration `payables` and `payable_lines` created idempotently from Phase-5 `payable.created` outbox events
- [ ] Payable fields: supplier, source invoice, invoice number, invoice date, due date, terms, amount, outstanding amount, status, station/product dimensions where available
- [ ] Payable lifecycle: `open -> partially_paid -> paid`; `voided` requires reversal and source-document reason
- [ ] Posting: credit accounts payable and debit inventory/COGS/expense clearing based on source mapping
- [ ] Permissions: `payable.read`, `payable.manage`
- [ ] Audit + outbox: `payable.created`, `payable.adjusted`, `payable.voided`

**Done when:** A Phase-5 approved supplier invoice appears once as an open payable, ages by due date, and posts to AP through the journal engine.

---

### Stage 8 - Supplier payments

**Goal:** Finance records supplier payments and clears accounts payable when money leaves the business.

- [ ] Migration `supplier_payments` and `supplier_payment_allocations` with supplier, bank/cash account, payment method, reference, payment date, amount, status, and allocated payables
- [ ] Payment lifecycle: `draft -> approved -> paid -> posted`; `voided` creates a reversal when already posted
- [ ] Partial payment and multi-invoice allocation support
- [ ] Guard: total allocations cannot exceed payment amount or payable outstanding balance
- [ ] Posting: debit accounts payable, credit bank/cash
- [ ] Permissions: `supplier_payment.manage`, `supplier_payment.approve`, `supplier_payment.post`
- [ ] Audit + outbox: `supplier_payment.approved`, `supplier_payment.paid`, `supplier_payment.posted`, `supplier_payment.voided`

**Done when:** A payment can settle one or more supplier invoices, reduce payable aging, and post a balanced AP-clearing journal entry.

---

## Category D - Receivables and customer settlement

The finance layer for customer invoices and payments, while deeper fleet credit remains Phase 8.

### Stage 9 - Customer invoices

**Goal:** Credit and billable sales from Phase 6 can become customer invoices without building the full Phase-8 fleet-credit system yet.

- [ ] Migration `customers` minimal finance profile, `customer_invoices`, and `customer_invoice_lines`
- [ ] Source support: Phase-6 credit sale, manual finance invoice, adjustment invoice
- [ ] Invoice lifecycle: `draft -> issued -> partially_paid -> paid -> voided`
- [ ] Posting: debit accounts receivable, credit revenue/tax/clearing accounts
- [ ] Guard: issued invoices are immutable except credit note / reversal workflow
- [ ] Permissions: `customer.manage`, `customer_invoice.manage`, `customer_invoice.issue`
- [ ] Audit + outbox: `customer_invoice.created`, `customer_invoice.issued`, `customer_invoice.voided`

**Done when:** A credit sale or manual finance charge can be issued as an invoice, posted to AR, and shown in customer aging.

---

### Stage 10 - Customer payments

**Goal:** Finance records customer payments and allocates them against invoices.

- [ ] Migration `customer_payments` and `customer_payment_allocations` with customer, method, bank/cash account, reference, payment date, amount, status, and allocated invoices
- [ ] Support unapplied payments, partial allocations, and overpayment credit balances
- [ ] Posting: debit bank/cash, credit accounts receivable or unapplied customer credits
- [ ] Match imported bank lines to customer payments from Stage 6
- [ ] Permissions: `customer_payment.manage`, `customer_payment.post`
- [ ] Audit + outbox: `customer_payment.received`, `customer_payment.allocated`, `customer_payment.posted`, `customer_payment.voided`

**Done when:** A customer payment can be recorded, matched to one or more invoices, reduce AR aging, and reconcile to the bank statement.

---

## Category E - Expenses and petty cash

Non-fuel operational spend controlled without bypassing accounting.

### Stage 11 - Expenses

**Goal:** Operating expenses are captured, approved, and posted with account/category discipline.

- [ ] Migration `expense_categories`, `expenses`, and `expense_lines` with station, vendor/payee, category/account, date, amount, tax fields, receipt attachment metadata, status, and approver
- [ ] Expense lifecycle: `draft -> submitted -> approved -> posted`; `rejected` returns to draft; `voided` reverses if posted
- [ ] Configurable approval threshold by amount and station/tenant
- [ ] Posting: debit expense accounts, credit cash/bank/AP depending on payment mode
- [ ] Permissions: `expense.manage`, `expense.approve`, `expense.post`
- [ ] Audit + outbox: `expense.submitted`, `expense.approved`, `expense.posted`, `expense.voided`

**Done when:** A station expense can be submitted with category/account, approved, posted, and reversed without deleting history.

---

### Stage 12 - Petty cash

**Goal:** Petty cash floats, top-ups, and spend are controlled per station or cash custodian.

- [ ] Migration `petty_cash_floats`, `petty_cash_transactions`, and `petty_cash_reconciliations`
- [ ] Float lifecycle: `active -> suspended -> closed`
- [ ] Transaction types: top-up, spend, reimbursement, adjustment, transfer
- [ ] Reconciliation compares expected float balance to counted cash and posts variance to cash over/short
- [ ] Guard: no transaction can overdraw a float unless a permitted override is recorded
- [ ] Permissions: `petty_cash.manage`, `petty_cash.approve`, `petty_cash.reconcile`
- [ ] Audit + outbox: `petty_cash.topup`, `petty_cash.spend`, `petty_cash.reconciled`

**Done when:** A station can run a petty cash float, record spend, top up the float, and reconcile the balance with posted variances.

---

## Category F - Financial reporting and exports

Finance needs decision-grade statements and exportable accounting data.

### Stage 13 - Core finance reports

**Goal:** Finance can view reliable P&L, balance sheet, cash position, AP aging, AR aging, and account activity.

- [ ] Reporting queries over posted journal lines with station, account, period, supplier, customer, product, and source filters
- [ ] Reports: profit/loss, balance sheet, trial balance, general ledger, cash position, AP aging, AR aging, expense summary, supplier statement, customer statement
- [ ] Reconciliation badges: unposted documents, unmatched bank lines, open deposits, overdue payables, overdue receivables
- [ ] Permission `finance.read`; sensitive export requires `finance.export`
- [ ] SDK methods and frontend report routes under `/finance/reports`
- [ ] Audit read/export events for sensitive reports

**Done when:** Posted finance activity can be summarized into P&L, balance sheet, AP aging, and AR aging that tie back to journal lines and source documents.

---

### Stage 14 - Accounting exports

**Goal:** Finance can export clean accounting data for external accountants or downstream accounting systems.

- [ ] Export profiles with mapped account codes, date formats, currency, tax labels, and file type (`csv`, `xlsx`, later API connector)
- [ ] Exports: journal entries, trial balance, AP aging, AR aging, bank reconciliation, expenses
- [ ] Export run table with filters, generated file metadata, checksum, actor, and generated_at
- [ ] Guard: exports from locked periods are reproducible; exports from open periods are marked provisional
- [ ] Permission `finance.export`
- [ ] Audit + outbox: `accounting_export.generated`

**Done when:** A tenant can export a period's journal entries and trial balance, re-run the same locked-period export, and get the same totals.

---

## Category G - Finance workspaces

The daily UX for finance users.

### Stage 15 - Finance dashboard

**Goal:** A finance manager sees the business's cash, payables, receivables, and close status from one screen.

- [ ] Route `/finance`: cash position, pending reconciliations, deposits in transit, AP due/overdue, AR due/overdue, open expenses, unmatched bank lines, period close status
- [ ] Backend `GET /api/v1/finance/overview` returning dashboard cards and actionable queues
- [ ] Drill-through links to cash reconciliation, deposits, AP, AR, expenses, reports, and period close
- [ ] Permission gate `finance.read`
- [ ] Mobile responsive for owner/operator review

**Done when:** `/finance` tells a manager what needs attention today and links directly to the workflow that resolves it.

---

### Stage 16 - Close and control console

**Goal:** Finance can run a period close checklist and see blocking issues before locking a period.

- [ ] Route `/finance/close`: period selector, checklist, blockers, close actions, lock actions
- [ ] Close checks: unposted cash reconciliations, unconfirmed deposits, unmatched bank lines, open payables, open expenses awaiting posting, unposted customer invoices/payments, unbalanced journal drafts
- [ ] Close workflow writes checklist snapshots so later audits can see why a period was closed
- [ ] Permission gates `period.close`, `period.lock`
- [ ] Audit + outbox for close checklist completion and lock

**Done when:** A finance user can run the close checklist, resolve blockers, close the period, and lock it when final.

---

## Phase 7 acceptance criteria

Phase 7 is complete when all of the following are true:

1. A tenant has a chart of accounts, accounting periods, and a balanced journal engine.
2. Posted finance entries are immutable and corrected through reversals or adjustment entries.
3. Cash reconciliation consumes Phase-6 payment facts and posts approved cash variances.
4. Bank deposits move approved cash to bank and can be matched to imported bank statement lines.
5. Phase-5 supplier invoices become payables exactly once, age correctly, and settle through supplier payments.
6. Customer invoices and customer payments post to AR and support aging, allocation, and bank matching.
7. Expenses and petty cash post through controlled approval and reconciliation flows.
8. P&L, balance sheet, trial balance, AP aging, AR aging, and general ledger reports tie back to posted journal lines.
9. Accounting exports are permissioned, audited, and reproducible for locked periods.
10. Finance users have a dashboard and close console that surface blockers before period close.

---

## Out of scope for Phase 7 intentionally

Reserved for later phases:

- Full fleet/customer-credit operating system: vehicles, drivers, fuel cards, QR/RFID authorization, odometer capture, credit scoring - Phase 8.
- Multi-company consolidation, intercompany eliminations, regional finance dashboards - Phase 9.
- Fraud scoring, anomaly detection, investigation workflow, predictive cash-risk models - Phase 10.
- Payroll, HR, tax filing, statutory e-invoicing integrations, and automated bank payment execution.
- Direct accounting-system API connectors. Phase 7 exports clean files; connectors can follow after the accounting data model proves stable.
- Multi-currency revaluation and FX gains/losses unless explicitly required by a tenant before implementation starts.

---

## Cross-phase considerations

- Phase 5's `payable.created` event is the contract for accounts payable. Do not create finance payables by re-reading procurement tables without idempotency against the source event/document.
- Phase 6's sales and payment events are the contract for cash reconciliation, customer receivables, and revenue posting. Phase 7 should not re-price sales from nozzle defaults or meter deltas.
- Phase 4's stock ledger remains the litre source of truth. Phase 7 can report inventory value only from posted financial entries and landed-cost/source mappings, not by mutating stock movements.
- Period locking is a hard control boundary. Later analytics and enterprise reporting must read locked finance periods as immutable history.
- Every report total must drill down to journal lines, and every journal line must drill back to a source document or manual adjustment reason.

If any of these contracts change, Phase 7 migration sequencing must be revisited before implementation.
