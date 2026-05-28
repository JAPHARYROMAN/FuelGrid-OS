# Audit 09 — Accounting / General Ledger / Payables / Expenses / Finance Reports (Phase 7)

Read-only, atomic-level audit of the double-entry finance core. Priority: **double-entry GL correctness**. RLS is treated as inert at runtime (per prior audits); the focus is the app layer and double-entry integrity.

## Scope (files + approximate LOC)

Domain repos (`./internal`):
- `accounting/accounts.go` (182) — chart of accounts, system-key resolution, seed defaults.
- `accounting/journal.go` (270) — posting primitive, balance check, reversal.
- `accounting/periods.go` (171) — period lifecycle + posting guard.
- `accounting/reports.go` (138) — trial balance, P&L, balance sheet, GL.
- `accounting/exports.go` (107) — journal export, checksum/provisional, run log.
- `payables/repo.go` (173) — AP ledger, import, allocation drawdown, aging.
- `payables/supplier_payments.go` (106) — supplier payment header + allocations.
- `expenses/repo.go` (25), `expenses/expenses.go` (231) — categories + expense lifecycle.
- `expenses/petty_cash.go` (223) — floats, transactions, reconciliation.

API handlers (`./services/api/internal/server`):
- `accounting_handlers.go` (582), `payables_handlers.go` (280), `expenses_handlers.go` (663), `finance_handlers.go` (179), `export_handlers.go` (161), `close_handlers.go` (73).
- Routing: `server.go:625-750`.

Migrations: `0037_accounts`, `0038_accounting_periods`, `0039_journals`, `0040_payables`, `0041_supplier_payments`, `0047_expenses`, `0048_petty_cash`, `0049_accounting_exports`. Tests: `phase7_integration_test.go` (554).

---

## Flow 1 — Chart of accounts

`defaultChart` (`accounts.go:62-80`) seeds 17 system accounts, each tagged with a `system_key`. Types are checked at the DB (`chk_accounts_type`, `0037:21-23`); `normal_balance` constrained to debit/credit. `SeedDefaultChart` (`accounts.go:85-99`) is idempotent via `ON CONFLICT (tenant_id, lower(code)) DO NOTHING` and returns the count actually inserted — re-running yields 0 created. Confirmed by the test (`seeded["created"] == 17` then a second run is implied idempotent).

`resolveSystemAccount` (`accounts.go:172-181`) maps a `system_key` to an id, **filtering on `status = 'active'`**. If a system account is missing or deactivated, posting fails with `ErrSystemAccount` → `400` "seed the chart of accounts" (`accounting_handlers.go:576`). This is the correct fail-closed behaviour.

**Parent hierarchy** is enforced by a composite self-FK `accounts_parent_fk (tenant_id, parent_id)` (`0037:35-36`) — tenant-safe. However, `CreateAccount` (`accounts.go:130-141`) accepts `parent_id` but the handler `createAccountRequest` (`accounting_handlers.go:187-193`) **does pass ParentID** — OK. There is **no cycle guard** on parent chains: account A can be parented to B and B to A (or a self-parent where `id = parent_id` after creation via a future update path). No update path exposes `parent_id` today, so this is latent (Low).

**system_key on manual create:** `createAccountRequest` does not expose `system_key`, and `CreateAccount` always passes `in.SystemKey` (nil for manual accounts). So users cannot mint a duplicate/competing system account — good. The unique partial index `idx_accounts_system_key` (`0037:30-31`) guarantees one account per key per tenant.

**Deactivation guard:** `handleUpdateAccount` (`accounting_handlers.go:257-267`) blocks setting `inactive` when `AccountHasPostings` is true. Note the check runs **outside** the write tx (`accounting_handlers.go:258`, using `r.pool`), then the update runs in a separate tx — a TOCTOU window where a posting lands between the check and the update. Low severity (deactivating a freshly-used account is a rare admin race).

## Flow 2 — Journal entries & lines (the posting primitive)

`PostEntry` (`journal.go:100-152`): resolves the period, inserts the header, inserts each line (resolving account by id or system_key), then verifies balance in SQL:

