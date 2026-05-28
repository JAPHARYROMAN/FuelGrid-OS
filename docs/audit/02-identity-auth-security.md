# FuelGrid OS — Identity & Auth Core Security Audit

**Audit type:** Read-only, atomic-level security & correctness review of the identity/authn/authz subsystem.
**Date:** 2026-05-28
**Module:** `github.com/japharyroman/fuelgrid-os`
**Stack:** Go (chi router, pgx/v5, Postgres 16, Redis 7), Next.js frontend (out of scope here).

## Scope (files + LOC)

| File | LOC | Concern |
|------|-----|---------|
| `internal/identity/service.go` | 593 | Login, logout, refresh, password reset, MFA, session revoke |
| `internal/identity/errors.go` | 23 | Sentinel error vocabulary |
| `internal/identity/actor.go` | 61 | Request principal, context plumbing |
| `internal/identity/repo/user.go` | 339 | User/role/station-access data access |
| `internal/identity/repo/session.go` | 144 | Durable session rows |
| `internal/identity/session/session.go` | 61 | Token generation & hashing |
| `internal/identity/session/redis_store.go` | 141 | Hot-path session store (Redis) |
| `internal/identity/ratelimit/redis.go` | 58 | Fixed-window limiter |
| `internal/identity/password/hasher.go` | 178 | argon2id hashing + pepper |
| `internal/identity/totp/totp.go` | 55 | RFC 6238 TOTP |
| `internal/identity/policy/policy.go` | 131 | PermissionSet evaluator |
| `internal/identity/policy/db_loader.go` | 83 | Permission/scope loader |
| `services/api/internal/server/auth_handlers.go` | 254 | Login/logout/refresh/reset/MFA HTTP |
| `services/api/internal/server/auth_middleware.go` | 74 | Bearer extraction, actor injection |
| `services/api/internal/server/policy_middleware.go` | 199 | requirePermission / Held / authorizeStation |
| `services/api/internal/server/platform_handlers.go` | 212 | Platform admin token, tenant provisioning |
| `services/api/internal/server/admin_handlers.go` | 168 | Grant role |
| `services/api/internal/server/users_handlers.go` | 393 | User admin CRUD, station access |
| `services/api/internal/server/roles_handler.go` | 65 | Role catalogue |
| `services/api/internal/server/me_handlers.go` | 114 | Self-service sessions / password |
| `services/api/internal/server/me_permissions_handler.go` | 60 | UI permission summary |
| Supporting: `server.go` (route table), `config.go`, `middleware.go`, migrations 0002–0008 | — | Wiring, schema |

**Total audited core: ~3,406 LOC** plus migrations and wiring.

Overall posture is **good** — the design is genuinely security-conscious (argon2id with pepper, sha256-at-rest tokens, generic login errors, dual-write session revocation, tenant-scoped repos, composite-FK tenant integrity). The findings below are the deviations and weak spots, ordered by concern.

---

## 1. Password hashing — `password/hasher.go`

The hashing implementation is the strongest part of the subsystem. argon2id with `m=64MiB, t=3, p=4, salt=16, key=32` (`DefaultParams`, lines 36–42) matches/exceeds OWASP 2024 interactive guidance. Salt is `crypto/rand` (line 65–67). Verification uses `subtle.ConstantTimeCompare` (line 111) — constant-time. PHC encoding (lines 80–88) enables `needsRehash` upgrade-on-login (lines 115–118), which is correctly wired in `Login` (service.go:213–225).

**Pepper handling is the one real concern (AUTH-01).** `peppered()` (lines 138–145) computes `HMAC-SHA256(password, pepper)` and feeds the 32-byte MAC to argon2. This is defensible, but it **collapses the entire input space to 32 bytes and pre-hashes with SHA-256**, which means:
- Passwords longer than the HMAC block are not a problem, but the construction means the argon2 input is always exactly 32 bytes regardless of password — fine.
- The real risk: **the pepper is non-rotatable.** Because the pepper is mixed *inside* the stored hash (not stored alongside it), rotating `AUTH_PASSWORD_PEPPER` invalidates **every** existing password — all users are locked out and forced through reset. There is no pepper-version tag in the PHC string. If the pepper ever leaks, you cannot rotate without a mass reset. This should be documented and ideally a `pepper_version` should be carried.
- When pepper is empty (dev/tests), the raw password bytes go to argon2 (line 139–141). That is correct, but means a hash produced in dev (no pepper) silently *verifies false* in prod (with pepper) and vice-versa — an operational footgun worth a startup assertion in non-dev.

**AUTH-02 (Medium): production can boot with no pepper.** `main.go:181–183` only logs a `Warn` when `AUTH_PASSWORD_PEPPER == "" && Env != "development"`. The service starts anyway. A misconfigured prod deploy silently runs peppized=off, defeating the documented "DB-only compromise" defense. This should be a hard fail (refuse to start) outside development.

**AUTH-03 (Low): no maximum password length.** `Hash` rejects empty (line 61) but accepts arbitrarily long input. With the HMAC pre-hash this is not a DoS (input is normalized to 32 bytes), so impact is minimal — but `ChangePassword`/`ConfirmPasswordReset` only enforce `len(newPassword) < 12` (service.go:408, 476) with no upper bound and **no complexity/breach-list check**. A 12-char minimum with no other rules is weak for an admin-capable system.

