# FuelGrid OS — Consolidated Findings Register

Prioritized, deduplicated list of **Critical** and **High** findings across sections 01–14. Each row cites `file:line` and a fix. Medium/Low/Info findings (and full context) live in the per-section reports. Section 15–18 findings (frontend/SDK/testing) will be appended after completion.

**Tally (sections 01–14):** Critical 6 · High 65 · Medium 98 · Low 122 · Info 39 = **330**.

---

## CRITICAL (6)

| ID | File:Line | Issue | Fix |
|---|---|---|---|
| INV-001 | `internal/inventory/repo.go:80-81,100`; `reconciliation_handlers.go:45-47,174-177` | Litres and all reconciliation figures are `float64` end-to-end; variance/tolerance/seal math in floating point. The `0.0005` seal threshold is float residue on a money/litre path. | Carry litres/money as decimal strings; do all arithmetic in SQL `numeric`. Remove float thresholds. |
| PROC-12 | `internal/inventory/deliveries.go:228` | `landed_cost_per_litre = total / volume_litres` with no zero-guard in the repo; only the handler checks `>0`, so any other caller triggers SQL divide-by-zero on the cost path. | Guard `volume_litres > 0` in the repo method; use `NULLIF`. |
| REV-01 | `internal/revenue/repo.go:104-109` (+3 sibling sites) | Weighted-average delivery cost omits `status='posted' AND supersedes_id IS NULL`; a reversed delivery's original positive litres stay in the average forever. Corrupts COGS, margin, stock value, and the below-cost price guard. | Add the status/supersedes filter to every cost-average query. |
| RISK (pause) | `internal/risk/governance.go:77-83`; `internal/risk/alerts_detect.go:38-77` | Engine "pause" is a no-op — `RunDetection` ignores rule status and runs three **hardcoded** SQL packs regardless of configured thresholds/lookback/tuning. Rules are disconnected from detection entirely. | Make detection read active rules and their params; honor pause. |
| INFRA-03 | `internal/events/bus.go:70`; `events/publisher.go:170`; `cmd/api/main.go:251` | `InProcessBus.Publish` always returns `nil`; failed handlers are marked published and lost. The only consumer is a log line — the "event-driven" layer is effectively a no-op. | Propagate handler errors; only mark published on success; add real consumers or remove the claim. |
| INFRA-01 / AUTH-25 | `migrations/0005_rls.up.sql:12`; `internal/database/tenant.go:29`; `cmd/api/main.go:146` | RLS is defined on ~51 tables but **inert at runtime**: the API connects as the table owner and `WithTenant` (the only `SET LOCAL app.current_tenant` issuer) is never called. Tenant isolation has zero DB-level backstop. | Connect as a non-owner role and call `WithTenant` per request inside the tx; add a test that proves cross-tenant reads are blocked at the DB. |

---

## HIGH (selected — full set in section reports)

### Accounting / treasury integrity
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| ACCT-016 | `internal/accounting/reports.go:89-102` | Balance sheet omits net income from equity; no close-to-retained-earnings entry → **A ≠ L + E** for any active tenant. | Compute retained earnings / current-year income into equity; assert the equation in tests. |
| PAY-013 | `banking_handlers.go:316` | Cash-recon approval credits `sales_clearing` but nothing ever debits it; sales revenue never recognized, clearing liability grows unbounded. | Post the offsetting debit; recognize revenue end-to-end. |
| PAY-003 | `payments_handlers.go:122`; `receivables/repo.go:207` | Credit tenders write `ar_entries` but post no journal; operational AR and GL/customer-invoice AR are disjoint and unreconcilable. | Unify on one AR ledger backed by the GL. |
| ACCT-001 | `internal/accounting/journal.go:140-150`; `0039_journals.up.sql` | Debits==credits and posted-entry immutability enforced **only in Go**; no DB balance/immutability trigger. | Add DB constraints/triggers for balance and append-only. |
| ACCT-012 | `expenses_handlers.go:492-509` | Petty-cash `adjustment`/`transfer` mutate the float balance but post no journal entry — live double-entry break. | Journal every petty-cash movement. |
| ACCT-009 | `internal/expenses/expenses.go:183-198`; `server.go:720-728` | No separation of duties: one user can create→submit→approve→post an expense / supplier payment. | Reject `approved_by == created_by`; require approver role. |
| ACCT-004 | `internal/accounting/periods.go:134-144`; `close_handlers.go` | Period close/lock checks only the status edge; never verifies blockers or that the period balances. | Gate close on the checklist + balance check. |
| PAY-008 | `customer_invoices_handlers.go:327`; `invoices.go:208` | Customer-payment allocations aren't bound to the paying customer; a payment can settle another customer's invoices. | Constrain allocations to the payer's invoices. |
| PAY-001 | `receivables/repo.go:207` | Credit-limit check is INSERT…SELECT at READ COMMITTED with no row lock; concurrent tenders overshoot the limit and corrupt `balance_after`. | `SELECT … FOR UPDATE` the customer row inside the tx. |
| PAY-021 | `banking/statements.go:129` | `UnmatchLine` nulls `journal_entry_id` on any line incl. posted `bank_fee`, orphaning the GL entry and enabling double-posting. | Block unmatch on posted fee lines; reverse via contra entry. |