```sql
SELECT COALESCE(SUM(debit),0) = COALESCE(SUM(credit),0)
       AND COALESCE(SUM(debit),0) > 0, ...
FROM journal_lines WHERE tenant_id=$1 AND journal_entry_id=$2
```

This requires debits == credits **and** total > 0 (rejects all-zero entries). The arithmetic is in SQL on `numeric(14,2)` — money discipline is correct. Line-level `chk_journal_line_amounts` (`0039:62-64`) enforces `debit>=0 AND credit>=0 AND NOT(debit>0 AND credit>0)` (no mixed line).

**ACCT-001 (Critical-adjacent / High): the balanced-entry invariant is enforced ONLY in Go, never at the DB.** Migration `0039` has no entry-level balance trigger and no deferred constraint. The "Posted entries are immutable" claim (`0039:4`) is **a comment, not a mechanism** — there is no `BEFORE UPDATE`/`BEFORE DELETE` trigger on `journal_entries` or `journal_lines` (only `set_updated_at`). Consequences:
1. Any future code path, a bug, or direct SQL can insert a single unbalanced line into an existing entry, or update a posted line's amount, and the ledger silently goes out of balance. The only thing that catches it is the *display* `balanced` boolean in the trial-balance handler (a float comparison, see ACCT-004).
2. There is no DB guarantee that an entry, once balanced at insert time, *stays* balanced. The posting tx checks the aggregate after inserting all lines, but nothing prevents a later `UPDATE journal_lines SET debit = ...`.
3. There is no trigger blocking `UPDATE`/`DELETE` of a `status='posted'`/`'reversed'` entry — immutability is purely "we don't expose a handler for it." For a finance system where double-entry correctness is paramount, the integrity guarantee should live in the database. Recommend a constraint trigger that (a) re-checks `SUM(debit)=SUM(credit)` per entry on any line change, and (b) rejects mutation/deletion of lines belonging to a non-draft entry.

**Reversal** (`ReverseEntry`, `journal.go:228-269`): loads the original, rejects if `status != 'posted'` (`ErrAlreadyReversed`), resolves the period at `orig.EntryDate` with `allowClosed=true` (so reversals can post into a *closed* but not *locked* period — correct), inserts a `source_type='reversal'` header with `reverses_entry_id`, copies lines with **debit/credit swapped** (`journal.go:253-258`), then marks the original `reversed` with `reversed_by_entry_id`. Both-way linkage is correct, and double-reversal of the *same* original is blocked.

**ACCT-002 (Medium): a reversal entry can itself be reversed (no terminal guard).** The reversal is inserted with the default `status='posted'` (`0039:18`), so `ReverseEntry` on the reversal's id passes the `status=='posted'` gate. Reversing a reversal re-creates the original amounts as a new entry — arguably valid "re-instatement," but there is no guard or intent flag, and the chain `reverses_entry_id`/`reversed_by_entry_id` is not validated to prevent a reversal pointing at another reversal indefinitely. At minimum this should be a deliberate, documented behaviour; today it is incidental. Also note the reversal copies lines via a single `INSERT...SELECT` and **does not re-run the balance check** on the reversal (it trusts that swapping a balanced entry stays balanced — true, but only because ACCT-001's invariant held for the original).

**ACCT-003 (Low): `ReverseEntry` does not block reversing an entry whose own status is `draft`.** Entries are never created with `draft` today (default is `posted`), so this is latent, but `status != "posted"` returns `ErrAlreadyReversed` ("already reversed") which would be a misleading message for a draft.

## Flow 3 — Accounting periods

Lifecycle is a guarded state machine in `transition` (`periods.go:103-128`) using `WHERE status = $from` so only the legal edge fires; a no-row result is disambiguated into `ErrPeriodNotFound` vs `ErrPeriodTransition`. Edges: open→closing (`StartClose`), closing→closed (`ClosePeriod`), closed→open (`ReopenPeriod`), closed→locked (`LockPeriod`). Overlap prevention is a GiST exclusion `accounting_periods_no_overlap` (`0038:26-30`) over `daterange(start,end,'[]')` — solid, DB-enforced, and surfaced as `ErrPeriodOverlap`→409 via SQLState `23P01` (`periods.go:64-67`). `chk_period_dates` enforces `end >= start`.

