# FuelGrid OS — Pre-Production Security Review

**Date:** 2026-06-04
**Reviewer:** Lead security reviewer (consolidating 7 independent dimension reviews)
**Scope:** Full pre-production review across authentication/access control, multi-tenancy & IDOR, injection/input/SSRF/upload/redirect, secrets/crypto/config/CORS/headers, web frontend, financial integrity/audit/idempotency, and deploy/container/CI supply-chain.
**Method:** Every Critical/High candidate was independently re-verified against live code by the lead reviewer (cited files read directly). Medium/Low/Info findings were verified more lightly and deduplicated across dimensions.

---

## 1. Executive posture

**Verdict: GO.** (Originally GO-with-conditions; both Medium conditions are now **RESOLVED** — see SR-M1 and SR-M2 below.)

There are **no Critical and no High-severity findings**. The codebase demonstrates mature, defense-in-depth security: Postgres RLS enforced per request via a non-owner role, append-only ledgers with DB triggers, an httpOnly BFF token-stripping proxy, argon2id+pepper password hashing, AES-256-GCM encryption of TOTP secrets at rest, epoch-based global session revocation, separation-of-duties on every approval flow, and production fail-stops that refuse to boot with wildcard CORS, plaintext origins, or a missing RLS-enforcement database role.

The original "with conditions" qualifier was driven by **two confirmed Medium findings** that were real and exploitable via direct API calls (though not data-exfiltration or tenant-isolation breaks). **Both are now resolved:**

1. **MFA requirement is not enforced server-side** — **RESOLVED** (branch `fix/sec-m1-mfa-enforcement`): the API now blocks privileged-but-unenrolled sessions on the sensitive admin-console surface with `403 mfa_required` via the `requireMFASatisfied` middleware (gated by `AUTH_ENFORCE_MFA_FOR_PRIVILEGED_ROLES`, default true). See SR-M1.
2. **Payment recording has no idempotency guard** — **RESOLVED** (branch `fix/sec-m2-payment-idempotency`): a client `Idempotency-Key` + partial unique index on `payments` (migration 0096) makes the record path idempotent — a retry/replay returns the existing record instead of double-recording the shift tender. See SR-M2.

Neither was deploy-blocking for a controlled launch, and both are now closed. No genuinely Critical, deploy-blocking issue was confirmed.

### Confirmed counts by severity

| Severity | Count |
|----------|-------|
| Critical | 0 |
| High | 0 |
| Medium | 2 |
| Low | 4 |
| Info | 3 |

---

## 2. Confirmed findings

### Medium