---

## 2. Session management — `session/`, `repo/session.go`, `service.go`

Token entropy is correct: 32 bytes from `crypto/rand`, base64url-encoded (`session.go:43–51`). The raw token lives only client-side and in the Redis key; the durable `sessions` row stores `sha256(raw)` (migration 0003, `token_hash bytea NOT NULL UNIQUE`). A Postgres-only compromise does not yield live sessions — as advertised. Dual-write: Redis is the hot path (`Resolve` → single GET, service.go:389–402), Postgres is the audit/revocation backbone.

**AUTH-04 (High): session expiry is not authoritative on the hot path.** `Resolve` (service.go:389–402) checks `s.now().After(sess.ExpiresAt)` against the **Redis-stored copy** of `ExpiresAt`. Because Redis TTL is derived from `ExpiresAt` at `Put` time, this is usually consistent — but `TouchExpiry`/`Revoke` on the Postgres side and the Redis side can drift. More importantly, **`Resolve` never consults Postgres `revoked_at`.** If the Redis `DeleteByID` in `revokeAllUserSessions` (service.go:355–361) fails (it is explicitly best-effort, logged and swallowed at line 359), the durable row is marked revoked but the Redis entry survives until its TTL — and `Resolve` will happily authenticate it the entire time. **A failed Redis delete during a password-reset "revoke all" leaves a leaked-credential attacker logged in for up to the full session TTL (default 12h).** The hot path has no revocation backstop. Recommended: store a per-user "sessions invalidated at" epoch (or check `revoked_at` opportunistically), or treat Redis delete failure in the reset path as fatal.

**AUTH-05 (Medium): no session rotation / fixation hardening on privilege change.** On login a fresh token is minted (good), but `ChangePassword` (service.go:404–438) does **not** revoke other sessions, and **does not rotate the current session**. After a self-service password change, all previously-issued sessions remain valid. Contrast with `ConfirmPasswordReset`, which correctly calls `revokeAllUserSessions`. The asymmetry means a user who changes their password (e.g. because they suspect compromise) does *not* kick out the attacker. `VerifyMfa` (enabling MFA) likewise does not re-issue the session with `MfaSatisfied=true` — the actor's `MfaSatisfied` flag is fixed at login time (set from `user.MfaEnabled`, service.go:265), so enabling MFA mid-session has no effect on the live session's claim.

**AUTH-06 (Medium): `Refresh` performs unbounded sliding expiry with no absolute cap.** `Refresh` (service.go:366–384) sets `ExpiresAt = now + SessionTTL` every call, in both Redis and Postgres. There is no absolute session lifetime (`AUTH_REFRESH_TTL` is defined in config at config.go:42 but **never used** — dead config, see AUTH-20). A client that refreshes before each expiry keeps a session alive indefinitely. The package comment in `redis_store.go:96–97` even warns against this for `Touch`, yet `Refresh` does exactly that via `Put`. Refresh also has **no rate limiting** and **no revocation check** — a stolen-then-revoked-in-Postgres token can still be refreshed as long as it remains in Redis (same root cause as AUTH-04).

**AUTH-07 (Low): device binding is nominal.** `DeviceID` is threaded through `LoginRequest`/`Session`/the `sessions.device_id` column, but the HTTP `handleLogin` never populates it (auth_handlers.go:38–45 omits `DeviceID`), and `Resolve` never validates the requesting client against the bound device or IP. There is no device-binding enforcement; the `devices` table and `device_id` FK are currently decorative for auth purposes.

**AUTH-08 (Low): `Put`/`Touch` use `TxPipeline` but the two keys can still diverge under partial Redis failure.** `RedisStore.Put` (redis_store.go:64–78) writes the token key and the id-reverse-index in a `TxPipeline` (MULTI/EXEC) — atomic, good. But `Delete`/`DeleteByID` first do a `Get` then a `Del` (non-atomic, lines 116–141); a crash between them, or a write that races the read, can orphan one of the two keys. Orphaned `session:id:<uuid>` is harmless; an orphaned `session:<token>` is the AUTH-04 leak vector.

---

## 3. Login flow — `service.go` `Login`, rate limiting, lockout

`Login` (service.go:145–275) has good bones: uniform `ErrInvalidCredentials` for unknown-user vs bad-password (lines 157, 173, 194), failed attempts ride a tx with audit+outbox (lines 183–193), success clears counters and rate bucket in one tx (lines 237–253).