`resolvePostingPeriod` (`periods.go:149-170`): finds the period covering the date, rejects `locked` always, rejects `closed` unless `allowClosed`. `closing` is **not** rejected — you can still post into a period that is mid-close. That is defensible (closing is a soft state) but means the "start-close" step provides no posting freeze.

**ACCT-004 (High): the period close has NO enforced blockers — the "close checklist" is advisory only.** `ClosePeriod`/`LockPeriod` (`periods.go:134-144`) only check the status edge. `handleCloseChecklist` (`close_handlers.go`) computes counts of unposted cash reconciliations, expenses awaiting posting, unissued invoices, etc., and returns `can_close`, but **nothing in the close transition consults it**. A finance user (or API client) can `POST .../close` and `.../lock` a period while expenses are unposted, deposits are in flight, and bank lines are unmatched. Worse, there is **no check that the period actually balances** before locking (trial balance is never computed at close). For a period-close control this is a material gap: locking should be refused (or require an explicit force flag) when `blockers > 0` and when the period's debits != credits.

**ACCT-005 (Medium): no period can be created automatically; posting fails hard with `ErrNoPeriod` if none covers the date.** `resolvePostingPeriod` returns `ErrNoPeriod`→422 "no accounting period covers this date" (`accounting_handlers.go:570`). Every downstream poster (payable import, supplier payment, expense post, petty cash) will 422 if the operator forgot to create the period. This is correct fail-closed behaviour, but the import flow (`handleImportPayables`) posts *each* created payable in a loop and aborts the whole tx on the first `ErrNoPeriod`, having already inserted the payable rows in the same tx — the rollback saves it, but the operator gets a partial-sounding error with no indication which invoice date lacked a period. Usability/observability (Low–Medium).

## Flow 4 — Payables

`ImportApprovedInvoices` (`payables/repo.go:60-76`): one `INSERT...SELECT` from `supplier_invoices` where `status='approved'` and `NOT EXISTS` a payable for that `source_invoice_id`. Idempotency is also enforced at the DB by `uq_payables_source_invoice` (`0040:27`). Money copied as-is (`total_amount`). `handleImportPayables` (`payables_handlers.go:43-99`) then posts **debit inventory / credit AP** per payable and links the entry. Correct double entry. **N+1:** posting is per-row in a loop (`payables_handlers.go:63-84`) — acceptable at import volumes; the balance check re-aggregates per entry.

**Aging** (`payables/repo.go:137-159`) sums `outstanding_amount` per supplier where status not paid/voided — but it is **not bucketed by age** despite the name "AP aging." There are no 0-30/31-60/61-90/90+ buckets; it is a single outstanding total + open count. The export type is literally `ap_aging` and the close checklist references aging, but no date-bucketing exists. **ACCT-006 (Medium):** "aging" is a misnomer — it is an open-balance summary, so any UI/report promising true aging buckets is unbacked.

**Supplier payment** (`handleRecordSupplierPayment`, `payables_handlers.go:151-256`): creates the header with `amount=0`, loops allocations calling `ApplyPayment` (decrements outstanding, guarded by `outstanding_amount >= $3` → `ErrOverAllocated`→422) and `AddAllocation` (inserts the allocation row + bumps `allocated_amount`), builds a balanced **debit AP / credit source** line pair per allocation, then sets `amount = allocated_amount` in SQL and posts. Money stays in SQL throughout — good.

**ACCT-007 (Medium): no cross-supplier guard on allocations.** The handler validates `req.SupplierID` exists implicitly only through the payment header, but `ApplyPayment`/`AddAllocation` operate on `payable_id` **without checking the payable belongs to `req.SupplierID`**. A caller can create a payment "for supplier X" and allocate it against supplier Y's payable (same tenant). The AP drawdown and journal are still balanced, but the `supplier_payments.supplier_id` then misrepresents who was paid, corrupting per-supplier aging and statements. Recommend `ApplyPayment` join `payables.supplier_id = payment.supplier_id`.

