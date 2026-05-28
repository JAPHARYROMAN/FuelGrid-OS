# Audit 08 — Payments / Receivables / Cash & Banking (Phase 6 Tender + Phase 7 Treasury)

Read-only, atomic-level audit of the money-handling domain of FuelGrid OS: shift tender
capture, accounts-receivable (credit customers, customer invoices, customer payments),
cash reconciliation, bank deposits, and bank-statement import/matching. Priority is
**money / double-entry correctness**, then tenant isolation, authZ, tx+audit+outbox
atomicity, idempotency, separation of duties, and dead code.

The verdict up front: the **journal engine is sound** (debits==credits enforced in SQL,
period gating, re-read in-tx), money is carried as decimal strings with arithmetic in SQL
(no float in any domain path), and tenant scoping is applied at the application layer on
every query. The defects are concentrated in (1) a **concurrency race in credit-limit
enforcement**, (2) **customer-payment allocations not bound to the paying customer**
(cross-customer draw-down), (3) a **structural divergence between the Phase-6 AR ledger
(`ar_entries`) and the Phase-7 GL / customer-invoice ledger**, with sales revenue never
recognized in the GL, and (4) several missing business-rule guards (customer status, dead
states, unbalanced `sales_clearing`).

## Scope & files (with LOC)

Domain layer (`./internal`):

| File | LOC | Role |
|------|-----|------|
| `internal/payments/repo.go` | 113 | Shift tender records, shift reconciliation |
| `internal/receivables/repo.go` | 317 | Customers, AR ledger (`ar_entries`), credit-limit charge, aging |
| `internal/receivables/customer_payments.go` | 100 | Customer payment header + allocations |
| `internal/receivables/invoices.go` | 256 | Customer invoices, lines, issue, apply-payment, aging |
| `internal/banking/repo.go` | 28 | Banking errors + repo ctor |
| `internal/banking/cash_reconciliation.go` | 206 | Cash recon create/submit/approve, posting amounts |
| `internal/banking/deposits.go` | 208 | Bank accounts, deposits (create/prepare/confirm) |
| `internal/banking/statements.go` | 159 | Statement import + line match/unmatch/mark |

HTTP layer (`./services/api/internal/server`):

| File | LOC | Role |
|------|-----|------|
| `payments_handlers.go` | 234 | Record tender, list, shift reconciliation |
| `customer_invoices_handlers.go` | 401 | Invoice CRUD/issue, aging, customer-payment posting |
| `customers_handlers.go` | 324 | Customer CRUD, statement, Phase-6 AR payment |
| `banking_handlers.go` | 958 | Cash recon, bank accounts, deposits, statements |

Migrations: `0034_customers_ar`, `0035_payments`, `0042_cash_reconciliations`,
`0043_bank_deposits`, `0044_bank_statements`, `0045_customer_invoices`,
`0046_customer_payments`, `0051_customer_credit_profiles`. Supporting: `0004_rbac`
(permissions), `internal/accounting/journal.go` (`PostEntry`), `accounts.go` (system keys),
`internal/database/tenant.go` (`WithTenant`). Tests: `phase7_integration_test.go` (553).

---

## Flow 1 — Tender capture at shift (`payments`)

`handleRecordPayment` (payments_handlers.go:56) is the entry point for `POST
/shifts/{id}/payments`. The route is registered **without** middleware authZ
(server.go:389); authZ is in-handler via `s.authorizeStation(w, r, actor,
"payment.record", shift.StationID)` (payments_handlers.go:95) after loading the shift —
correct, because the station id is on the shift row, not the URL.

Validation: tender must be in `{cash, mobile_money, card, credit, voucher}`
(payments_handlers.go:45, mirrored by the DB CHECK at 0035:24). Amount validated as
non-negative decimal via `parseDecimal` (line 76). A `credit` tender requires
`customer_id` (line 80). Good.

The tx wraps `Record` → optional `PostCharge` → `audit.WriteWithOutbox` → commit (lines
99–151) — correct atomicity, single tx, audit+outbox inside it.

### Defects in the tender flow