### Money discipline (float leakage)
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| OPS-001 | `internal/readings/repo.go:52`; `operations/close.go:17-37`; `shift_close_handlers.go:174,328,341` | Money/litres scanned to `float64`; expected-cash, litres-sold, variance computed in Go float. | Decimal strings + SQL arithmetic. |
| PROC-06 | `internal/procurement/purchase_orders.go:51`; `inventory/deliveries.go:207` | Litres flow as `float64` through PO→receipt→invoice, including the tolerance boundary. | Decimal strings end-to-end. |
| ORG-04 | `products/repo.go:23-28`; `tanks/repo.go:23-26`; `nozzles/repo.go:27` | Prices/capacities/density carried as `float64`. | Decimal strings. |
| FLEET-008 | `internal/fleet/odometer.go:25` | `RecordOdometer` runs outside a tx, audits in a separate best-effort tx whose error is ignored, and decides monotonicity in `float64`. | Single tx; numeric comparison; don't swallow audit errors. |

### Procurement / inventory correctness
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| PROC-13 | `internal/inventory/deliveries.go:208,288` | Product **loss tolerance** reused as a **receiving** tolerance; supplier can under-deliver up to loss% and the PO auto-completes as fully received (financial leak). | Separate receiving tolerance; don't auto-complete on shortfall. |
| PROC-07 | `internal/inventory/deliveries.go:238` | No over-receipt cap; one receipt can post stock far past ordered qty with only an advisory `over` flag. | Cap received ≤ ordered (+ explicit tolerance). |
| PROC-18 | `procurement_handlers.go:1043`; `overview.go:59` | Supplier-invoice "approval" only emits a fire-and-forget `PayableCreated` event — no payable created; overview reports lifetime approved spend as "outstanding," never netting payments. | Create the payable in-tx; compute true outstanding. |
| INV-014 | `reconciliation_handlers.go:309,516,535` | Compute runs on the pool **before** the persist/seal tx (TOCTOU); seal write-off taken from a stale snapshot. | Compute and seal in one locked tx. |
| INV-016 | `reconciliation_handlers.go:533-559` | Balance-forward anchor depends on a float-thresholded, pre-tx seal write-off; a wrong write-off cascades into every later day's opening. | Fix INV-001/INV-014 first; carry exact sealed physical. |
| INV-002/003 | `internal/inventory/repo.go:145-159`; `0024_*.up.sql` | Ledger is append-only by convention only (no DB guard); `balance_after` via unlocked `SUM` under Read Committed → inconsistent balances on concurrent posts. | Append-only trigger; serialize per-tank posts with a lock. |

### Operations / shift control
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| OPS-002 | `shift_exceptions_handlers.go:48` | No separation of duties: one supervisor can open, attend, read, close, submit cash, resolve the variance exception, and approve the same shift. | Enforce distinct approver; block self-approval. |
| OPS-008 | `shift_close_handlers.go:93-226` | Close validates readings then snapshots close lines in a *later* tx; a concurrent correction makes the snapshot inconsistent. | Validate + snapshot in one locked tx. |
| OPS-007/009 | `operating_days_handlers.go:227`; `shift_exceptions_handlers.go:74` | TOCTOU on day-close and approval (no row lock): a closed day can gain an open shift; approval can bypass a concurrently-raised exception. | Lock rows inside the mutating tx. |

### Fleet / credit enforcement
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| FLEET-007 | `internal/fleet/drivers.go:41` | PINs & credential tokens hashed with unsalted single-round SHA-256; 4-digit PINs trivially brute-forced. | Salt + stretch (argon2/bcrypt); rate-limit validation. |
| FLEET-002 | `receivables/repo.go:207` | The real credit sale `PostCharge` checks only raw `credit_limit` — ignores authorization holds, profile `hold`, and `on_hold/suspended` status. The authorization gate is advisory-only. | Enforce full exposure + status on the sale path. |
| FLEET-005/006 | `internal/fleet/authorizations.go:129,184`; `fleet_authorization_handlers.go:145` | Authorization holds never auto-released/expired; `consumed_by` sale-id accepted with no validation/FK. Exposure inflates forever; fulfillment can point at phantom sales. | Auto-expire holds; validate/FK the sale link; release on fulfillment. |
| FLEET-003/004 | `internal/fleet/authorizations.go:113-143` | Only `period='transaction'` limits enforced (day/week/month inert; `max_litres`/`product_id` ignored); no row lock → concurrent requests over-reserve. | Enforce all periods/dimensions; lock the customer row. |
| FLEET-001 | `internal/fleet/price_agreements.go:157` | `ResolveCustomerPrice` has zero callers; negotiated fleet pricing never applied to a sale. | Wire into the sale pricing path. |