**ACCT-008 (Low): `parseDecimal` (float64) gates allocation positivity** (`payables_handlers.go:198`). This is validation-only (`v <= 0` rejected) and the stored/арithmetic value is the original string in SQL, so no precision is lost in the ledger — but it is a float touch on a money field; a malformed but float-parseable amount (e.g. `"40000.005"`) passes the guard and is then truncated by `numeric(14,2)` rounding at insert. Prefer a string/`numeric`-side positivity check.

**Allocation status edge:** `ApplyPayment` sets `paid` when `outstanding - amount <= 0`, else `partially_paid`. It guards `status <> 'voided'` and `outstanding_amount >= $3`. A fully-paid payable (`outstanding=0`) cannot be over-allocated (0 >= positive fails). Correct.

## Flow 5 — Expenses

Lifecycle: draft→submitted→approved→posted, each a guarded `UPDATE...WHERE status=...RETURNING` (`expenses/expenses.go:178-216`). `ApproveExpense` records `approved_by`. `MarkExpensePosted` requires `status='approved'`. `handlePostExpense` (`expenses_handlers.go:274-336`) re-reads the expense, asserts `approved`, posts **debit expense account / credit payment-mode account** (`paymentModeAccount`, `expenses_handlers.go:46-57`), marks posted, audits.

**ACCT-009 (High): no separation of duties — the creator can approve and post their own expense.** `expense.manage` (create/submit), `expense.approve`, and `expense.post` are three distinct permissions, but `ApproveExpense` (`expenses/expenses.go:183-198`) never compares `approverID` against `created_by`, and nothing stops one user holding all three permissions (the seed grants all three to `system_admin` and `finance_officer`, `0047:87-92`). For a controlled-spend workflow the approve step must reject `approved_by == created_by`. Today a finance_officer can create → submit → approve → post a payment to themselves with zero second pair of eyes. The same pattern repeats for supplier payments (single `supplier_payment.manage`, no maker/checker).

**ACCT-010 (Medium): category→account mapping has no validation that the resolved `account_key` is an expense-type account.** `resolveAccountKey` (`expenses/expenses.go:110-121`) falls back to `operating_expense`, but `account_key` is free text on create (`ExpenseInput.AccountKey`), and at post time `PostEntry` will resolve it via `resolveSystemAccount`. A caller can set `account_key="sales_revenue"` and post a "debit revenue / credit cash" entry that is balanced but books an expense against an income account, silently distorting the P&L. There is no check that the debit account is `type IN ('expense','contra_*')`. Recommend constraining `account_key` to a whitelist or validating the resolved account's type.

**ACCT-011 (Low): `chk_expense_status` allows `rejected`/`voided` (`0047:54-55`) but no handler ever transitions to them.** A submitted expense cannot be rejected — only approved or left dangling. Dead states; the reject path is unimplemented, so a bad expense stuck in `submitted` blocks the close (it counts as a blocker in the checklist) with no UI remedy.

**Money:** `amount` validated `> 0` via `parseDecimal` (float) in the handler (`expenses_handlers.go:143`) and `chk_expense_amount` (`0047:53`); stored/posted as string→`numeric`. Acceptable (same float-validation caveat as ACCT-008).

## Flow 6 — Petty cash

Floats (`petty_cash.go`): balance is `numeric(14,2)`. `RecordTransaction` (`petty_cash.go:123-165`) checks the float is `active`, computes `sign` from `increases()` (topup/reimbursement/adjustment = +1; spend/transfer = −1), and updates balance with an overdraw guard in SQL: `balance + (amount*sign) >= 0 OR $overdraw`. The transaction row records `balance_after` and the `overdraw` flag. Reconciliation (`ReconcileFloat`, `petty_cash.go:192-217`) inserts the count, computes `variance = counted - expected`, `ShortAmount = GREATEST(expected-counted,0)`, `OverAmount = GREATEST(counted-expected,0)`, and **sets the book balance to the counted amount**. All money math in SQL — good.

