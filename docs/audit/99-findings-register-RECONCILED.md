# FuelGrid OS — Reconciled Findings Register (Critical + High)

> Generated 2026-05-29 by reconciling `99-findings-register.md` (written 07:21) against current `main` after the P0/P1 remediation campaign. Each finding re-checked against live code + git history. See `99-findings-register.md` for the original pre-remediation list.

Of the 96 Critical+High findings re-checked against current `main` on 2026-05-29: **34 are now FIXED, 48 remain STILL OPEN, 14 are PARTIAL, and 0 are UNVERIFIABLE.** Of the 9 Critical findings, 6 are fixed, 2 are partial (INV-001, INFRA-01), and 1 (TEST-02/TEST-03 cluster) is partial; only the float-discipline split inside INV-001 keeps a Critical from fully closing — no Critical remains fully open.

## STILL OPEN — Critical

None remaining.

## STILL OPEN — High

**01 — Architecture / Build / Tooling**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| ARCH-01 | docs/openapi.yaml:4 vs services/api/internal/server/server.go | OpenAPI spec stale — documents only Phases 1–3 (~40% of routes) | Regenerate spec from chi route metadata or hand-add Phase 4–10 paths, bump version; add CI route-vs-spec diff | Spec still version 0.1.0, 70 paths, tags end at 'Operations'; grep for procurement/finance/fleet/etc returns 0; server.go registers 236 routes; CI (ci.yml:51-60) runs only `redocly lint` |

**02 — Identity / Auth / Security**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| AUTH-04 | internal/identity/service.go:404-417,365-378 | Session expiry/revocation not authoritative on hot path; best-effort Redis delete leaves revoked sessions live | Check revoked_at / per-user invalidation epoch on hot path; make reset-path Redis delete fatal on failure | Resolve only checks ExpiresAt against Redis copy, never Postgres revoked_at; revokeAllUserSessions swallows DeleteByID error with Warn (line 374); no invalidation epoch |
| AUTH-09 | services/api/internal/server/auth_handlers.go:252-260; internal/identity/service.go:150 | Rate-limit/lockout keys bypassable behind proxy; per-account lockout weaponizable | Configurable trusted-proxy XFF parsing; per-account throttle; soften hard lockout | clientIP always returns r.RemoteAddr, ignores XFF; bucket is IP-only 'login:'+IP; no per-account limit; MarkLoginFailure DoS-able |
| AUTH-20 | internal/identity/policy/db_loader.go:80 | Zero station-access rows ⇒ tenant-wide for any role; fails open on missing scope | Require explicit tenant-wide flag; 'no scope' = no access for non-admin; guard last-station revoke | TenantWide = len(StationIDs)==0 for any role; comment still says absence = tenant-wide; no explicit flag/guard |
| AUTH-21 | services/api/internal/server/server.go:301-303,313-318,339-371,783-784,790-791,802-803,813-814 | Many mutating routes have no requirePermission; rely on unverifiable in-handler authorizeStation | Default-deny: explicit permission middleware on every mutating route | Tanks/pumps/nozzles/deliveries/PO transitions/incidents/operating-days/shift routes have only requireAuth; depend on in-handler authorizeStation (tanks_handlers.go:175,300) |
| AUTH-27 | services/api/internal/server/server.go:173-180 | CORS AllowCredentials:true with operator origins and no wildcard guard | Drop AllowCredentials (header tokens); reject wildcard/non-https origins | AllowCredentials:true (server.go:178) with AllowedOrigins:cfg.CORSOrigins; no '*' guard; API uses header bearer tokens |