| ID | Finding | Evidence (file:line) | Impact / exploitability | Remediation | Owner / effort |
|----|---------|----------------------|--------------------------|-------------|----------------|
| SR-M1 | **RESOLVED** (branch `fix/sec-m1-mfa-enforcement`): added the `requireMFASatisfied` middleware (`services/api/internal/server/policy_middleware.go`) on the admin-console route group plus the audit/role-grant routes — a `RoleRequiresMfa` actor without `MfaSatisfied` now gets `403 {"code":"mfa_required"}`; enrollment/`/me`/`/auth`/logout stay reachable (no lockout). Gated by `AUTH_ENFORCE_MFA_FOR_PRIVILEGED_ROLES` (default true). `finance_officer` added to the MFA-required role set; misleading service.go comment corrected. Original finding below. — **MFA requirement not enforced server-side for privileged roles.** `RoleRequiresMfa` and `Actor.MfaSatisfied` are report-only: `MfaState` (service.go:850) is consumed solely by the `/me` handler, and login enforces a second factor only when `user.MfaEnabled` is already true. A privileged user (system_admin/tenant_admin/finance) who has never enrolled MFA logs in with `MfaSatisfied=false` and is not blocked from any route. The comment at service.go:1000-1002 ("the HTTP layer uses this to refuse a privileged session") overstates reality — no middleware checks it. | `internal/identity/service.go:224-280` (login enforces only when `user.MfaEnabled`); `internal/identity/service.go:847-869` (`MfaState`/`RoleRequiresMfa` are status-report only); `internal/identity/service.go:1000-1010`; `services/api/internal/server/auth_handlers.go:217,223-225` (reported in `/me`, never gated) | A direct API client (bypassing the web UI's client-side enrollment prompt) authenticated as a privileged-but-unenrolled user can call sensitive endpoints (role assignment, approvals, user management). The UI forces enrollment; raw API calls do not. Confirmed real and exploitable. | Add a `requireMFASatisfied` middleware on sensitive route groups (user/role management, approvals, governance) that loads the actor's roles, and if `RoleRequiresMfa(roles)` is true, rejects with 403 unless `actor.MfaSatisfied`. Correct the misleading comment at service.go:1000-1002 to match. | Identity/Auth — M (1–2 days incl. tests) |
| SR-M2 | **RESOLVED** (branch `fix/sec-m2-payment-idempotency`) — added an optional client `Idempotency-Key` (header or `idempotency_key` body field) persisted on `payments` with a partial unique index `uq_payments_tenant_idempotency_key` on `(tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL` (migration 0096); `payments.Record` uses `INSERT ... ON CONFLICT DO NOTHING` and returns the already-recorded row (HTTP 200, side effects skipped) on a replay instead of double-inserting. Tenant-scoped so the same key under a different tenant does not collide; no key supplied preserves prior behaviour. — **No idempotency guard on payment recording.** `handleRecordPayment` calls `payments.Record()` with a fresh UUID and no idempotency key; the `payments` schema has only `PRIMARY KEY (id)` and `UNIQUE (tenant_id, id)` — no constraint on a client-supplied request/idempotency key. A retried request or replayed call records a duplicate tender for the same shift. (Note: the M-Pesa callback path and the credit-charge GL journal *are* idempotent — `SettleMpesaByCheckoutID` and `EntryExistsForSource` — but the base `payments` insert is not.) | `services/api/internal/server/payments_handlers.go:107-111` (`payments.Record` with no idempotency key); `services/api/migrations/0035_payments.up.sql:8-46` (PK + `uq_payments_tenant_id` only) | A client/network retry or a replayed request double-counts cash/mobile-money/card tenders against a shift, inflating tendered revenue and creating reconciliation variances. Soft controls (auth, station scope) exist; no hard dedup. | Add an idempotency key: accept a client `Idempotency-Key` (or reuse the request id already written to `audit_logs`) and add a partial unique constraint on `(tenant_id, shift_id, idempotency_key)`; return the existing record on conflict. Alternatively document that downstream reconciliation is the dedup authority. | Payments/Finance — M (1–2 days incl. migration + tests) |

### Low

| ID | Finding | Evidence (file:line) | Impact | Remediation | Owner / effort |
|----|---------|----------------------|--------|-------------|----------------|
| SR-L1 | **No shift-closure check before recording a payment.** `handleRecordPayment` loads the shift and authorizes the station but never checks shift status before `payments.Record()`. If shift closure is meant to be terminal, payments can still post to a closed shift. | `services/api/internal/server/payments_handlers.go:87-98` | A tender can be recorded against an already-closed shift. Whether this is a gap or intentional (post-close corrections) is a product decision. | Confirm intent; if closure is terminal, reject with 409 when `shift.Status == "closed"`. Otherwise document that post-close recording is allowed for corrections. | Payments — S |
| SR-L2 | **Logo download missing `X-Content-Type-Options: nosniff`.** The download re-serves the stored content-type without `nosniff`, unlike the attachments handler which sets both `nosniff` and a content disposition. Upload strictly validates PNG/JPEG via `http.DetectContentType`, so escalation to script execution is not plausible. | `services/api/internal/server/branding_handlers.go:287-290` | Defense-in-depth inconsistency; very low exploitability (content-type validated at upload, image types only). | Add `X-Content-Type-Options: nosniff` (and an `inline` content disposition) to the logo download for parity with `attachments_handlers.go`. | Platform/Assets — S |
| SR-L3 | **`/auth/password-reset/confirm` has no HTTP-layer rate limit.** The route is registered outside any rate-limit group; the per-tenant limiter applies only to authenticated routes. (`/login` itself is protected by the identity service's dual IP+account buckets.) | `services/api/internal/server/server_routes.go:154-155`; `services/api/internal/server/ratelimit_middleware.go` | Low: each confirm requires a unique one-time token hashed in Redis (1h TTL, cleared on use), so there is no high-yield brute-force surface. Risk is limited to an attacker who already intercepted a reset token. | Apply a per-IP/per-token limit (e.g. 3 attempts / 15 min) on confirm, or rate-limit at the reverse proxy. | Identity/Auth — S |
| SR-L4 | **Enterprise scope grants lack `scope_id` tenant-ownership validation.** `GrantScope` does not verify that a non-`tenant` `scope_id` belongs to the granting tenant. | `services/api/internal/server/enterprise_governance_handlers.go:119-140`; `internal/enterprise/governance.go:97-104`; `services/api/migrations/0057_enterprise_governance.up.sql:45-61` | Low: enterprise scopes drive UI filtering/navigation only — authorization is enforced by the separate `policy.LoadFor()` + `authorizeStation()` path, so a cross-tenant scope grant grants no actual resource access. Defensive hardening only. | Add an application-level (or CHECK/FK) validation that `scope_id` resolves to a row owned by the tenant for `station`/`company` scope types. | Enterprise — S |

### Info

| ID | Finding | Evidence | Note |
|----|---------|----------|------|
| SR-I1 | `AUTH_PASSWORD_PEPPER` rotation is not supported and would force global MFA re-enrollment (HKDF key for TOTP-at-rest is derived from the pepper). | `internal/identity/secretcrypto/secretcrypto.go:77-96` | Acceptable for a stable production secret. Document the constraint; if rotation is needed later, add a `key_id` to the `v1:`/`v2:` versioning scheme and a key-rotation table. Already partly noted in `docs/deployment.md` rotation section. |
| SR-I2 | Sentry has no active breadcrumb scrubber. No PII reaches Sentry today (only request_id/method/status/route tags), but future breadcrumbs added in auth paths could leak. | `internal/observability/sentry.go:36` | Proactively install a `BeforeSend`/breadcrumb filter that redacts email/phone/token patterns before any auth-path breadcrumbs are introduced. |
| SR-I3 | MFA code brute-force is bounded only by per-account login lockout (no tighter per-code window); `golang.org/x/crypto v0.51.0` pinning is current and Trivy-gated. | `internal/identity/service.go:259-279`; `go.mod:25` | Lockout is adequate given 90s TOTP windows + single-use replay guard. Weekly + per-PR Trivy scans gate fixable CRITICAL/HIGH. No action required. |

### Re-verified false positives / dropped

- **PostCSS `<8.5.10`** — false positive: lockfile has `postcss@8.5.15` (patched). Dropped.
- No Critical/High candidates were present in any dimension to re-verify upward; the highest claimed severity across all 7 reviews was Medium, and both Mediums were confirmed against live code (Section 2).

---

## 3. Verified-sound controls

The following controls were verified directly and are sound. They are the backbone of the GO verdict.

**Tenant isolation & access control**
- **Postgres RLS on the request path** — auth middleware acquires a tenant-scoped, non-owner (`fuelgrid_app`) connection and `SET app.current_tenant` per authenticated request; every tenant-owned table has `ENABLE ROW LEVEL SECURITY` + a `tenant_isolation` policy (migration 0074). Validated by `rls_blast_integration_test.go` across 13 tables. (`services/api/internal/database/postgres.go:24-65`; `services/api/internal/server/auth_middleware.go:50-64`)
- **Production fail-stop** — outside development the API refuses to boot unless `DATABASE_APP_URL` is set and distinct from `DATABASE_URL` (forces the RLS-enforcing role), and rejects wildcard or non-`https://` CORS origins. (`services/api/internal/config/config.go:269-296`)
- **Station-scoped authorization & IDOR defense** — single-resource and list endpoints (`/supplier-invoices/{id}`, `/purchase-orders/{id}`, sale-void, attachments, etc.) load the resource and call `authorizeStation()` against its `station_id`; list filters via `stationReadFilter()`. Fail-open-to-empty guards prevent empty scope collapsing to "all stations." (`services/api/internal/server/policy_middleware.go:70-98`; `internal/identity/policy/policy.go:68-92`)

**Authentication & sessions**
- **Session tokens** — 32 random bytes; only `sha256(token)` persisted in Postgres; raw token lives in Redis + httpOnly cookie. Epoch-based global revocation (`session_epoch` bump in one tx with audit+outbox) authoritatively invalidates sessions even if a Redis entry lingers. Password change/reset revoke all sessions. (`internal/identity/session/session.go:50-68`; `internal/identity/service.go:423-445,507-547`)
- **Password & PIN hashing** — argon2id (OWASP 2024 params m=64MiB/t=3/p=4) with HMAC-SHA256 pepper, constant-time compare, auto-rehash. Driver PINs use the identical scheme. (`internal/identity/password/hasher.go:36-121`; `internal/fleet/drivers.go:50-77,159-196`)
- **TOTP** — AES-256-GCM at rest (HKDF-SHA256 key from pepper), single-use replay guard within the 90s window via Redis `SetNX`, single-use hashed backup codes. (`internal/identity/secretcrypto/secretcrypto.go:36-96`; `internal/identity/totp/guard.go:74-91`)
- **Login rate limiting** — dual-bucket (per-IP + per-account); lockout after configurable failures; bad MFA codes count toward lockout; buckets reset on success. (`internal/identity/service.go:168-179,259-279`)
- **Uniform error messages / no enumeration** — login, user-not-found, bad password, MFA failures map to generic errors; password-reset request always returns 202. (`services/api/internal/server/auth_handlers.go:104-140`)

**Web frontend (BFF)**
- **httpOnly token-stripping BFF proxy** — the session token never reaches client JS; it lives only in an httpOnly/Secure/SameSite=Lax `fg_session` cookie. The same-origin Next.js BFF reads it server-side and adds `Authorization: Bearer` upstream, stripping hop-by-hop headers and stripping the token from the login JSON response. (`apps/web/src/app/api/bff/[...path]/route.ts`; `apps/web/src/lib/server/session-cookie.ts`)
- **CSP / headers** — per-request nonce CSP with `strict-dynamic` (dev-gated unsafe-eval), `X-Frame-Options: DENY`, `nosniff`, `Referrer-Policy`, strict `Permissions-Policy`, `frame-ancestors 'none'`, `form-action 'self'`. No `dangerouslySetInnerHTML`/`eval`/`innerHTML` in source. (`apps/web/src/middleware.ts`; `apps/web/next.config.ts:14-22`)
- **Open-redirect guard** — `safeRedirect()` rejects protocol-relative, backslash-tricked, absolute, and `javascript:` `?next=` payloads; full test coverage. (`apps/web/src/lib/safe-redirect.ts:15-23`)

**Financial integrity & audit**
- **Append-only ledgers** — journal-immutability and outbox-guard triggers enforce posted→reversed-only transitions and payload immutability. (`services/api/migrations/0065_journal_immutability_trigger.up.sql`; `0075_outbox_events_guard.up.sql`)
- **Reversals not deletes; idempotent recognition** — sales/journal use contra entries; `RecognizeShiftSales` uses `ON CONFLICT (shift_id, nozzle_id) DO NOTHING`; M-Pesa settlement is idempotent via `WHERE status='pending'` + unique `checkout_request_id`; opening stock is single-genesis via a partial unique index; balance posting serializes via `pg_advisory_xact_lock`. (`internal/revenue/repo.go:101-154`; `internal/payments/mpesa_repo.go:102-140`; `internal/inventory/repo.go:143-181,371-412`)
- **Decimal discipline** — money never passes through Go `float64` on write paths; all amounts bind as `$N::numeric` decimal strings.
- **Separation of duties** — void, adjustment, opening-stock, period, and retention approvals enforce `requestedBy != approverID` under `FOR UPDATE` locks, all audited.
- **Audit immutability** — audit rows written in the same tx as the change; no UPDATE/DELETE path. (`internal/audit/audit.go:42-78`)

**Injection / SSRF / upload**
- Parameterized SQL throughout (pgx `$N`); attachment uploads multi-layer validated (bounded body, content-type sniff, allowlist, 5 MiB cap) and served with `nosniff` + attachment disposition; BFF and OTLP endpoints are not user-controlled (no SSRF); M-Pesa callback is keyed by globally-unique `checkout_request_id` and always 200-acks.

**Secrets / config / deploy**
- `config.Secret` type redacts secrets across all fmt/slog paths (tested); env-gated integrations (Sentry/SMTP/M-Pesa/OTLP) are safe no-ops when unconfigured; HTTP timeouts + 4 MiB body cap; error responses are generic.
- **Container/CI** — distroless `nonroot` API image and slim non-root web image; no secrets in layers; `.dockerignore` excludes `.git`/`.env`; seed/migrate guarded by `ALLOW_SEED`/`NODE_ENV`/`MIGRATE_CONFIRM`; immutable `:sha-<full-sha>` image pinning; weekly + per-PR Trivy with fixable CRITICAL/HIGH gating + CycloneDX SBOM; least-privilege CI permissions.

---

## 4. Pre-launch security checklist

These are the must-do items before public production exposure. Deploy-blocking status is noted; cross-references are to [docs/deployment.md](../deployment.md).

**Should-fix before public launch (not strictly deploy-blocking, but close the two confirmed gaps):**

- [x] **SR-M1 — Enforce MFA server-side for privileged roles. RESOLVED** (branch `fix/sec-m1-mfa-enforcement`). Added `requireMFASatisfied` on the admin-console route group (user/role management, approvals, finance, governance, …) and on the audit/role-grant routes in the station-read group; a privileged-but-unenrolled session is rejected with `403 {"code":"mfa_required"}`. Enrollment/`/me`/`/auth`/logout remain reachable so there is no enrollment lockout. Behaviour is gated by `AUTH_ENFORCE_MFA_FOR_PRIVILEGED_ROLES` (default `true`; set it for production). `finance_officer` was added to the MFA-required role set so the finance tier is actually covered, and the misleading comment at `internal/identity/service.go` was corrected. Integration tests in `services/api/internal/server/mfa_enforcement_integration_test.go` prove block / no-lockout / pass-when-satisfied / non-privileged-unaffected.
- [x] **SR-M2 — Add payment idempotency. RESOLVED** in `fix/sec-m2-payment-idempotency`: client `Idempotency-Key` persisted on `payments`, partial unique index `(tenant_id, idempotency_key) WHERE idempotency_key IS NOT NULL` (migration 0096), `payments.Record` returns the existing record (HTTP 200, side effects skipped) on conflict. Tenant-scoped; no-key path unchanged.

**Operational pre-launch confirmations (configuration, per docs/deployment.md):**

- [ ] Confirm `DATABASE_APP_URL` is set and distinct from `DATABASE_URL` in production (RLS-enforcing non-owner role) — the API will refuse to boot otherwise (`config.go:269-296`). See deployment.md "Required secrets / configuration."
- [ ] Confirm `API_CORS_ALLOWED_ORIGINS` lists only explicit `https://` origins (no `*`, no `http://`).
- [ ] Confirm all secrets are injected via `fly secrets` (never committed): `AUTH_PASSWORD_PEPPER`, `DATABASE_URL`, `MPESA_*`, `FLY_API_TOKEN`. Treat `AUTH_PASSWORD_PEPPER` as permanent (SR-I1) — rotation forces a password-reset wave and MFA re-enrollment; see deployment.md secrets/rotation section.
- [ ] Confirm seed data is disabled in production (`ALLOW_SEED` unset, `NODE_ENV` != development) and migrations run only via the CD `migrate` job (`MIGRATE_CONFIRM` discipline).
- [ ] Confirm the `/readyz` smoke gate is wired (`DEPLOY_HEALTH_URL`) so a rollout fails unless Postgres + Redis health pass.
- [ ] Confirm Trivy scans pass with no fixable CRITICAL/HIGH at the deployed image SHA.

**Low/Info hardening (post-launch backlog):**

- [ ] SR-L1 — Decide and enforce/document post-close payment behavior.
- [ ] SR-L2 — Add `nosniff` to logo download.
- [ ] SR-L3 — Rate-limit `/auth/password-reset/confirm` (per-IP/token).
- [ ] SR-L4 — Validate enterprise `scope_id` tenant ownership.
- [ ] SR-I2 — Install a Sentry breadcrumb/PII scrubber before adding auth-path breadcrumbs.

---

*Independently re-verified findings only. No Critical or High-severity issue was confirmed. The two confirmed Medium findings are policy/idempotency gaps exploitable via direct API calls, not tenant-isolation or data-exfiltration breaks.*