**ACCT-012 (High / GL integrity): `adjustment` and `transfer` transactions change the float balance but post NO journal entry.** In `handlePettyCashTransaction` (`expenses_handlers.go:492-509`) the journal `switch` builds `lines` **only** for `topup`, `reimbursement`, and `spend`. For `txn_type` `adjustment` and `transfer`, `RecordTransaction` still mutates `petty_cash_floats.balance` (an asset), but `lines` is empty so `len(lines) > 0` is false and no entry is posted. Result: the petty-cash *float* and the petty-cash *GL account* diverge with no offsetting entry — a direct double-entry break (cash on the books does not move while the float does, or vice-versa). `adjustment` especially is the type most likely to need a contra entry (cash over/short). Either these types must post (e.g. adjustment → cash_over_short; transfer → another float/cash account) or they must be rejected. The integration test only exercises topup/spend, so this is uncovered.

**ACCT-013 (Medium): `reimbursement` posts the wrong direction.** `reimbursement` is grouped with `topup` and posts **debit petty_cash / credit bank** (`expenses_handlers.go:495-499`), i.e. it *increases* the float exactly like a top-up. But "reimbursement" in petty-cash accounting normally means replenishing the float by reimbursing documented spend — the offset should be the expense account(s) already drawn down, not a second bank debit, otherwise spend is double-counted (once at spend time as expense, once at reimbursement as a fresh bank outflow that re-inflates the float). As coded, reimbursement is indistinguishable from topup; the distinct `txn_type` is misleading and the GL effect is likely wrong for its intended semantics. Needs a defined posting model.

**ACCT-014 (Low): reconciliation does not freeze the float, and a concurrent transaction can race the count.** `ReconcileFloat` reads `expected = balance`, then later `SET balance = counted` (`petty_cash.go:213`) without `SELECT ... FOR UPDATE`. A spend committing between the read and the overwrite is lost (its delta is discarded when balance is forced to `counted`). The variance journal posts off a stale `expected`. Low because petty-cash concurrency is rare, but a `FOR UPDATE` on the float row would close it.

**Reconciliation journal:** short → debit cash_over_short / credit petty_cash; over → debit petty_cash / credit cash_over_short (`expenses_handlers.go:622-632`). Directions are correct (a shortfall is an expense; an overage credits the over/short account). Uses `isZeroMoney` (float) only to decide *which* branch — the posted amounts are the exact strings. Acceptable.

## Flow 7 — Finance reports

All reports read `journal_lines` joined to `journal_entries` filtered `status IN ('posted','reversed')` and join `accounts` for type — so a reversed entry and its reversal both remain and net to zero (`reports.go:10-12`). Drafts excluded.

- **Trial balance** (`reports.go:27-55`): per-account `SUM(debit)`, `SUM(credit)`, balance, with `HAVING` to drop zero rows. The aggregation is in SQL on `numeric`. **ACCT-015 (Medium): the `balanced` flag returned to the client is computed in Go with float64 and a 0.005 tolerance** (`finance_handlers.go:36-52`). The real per-account sums are exact strings, but the headline "balanced: true/false" parses each row's debit/credit to `float64`, accumulates, and compares `abs(diff) < 0.005`. With thousands of rows this float accumulation can drift past 0.005 and report a *balanced* ledger as unbalanced (false alarm) or, more dangerously, mask a genuine small imbalance under the tolerance. The trial balance should sum debits and credits **in SQL** (one extra aggregate) and compare exact `numeric` equality. This is the only place the system asserts "the books balance," and it does so in float.
- **Income statement** (`reports.go:64-80`): revenue = income credit−debit and contra_income handled; expenses = expense debit−credit; net = revenue−expenses. Correct sign handling. Filters `entry_date BETWEEN from AND to`.
- **Balance sheet** (`reports.go:89-102`): assets (asset+contra_asset) debit−credit, liabilities credit−debit, equity credit−debit. **Does NOT roll current-period net income into equity**, and **A = L + E will generally NOT hold during an open period** because revenue/expense balances live in income/expense accounts, not equity, until a close/retained-earnings posting occurs — and there is **no period-close journal that sweeps P&L to retained earnings** anywhere in this codebase. **ACCT-016 (High):** the balance sheet equation does not balance for any tenant with posted revenue/expense activity, because net income is never closed to equity and the balance-sheet query ignores income/expense accounts entirely. The Phase-7 test only asserts `assets == "5000.00"` after a single cash/revenue entry — it never checks `assets == liabilities + equity` (it can't, because it wouldn't hold: assets 5000, equity 0, liabilities 0). This is a fundamental accounting-correctness gap: either the balance sheet must include retained-earnings-to-date (current income−expense) in equity, or a year/period-end close entry must post it.
- **General ledger** (`reports.go:116-137`): per-account posted lines in `entry_date, entry_number` order. No running balance column and no opening balance, but correct as a line list. No `as_of`/date filter (returns the entire history) — minor.