**03 — Platform / Org / Assets**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| ORG-01 | services/api/internal/server/stations_handlers.go:47-73 | handleListStations ignores actor's station scope (horizontal IDOR) | Apply stationReadFilter like tanks/pumps/nozzles; thread id slice into stations.List | stations_handlers.go:62 calls stations.List(...) with no scope filter; tanks/pumps/nozzles handlers all call stationReadFilter, stations does not |
| ORG-02, OPS-001, PROC-06, TEST-08 | internal/products/repo.go:23-28; internal/tanks/repo.go:23-26; internal/nozzles/repo.go:27; internal/readings/repo.go:23,52; internal/readings/dips.go:20-23; internal/operations/close.go:17-37; internal/readings/meter.go:22-41; internal/procurement/purchase_orders.go:51-53; internal/inventory/deliveries.go:207,240; internal/inventory/repo.go:79-80 | Money/litres/density carried as Go float64 and arithmetic done in Go float (decimal-string house-rule violation) across products/tanks/nozzles, operations/shifts, procurement PO/receipt boundary, and inventory litres; no property/fuzz test for accumulation | Scan/serialise as string/pgtype.Numeric; do all arithmetic in SQL; add a property test summing many fractional movements | products/tanks/nozzles repos declare float64; readings/close/meter use Go float (ExpectedValue=litres*price at shift_close_handlers.go:175, math.Abs variance at :402); PO OrderedLitres/ReceivedLitres float64 with Go-float variance (deliveries.go:213-214); inventory Litres/BalanceAfter still float64 (repo.go:79-80); no testing/quick or Fuzz in first-party code. Note: currency in deliveries.go now decimal strings; litres remain float |
| ORG-03 | services/api/internal/server/tanks_handlers.go:362-432 | Tank delete guards only live nozzles, not on-hand stock / ledger balance | Block delete when book balance != 0 (or opening balance / dips exist), inside the tx | handleDeleteTank only checks CountActiveForTank (392-398) before SoftDelete; no balance/stock/ledger/dip guard |
| ORG-04 | internal/calibration/repo.go:75-85; services/api/internal/server/calibration_handlers.go:223-231,271 | Active calibration chart chosen by status only, ignoring effective_from | Select active by effective_from window; validate effective_from >= superseded; set superseded effective_until = new.effective_from | ActiveChart selects WHERE status='active' with no window; SupersedeActive sets effective_until=now() unconditionally; upload accepts arbitrary effective_from; migration 0012 added no CHECK |

**05 — Inventory / Reconciliation**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| INV-002 | services/api/migrations/0024_stock_movements.up.sql (no trigger) | No DB-level append-only enforcement; litres freely UPDATE-able | BEFORE UPDATE trigger rejecting litres/tank/type/seq/balance_after changes; forbid DELETE | 0024 has only set_updated_at trigger; 0065 immutability covers only journal tables, not stock_movements; 0026/0030 add only indexes/columns |
| INV-003 | internal/inventory/repo.go:145-159 | PostMovement computes balance_after with no row lock → races under concurrent posts | pg_advisory_xact_lock(tank) or SELECT...FOR UPDATE on tank at top of post | PostMovement inserts balance_after from unlocked SUM; PostWriteOff (reconciliation/repo.go:220-239) same; only FOR UPDATE found is on purchase_orders (deliveries.go:174) |
| INV-010 | reconciliation_handlers.go:157; inventory/periods.go:18 | Opening-balance double-count between openingBook and period opening_total on genesis path | Exclude opening-type rows from period sum after anchor; assert at most one opening | Compute SUMs opening-type rows for seq>fromSeq as opening_total (reconciliation/repo.go:150) and adds on top of opening_book (line 164); no single-opening assertion/unique index |

**06 — Procurement**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| PROC-19 | internal/procurement/invoices.go:342 | Quantity-discrepancy tolerance pivots on received_litres; multi-invoice re-aggregation double-counts | Base tolerance on ordered/invoiced; scope receipts per-invoice | invoices.go:362 uses abs(invoiced-received) > greatest(received*0.005,1); received summed via LEFT JOIN over all receipts ever (345-352), no period scoping |

*(PROC-06 merged into the float-discipline cluster above with ORG-02.)*