### Enterprise / governance authenticity
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| ENT-25 | `internal/enterprise/central_commercial.go:199-288` | Stock transfers have no product alignment; litres of one product can move out of a tank holding another, corrupting both ledgers. | Validate product match across tanks. |
| ENT-24 | `internal/enterprise/central_commercial.go:254-278` | `ReceiveTransfer` derives `balance_after` from a stale `status='posted'` snapshot in Go, bypassing `inventory.PostMovement` and the opening-balance guard. | Post both legs through `PostMovement` in one tx. |
| ENT-01 | `internal/enterprise/governance.go:87` | Delegated enterprise scope grants enforce nothing; `EffectiveStations` feeds one read endpoint, so every RBAC holder acts tenant-wide. Feature is cosmetic. | Integrate scopes into the policy engine or remove the claim. |
| ENT-04 | `internal/enterprise/governance.go:270-303` | Approval requests have no self-approval guard and `required_role` is never enforced; one user can raise and approve a high-value request. | Enforce approver ≠ requester and required role/quorum. |

### Risk / intelligence authenticity
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| RISK (state) | `alerts_detect.go:114-129`; `investigations.go:99-125` | No alert/case state machine; unconditional `UPDATE status` allows illegal transitions (resolved→open, disposition overwrite); `ErrBadState` is dead code. | Enforce a transition table. |

### Identity / auth
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| AUTH-16 | `auth_handlers.go:113-121` | Raw password-reset token logged at Info with the email — log access = account takeover. | Never log secrets; log a correlation id only. |
| AUTH-13 | `migration 0003:6`; `repo/user.go:157` | TOTP secrets stored plaintext in `users.mfa_secret`; a DB read defeats MFA tenant-wide. | Encrypt at rest (KMS/app key). |
| AUTH-04/10 | `internal/identity/service.go:389,355,197-211` | `Resolve` trusts Redis only; best-effort revoke leaves revoked sessions live up to 12h; MFA has no throttle/lockout/replay protection. | Authoritative DB check on sensitive ops; throttle MFA; bind/expire codes. |
| AUTH-20/21 | `policy/db_loader.go:80`; `server.go:277-810` | AuthZ fails **open**: zero station-access rows ⇒ tenant-wide for any role; many mutating routes lack a declarative permission gate. | Default-deny on empty scope; add declarative gates to every mutation. |

### Architecture / infra / availability
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| INFRA-22 | `cmd/seed/main.go:276,46` | `seed` provisions a known-password `system_admin` with no non-dev guard — production backdoor. | Guard seed to dev; randomize/print-once admin secret. |
| ARCH-02 | `cmd/api/main.go:181-183` | Empty `AUTH_PASSWORD_PEPPER` in non-dev only warns, then boots. | Hard-fail in non-dev. |
| INFRA-12 | `internal/database/postgres.go:40`; `config.go:29` | No statement/lock/idle timeouts on a 25-conn pool → one slow query stalls the API. | Set `statement_timeout`, `idle_in_transaction_session_timeout`; size pool. |
| ARCH-11 / INFRA-06/07 | `handlers.go:63`; `server.go:149`; `accounting_handlers.go:134` | No request-body cap (DoS) and no list pagination (memory exhaustion). | `http.MaxBytesReader`; mandatory pagination. |
| ARCH-01 | `docs/openapi.yaml`; `server.go:168-834` | OpenAPI stops at Phase 3 (~70 paths) while ~237 routes are registered; ~140+ undocumented; CI only lints YAML validity. | Generate/verify spec from routes; contract-test in CI. |

### Data model (schema is otherwise A−)
| ID | File:Line | Issue | Fix |
|---|---|---|---|
| DB-001 | `0040_payables.up.sql:12-13,19,21` | AP ledger stores `supplier_id`/`station_id`/`journal_entry_id` as bare UUIDs with **no FK** — on the most fraud-sensitive table. | Add composite tenant FKs (targets already exist). |
| DB-002 | `0039_journals.up.sql:17,58` | `journal_entries`/`journal_lines.station_id` not FK'd to stations (cross-tenant station possible; also unindexed). | Add FK + index. |
| DB-003 | `0039_journals.up.sql:20-21` | Reversal links `reverses_entry_id`/`reversed_by_entry_id` have no self-FK; can dangle/cross tenant, undermining the correction chain. | Add self-referential composite FK. |

---

*Medium, Low, and Info findings (≈259 more) are catalogued in full in the per-section reports `01`–`14`. Sections `15`–`18` (frontend, SDK, testing) and their findings are pending completion.*