**Finance overview** (`finance_handlers.go:122-178`): aggregates balance sheet, P&L (trailing month), AP aging, open-period count, recent entries. `apOpen` is computed by a pointless `for range apAging { apOpen++ }` loop (`finance_handlers.go:166-169`) instead of `len(apAging)` — dead/awkward but harmless. Inherits the balance-sheet defect (ACCT-016) in its `balance_sheet` block.

## Flow 8 — Exports

`handleGenerateExport` (`export_handlers.go:29-138`) builds a CSV with `encoding/csv`, SHA-256s the bytes, records the run (type, filters, row_count, checksum, provisional) and returns the CSV inline. `RangeProvisional` (`exports.go:53-63`) marks the export provisional if any covering period is not `locked` — so a fully-locked range is final and reproducible (the test verifies identical checksums on re-run).

**ACCT-017 (Medium): CSV formula injection.** The export writes `account_name`, `memo`, `source_type`, and supplier/customer fields straight into CSV cells with no neutralization (`export_handlers.go:64-67, 81, 107`). A value beginning with `=`, `+`, `-`, `@`, or tab/CR (e.g. an expense memo or account name like `=cmd|'/c calc'!A1`) is interpreted as a formula when the CSV is opened in Excel/Sheets. Since account names, memos, and references are user-controlled and the export is the artifact handed to auditors/accountants, this is a real injection vector. Mitigation: prefix risky leading characters with `'` or wrap in quotes per the OWASP CSV-injection guidance.

**ACCT-018 (Low): provisional range for `trial_balance`/`ap_aging`/`ar_aging` uses `from = time.Time{}` (zero/year-1)** (`export_handlers.go:71,85,98`). `RangeProvisional` then asks "any non-locked period overlapping [0001-01-01, as_of]" — effectively "is *any* historical period unlocked," so an as-of trial-balance export is flagged provisional whenever *any* period in all of history is open, even if irrelevant to the as-of snapshot. Over-broad provisional flagging (conservative, so not dangerous, but imprecise).

The export `RecordExport` (`exports.go:78-86`) runs on `r.pool` **outside any tx and with no audit/outbox record** — unlike every other mutating finance route. The route is gated by `finance.export` and the run row is itself the audit trail, but it bypasses the `WriteWithOutbox` convention. **ACCT-019 (Low):** export generation (a sensitive data-egress action) is not written to the central audit log, only to `accounting_exports`. Inconsistent with house convention.

---

## Cross-cutting findings

**Tenant isolation / IDOR:** every repo query is scoped `WHERE tenant_id = $1` with `tenantID` passed first, and composite tenant FKs are used throughout (`accounts_parent_fk`, `journal_lines_*_fk`, etc.). All handlers derive `actor.TenantID` from `identity.Require` — no tenant id is accepted from the request body or path. No IDOR found in this domain (the cross-supplier allocation in ACCT-007 is intra-tenant data confusion, not cross-tenant).

**AuthZ:** every mutating route is gated (`server.go:632-750`): `account.manage`, `period.manage/close/reopen/lock`, `journal.adjust`, `payable.manage`, `supplier_payment.manage`, `expense.manage/approve/post`, `petty_cash.manage/reconcile`, `finance.export`. Reads ride `finance.read`/`journal.read`/`payable.read`. No unauthenticated or ungated mutating route in scope. The gap is **separation-of-duties within those permissions** (ACCT-009), not missing gates.