**07 — Pricing / Sales / Revenue**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| REV-02 | internal/revenue/repo.go:105; internal/inventory/costing.go:11-15 | Cumulative/lifetime average mislabeled as moving-average; consumed stock never lowers cost basis | Implement perpetual weighted-average over on-hand stock, or rename + document the cumulative policy | Aggregation SUMs over ALL delivery movements, never decremented on sale; 54b90fb only added status/supersedes filter; comments/function still say 'moving-average'; no policy doc |
| REV-03 | internal/revenue/repo.go:121-122; phase6_integration_test.go:64-66; cmd/seed/main.go:165 | Tax-inclusive net/tax split untested while production seeds 18% | Add integration test with non-zero tax_rate asserting exact net/tax/gross through RecognizeShiftSales | Split formula only exercised at tax_rate=0; phase7 INSERTs pre-computed tax manually and only tests GL posting, never runs the split SQL |

**08 — Payments / Receivables / Cash & Banking**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| PAY-008 | services/api/internal/server/customer_invoices_handlers.go:327-352; internal/receivables/invoices.go:208-228 | Customer-payment allocations not bound to the paying customer | Verify each invoice's customer_id == payment customer (or add predicate to ApplyInvoicePayment); add DB FK/CHECK | Handler loops calling ApplyInvoicePayment with no customer check (332-339); ApplyInvoicePayment filters only tenant_id+id (invoices.go:217); payment 'from A' can settle B's invoices |
| PAY-021 | internal/banking/statements.go:129-142; services/api/internal/server/banking_handlers.go:868-897 | UnmatchLine nulls journal_entry_id on any line incl. posted bank_fee, orphaning the GL entry | Refuse unmatch on bank_fee/posted lines (or require reversal); restrict UnmatchLine to status='matched' | UnmatchLine UPDATEs journal_entry_id=NULL with no status restriction; handler calls with no guard/reversal; posted entry orphaned, fee re-postable |

**09 — Accounting / Payables / Expenses / Finance Reports**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| ACCT-004 | internal/accounting/periods.go:134-144; services/api/internal/server/close_handlers.go | Period close/lock enforces no blockers, never checks period balances; checklist advisory only | Refuse lock (or require force flag) when blockers>0 and debits != credits | handlePeriodTransition delegates to bare status-edge transition() (periods.go:103-128); nothing reads blocker counts or trial balance; can POST .../locked with blockers and unbalanced period |

**10 — Fleet / Customer Credit**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| FLEET-001 | internal/fleet/price_agreements.go:157 | ResolveCustomerPrice has zero callers — negotiated fleet prices never applied to sales | Wire resolution into credit-sale price path; snapshot applied price on the sale | Repo-wide grep finds only definition; credit-sale charge path takes client-supplied amount, never resolves agreement price |
| FLEET-005 | services/api/internal/server/fleet_authorization_handlers.go:145-185 | consumed_by (sale id) accepted unvalidated — phantom-sale fulfillment | Validate sale exists, same tenant+customer, amount matches; add FK | Handler only checks ConsumedBy != Nil; FulfillAuthorization sets consumed_by with no check; migration 0054:49 has no FK; test fulfills with uuid.New() expecting 200 |
| FLEET-008 | internal/fleet/odometer.go:25-61; services/api/internal/server/fleet_odometer_handlers.go:54-62 | RecordOdometer runs outside a tx, audits best-effort (error ignored), uses float64 for monotonicity | Wrap read+insert+audit in one tx; do comparison in SQL (numeric) | Three separate non-tx pool queries; float-based monotonicity (var last *float64); handler discards audit error (_ = WriteWithOutbox, _ = tx.Commit) |