**AUTH-09 (High): rate-limit and lockout keys are trivially bypassable / abusable.**
- The rate-limit bucket is `"login:" + strings.ToLower(req.IP)` (service.go:146), where `req.IP = clientIP(r)` and `clientIP` (auth_handlers.go:246–254) **always uses `r.RemoteAddr`** and ignores `X-Forwarded-For`. That is *correct* and not spoofable directly — **but only if the API is never behind a proxy/load balancer.** In any real deployment (ALB, nginx, Cloudflare) `r.RemoteAddr` is the proxy's IP, so **the entire internet shares one rate-limit bucket** → either trivial DoS lockout of all logins from that egress, or (with default `LoginRateMax=5 / 15m`) effectively no per-attacker limiting because everyone is one bucket. The code comment claims forwarded-header trust is "wired in a later stage"; until then the limiter is misconfigured for production topology. There is no documented trusted-proxy hop count.
- **Account lockout is keyed on the user but the rate limiter is keyed on IP only** — there is no per-account login rate limit. An attacker spreading attempts across IPs (or coming through a single shared egress that other users also use) interacts badly: lockout (`MarkLoginFailure`, repo/user.go:137–153, default 10 attempts → 30m lock) is per-user and **can be weaponized to DoS a specific victim** (lock any known email by submitting 10 bad passwords). The generic error means the attacker can't tell, but the victim is locked out. Consider not hard-locking on password failures from unverified sources, or using a stepped delay.

**AUTH-10 (Medium): MFA verification has no rate limit and no replay protection.** In `Login`, after password success, the TOTP check (service.go:197–211) calls `totp.Verify` with `Skew: 1` (totp.go:47–54), accepting a **3-period (90-second) window**. A failed MFA code **does not** increment `failed_login_count` or trip lockout — it only emits an slog line (lines 202–208). Combined with AUTH-09 (login rate limit shared across all users behind a proxy), an attacker who has the password can **brute-force the 6-digit TOTP** (1,000,000 codes, 3 valid per window) with no per-account throttle and no lockout. There is also **no replay cache** — the same valid code can be submitted repeatedly within its window to mint multiple sessions. RFC 6238 §5.2 requires single-use enforcement; it is absent.

**AUTH-11 (Low): timing side-channel for user existence.** When the user is not found (service.go:156–159) the handler returns immediately *without* doing an argon2 verify, whereas a found user pays the ~50–80ms argon2 cost. The response-time delta lets an attacker enumerate valid emails despite the uniform error string. Mitigation: perform a dummy argon2 verify against a fixed hash on the not-found path.

**AUTH-12 (Low): rate-limit reset on success enables a known abuse.** `Login` resets the IP bucket on success (service.go:253). An attacker with *one* valid credential pair on the shared-IP bucket can periodically log in successfully to reset the counter, keeping a parallel brute-force alive. Minor given other issues, but worth noting.

---

## 4. MFA / TOTP — `totp/totp.go`, enrollment/verify in `service.go`

`Enroll` generates a 20-byte (160-bit) SHA-1 TOTP secret (totp.go:26–42) — RFC-compliant. The `EnrollMfa`/`VerifyMfa` two-step (service.go:525–571) correctly stores the secret disabled and only flips `mfa_enabled` after a proof, preventing lock-out from a never-confirmed secret.

**AUTH-13 (High): TOTP secret is stored in plaintext.** `users.mfa_secret` is a plain `text` column (migration 0003:6) and `EnrollMfa` writes the base32 secret verbatim (repo/user.go:157–165; service.go:540). The documented threat model for sessions/passwords ("a Postgres compromise does not yield active sessions / passwords") **explicitly does not hold for MFA** — a DB read yields every user's TOTP seed, fully defeating the second factor for the entire tenant base. Per the audit conventions this is a deviation from the encrypt-secrets posture applied elsewhere (the pepper for passwords). The secret should be encrypted at rest (e.g. AES-GCM with an env key) or at minimum the report should flag that the password threat model claims do not extend to MFA.

**AUTH-14 (Medium): no recovery/backup codes.** There is no recovery-code table or flow (confirmed: no `recovery_code`/`backup_code` anywhere in the tree). A user who loses their authenticator is permanently locked out of MFA with **no self-service recovery and no disable-MFA endpoint** — the only `mfa/*` routes are enroll and verify (server.go:184–185). Account recovery requires DB surgery. Also: there is no `disable MFA` flow that re-checks a current code, so once on, MFA can only be removed out-of-band.

**AUTH-15 (Low): MFA enroll secret returned and otpauth URL in response; reset state on re-enroll.** `handleMfaEnroll` returns the raw secret + otpauth URL (auth_handlers.go:161–164) — necessary for QR provisioning, acceptable. But `EnrollMfa` blocks only when `MfaEnabled==true` (service.go:532–534); a user mid-enrollment (secret set, not enabled) can re-enroll and overwrite. Benign, but the new secret silently invalidates a half-provisioned authenticator with no audit distinction.

---

## 5. Password reset — `service.go` `RequestPasswordReset` / `ConfirmPasswordReset`

Token is a 32-byte `session.NewToken()` (service.go:455), stored in Redis keyed by `base64url(sha256(token))` (lines 459–460) with TTL `PasswordResetTTL` (default 1h). The raw token is never persisted server-side except as a hash. `ConfirmPasswordReset` hashes the presented token, looks it up, sets the new password, **revokes all sessions**, audits, then `Del`s the key (service.go:475–523) — single-use is enforced by the post-success `Del`. Enumeration is correctly defended: `RequestPasswordReset` returns `nil` for unknown user (service.go:449–451) and the handler always replies 202 (auth_handlers.go:123–126).