**tx + audit + outbox atomicity:** the `txAudit` helper (`accounting_handlers.go:102-130`) wraps fn + `WriteWithOutbox` + commit and is used for accounts, periods, journal adjust/reverse, expense create/transition, category, float create. The multi-step flows that can't use the helper (import payables, supplier payment, post expense, petty-cash txn, reconcile) open their own tx, do all work + post + `WriteWithOutbox` + commit in one tx — verified atomic. **Exception:** export generation (ACCT-019) is the one mutating action with no audit/outbox.

**Money discipline:** ledger arithmetic is consistently SQL-on-`numeric`; values carried as decimal strings (`::text` casts on read, `$n::numeric` on write). Float64 appears only as (a) input validation `parseDecimal` (ACCT-008) and (b) the trial-balance `balanced` display flag (ACCT-015). No float64 is ever stored or used for a posted amount. The ACCT-015 case is the material one because it is the system's balance assertion.

**Idempotency:** payable import is idempotent (unique source_invoice_id + NOT EXISTS). Seed chart is idempotent. Bank-statement re-import is blocked (per test). However, **journal posting itself is not idempotent** — `handleImportPayables`, supplier payment, expense post, and petty-cash txn have no idempotency key, so a client retry (network timeout after commit) double-posts (e.g. two AP imports cannot duplicate the payable, but two supplier-payment submits with the same allocations *will* create two payments and two journal entries, double-drawing the payable until it over-allocates). **ACCT-020 (Medium):** the mutating finance POSTs lack idempotency keys.

**N+1:** payable import posts per-row (acceptable). No problematic N+1 in reports (all single aggregate queries). `GeneralLedger` returns full history unbounded (Low).

**Dead code / smells:** `apOpen` counting loop (`finance_handlers.go:166-169`); unimplemented `rejected`/`voided` expense states (ACCT-011); `money0` helper duplicates the DB default but is harmless; `RangeProvisional` zero-time bound (ACCT-018).

---