**11 — Enterprise**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| ENT-01 | internal/enterprise/governance.go:91; services/api/internal/server/server.go:531 | Delegated scopes enforce nothing — EffectiveStations used only by a read endpoint; mutations tenant-wide | Intersect target stations with effective set in mutating handlers, or document grants as advisory | Grep finds only definition + read handler (wired at server.go:531); CreateTransfer/ReceiveTransfer/ActivatePriceRollout never intersect |
| ENT-24 | internal/enterprise/central_commercial.go:253-277 | Transfer balance_after from stale snapshot + status='posted' filter; bypasses PostMovement opening-balance guard | Post both legs via inventory.PostMovement / SUM-based balance | ReceiveTransfer reads balance_after via stale LIMIT 1 into float64, checks overdraw in Go, raw-INSERTs both movements; does not route through PostMovement; skips opening-balance guard |
| ENT-25 | internal/enterprise/central_commercial.go:199-288; services/api/migrations/0059_central_commercial.up.sql:80,89-94 | No product alignment — transfer can move one product out of a tank holding another | Enforce from.product = to.product = product_id via composite FK/check | Only chk_sto_* and independent FKs; no composite FK/CHECK tying tank product_ids; CreateTransfer/ReceiveTransfer do no alignment check; no migration after 0059 adds it |

**12 — Risk, Fraud & Intelligence**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| RISK-003 | internal/risk/alerts_detect.go:214-229; risk_handlers.go:232-271 | Alert transitions unconditional; no state machine; ErrBadState dead; disposition not required | Add `AND status=<allowed-from>` (or transition map) → ErrBadState/409; require disposition on resolve/dismiss | TransitionAlert UPDATEs with no current-status guard; ErrBadState (repo.go:16) never returned; resolve/dismiss handler leaves Disposition optional |
| RISK-004 | internal/risk/investigations.go:99-125; risk_investigation_handlers.go:237-318 | Case and action status transitions unconditional; case lifecycle unenforced | Enforce a transition table for case and action status; reject illegal moves with 409 | SetCaseStatus / SetActionStatus run unguarded UPDATEs; no transition table/409; only commit touching file is original feature e650e82 |

**13 — Cross-cutting Infra**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| INFRA-07 | accounting_handlers.go:134; (most list handlers) | No shared pagination; list endpoints return all tenant rows with no LIMIT/OFFSET | Shared parsePage(r) helper enforcing max limit + offset/cursor; thread LIMIT/OFFSET into every list query | No pagination helper (grep returns nothing); handleListAccounts returns uncapped {items,count}; accounts.go:103 has no LIMIT/OFFSET; 453eb95 explicitly deferred INFRA-07 |

**14 — Data Model / Migrations**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| DB-001 | services/api/migrations/0040_payables.up.sql:12-13,19,21 | payables.supplier_id/station_id/journal_entry_id are bare uuid with no FK | Add composite FKs to suppliers/stations/journal_entries (ON DELETE RESTRICT) | Columns plain uuid; only chk/uq constraints; comment 'No FK to Phase-5 tables'; no ALTER ADD FOREIGN KEY anywhere |
| DB-002 | services/api/migrations/0039_journals.up.sql:17,58 | journal_entries.station_id and journal_lines.station_id have no FK to stations | Add FK (tenant_id, station_id) → stations(tenant_id, id) + index | station_id plain uuid; existing FKs cover period/posted_by/entry/account only; no later migration adds station FK |
| DB-003 | services/api/migrations/0039_journals.up.sql:20-21 | reverses_entry_id / reversed_by_entry_id have no self-FK | Add self composite FKs to journal_entries(tenant_id,id) for both columns | Both plain uuid despite valid composite target uq_journal_entries_tenant_id; 0065 trigger adds no FK; reversal links can dangle/cross tenant |