**PAY-001 (High) — credit-limit enforcement has a concurrency race.**
`receivables.PostCharge` (repo.go:207–229) enforces the limit with a single
`INSERT ... SELECT ... WHERE $8 OR (SUM(amount) + $amount <= credit_limit)`. The SUM and
the comparison are computed at statement execution, but the customer/`ar_entries` rows are
**not locked** (`SELECT ... FOR UPDATE` absent) and the transaction runs at the pgx default
**READ COMMITTED** (`internal/database/tenant.go:34`, `pgx.TxOptions{}`). Two concurrent
credit tenders for the same customer each read the same pre-charge balance, both pass the
`<= credit_limit` check, and both insert — overshooting the limit. The same race corrupts
`balance_after`: both rows snapshot the same running balance, so the ledger's
`balance_after` column becomes non-monotonic / wrong. Fix: take `SELECT ... FOR UPDATE` on
the `customers` row (or a per-customer advisory lock) at the start of the charge, or run
the charge under `SERIALIZABLE` and retry on 40001.

**PAY-002 (High) — credit tender does not check customer status.**
A credit charge is posted to any customer whose `status <> 'deleted'`. There is no check
that the customer is `active` (vs `inactive`/`on_hold`/`suspended`/`closed`), and no
consult of the Phase-8 `customer_credit_profiles.hold` flag (0051). So a credit sale can be
booked against a customer who is explicitly on hold or suspended.
`handleRecordPayment` never loads the customer record at all — it relies solely on the FK
to reject an unknown id (line 111). Fix: load the customer in-tx, reject charges for
non-`active` status, and (if Phase-8 holds are meant to be enforced at the till) reject when
`hold = true` unless `credit.override_limit`/`customer_credit.override` is held.

**PAY-003 (High) — credit tender posts to the AR ledger but not to the GL.**
A `credit` tender calls `PostCharge` (ar_entries) only (payments_handlers.go:122–134). It
posts **no journal entry** — no debit `accounts_receivable` / credit `sales_revenue`. No
shift/sales path anywhere posts to the GL either (confirmed: `PostEntry` /
`accounts_receivable` / `sales_revenue` appear only in customer_invoices, banking,
expenses, payables handlers — never in operations/sales). Consequence: the operational AR
balance (`ar_entries`, surfaced by `Balance`/`Aging`/`handleCustomerStatement`) and the
financial AR balance (`customer_invoices` / GL `accounts_receivable`) are **two disjoint
sub-ledgers** with no bridge. A credit tender increases `ar_entries` but never produces a
customer invoice or a GL receivable; a customer invoice issued in Phase 7 increases the GL
but not `ar_entries`. They can never be reconciled, and AR aging shown to operators
(`receivables.Aging`) will not match finance's AR (`InvoiceAging`). This is the single most
important correctness issue in the domain. The migration comment (0034:8) acknowledges the
seam ("Phase 7 layers customer invoices/aging on top") but no code performs the layering.
Fix: when a credit tender is captured, either (a) post a balanced GL entry (DR AR / CR
sales clearing or revenue) in the same tx, or (b) generate/append to a customer invoice so
the two ledgers stay coupled. At minimum, document that `ar_entries` is non-GL and provide a
reconciliation report.

**PAY-004 (Low) — `over_threshold` uses float comparison on money.**
`handleShiftPaymentReconciliation` (payments_handlers.go:184–189) parses tendered/recognized
variance to `float64` to flag `variance > 1`. This is a display flag only (not stored), but
it is float math on a money value and will misbehave near the boundary. Acceptable per the
house "validation/soft-comparison only" rule, but the threshold should be a SQL `ABS(...) >
1` comparison for consistency.

**PAY-005 (Info) — `ReconcileShift` reconciles tenders against `sales.gross_amount`, not
recognized GL revenue.** `payments.ReconcileShift` (repo.go:103–113) sums
`payments.amount` vs `sales.gross_amount` for the shift. This is a pure operational
cross-check; it never touches the GL. Combined with PAY-003, "recognized" here means
"sales table gross", which is fine for the till but is not the accounting figure.

---

## Flow 2 — Receivables: customer invoices, customer payments, AR

### Customer invoices (`invoices.go`, `customer_invoices_handlers.go`)

Create → add lines → finalize amount is correct and money-safe: `CreateInvoice` seeds
`amount=0, outstanding=0` (invoices.go:84), each line is `amount::numeric` with a DB CHECK
`amount > 0` (0045:63), and `FinalizeInvoiceAmount` sets both `amount` and
`outstanding_amount` to the SQL `SUM` of lines (invoices.go:108–118). Issue
(`handleIssueCustomerInvoice`) posts DR `accounts_receivable` (total) / CR each revenue
group (invoices.go:140 `RevenueBreakdown`, grouped in SQL), then links the entry. The
journal is balanced by construction (one debit = total = sum of credits) and verified again
by `PostEntry` (journal.go:141–150). Period gating, system-account resolution, and the
in-tx re-read of the entry are all handled by `PostEntry`. Good.