## Findings table

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| ACCT-001 | High | journal.go:140-150; 0039_journals.up.sql (whole) | Balanced-entry invariant enforced only in Go; no DB balance trigger and no immutability trigger on posted `journal_entries`/`journal_lines` ("immutable" is a comment). | Add a constraint trigger re-checking `SUM(debit)=SUM(credit)` per entry on line change, and reject INSERT/UPDATE/DELETE of lines on non-draft entries. |
| ACCT-002 | Medium | journal.go:228-235 | A reversal entry (status `posted`) can itself be reversed; no terminal guard or chain validation. | Block reversing `source_type='reversal'` entries or flag intent; validate the reverses/reversed-by chain. |
| ACCT-003 | Low | journal.go:233 | `draft` entries return "already reversed" (misleading) on reverse attempt. | Distinguish draft vs reversed in the error. |
| ACCT-004 | High | periods.go:134-144; close_handlers.go | Period close/lock enforces no blockers and never checks the period balances; the close checklist is purely advisory. | Refuse lock (or require force flag) when blockers>0 and when period debits≠credits. |
| ACCT-005 | Low-Med | periods.go:155-156; payables_handlers.go:69-84 | Missing period → hard 422 mid-loop in import with no indication which date failed. | Pre-validate covering periods; clearer error. |
| ACCT-006 | Medium | payables/repo.go:137-159 | "AP aging" has no age buckets — it is a single outstanding total. | Implement true 0-30/31-60/61-90/90+ bucketing by due_date. |
| ACCT-007 | Medium | payables_handlers.go:202-209; payables/repo.go:107 | Allocations not checked against the payment's `supplier_id`; can pay supplier Y's payable on supplier X's payment. | Join `payables.supplier_id = payment.supplier_id` in `ApplyPayment`. |
| ACCT-008 | Low | payables_handlers.go:198; expenses_handlers.go:143 | Money positivity validated with float64; float-parseable over-precision passes then rounds. | Validate on the `numeric`/string side. |
| ACCT-009 | High | expenses/expenses.go:183-198; server.go:720-728 | No separation of duties: creator can submit, approve, and post the same expense; same for supplier payments. | Reject `approved_by == created_by`; enforce maker/checker. |
| ACCT-010 | Medium | expenses/expenses.go:110-121; expenses_handlers.go:306-312 | `account_key` is free text; an expense can be posted against a non-expense account (e.g. revenue), distorting P&L while staying "balanced." | Whitelist keys / validate resolved account type is expense/contra. |
| ACCT-011 | Low | 0047_expenses.up.sql:54-55 | `rejected`/`voided` expense states exist but have no transition handler; bad expenses stick in `submitted` and block close. | Implement reject/void or remove the states. |
| ACCT-012 | High | expenses_handlers.go:492-509; petty_cash.go:111-118 | `adjustment` and `transfer` petty-cash txns mutate float balance but post NO journal entry — direct double-entry break. | Post a contra entry for these types (or reject them). |
| ACCT-013 | Medium | expenses_handlers.go:495-499 | `reimbursement` posts identically to `topup` (debit petty_cash/credit bank), likely double-counting spend. | Define correct reimbursement posting (offset documented spend). |
| ACCT-014 | Low | petty_cash.go:192-216 | Reconcile reads expected then overwrites balance with no `FOR UPDATE`; concurrent txn lost; variance off stale balance. | Lock the float row during reconcile. |
| ACCT-015 | Medium | finance_handlers.go:36-52 | Trial-balance `balanced` flag computed in float64 with 0.005 tolerance — can mask/false-flag imbalance; the only "books balance" assertion is float. | Sum debits/credits in SQL; compare exact `numeric`. |
| ACCT-016 | High | reports.go:89-102 | Balance sheet omits current-period net income from equity and there is no close-to-retained-earnings entry, so A = L + E does not hold for active tenants. | Include income−expense to-date in equity, or post a period-end close journal. |
| ACCT-017 | Medium | export_handlers.go:64-67,81,107 | CSV formula injection: user-controlled names/memos written unescaped into exports handed to accountants. | Neutralize leading `= + - @`/control chars. |
| ACCT-018 | Low | export_handlers.go:71,85,98; exports.go:53-63 | Provisional check uses zero-time `from`, flagging as-of exports provisional if any historical period is open. | Bound provisional check to the relevant range. |
| ACCT-019 | Low | exports.go:78-86; export_handlers.go:127 | Export generation bypasses tx + `WriteWithOutbox`; no central audit record for a sensitive data egress. | Wrap in tx with audit/outbox. |
| ACCT-020 | Medium | payables_handlers.go:151; expenses_handlers.go:274,426 | Mutating finance POSTs lack idempotency keys; client retry double-posts payments/expenses/petty-cash entries. | Accept Idempotency-Key; dedupe before posting. |

---

## Severity counts

- **Critical:** 0
- **High:** 5 (ACCT-001, ACCT-004, ACCT-009, ACCT-012, ACCT-016)
- **Medium:** 8 (ACCT-002, ACCT-006, ACCT-007, ACCT-010, ACCT-013, ACCT-015, ACCT-017, ACCT-020)
- **Low:** 7 (ACCT-003, ACCT-005, ACCT-008, ACCT-011, ACCT-014, ACCT-018, ACCT-019)

## Top 5 risks

1. **ACCT-016 — Balance sheet never balances (A ≠ L + E).** Net income is never closed to equity and the balance-sheet query ignores income/expense accounts. The flagship report is structurally wrong for any tenant with revenue/expense activity, and the test never checks the accounting equation.
2. **ACCT-001 — Double-entry integrity has no DB enforcement.** The balance rule and "immutability" of posted entries live only in Go comments/checks; any future code path or direct SQL can unbalance the ledger or mutate posted lines with nothing to stop it.
3. **ACCT-012 — Petty-cash `adjustment`/`transfer` move cash with no journal entry.** A live double-entry break in a shipped, route-exposed flow (uncovered by tests): the float and its GL account silently diverge.
4. **ACCT-009 — No separation of duties.** One finance user can create, approve, and post an expense or supplier payment end-to-end; the maker/checker permissions exist but are never enforced against the actor.
5. **ACCT-004 — Period close/lock enforces nothing.** A period can be locked with unposted documents and without ever verifying it balances; the close checklist is advisory and disconnected from the transition.