**15 — Frontend Foundation**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| WEB-001 | apps/web/src/stores/auth-store.ts:33-39 | Bearer JWT persisted to localStorage; no httpOnly-cookie path | Document risk + ship CSP/SRI now; plan migration to httpOnly cookie + server session | auth-store.ts:35 uses localStorage, partialize persists {token,expiresAt}; SDK sends credentials:'omit' (client.ts:191); no CSP |
| WEB-002 | (no middleware.ts) + apps/web/src/app/(dashboard)/layout.tsx:15 | Route protection purely client-side; no server middleware gate; guard duplicated | Add server middleware gate (ties to WEB-001) | No middleware.ts anywhere; layout.tsx:15 wraps in <ProtectedRoute> 'use client' guard only |
| WEB-003 | packages/sdk/src/client.ts:196-225 + apps/web/src/lib/api.ts | Transport has no 401 handling; expired/revoked tokens don't force logout | Add 401 interceptor / QueryCache.onError → clearSession() + redirect to /login | client.ts:225 throws generic SdkError for all non-2xx, no 401 hook; only 401 check is login-form.tsx:88; no QueryCache.onError |
| WEB-004 | apps/web/src/app/providers.tsx:9-31 | No global query/mutation error handling, no React error boundary; Sentry never fed | Add QueryCache/MutationCache onError (→ Sentry + 401 logout) + app/global-error.tsx + segment error.tsx | makeQueryClient has only defaultOptions; no error.tsx/global-error.tsx; grep captureException returns nothing |
| WEB-005 | apps/web (filesystem) | 'Offline-first PWA' claim is false: no manifest, service worker, public/, or PWA dep | Either implement (manifest + Serwist/Workbox) or correct product claims | No public/ dir; no source manifest/SW; grep for serwist/workbox/next-pwa returns nothing |
| WEB-006 | apps/web/next.config.ts:3-12 | No security headers — no CSP, X-Frame-Options, HSTS, X-Content-Type-Options | Add headers() with strict CSP and standard hardening headers | next.config.ts (14 lines) has only reactStrictMode + transpilePackages; no headers()/CSP — primary XSS mitigation for WEB-001 absent |