**PAY-006 (Medium) — line-amount validation runs *after* `CreateInvoice` and *before*
`AddInvoiceLine`, inside the loop, but the invoice row is already inserted.** Not a money
bug (the tx rolls back on the 400), but `handleCreateCustomerInvoice`
(customer_invoices_handlers.go:103–112) validates `ln.Amount > 0` per iteration; an invalid
amount aborts mid-loop and rolls back — correct outcome, but the validation should precede
any insert for clarity and to avoid partial-line confusion in logs. Minor.

**PAY-007 (Low) — issue allows a zero-total invoice to post nothing meaningful, but
`PostEntry` will reject it.** If all lines somehow summed to 0 (impossible given the `>0`
CHECK), `inv.Amount = "0"` would make `PostEntry` fail `ErrUnbalanced` (debits must be
`> 0`). The `>0` CHECK on lines makes this unreachable; noting for completeness.

### Customer payments & allocation (`customer_payments.go`, handler at
`customer_invoices_handlers.go:271`)

`handlePostCustomerPayment` creates a payment header (amount 0), then for each allocation:
`ApplyInvoicePayment` (guarded draw-down) → `AddCustomerAllocation` (insert allocation, bump
header `amount`/`allocated_amount`) → append a balanced DR `srcKey` / CR
`accounts_receivable` pair to the journal lines. Then one `PostEntry` for all pairs, link,
audit, commit. Allocation amounts validated `> 0` (line 328).

`ApplyInvoicePayment` (invoices.go:208–228) is **correctly race-safe**: the single
`UPDATE ... WHERE status IN ('issued','partially_paid') AND outstanding_amount >= $amount`
takes the row lock and re-evaluates the guard, returning `ErrOverAllocated` (→ 422) on
`pgx.ErrNoRows`. The status transition to `paid`/`partially_paid` uses
`outstanding_amount - $amount <= 0` in SQL — money-correct.