**AUTH-16 (High): reset token is logged in plaintext.** `handlePasswordResetRequest` (auth_handlers.go:113–121) logs the **raw reset token alongside the tenant and email** at `Info` level: `s.logger.Info("password reset token issued ...", "token", token)`. In any environment shipping logs to a collector (and prod logs at Info), **anyone with log access can take over any account** by reading the token and POSTing it to `/password-reset/confirm` within the TTL. The same plaintext-token-to-logs pattern is the *only* delivery in dev, but the code path is not gated on `Env=="development"` — it always logs. This is the most directly exploitable issue if log access is broader than DB access (it usually is).

**AUTH-17 (Medium): reset `Del` is non-transactional with the password set, so a token can be reused on partial failure.** `ConfirmPasswordReset` commits the password change + session revoke in a tx (service.go:509–520), then `_ = s.redis.Del(...)` **after** commit, ignoring the error (line 521). If the `Del` fails, the token remains valid in Redis until TTL and can be replayed to set the password again (within the hour). Low likelihood, but single-use is not truly guaranteed. Better: `GETDEL`/Lua to atomically consume the token *before* the password change, so the token is spent regardless of downstream outcome.

**AUTH-18 (Low): `IssueResetToken` (new-admin provisioning) bypasses enumeration controls and is returned in an API response.** `handleCreateTenant` returns `PasswordResetToken` directly in the JSON body (platform_handlers.go:54, 178–191). This is gated by the platform admin token so the audience is trusted, but the token then also has the AUTH-16 logging exposure if request/response logging is enabled. Acceptable given the trust boundary, noted for completeness.

**AUTH-19 (Low): reset does not check user status.** `RequestPasswordReset`/`ConfirmPasswordReset` resolve by email/id but never check `status` — a `suspended` user can complete a reset and `SetPassword` flips `invited→active` (repo/user.go:117) but leaves `suspended` as-is, so a suspended user can reset their password (then still be blocked at login by `ErrUserSuspended`). Minor, but a suspended account should arguably reject reset.

---

## 6. AuthZ engine — `policy/`, `policy_middleware.go`, route table

The evaluator is clean and well-tested in principle: `PermissionSet.Can` (policy.go:68–92) is a pure function — hold-the-permission → station-scope routing → tenant-wide bypass. `DBLoader.Load` (db_loader.go:25–82) derives `TenantWide = len(StationIDs)==0`. The "no station-access rows ⇒ tenant-wide" model is documented (migration 0004) and consistently applied.

**AUTH-20 (High): the absent-station-rows ⇒ tenant-wide rule is a privilege-escalation footgun.** `DBLoader.Load` (db_loader.go:80) sets `TenantWide=true` whenever the user has zero `user_station_access` rows — *for any role*. This means: **a station-restricted role whose station-access rows are accidentally deleted (or never created) silently becomes tenant-wide.** Example path: an admin grants `supervisor` to a user and forgets to grant station access, or `RevokeStationAccess` removes the user's *last* station (users_handlers.go:335–393 has no guard against removing the final row) — the user is instantly promoted from "one station" to "every station in the tenant" for every station-scoped permission they hold. The model fails *open* on missing scope data. This is by design per the docs, but it is a dangerous default for a multi-tenant fuel-retail system; "no scope" should arguably mean "no access" for non-admin roles, with tenant-wide reserved for an explicit flag.

**AUTH-21 (High): many write routes have NO permission middleware and rely entirely on in-handler `authorizeStation` — easy to forget, impossible to verify from the route table.** Looking at `server.go`, a large set of mutating routes are registered with **no `requirePermission` wrapper at all**, only the ambient `requireAuth`:
- `POST/PATCH/DELETE /tanks`, `/pumps`, `/nozzles` (lines 277–294)
- `POST /tanks/{id}/calibration-charts`, `/opening-balance`, `/deliveries` (304, 315, 321)
- `POST /purchase-orders`, `/supplier-invoices`, all PO transitions (339–347)
- `POST /shifts/{id}/...` attendants, nozzle-assignments, meter-readings, dip-readings, close, cash (789–810)
- `PATCH /pumps/{id}/status`, `/tanks/{id}/status`, `/incidents`, `/operating-days/{id}/status|lock` (759–778)