**16 — Frontend Pages**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| PAGE-013 | apps/web/src/app/(dashboard)/** (all); apps/web/src/hooks/use-permissions.ts (unused) | No page uses usePermission; every mutation button shows to everyone, relying on API 403 post-click | Wrap/disable action controls with usePermission(code,{stationID}); hide/disable + tooltip when false | Grep for usePermission/hasPermission in dashboard pages returns zero; hook never imported by any page; action buttons render unconditionally |
| PAGE-011 | apps/web/src/app/(dashboard)/settings/products/page.tsx:77,193; packages/sdk/src/types.ts:84 | Product default_price/tax_rate are JS floats end-to-end | Carry product price as decimal string; format without Number/toFixed | buildPayload does Number(...)||0 (77-78); display p.default_price.toFixed(2) (193); SDK Product types default_price/tax_rate number |
| PAGE-002 | apps/web/src/app/(dashboard)/procurement/page.tsx:156-164 | Sums money strings via Number() then re-stringifies — float drift across suppliers | Sum with decimal-safe reducer (integer cents or decimal lib); or have API return aggregate | Outstanding balance computed as money(String(reduce(sum+Number(b.outstanding_amount||0),0))) — float reduce over decimal strings |
| PAGE-008 | risk:34-48,126; enterprise/approvals:39-43; settings/users:62-90; profile:38-41 | Multiple mutations have no onError → silent failures | Add onError → visible banner/toast on each, matching operations/revenue pattern | risk detect/resolve, approvals decide, users grant/revoke/status, profile revoke all have onSuccess only; resolve.isPending disables every row button |

**17 — SDK / UI Packages**

| ID | Location | Issue | Recommended fix | Evidence |
|---|---|---|---|---|
| SDK-01 | packages/sdk/src/client.ts:228 | `return parsed as T` unchecked cast; zero runtime validation in SDK | Add zod/valibot schema (or dev-mode shape assertion) before casting | request<T>() ends with `return parsed as T`; grep for zod/valibot returns nothing; no schema/assert/validate |
| SDK-08 | packages/sdk/src/client.ts:546,562,590-592,636,694,864,1052,1064,1131-1134,1209 | 15 request-body params type money/litre/rate values as number | Type money/litre/rate request params as decimal string | ordered_litres, volume/dip litres, invoiced_litres, capacity_litres, default_price, reading, cash/mm/card/credit amounts, adjust litres all number; transferStock litres:string (2604) shows the inconsistency |
| SDK-09 | packages/sdk/src/types.ts:84,111,113,145,169-170,208,263-266,273,301 | Response interfaces carry the same money/litre-as-number rot | Type money/litre response fields as decimal string | Product.default_price, PO line litres, Delivery.volume_litres, StockMovement.balance_after, SupplierInvoiceLine.invoiced_litres, Tank litres, Nozzle.default_price all number while sibling amounts are string |
| SDK-19 | packages/ui/src/components/tank-visual.tsx:13-22; packages/ui/src/components/pump-card.tsx:13,104 | TankVisual/PumpCard consume money/litres as number, calling .toFixed() | Accept decimal strings; format with Intl.NumberFormat instead of Number/.toFixed() | TankVisualProps litres number; PumpCardNozzle.price number and render calls n.price.toFixed(2) (throws on decimal string) |

## PARTIAL — needs follow-up

| ID | Severity | Location | What remains | Evidence |
|---|---|---|---|---|
| AUTH-10 | High | internal/identity/service.go:205-225; internal/identity/totp/totp.go:47-54 | One-time-use / replay protection still missing | 69089ba added MarkLoginFailure on bad TOTP (brute-force addressed); but no used-code cache (grep for replay/GETDEL/consumed finds nothing) and totp.Verify still Skew:1 (90s) — code replayable within window. Fix commit 69089ba |
| AUTH-25 | High | services/api/internal/server/auth_middleware.go:55-64; server.go:130; internal/database/postgres.go:102-121 | RLS mechanism exists but opt-in and OFF by default | ab3ce3d+8b19a86 added AcquireTenant wired into requireAuth; but rlsEnabled false unless DATABASE_APP_URL set; default falls back to owner pool, RLS bypassed. Fix commit ab3ce3d |
| OPS-002 | High | services/api/internal/server/shift_exceptions_handlers.go:48-128; db/migrations/0004_rbac.up.sql:164-166 | Approver != cash-submitter and approver-not-attendant checks still missing | eef7123 blocks closer self-approval (403 if ClosedBy==actor); but no check against CashSubmission.SubmittedBy nor shift_attendants membership — an attendant/cash-submitter can still approve. Fix commit eef7123 |
| OPS-008 | High | services/api/internal/server/shift_close_handlers.go:93-226 | Close-line snapshot still built from reads before Begin; no in-tx re-read | 78f3e94 added GetShiftForUpdate (FOR UPDATE) closing double-close race; but litres/expected_value built from pre-Begin reads (93-110, 147-177); meter/dip-correction window acknowledged as follow-up. Fix commit 78f3e94 |
| INV-001 | Critical | internal/inventory/repo.go:80-81,100; reconciliation/repo.go:37-51; reconciliation_handlers.go:45-47,174-177 | Inventory ledger side still float64 (recon side fixed) | cd6b74c made recon figures decimal strings with SQL numeric compute; but inventory Movement.Litres/BalanceAfter and PostInput.Litres still float64 (repo.go:79-80,100), CurrentBalance float64, stockMovementDTO float64 JSON. Fix commit cd6b74c |
| PROC-18 | High | services/api/internal/server/procurement_handlers.go:1036-1051; internal/procurement/overview.go:59 | Approval still creates no payable directly; SupplierBalances still sums lifetime approved spend | a4fc0ec added payables ledger (0040) with drawdown; but approval handler still fire-and-forget PayableCreated with no consumer; payables created by separate batch (payables/repo.go:60); overview.go SUMs total_amount WHERE status='approved', never netting payments. Fix commit a4fc0ec |
| PAY-002 | High | services/api/internal/server/payments_handlers.go:122-134; internal/receivables/repo.go:218-240 | Only on_hold/suspended/hold blocked; prospect/inactive/closed still allowed | c956289 added in-tx hold + on_hold/suspended block (ErrCreditHold→422); but 0050 broadened status enum and SQL blocks only on_hold/suspended/hold — prospect/inactive/closed can still charge. Fix commit c956289 |
| FLEET-006 | High | internal/fleet/authorizations.go:129,184; (no expiry job) | Sale path never auto-fulfills holds; no background job; PostCharge exposure lacks expiry filter | 4865a11 added lazy expiry on new request + CreditPosition expiry_at filter (bounded ~1h); but no FulfillAuthorization call on sale, no cron/ticker, PostCharge exposure SUM (repo.go:225-226) lacks expiry_at>now(). Fix commit 4865a11 |
| ENT-04 | High | internal/enterprise/governance.go:270-307 | required_role still not enforced on Decide | 45a1530 added self-approval guard (ErrSelfApproval if RequestedBy==deciderID); but Decide never reads/verifies required_role; CreatePolicy only writes it. Fix commit 45a1530 |
| RISK-002 | Critical | internal/risk/governance.go:77-83; alerts_detect.go:38-77 | Packs with no configured rule row fall through to built-in defaults and still run | f194d64 made RunDetection skip paused/retired rule packs (kill switch works for configured rules); but a pack with no rule row runs built-in defaults, so PauseAllRules can't silence those. Audited claim resolved. Fix commit f194d64 |
| INFRA-01 | Critical | migrations/0005_rls.up.sql:12; database/tenant.go:29; cmd/api/main.go:146 | RLS opt-in/OFF by default; FORCE ROW LEVEL SECURITY never added | ab3ce3d wired RLS into request path with CI isolation test; but rlsEnabled only when DATABASE_APP_URL set, default main.go:190-191 uses owner pool ('RLS bypassed'), CI sets no DATABASE_APP_URL; 0005:12 still 'ENABLED but NOT FORCED'. Fix commit ab3ce3d |
| WEB-007 | High | apps/web + packages/sdk | apps/web still has ZERO tests | f7d9c26 added packages/sdk/src/client.test.ts (146 lines); but find under apps/web returns nothing — safeRedirect, auth store, ProtectedRoute untested. Fix commit f7d9c26 |
| TEST-02 | Critical | root package.json:18; apps/web/package.json:6-12; packages/sdk/package.json:11-13 | web + UI portions of the test gate remain no-ops | f7d9c26 added SDK "test":"vitest run" so root `pnpm -r --if-present test` runs it; but apps/web, packages/ui, packages/config define no test script. Fix commit f7d9c26 |
| TEST-03 | Critical | packages/sdk/ (whole), apps/web/ (whole) | No web component/RTL/e2e coverage of the Next.js app | f7d9c26 added 11 SDK tests incl. fetch.bind 'Illegal invocation' guard; but apps/web glob returns no tests. Fix commit f7d9c26 |

## Already fixed since the register

- ARCH-02 (High) — Non-dev boot with empty AUTH_PASSWORD_PEPPER now fail-stops via wireDeps error (main.go:212-214) — fix commit 54b90fb
- ARCH-03 (High) — Request-scoped RLS enforcement path added (AcquireTenant + pool wrapper + per-request GUC); docs corrected to RLS opt-in — fix commit (see ab3ce3d cluster)
- AUTH-13 (High) — TOTP secret now AES-256-GCM encrypted at rest (HKDF from pepper), versioned, legacy upgrade — fix commit 79cab1e
- AUTH-16 (High) — Raw password-reset token logged only when Env==development; production logs tenant only — fix commit 54b90fb
- PROC-12 (Critical) — landed_cost_per_litre divisor guarded with NULLIF($6,0) — fix commit 54b90fb
- PROC-07 (High) — Over-receipt capped: rejects ErrOverReceipt beyond ordered*tolerance — fix commit 7ebc416
- PROC-13 (High) — Distinct receivingTolerancePct introduced; loss-tolerance no longer leaks into receiving/auto-complete — fix commit 7ebc416
- INV-014 (High) — Persist/seal now recompute inside the tx (TOCTOU closed) — fix commit ef06f13
- INV-016 (High) — Write-off + sealed figures from single in-tx numeric compute; float epsilon gone — fix commits cd6b74c, ef06f13
- REV-01 (Critical) — Reversed/superseded movements excluded from all four delivery-cost aggregations (status='posted' AND supersedes_id IS NULL) — fix commit 54b90fb
- PAY-001 (High) — PostCharge locks customer row FOR UPDATE; exposure includes approved authorizations — fix commit c956289
- PAY-003 (High) — Credit tender now posts balanced GL (DR AR / CR sales_clearing), idempotent — fix commit faef8f0
- PAY-013 (High) — PostShiftRevenueJournal posts DR sales_clearing / CR revenue / CR output_vat, wired to outbox consumer — fix commit 67ad65c
- ACCT-001 (High) — Deferred constraint trigger re-checks balance at COMMIT + immutability triggers on entries/lines — fix commits 431ac46, 1398593
- ACCT-009 (High) — ApproveExpense locks row, rejects ErrSelfApproval when createdBy==approverID — fix commit 058d988
- ACCT-012 (High) — Petty-cash adjustment/transfer now post offsetting GL entries — fix commit d4a3f18
- ACCT-016 (High) — Balance sheet folds net income into equity; SQL-computed balanced flag — fix commit dbd3d66
- FLEET-002 (High) — PostCharge enforces credit hold + full exposure on the real sale path — fix commit c956289
- FLEET-003 (High) — All fuel-limit periods (transaction/day/week/month) enforced, strictest reported — fix commit 4865a11
- FLEET-004 (High) — RequestAuthorization SELECT...FOR UPDATE on customer, serializes with sales — fix commit 4865a11
- FLEET-007 (High) — Driver PINs argon2id (per-record salt); credential tokens pepper-keyed HMAC-SHA256 — fix commit d7a8701
- RISK-001 (Critical) — Detection now driven from risk_rules config (severity/threshold/lookback applied) — fix commit f194d64
- INFRA-02 (High) — Real RevenueRecognized consumer posts GL revenue journal in its own tx — fix commit (RevenueRecognized wiring)
- INFRA-03 (High) — InProcessBus.Publish returns errors.Join; failed handlers leave outbox rows unpublished for retry — fix commit 54b90fb
- INFRA-06 (High) — limitRequestBody middleware (4 MiB MaxBytesReader) in global stack — fix commit 453eb95
- INFRA-12 (High) — statement_timeout + idle_in_transaction_session_timeout set on every pooled conn — fix commit 453eb95
- INFRA-22 (High) — Seed refuses default passwords outside development (NODE_ENV guard) — fix commit 54b90fb
- SDK-02 (High) — packages/sdk/src/client.test.ts added (mock fetch, fetch-binding regression) — fix commit f7d9c26
- SDK-26 (High) — Dead `./styles` export removed from packages/ui/package.json — fix commit 296a912
- TEST-01 (Critical) — DB-backed integration suite (Postgres+Redis) now runs in CI migrations job — fix commit a88d982
- TEST-04 (High) — RLS now tested from Go through production AcquireTenant path incl. fail-closed — fix commit 977a19f
- TEST-05 (High) — Balance-sheet A = L + E asserted via sumDecimalEq after multi-entry scenario — fix commit (phase7 update)
- TEST-06 (High) — Two-actor self-approval-rejection tests across approvals/recon/expense/invoice/shift — fix commit (multi-phase tests)

## Unverifiable

None.