**PAY-008 (High) — allocations are not bound to the paying customer.**
The handler accepts `customer_id` for the payment header and a list of
`customer_invoice_id` allocations, but **never verifies the invoices belong to that
customer**. `ApplyInvoicePayment` filters only by `tenant_id` + invoice `id`
(invoices.go:217); `AddCustomerAllocation` inserts only `(tenant, payment, invoice, amount)`
(customer_payments.go:65–70); the DB FK `cpa_invoice_fk` (0046:56) references
`customer_invoices(tenant_id, id)` with no customer linkage. So a finance user can create a
payment "from customer A" that draws down customer B's invoices. The GL still balances
(DR bank / CR AR), but the customer payment record, the allocation rows, and any
per-customer payment history are wrong, and customer B's invoices are silently settled
against A's money. Fix: in the loop, load each invoice (or add a `customer_id = $payment
customer` predicate to `ApplyInvoicePayment`) and reject mismatches with 422/400; ideally
add a composite FK / CHECK so the DB enforces it.

**PAY-009 (Medium) — payment `amount` can never exceed allocations; the documented
"customer credit" for unapplied amounts does not exist.** Migration 0046:5–6 states
"unapplied amounts stay as customer credit". But `CreateCustomerPayment` inserts `amount =
0` (customer_payments.go:54) and `AddCustomerAllocation` does `amount = amount + $alloc`
**and** `allocated_amount = allocated_amount + $alloc` (lines 72–75), so `amount` is always
identically equal to `allocated_amount`. There is no input field for a payment total
distinct from the allocations, so over-payment / on-account credit is impossible, and the
`amount >= allocated_amount` semantics implied by the schema are vacuous. The `bank/cash`
debit equals the sum of allocations, which is at least internally consistent — but the
feature described in the migration is unimplemented. Fix: accept a payment `amount`, post
DR bank for the full amount, credit AR for allocated, and credit `customer_credits`
(system key exists, accounts.go:71) for the unapplied remainder; or remove the misleading
comment.

**PAY-010 (Low) — `handleRecordCustomerPayment` (Phase-6 AR payment,
customers_handlers.go:268) posts to `ar_entries` only, never to the GL.** Same divergence
as PAY-003 but on the payment side: `PostPayment` (repo.go:232–246) appends a negative
`ar_entries` row with no journal entry. So the operational AR can be paid down without any
GL movement. Coupled with PAY-003 this confirms `ar_entries` is a fully parallel, non-GL
ledger. Note: this route is gated by `credit.manage` (server.go:403) while the Phase-7
customer payment is gated by `customer_payment.manage` (server.go:708) — two payment paths,
two permissions, two ledgers.

**PAY-011 (Low) — no idempotency key on customer payments.** A double-submitted
`POST /customer-payments` (network retry) creates two payments, two sets of allocations,
and two journal entries, each drawing down outstanding. The `ApplyInvoicePayment` guard
prevents over-drawing past zero, but a retry that still has headroom double-pays. Supplier
payments and journal entries elsewhere appear to share this gap; consider a client-supplied
idempotency token.

### AR aging (`receivables.Aging` repo.go:287, `InvoiceAging` invoices.go:233)

Two aging views exist. `Aging` sums `ar_entries.amount` per customer (operational);
`InvoiceAging` sums `customer_invoices.outstanding_amount` for issued/partially_paid
(finance). **Neither is a true aging report** — there are no 0-30 / 31-60 / 61-90 / 90+
buckets despite "aging" in the name and the `due_date`/`InvoiceDate` columns being
available.

**PAY-012 (Medium) — "aging" endpoints do not bucket by age.**
`handleCustomerInvoiceAging` (customer_invoices_handlers.go:249) and the customer-statement
balance return a single outstanding total per customer, ignoring `due_date`. A real AR aging
needs `CASE WHEN now() - due_date ...` buckets. As shipped these are "open balance by
customer" lists. Fix: bucket in SQL by `due_date` (invoices) / `recorded_at` (entries).

---

## Flow 3 — Cash reconciliation (`cash_reconciliation.go`, banking_handlers.go:114–356)

Lifecycle: create (draft, expected seeded from Phase-6 cash tenders) → submit (counted,
variance) → approve (post GL, → posted). Separation of duties is enforced at the route
layer: create/submit need `cash_reconciliation.manage`, approve needs
`cash_reconciliation.approve` (server.go:676–681) — **distinct permissions**, good.

`CreateCashReconciliation` (cash_reconciliation.go:67) seeds `expected_cash` from
`SUM(payments.amount) WHERE tender_type='cash' AND status='recorded'` joined to shifts of
the operating day — all in SQL, money-safe — and writes per-shift breakdown lines. A second
recon for the same day hits `uq_cash_recon_day` (0042:38) → `ErrDuplicate` → 409. Good
idempotency.

`SubmitCashReconciliation` recomputes `variance = counted - expected` in SQL
(cash_reconciliation.go:146). `PostingFor` computes `ShortAmount =
GREATEST(expected-counted,0)`, `OverAmount = GREATEST(counted-expected,0)` in SQL
(lines 168–169). The approval posts (banking_handlers.go:311–323): DR `cash_on_hand`
(counted), CR `sales_clearing` (expected), and the over/short balancer
(`cash_over_short` DR if short, CR if over). I verified the arithmetic balances in both
directions (short: DR 49500 + DR 500 = CR 50000; over: DR 51000 = CR 50000 + CR 1000).
`PostEntry` re-verifies. **The cash-recon double-entry is correct.**

**PAY-013 (High) — `sales_clearing` is credited on approval but never debited anywhere;
sales revenue is never recognized.** The credit to `sales_clearing` (banking_handlers.go:316)
presupposes a prior revenue-recognition entry that debited `sales_clearing` when the cash
sale occurred. No such entry exists — `sales_clearing` is written **only** by this approval
(grep: `sales_clearing` appears only in `accounts.go`, this handler, and the test). So every
approved cash recon pushes `sales_clearing` further into a credit balance that is never
offset, and no `sales_revenue` is ever credited from operations. The trial balance still
reports `balanced: true` (each entry self-balances), which is exactly why
`TestPhase7_CashAndBanking` passes — the test only asserts the books balance, not that
revenue exists. But the P&L will show zero operating revenue from fuel sales, and
`sales_clearing` (a liability) grows without bound. Fix: a sales/shift-close entry must
debit `sales_clearing` and credit `sales_revenue`/`accounts_receivable` (this is the GL
half of PAY-003). This is an accounting-completeness defect, not just cosmetics.

**PAY-014 (Medium) — approval has no period-reopen / variance ceiling and no second-person
check beyond the permission.** Any single actor holding `cash_reconciliation.approve` can
approve. There is no rule that the approver differ from the submitter/creator
(`created_by`), so true four-eyes separation is not enforced at the row level even though
the permissions differ. A user holding both `.manage` and `.approve` (the system_admin and
finance_officer roles both get both, per 0042:81–86) can create, submit, and approve the
same recon. Fix: reject approval when `actor.UserID == created_by` (configurable), or
require a maker/checker flag.

**PAY-015 (Low) — `approved` status is dead.** The CHECK allows
`draft/submitted/approved/posted/rejected` (0042:28) and the migration narrative describes
"submitted -> approved -> posted", but the code goes `submitted -> posted` directly
(`MarkCashReconciliationPosted`, cash_reconciliation.go:186). `approved` and `rejected` are
never set by any code path (`SubmitCashReconciliation` accepts `rejected` as an input state
but nothing ever writes it). Dead states / unreachable transitions.

---

## Flow 4 — Bank deposits (`deposits.go`, banking_handlers.go:412–690)

Lifecycle: create (draft, lines = posted cash recons) → prepare (DR `bank_clearing` / CR
`cash_on_hand`, → prepared) → confirm (DR `bank` / CR `bank_clearing`, → posted, records
actual bank date). Permissions: `bank_deposit.manage` for create+prepare,
`bank_deposit.confirm` for confirm (server.go:684–689) — separation of duties present.

`AddDepositLine` is guarded by `uq_bdl_recon` (0043:90) so a cash recon can be deposited at
most once → `ErrDuplicate` → 409 (verified by test line 288). `PrepareDeposit` sets `amount
= SUM(lines)` in SQL and gates on `status='draft'` (deposits.go:169–182). `ConfirmDeposit`
gates on `status IN ('prepared','in_transit','confirmed')`. Both journal halves balance
trivially (single DR = single CR = amount), re-verified by `PostEntry`.

**PAY-016 (Medium) — deposit lines are not validated against the cash recon's actual
amount or status.** `AddDepositLine` accepts an arbitrary client-supplied `amount` for each
`cash_reconciliation_id` (banking_handlers.go:469–485, deposits.go:122). Nothing checks that
the line amount equals (or is `<=`) the reconciliation's `counted_cash`, nor that the recon
is in `posted` status (the migration says "approved/posted cash reconciliations", but only
the FK existence is checked). So a deposit can claim 49,500 against a recon that counted
1,000, and `PrepareDeposit` will move that fabricated amount cash→clearing, debiting
`bank_clearing` for cash that was never reconciled. Fix: derive the line amount from the
reconciliation (don't trust the client), and require the recon `status='posted'`.

**PAY-017 (Low) — `in_transit` / `confirmed` deposit states are dead; confirm accepts
them.** Nothing transitions a deposit to `in_transit` or `confirmed`; `ConfirmDeposit`
accepts those states (deposits.go:196) but no code reaches them. The status set in the CHECK
(0043:52) is broader than the implemented two-step flow. Dead code / unreachable states.

**PAY-018 (Low) — `handlePrepareBankDeposit` reads the deposit (for `postDate`) outside the
tx, then re-reads via the guarded UPDATE inside the tx.** The `GetDeposit` at line 541 is
outside the tx and only supplies `ExpectedBankDate` and `StationID`; the authoritative state
transition is the in-tx `PrepareDeposit`. Functionally safe (the UPDATE re-checks
`status='draft'`), but the station id used on the journal lines comes from the
pre-tx read — acceptable since deposit station is immutable. Noting the pattern.

**PAY-019 (Low) — confirm reuses pre-tx station, and `confirmed_entry_id` is overwritten
without a guard.** `SetDepositConfirmedEntry` (deposits.go:205) does an unconditional
`UPDATE ... SET confirmed_entry_id`. Re-confirmation is blocked by the status guard so this
is currently safe, but the setter itself has no `WHERE confirmed_entry_id IS NULL` belt to
prevent a future caller from orphaning an entry. Defensive nit.

---

## Flow 5 — Bank statement import & matching (`statements.go`, banking_handlers.go:692–947)

Import (`handleImportBankStatement`) reads the body (capped at 1 MiB,
banking_handlers.go:712 — good), hashes the **raw bytes** with SHA-256, and
`ImportStatement` inserts an import row guarded by `uq_bsi_hash (tenant, account, hash)`
(0044:27). A re-import of identical content → `ErrDuplicate` → 409 (verified, test line
303). **Idempotency is genuine and content-based.** Lines are inserted in a loop in the
same tx; amounts are `numeric` (negative allowed — a debit fee), validated only as parseable
decimals (line 732). Good.

**PAY-020 (Medium) — import idempotency is byte-exact, so trivially re-orderable/whitespace
payloads bypass it.** The hash is over the exact request bytes (line 748), not over a
canonicalized line set. Re-posting the same statement with a reordered `lines` array, an
added space, or a different `statement_start` formatting produces a different hash and
imports the lines again — duplicating every line (no per-line uniqueness constraint exists).
For a bank feed this is a real double-count risk. Fix: hash a canonical projection of the
parsed lines (sorted, normalized), and/or add a per-line natural-key unique index
(account, txn_date, amount, reference).

`MatchLine` (statements.go:113) transitions `unmatched/unknown → matched`, sets
`matched_doc_type/id`. **It does not post a journal entry** — matching is purely a
bookkeeping link, which is correct for deposit/payment reconciliation (those already posted
their own entries). `UnmatchLine` reverts and nulls links including `journal_entry_id`.

**PAY-021 (High) — unmatch nulls `journal_entry_id` without reversing the posted entry.**
`UnmatchLine` (statements.go:129–142) sets `journal_entry_id = NULL` for **any** line,
including a `bank_fee` line that already posted a real GL entry via
`handleBankFeeStatementLine`. The route also has no status restriction (unlike `MatchLine`
which requires `unmatched/unknown`). So unmatching a `bank_fee` line orphans its journal
entry: the line forgets it posted, but the DR `operating_expense` / CR `bank` remains in the
GL. The operator can then re-post the fee, double-counting the expense. Fix: refuse unmatch
on `status='bank_fee'` (or require a reversal of the linked entry first), and restrict
`UnmatchLine` to `matched` lines only.

**PAY-022 (Medium) — `MatchLine` does not verify the matched document exists, belongs to
the tenant, or has a consistent amount.** `doc_type`/`doc_id` are accepted as opaque
(banking_handlers.go:824–831); `doc_type` is not validated against an allow-list, and the
referenced deposit/payment is never loaded. An operator can "match" a statement line to a
random UUID or a document of another tenant (no IDOR data leak since nothing is read, but
the reconciliation record is meaningless). Fix: validate `doc_type` against a known set and
confirm the referenced doc exists for the tenant with a comparable amount.

**PAY-023 (Low) — bank-fee posting uses the line's txn_date for the entry, with no
period-open re-check beyond `PostEntry`.** Acceptable — `PostEntry` enforces period gating
and returns `ErrPeriodClosed/Locked` → 409 via `journalErrorResponse`. The `absMoney` string
trim (banking_handlers.go:107) correctly takes magnitude without float. No defect; noted as
verified-correct.

---

## Cross-cutting checks

**Tenant isolation / IDOR.** Every repo method takes `tenantID` and filters on it; every
single-row fetch (`GetCashReconciliation`, `GetDeposit`, `GetStatementLine`, `GetInvoice`,
`GetCustomer`) includes `WHERE tenant_id = $1 AND id = $2`. Mutations gate on the same. No
IDOR found in this domain. RLS is declared on every table but is **inert at runtime**:
these handlers begin transactions via `s.deps.DB.Begin(ctx)` (e.g.
banking_handlers.go:135), which never executes `SET LOCAL app.current_tenant`; only
`database.WithTenant` (tenant.go:29–58) sets the GUC, and it is not used by the
payments/banking/receivables paths. Defense is entirely app-layer (and is applied
consistently). **PAY-024 (Info)** — RLS confirmed inert; app-layer tenant scoping is the
sole and consistent control.

**tx + audit + outbox atomicity.** Every mutating handler either uses the `txAudit` helper
(accounting_handlers.go:102 — match/unmatch/create-bank-account) or hand-rolls
`Begin → … → audit.WriteWithOutbox → Commit` with `defer Rollback` (record payment, issue
invoice, post customer payment, cash recon, deposits, import, bank fee). The audit write is
inside the business tx in all cases reviewed. Good. Minor inconsistency: some handlers use
`txAudit`, most don't — not a defect.

**After UPDATE...RETURNING re-read via same tx.** Where a fresh read is returned to the
client after commit (e.g. `handleCreateBankDeposit` line 499, `handleIssueCustomerInvoice`
line 245, `handleApproveCashReconciliation` line 354), the re-read happens **after**
`tx.Commit` via the pool (`s.banking.GetDeposit` / `GetCashReconciliation` / `GetInvoice`),
which is the post-commit DTO refresh, not an in-tx re-read — acceptable here because the
authoritative state was already returned by the RETURNING in the tx and these are
read-your-own-write post-commit. **PAY-025 (Low)** — these post-commit re-reads ignore their
error (`out, _ := s.banking.GetDeposit(...)`); if the follow-up read fails, a zero-value DTO
is serialized to the client (e.g. an all-empty deposit) with a 200/201. Surface the error or
return the in-tx entity.

**Money discipline.** No `float64`/`ParseFloat` in any of `internal/payments`,
`internal/receivables`, `internal/banking`. All amounts are decimal strings, cast `::numeric`
in SQL, summed/compared in SQL. `parseDecimal` (float) is used only for handler-side
validation and the one display threshold (PAY-004). Money discipline is otherwise clean.

**N+1.** `ImportStatement` inserts lines one-per-`Exec` in a loop (statements.go:65–73) and
`handleCreateCustomerInvoice`/`handlePostCustomerPayment` do per-line/per-allocation round
trips. Within a single tx these are bounded by request size (import capped at 1 MiB) and not
a classic read N+1, but a `COPY` / multi-row `INSERT` would be faster for large statements.
**PAY-026 (Low)** — batch the statement-line and invoice-line inserts.

**Error handling / status codes.** Generally correct: 409 for duplicates/bad-state, 422 for
over-allocation / unbalanced / over-limit, 400 for validation, 404 for not-found, 403 via
middleware. `journalErrorResponse` maps posting errors cleanly. One inconsistency:
over-credit-limit returns 422 (payments_handlers.go:127) while invalid customer returns 400
(FK violation) — reasonable.

**Dead code / unreachable.** Dead statuses: `approved`/`rejected` on cash recon (PAY-015),
`in_transit`/`confirmed` on deposits (PAY-017). The two parallel credit-override permissions
(`credit.override_limit` from 0004 vs `customer_credit.override` from 0051) — only the former
is consulted (payments_handlers.go:123); the latter is unused by any enforcement path.
**PAY-027 (Info)** — reconcile the duplicate override permissions.

---

## Findings

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| PAY-001 | High | internal/receivables/repo.go:207-229 | Credit-limit check (INSERT…SELECT, READ COMMITTED, no row lock) races: concurrent credit tenders overshoot the limit and corrupt `balance_after`. | `SELECT … FOR UPDATE` on the customer row / advisory lock, or SERIALIZABLE + retry. |
| PAY-002 | High | services/.../payments_handlers.go:122-134 | Credit tender ignores customer `status` and Phase-8 `hold`; charges post to suspended/on-hold customers. | Load customer in-tx; reject non-`active`/held unless override held. |
| PAY-003 | High | services/.../payments_handlers.go:122-134; internal/receivables/repo.go:207 | Credit tender posts to `ar_entries` only, never to the GL; no shift/sales path posts revenue. `ar_entries` and GL/customer_invoices AR are disjoint, unreconcilable ledgers. | Post DR AR / CR sales-clearing (or generate an invoice) in the same tx; couple the two ledgers. |
| PAY-008 | High | services/.../customer_invoices_handlers.go:327-346; internal/receivables/invoices.go:208 | Customer-payment allocations not bound to the paying customer; payment "from A" can settle B's invoices. | Verify each invoice's `customer_id` == payment customer; add DB FK/CHECK. |
| PAY-013 | High | services/.../banking_handlers.go:316 | `sales_clearing` is credited at recon approval but never debited; sales revenue never recognized in GL; `sales_clearing` grows unbounded. | Add sales-close entry DR sales_clearing / CR revenue (GL half of PAY-003). |
| PAY-021 | High | internal/banking/statements.go:129-142 | `UnmatchLine` nulls `journal_entry_id` on any line incl. posted `bank_fee`, orphaning the GL entry and enabling double-posting. | Refuse unmatch on `bank_fee`/posted lines; restrict to `matched`. |
| PAY-006 | Medium | services/.../customer_invoices_handlers.go:103-112 | Line-amount validation runs after invoice insert, mid-loop. | Validate all lines before any insert. |
| PAY-009 | Medium | internal/receivables/customer_payments.go:54,72-75 | `amount` always == `allocated_amount`; documented "customer credit" for over-payment is unimplemented. | Accept a payment total; credit `customer_credits` for unapplied; or fix the comment. |
| PAY-012 | Medium | internal/receivables/repo.go:287; invoices.go:233 | "Aging" endpoints return a single open balance per customer — no age buckets despite due_date. | Bucket by `due_date`/`recorded_at` in SQL (0-30/31-60/61-90/90+). |
| PAY-014 | Medium | services/.../banking_handlers.go:277-340 | Cash-recon approver may equal submitter/creator; no row-level four-eyes even though perms differ. | Reject approve when `actor==created_by`; maker/checker flag. |
| PAY-016 | Medium | services/.../banking_handlers.go:469-485; deposits.go:122 | Deposit line amount is client-supplied; not validated against recon `counted_cash` or `posted` status; can move fabricated cash. | Derive amount from the reconciliation; require `status='posted'`. |
| PAY-020 | Medium | services/.../banking_handlers.go:748 | Statement import dedup hashes raw bytes; reorder/whitespace bypasses it; no per-line uniqueness → double-count. | Hash a canonical parsed-line projection; add per-line natural-key unique index. |
| PAY-022 | Medium | services/.../banking_handlers.go:824-831; statements.go:113 | `MatchLine` doesn't validate `doc_type`/existence/amount of the matched document. | Allow-list doc_type; load & amount-check the referenced doc for the tenant. |
| PAY-004 | Low | services/.../payments_handlers.go:184-189 | Variance `over_threshold` uses float comparison on money. | Compute `ABS(variance) > 1` in SQL. |
| PAY-007 | Low | internal/receivables/invoices.go (issue) | Zero-total invoice would fail `ErrUnbalanced` (unreachable due to line `>0` CHECK). | None required; noted. |
| PAY-010 | Low | services/.../customers_handlers.go:268-324 | Phase-6 AR payment posts to `ar_entries` only, no GL (payment side of PAY-003). | Couple to GL or document as non-GL. |
| PAY-011 | Low | services/.../customer_invoices_handlers.go:271 | No idempotency key on customer payments; retry double-pays within headroom. | Client idempotency token. |
| PAY-015 | Low | internal/banking/cash_reconciliation.go:186; 0042:28 | `approved`/`rejected` recon statuses never set — dead states. | Remove or implement the reject/approve steps. |
| PAY-017 | Low | internal/banking/deposits.go:196; 0043:52 | `in_transit`/`confirmed` deposit states unreachable; confirm accepts them. | Trim status set or implement. |
| PAY-018 | Low | services/.../banking_handlers.go:541-553 | Prepare reads deposit outside tx for post-date/station. | Acceptable; could read in-tx. |
| PAY-019 | Low | internal/banking/deposits.go:205 | `SetDepositConfirmedEntry` overwrites `confirmed_entry_id` unconditionally. | Add `WHERE confirmed_entry_id IS NULL`. |
| PAY-025 | Low | banking_handlers.go:499,604,688; customer_invoices_handlers.go:130,245 | Post-commit DTO re-reads ignore errors → may serialize zero-value DTO with 200/201. | Surface the error or return the in-tx entity. |
| PAY-026 | Low | internal/banking/statements.go:65-73 | Per-line `Exec` loop on import (and invoice/allocation inserts). | Batch via multi-row INSERT/COPY. |
| PAY-005 | Info | internal/payments/repo.go:103-113 | Shift reconciliation compares tenders to `sales.gross_amount`, not GL revenue. | Operational by design; note alongside PAY-003. |
| PAY-024 | Info | internal/database/tenant.go:29; banking_handlers.go:135 | RLS inert at runtime (GUC never set on these tx); app-layer scoping is the sole control. | Confirmed; consider wiring `WithTenant` or accept app-layer as canonical. |
| PAY-027 | Info | services/.../payments_handlers.go:123; migrations 0004/0051 | Duplicate credit-override permissions (`credit.override_limit` vs `customer_credit.override`); only the former is enforced. | Consolidate to one. |

### Severity counts

- **Critical: 0**
- **High: 6** — PAY-001, 002, 003, 008, 013, 021
- **Medium: 7** — PAY-006, 009, 012, 014, 016, 020, 022
- **Low: 10** — PAY-004, 007, 010, 011, 015, 017, 018, 019, 025, 026
- **Info: 3** — PAY-005, 024, 027

Total: 26 findings.

### Top 5 risks

1. **PAY-003 — Operational AR (`ar_entries`) and the GL/customer-invoice AR are disjoint
   and unreconcilable; fuel sales revenue is never recognized in the GL.** This is the
   deepest defect: a credit tender increments `ar_entries` with no journal entry, and
   nothing posts sales revenue, so finance reports understate AR and revenue while the
   trial balance still reads "balanced".
2. **PAY-013 — `sales_clearing` is credited at cash-recon approval but never debited**,
   confirming PAY-003 from the cash side and leaving an ever-growing unoffset liability;
   the test suite's `balanced:true` assertion masks this.
3. **PAY-008 — Customer-payment allocations are not bound to the paying customer**, so a
   payment can silently settle a different customer's invoices.
4. **PAY-001 — Credit-limit enforcement races under concurrency** (READ COMMITTED, no row
   lock), allowing the limit to be overshot and `balance_after` to be corrupted.
5. **PAY-021 — Unmatching a posted `bank_fee` line orphans its GL entry** and enables
   double-posting the expense, because unmatch nulls the entry link without reversal and
   has no status guard.