These depend on each handler calling `s.authorizeStation(...)` internally against a station id resolved from the body or the target row. The pattern *works when applied*, but: (a) it is unverifiable by reading the route table — a reviewer cannot tell an authorized route from an unprotected one; (b) a single handler that forgets the in-handler call is a silent authorization bypass (any authenticated user, any tenant's permission set, can mutate). This audit did not read every one of those handlers, so I **cannot confirm none is missing the guard** — this is precisely the risk. House convention says permissions are declared via `requirePermission`/`Held`; these routes deviate. Recommendation: enforce a default-deny at the router (every mutating route gets an explicit permission middleware) even if the fine-grained station check stays in-handler.

**AUTH-22 (Medium): `requirePermissionHeld` deliberately ignores station scope, widening read access.** `requirePermissionHeld` (policy_middleware.go:135–160) checks `ps.HasPermission(perm)` only — it does **not** call `Can`, so it ignores `StationScoped`/`StationIDs` entirely. It is used for tenant-wide list endpoints (`/companies`, `/regions`, `/stations`, `/products`, `/tanks`, etc.) on the theory that "the repos are tenant-scoped." That is true for *tenant* isolation, but it means **a station-restricted user with `station.read` can hit `GET /tanks` and the *repo* must do the station filtering** (which `stationReadFilter`/`stationScope` do for tanks, per the Phase-2 test). The correctness therefore lives in each list handler's use of `stationScope`, not in the middleware. Any list handler under a `requirePermissionHeld` gate that forgets to apply `stationReadFilter` leaks cross-station rows within the tenant. Same unverifiable-by-route-table risk as AUTH-21, lower severity (read-only, intra-tenant).

**AUTH-23 (Medium): permissions used in routes are not all seeded — routes may be unreachable or always-403.** The route table references many permission codes (`companies.manage`, `tanks.manage`, `pumps.manage`, `inventory.read`, `purchase_order.read`, `supplier.manage`, `customer.read`, `customer_credit.read`, `enterprise.read`, `risk.read`, `finance.read`, `journal.read`, `payable.read`, etc.). Migrations 0004 + 0007 seed only ~22 codes (`station.*`, `shift.*`, `reading.edit`, `stock.*`, `price.change`, `margin.view`, `credit.*`, `period.lock`, `reports.export`, `audit.read`, `integrations.manage`, `users.*`, `companies.manage`, `regions.manage`, `sessions.revoke`). Codes like `tanks.manage`, `inventory.read`, `finance.read`, `risk.read` are presumably seeded in later-phase migrations (0009+) not in scope here — **if any referenced code is unseeded, that route is permanently 403** (the permission can never be held). This is a correctness/coverage risk: there is no test asserting "every `requirePermission(code,…)` in the router has a matching seeded permission row." `users.invite` *is* seeded (0007) and granted only to `system_admin`, so `POST /admin/users` works only for system_admin — likely intended, but inconsistent with `users.manage` gating the list/status routes.

**AUTH-24 (Low): `system_admin` catch-all grant means full cross-domain power with no separation of duties.** Migration 0004:202–204 grants `system_admin` *every* permission via `OR (r.code = 'system_admin')`. A single compromised system_admin session can do anything in the tenant — manage users *and* approve their own financial transactions, tune risk rules, suppress alerts, etc. No SoD between security-admin and finance-approver. Expected for an admin role, but worth an explicit note since `system_admin` is auto-granted to the first user of every tenant (platform_handlers.go:120–134).

---

## 7. Tenant isolation & RLS

Repos take `tenantID` and filter on it (`FindForLogin` joins tenants by slug, `FindByID` requires `tenant_id`, `List`/`UpdateStatus` filter by tenant — repo/user.go). Composite `(tenant_id, id)` FKs (migration 0008) are a solid DB backstop against cross-tenant linkage. The admin handlers correctly add `userInTenant` guards before role/station mutations (users_handlers.go:177–186, 210–216, 289–303, 357–363) and return uniform 404s to avoid cross-tenant enumeration (admin_handlers.go:82–96 comment).

**AUTH-25 (High): RLS is NOT an active runtime safety net — the API bypasses it.** Migration 0005 enables (not FORCES) RLS and creates `fuelgrid_app` as the RLS-subject role, but **the running API connects as the table owner/superuser**, which bypasses RLS entirely (confirmed in `docs/multi-tenancy.md:144` and `roadmap-phase-1.md:144`: "The API still connects as the table owner today"). Crucially, **no code path in the request lifecycle calls `database.WithTenant`** (grep confirms only `tenant.go` defines it; nothing in `internal/identity` or `services/api/internal/server` calls it). The identity service and policy loader query via `s.pool.Query(...)` directly with no `SET LOCAL app.current_tenant`. Therefore:
- RLS provides **zero** protection for the actual API process. The *sole* defense against cross-tenant reads is the hand-written `WHERE tenant_id = $1` in each query.
- Any single repo query that omits the tenant predicate is a real, unmitigated cross-tenant leak — there is no "RLS safety net" behind it, contrary to the house convention's stated intent. (`db_loader.go` querying `user_roles`/`user_station_access` by `user_id` alone is safe because user_id derives from the session, but it is illustrative that the loader trusts the absence of RLS.)
- The CI "RLS isolation" test exercises `fuelgrid_app`, not the path the API actually uses — it proves RLS *can* isolate, not that the API *is* isolated. This is a meaningful gap between the documented security model and the deployed reality. Migrate the API onto `fuelgrid_app` + `WithTenant` (and `FORCE ROW LEVEL SECURITY`) to make the safety net real.

**AUTH-26 (Low): `TenantOf` reads cross-tenant by design.** `repo.TenantOf` (user.go:181–187) selects `tenant_id` for any `user_id` with no tenant scoping — used by password reset where the only handle is a token-derived user id. Correct in context (the token already proved possession), but it is one of several queries that would return cross-tenant data if a user id were ever attacker-controlled here. The reset token is the gate, so acceptable.

---

## 8. Middleware, CORS, transport

`requireAuth` (auth_middleware.go:16–49) extracts the bearer, resolves the session, injects the actor — ordering is correct (auth before permission middleware). `extractBearer` (lines 53–71) tolerates case-insensitive "bearer", refuses `?token=` query strings (good, comment lines 69–70). Errors are mapped to uniform 401s; internal errors are logged server-side and return a generic body (no error leakage to clients).

**AUTH-27 (High): CORS is `AllowCredentials: true` with operator-supplied origins and no wildcard guard.** `server.go:155–162` sets `AllowCredentials: true` and `AllowedOrigins: cfg.CORSOrigins` (default `http://localhost:3000`). The danger: if an operator ever sets `API_CORS_ALLOWED_ORIGINS=*` (a common mistake), `go-chi/cors` with credentials+wildcard will reflect the origin and allow credentialed cross-origin reads — but note this API uses **bearer tokens in the `Authorization` header, not cookies**, so `AllowCredentials` is arguably unnecessary and the practical CSRF/credential-theft exposure is lower than a cookie-based app. Still: `AllowCredentials:true` is set without a cookie auth model that needs it, and there is no validation rejecting `*` in `CORSOrigins`. Recommendation: drop `AllowCredentials` (tokens are header-based) or validate origins are explicit https URLs.

**AUTH-28 (Medium): no security response headers.** No `Strict-Transport-Security`, `X-Content-Type-Options`, `X-Frame-Options`/`Content-Security-Policy`, `Referrer-Policy`, or `Cache-Control: no-store` on auth responses. `writeJSON` (handlers.go:55–59) sets only `Content-Type`. Login/refresh responses containing tokens are cacheable by default. TLS is assumed terminated upstream but HSTS is not asserted.

**AUTH-29 (Medium): no request body size limit on auth endpoints.** No `http.MaxBytesReader` anywhere (grep: zero matches). `handleLogin`, `handlePasswordResetConfirm`, etc. `json.NewDecoder(r.Body).Decode` an unbounded body. A client can POST a multi-GB body to `/auth/login` (and the password field is fed to HMAC/argon2 only after JSON parse) — memory-amplification DoS. `chimiddleware.Timeout(30s)` and server read timeouts bound the *time* but not the *size*. Note `auth_handlers.go` uses bare `json.NewDecoder` (no `DisallowUnknownFields`), unlike `decodeJSON` (handlers.go:63–70) used by `handleChangeMyPassword` — inconsistent decoder discipline.

**AUTH-30 (Low): `extractBearer` whitespace handling.** `extractBearer` (auth_middleware.go:63) does `strings.TrimSpace(h[len(prefix):])` after a case-insensitive prefix check — correct, but a header like `"Bearer  <tok>"` (double space) yields the token with the leading space trimmed; fine. No issue, noted as verified.

---

## 9. Self-service endpoints — `me_handlers.go`, `me_permissions_handler.go`

`handleRevokeMySession` correctly delegates to `RevokeSession`, which enforces ownership via `FindActiveOwnedBy` (service.go:323–344, repo/session.go:130–144) — a user cannot revoke another user's session (IDOR-safe). `handleListMySessions` lists only the actor's sessions. `handleChangeMyPassword` verifies the old password (service.go:418–426), preventing a stolen *session* from silently changing the password without the old one.

**AUTH-31 (Low): `/me/sessions` revoke has no `sessions.revoke` gate but that's intentional (self-scoped).** The seeded `sessions.revoke` permission (migration 0007) is meant for revoking *other* users' sessions; the self-revoke route correctly needs only ownership. No admin "revoke another user's sessions" endpoint appears to be wired despite the seeded permission — dead permission (see AUTH-33) and a missing admin capability.

**AUTH-32 (Low): change-password does not invalidate other sessions.** Restated cross-ref to AUTH-05: after `handleChangeMyPassword` succeeds, the user's *other* sessions stay live. For a "I think I'm compromised, changing my password" flow this is the wrong behavior.

---

## 10. Tests, dead code, misc

`phase2_integration_test.go` exercises the real auth path (login as admin + scoped operator), tenant-wide vs station-scoped reads, and the 403 on out-of-scope tank access — good coverage of the **happy authz path**. However the test harness sets `LoginLockAfter:1000, LoginRateMax:1000` (lines 99–101) — so **lockout and rate limiting are effectively disabled in tests; neither is exercised.** There are **no tests for**: rate-limit/lockout behavior, MFA enroll→verify→login, password reset request/confirm/single-use, session revocation propagation to Redis, the timing side-channel, or cross-tenant admin mutation (the `userInTenant` guards). `hasher_test.go`, `totp_test.go`, `policy_test.go` exist (unit-level) but were not the focus; the integration suite skips entirely without `TEST_DATABASE_URL`.

**AUTH-33 (Info): dead code / dead config.**
- `AuthRefreshTTL` (config.go:42) is loaded but never read — `Refresh` uses `SessionTTL` (AUTH-06).
- `sessions.revoke` permission (migration 0007) is seeded but no route consumes it (AUTH-31).
- `ErrMfaRequired`, `ErrSessionRevoked`, `ErrTenantNotFound` (errors.go) are defined; `ErrSessionRevoked` is mapped in `requireAuth` but **never returned** by `Resolve` (which only returns `ErrSessionNotFound`/`ErrSessionExpired`), so revoked-but-still-in-Redis sessions surface as "not found" at best — and as *valid* at worst per AUTH-04.
- `var _ = session.Session{}` (auth_middleware.go:74) is a no-op import-pin; the import is actually unused and should be removed.

**AUTH-34 (Low): audit actor is the target, not always the actor, for some auth events.** `auditAuth` (service.go:111–122) always sets `ActorID == EntityID == the subject user`. For `MarkLoginFailure`/login events that's correct. Fine, noted.

---

## Findings table

| ID | Severity | File:Line | Issue | Fix |
|----|----------|-----------|-------|-----|
| AUTH-01 | Medium | `password/hasher.go:138–145` | Pepper mixed into stored hash with no version tag → non-rotatable; leak forces mass reset | Add `pepper_version` to PHC string; support dual-pepper rotation |
| AUTH-02 | Medium | `cmd/api/main.go:181–183` | Prod can boot with empty pepper (warn only) | Hard-fail startup when `Env!=development` and pepper empty |
| AUTH-03 | Low | `service.go:408,476` | Only 12-char min; no max length, no complexity/breach check | Add max length + breached-password/complexity policy |
| AUTH-04 | High | `service.go:389–402,355–361` | `Resolve` trusts Redis only; best-effort Redis delete on revoke-all leaves revoked sessions live up to full TTL | Check `revoked_at`/per-user invalidation epoch on hot path; make reset-path Redis delete authoritative |
| AUTH-05 | Medium | `service.go:404–438` | `ChangePassword` doesn't revoke/rotate other sessions | Revoke all other sessions on password change; rotate current |
| AUTH-06 | Medium | `service.go:366–384` | `Refresh` slides expiry with no absolute cap, no rate limit, no revocation check; `AuthRefreshTTL` unused | Enforce absolute session lifetime; check revocation on refresh |
| AUTH-07 | Low | `auth_handlers.go:38–45`; `service.go:Resolve` | DeviceID never set on login; no device/IP binding enforced | Populate + validate device binding or drop the columns |
| AUTH-08 | Low | `redis_store.go:116–141` | `Delete`/`DeleteByID` are read-then-del (non-atomic) | Use Lua/`GETDEL` for atomic two-key removal |
| AUTH-09 | High | `service.go:146`; `auth_handlers.go:246–254` | Rate limit keyed on `r.RemoteAddr` → single bucket behind any proxy; per-account lockout weaponizable to DoS a victim | Configurable trusted-proxy XFF parsing; add per-account throttle; soften hard lockout |
| AUTH-10 | High | `service.go:197–211`; `totp.go:47–54` | MFA brute-forceable: no per-account rate limit/lockout on bad TOTP, no replay/single-use cache, 90s skew window | Throttle + lock on MFA failure; enforce one-time-use per code |
| AUTH-11 | Low | `service.go:156–159` | Timing side-channel: no argon2 work on unknown-user path | Dummy verify against fixed hash on not-found |
| AUTH-12 | Low | `service.go:253` | Rate bucket reset on success enables sustained parallel brute force | Don't fully reset; decay or separate success/fail buckets |
| AUTH-13 | High | `migration 0003:6`; `repo/user.go:157–165` | TOTP secret stored plaintext → DB read defeats MFA tenant-wide | Encrypt `mfa_secret` at rest (AES-GCM with env key) |
| AUTH-14 | Medium | `server.go:184–185` | No recovery/backup codes, no MFA-disable flow → permanent lockout on lost device | Add recovery codes + code-verified disable endpoint |
| AUTH-15 | Low | `service.go:532–534` | Re-enroll overwrites half-provisioned secret silently | Distinct audit + require confirm to replace |
| AUTH-16 | High | `auth_handlers.go:113–121` | Raw password-reset token logged at Info with email — log access = account takeover | Never log the token; gate dev convenience behind `Env==development` only |
| AUTH-17 | Medium | `service.go:509–521` | Reset token `Del` is post-commit, error-ignored → replayable on failure | Atomically consume token (`GETDEL`) before password change |
| AUTH-18 | Low | `platform_handlers.go:178–191` | New-admin reset token returned in API body (+ AUTH-16 log exposure) | Acceptable behind platform token; ensure no req/resp body logging |
| AUTH-19 | Low | `service.go:446–523` | Reset flow ignores user `status` (suspended can reset) | Reject reset for suspended/deleted users |
| AUTH-20 | High | `policy/db_loader.go:80` | Zero station-access rows ⇒ tenant-wide for *any* role; fails open on missing scope; revoking last station promotes user | Require explicit tenant-wide flag; "no scope" = no access for non-admin; guard last-station revoke |
| AUTH-21 | High | `server.go:277–294,304–347,759–810` | Many mutating routes have no `requirePermission`; rely on unverifiable in-handler `authorizeStation` | Default-deny: explicit permission middleware on every mutating route |
| AUTH-22 | Medium | `policy_middleware.go:135–160` | `requirePermissionHeld` ignores station scope; list handlers must self-filter or leak intra-tenant cross-station rows | Centralize scope filtering; test every Held-gated list applies `stationReadFilter` |
| AUTH-23 | Medium | `server.go` vs migrations 0004/0007 | Routes reference permission codes not seeded in audited migrations → permanent 403 or coverage gap | Add test: every routed permission code exists in `permissions` |
| AUTH-24 | Low | `migration 0004:202–204`; `platform_handlers.go:120–134` | `system_admin` gets all permissions, auto-granted to first tenant user; no SoD | Document; consider splitting security vs finance admin |
| AUTH-25 | High | `migration 0005`; `tenant.go`; `db_loader.go` | RLS enabled-not-forced; API connects as owner & never calls `WithTenant` → RLS gives the API zero protection; only WHERE clauses isolate tenants | Run API as `fuelgrid_app`, set `app.current_tenant` per tx, `FORCE ROW LEVEL SECURITY` |
| AUTH-26 | Low | `repo/user.go:181–187` | `TenantOf` reads any user cross-tenant (gated by reset token) | Acceptable; document the trust boundary |
| AUTH-27 | High | `server.go:155–162` | `AllowCredentials:true` with operator origins, no `*` guard; credentials unnecessary for header-token auth | Drop `AllowCredentials`; reject wildcard/non-https origins |
| AUTH-28 | Medium | `handlers.go:55–59` | No HSTS/CSP/X-Content-Type-Options/no-store on token responses | Add security headers middleware; `Cache-Control: no-store` on auth |
| AUTH-29 | Medium | `auth_handlers.go:29,98,135` | No `MaxBytesReader`; unbounded JSON body on auth endpoints (DoS) | Wrap bodies in `http.MaxBytesReader`; use `decodeJSON` consistently |
| AUTH-30 | Info | `auth_middleware.go:53–71` | Bearer extraction verified correct | None |
| AUTH-31 | Low | `server.go:203`; `migration 0007` | `sessions.revoke` permission seeded but no admin route consumes it; no admin session-revoke capability | Wire admin revoke endpoint or drop permission |
| AUTH-32 | Low | `me_handlers.go:85–114` | Self change-password leaves other sessions live (dup of AUTH-05) | See AUTH-05 |
| AUTH-33 | Info | `config.go:42`; `errors.go`; `auth_middleware.go:74` | Dead config (`AuthRefreshTTL`), unused `ErrSessionRevoked` path, no-op import pin | Remove dead code; wire revoked detection |
| AUTH-34 | Info | `service.go:111–122` | Audit actor==subject for auth events | Verified acceptable |

---

## Severity counts

- **Critical:** 0
- **High:** 8 (AUTH-04, AUTH-09, AUTH-10, AUTH-13, AUTH-16, AUTH-20, AUTH-21, AUTH-25, AUTH-27) — *9 listed; AUTH-27 reflects deployment-dependent CORS risk and is included in the High band.*
- **Medium:** 9 (AUTH-01, AUTH-02, AUTH-05, AUTH-06, AUTH-14, AUTH-17, AUTH-22, AUTH-23, AUTH-28, AUTH-29) — *10 listed in the Medium band.*
- **Low:** 11 (AUTH-03, AUTH-07, AUTH-08, AUTH-11, AUTH-12, AUTH-15, AUTH-18, AUTH-19, AUTH-24, AUTH-26, AUTH-31, AUTH-32)
- **Info:** 3 (AUTH-30, AUTH-33, AUTH-34)

(Counts: 9 High, 10 Medium, 12 Low, 3 Info, 0 Critical across 34 findings.)

## Top 5 risks

1. **AUTH-16 — Password-reset token logged in plaintext** (`auth_handlers.go:113–121`). The single most directly exploitable issue: anyone with Info-level log access can take over any account within the reset TTL. Log access is almost always broader than DB access.
2. **AUTH-25 — RLS is not a real safety net** (`migration 0005` + no `WithTenant` in the request path). The documented "RLS belt-and-braces" does not protect the running API at all; tenant isolation rests entirely on hand-written `WHERE tenant_id` clauses, with no backstop if one is forgotten.
3. **AUTH-13 — TOTP secrets stored in plaintext** (`migration 0003:6`). A DB compromise hands the attacker every user's second factor, contradicting the subsystem's own threat-model claims and nullifying MFA tenant-wide.
4. **AUTH-04 + AUTH-10 — Session revocation and MFA both fail to hard-stop attackers.** Best-effort Redis deletes leave revoked sessions live up to 12h (AUTH-04); MFA codes have no per-account throttle, no lockout, and no replay protection, making the second factor brute-forceable once the password is known (AUTH-10).
5. **AUTH-20 + AUTH-21 — Authorization fails open.** "No station rows ⇒ tenant-wide" silently promotes mis-provisioned or last-station-revoked users to full-tenant access (AUTH-20), and dozens of mutating routes carry no declarative permission gate, relying on in-handler checks that cannot be verified from the route table and bypass auth entirely if one is missing (AUTH-21).
