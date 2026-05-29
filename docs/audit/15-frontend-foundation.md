# 15 — Frontend Foundation Audit (Auth, Routing, State, Data Layer)

Brutally-honest, atomic-level, **read-only** audit of the FuelGrid OS web client's
foundation: app shell, the authentication UX flows, route protection, client state,
and the data layer that wires the hand-written `@fuelgrid/sdk` to TanStack Query.

> Scope discipline: this file audits the *foundation* only. Per-feature dashboard pages
> (operations, inventory, procurement, etc.) are out of scope except where they
> demonstrate a foundation-level pattern (error/loading/empty handling, 401 propagation).

## Scope (files + LOC)

| File | LOC | Role |
|---|---:|---|
| `apps/web/src/app/layout.tsx` | 21 | Root layout, `<html>`/`<body>`, metadata |
| `apps/web/src/app/page.tsx` | 26 | Client-side root redirector |
| `apps/web/src/app/providers.tsx` | 59 | Theme + TanStack Query + Sentry init |
| `apps/web/src/app/globals.css` | 106 | Design tokens, theme variables |
| `apps/web/src/app/(auth)/layout.tsx` | 7 | Centered auth shell |
| `apps/web/src/app/(auth)/login/page.tsx` | 11 | Login route (Suspense wrapper) |
| `apps/web/src/app/(auth)/mfa/page.tsx` | 17 | MFA placeholder card |
| `apps/web/src/app/(auth)/forgot-password/page.tsx` | 126 | Password-reset request flow |
| `apps/web/src/app/(auth)/reset-password/page.tsx` | 153 | Password-reset confirm flow |
| `apps/web/src/app/(dashboard)/layout.tsx` | 29 | Authenticated shell (guard + chrome) |
| `apps/web/src/app/(dashboard)/settings/layout.tsx` | 54 | Settings tab nav |
| `apps/web/src/components/auth/login-form.tsx` | 183 | The login form (RHF + zod) |
| `apps/web/src/components/auth/protected-route.tsx` | 46 | Client-side route guard |
| `apps/web/src/components/layout/sidebar.tsx` | 97 | Primary nav |
| `apps/web/src/components/layout/topbar.tsx` | 106 | Top chrome, station picker, logout, theme |
| `apps/web/src/components/layout/command-palette.tsx` | 67 | Cmd-K shell (skeleton) |
| `apps/web/src/components/layout/right-panel.tsx` | 25 | Insights placeholder |
| `apps/web/src/lib/api.ts` | 17 | SDK singleton wiring |
| `apps/web/src/lib/sentry.ts` | 30 | Sentry browser init |
| `apps/web/src/stores/auth-store.ts` | 45 | zustand persisted auth |
| `apps/web/src/stores/tenant-store.ts` | 38 | zustand persisted tenant context |
| `apps/web/src/hooks/use-permissions.ts` | 61 | Permission query + `usePermission` |
| Config: `next.config.ts`, `postcss.config.mjs`, `tsconfig.json`, `.env.example`, `package.json` | — | Build/env |
| Cross-ref: `packages/sdk/src/client.ts` | 2953 | `request()` transport, `SdkError` (in-scope for the data layer) |

Foundation LOC under primary review ≈ **1,324** (excluding the 2,953-line SDK and dashboard feature pages).

---

## Concern 1 — AuthN / Session Security

### 1.1 Token storage: bearer JWT in `localStorage` via `zustand/persist`

`auth-store.ts:33-39` persists `{ token, expiresAt }` to `localStorage` under key
`fuelgrid.auth`:

```ts
storage: createJSONStorage(() => localStorage),
partialize: (state) => ({ token: state.token, expiresAt: state.expiresAt }),
```

This is the single largest security decision in the foundation, and it is the classic
SPA trade-off taken in its **least defensible** form:

- **XSS = full session theft.** Any script that executes in the origin (a compromised
  npm dependency, a reflected/stored XSS in any dashboard page, a malicious browser
  extension with content-script access) can read `localStorage.getItem('fuelgrid.auth')`
  and exfiltrate a valid bearer token. With ~40 dashboard pages and a large dependency
  tree (cmdk, radix, lucide, sentry, recharts-adjacent UI), the XSS attack surface is
  non-trivial, and the blast radius of any single XSS is "steal the session token."
- **No `httpOnly` cookie option exists.** The SDK explicitly sends `credentials: 'omit'`
  (`client.ts:189`), so even if the API set an `httpOnly`/`SameSite` session cookie, the
  browser would never send it. The design is hard-committed to header-bearer auth.
- The code comments (`page.tsx:8-14`) frame this as a deliberate "Stage 8 localStorage-only
  auth contract" with "cookie-backed middleware can replace this in a later stage." That is
  honest, but it means the product currently ships the weaker model with no migration in
  sight.

This is a **High** finding rather than Critical because it is an industry-common pattern and
the comments acknowledge it, but for a *multi-tenant financial SaaS* handling money,
receivables, and accounting, `localStorage` token storage should be treated as a serious
risk that needs a documented mitigation plan (CSP, SRI, dependency pinning) at minimum.

### 1.2 No client-side token expiry / refresh — `expiresAt` is dead state

`expiresAt` is captured from the login response (`login-form.tsx:83`,
`setSession(res.token, res.expires_at)`) and persisted, but a repo-wide grep confirms it
is **never read for any expiry decision anywhere in the app**. The only consumer is the
*server-reported* `s.expires_at` rendered in the sessions table (`profile/page.tsx:136`) —
a different field. Consequences:

- A user holding an expired token sees the app behave as "logged in" (the `ProtectedRoute`
  guard only checks `token` truthiness, `protected-route.tsx:29`) until an API call returns
  401 — and as Concern 4 shows, a 401 doesn't log them out either.
- The SDK exposes `refresh()` (`client.ts:253-255`) returning `{ expires_at }`, but it is
  **never called** anywhere in `apps/web`. There is no idle-refresh, no near-expiry refresh,
  no interval. The "token refresh" mentioned in `api.ts:11` comment ("a token refresh /
  logout propagates without rebuilding the client") describes a capability that is wired but
  never exercised. Sessions silently rot.

**Medium** (security + UX). The fix is small: compare `expiresAt` to `Date.now()` in the
guard and/or schedule a `refresh()` before expiry, then `clearSession()` on failure.

### 1.3 No CSRF consideration (acceptable here, but undocumented)

Because auth is bearer-header (not cookie) and `credentials: 'omit'`, classic CSRF does not
apply — a cross-site form post can't attach the `Authorization` header. This is fine, but it
is an *implication* of the storage choice, not a deliberate CSRF defense, and it isn't
documented. If §1.1 is ever revisited toward cookies, CSRF protection must be added at the
same time. **Info.**

### 1.4 Route protection is **purely client-side** — unauthenticated users receive the dashboard HTML

There is **no `middleware.ts`** anywhere in the repo (verified: searched `apps/web` and the
repo root). Combined with the `(dashboard)/layout.tsx` being a `'use client'` component
wrapping `<ProtectedRoute>`, the protection model is:

1. Server renders/streams the dashboard route's HTML/JS *regardless of auth* — there is no
   server gate. An unauthenticated request to `/command-center` (or any dashboard route)
   gets a 200 and the page shell.
2. On the client, `ProtectedRoute` (`protected-route.tsx`) waits for hydration, then if
   `!token` calls `router.replace('/login?next=…')`.

Practical implications:

- **Confidentiality of *data* is not breached** — the actual data is fetched client-side with
  a bearer token, so an unauthenticated user sees the loading/redirect, not real data.
- **But the protected route's component code and structure are served to anyone.** For a
  product positioning itself as enterprise/financial, "anyone can `curl` the dashboard bundle"
  is a real, reportable gap, and there's a brief unauthenticated paint risk (see §1.6).
- The guard is **duplicated** at the layout via `<ProtectedRoute>` *and* re-implemented
  independently at the root (`page.tsx`). Two slightly different copies of the same redirect
  logic is a maintenance hazard.

**High.** The `(dashboard)/layout.tsx` *does* guard (it wraps children in `ProtectedRoute`,
`layout.tsx:15-27`), so the answer to "does the dashboard layout actually guard or just the
root redirect?" is: **the layout guards, client-side only.** What's missing is any
server-side enforcement. Add a `middleware.ts` matcher on `/(dashboard)` routes that checks
for a session indicator — but note this is impossible with the current `localStorage`-only
model (middleware can't read `localStorage`), which ties this finding back to §1.1. This is
the architectural knot at the center of the auth design.

### 1.5 Logout completeness — good, with one gap

`topbar.tsx:37-48` `handleLogout()`:

```ts
try { await api.logout(); } catch (err) { … }
clearSession();
resetTenantContext();
router.replace('/login');
```

Strengths: it calls the server `logout` (revokes the session server-side), tolerates a
failed/offline logout (still clears locally), and resets tenant context. **But it never
clears the TanStack Query cache.** After logout, `browserQueryClient` (a module-level
singleton, `providers.tsx:33-43`) retains every cached response from the previous session —
`me`, `me/permissions`, `stations`, financials, customer balances, etc. Because logout uses
`router.replace` (a client navigation, not a full reload), the next user to log in on the
**same browser tab** can momentarily render the *previous* user's cached data before queries
refetch (and cross-tenant if they switch tenant at login). At minimum call
`queryClient.clear()` (or `removeQueries`) on logout. **Medium** (data-leak-on-shared-device).

### 1.6 The hydration-gated redirect: flash + the "double guard" race

`page.tsx` (root) and `protected-route.tsx` both gate on `hydrated` to avoid flashing a
redirect before `localStorage` rehydrates — a correct instinct. Residual issues:

- **Root page renders `null` then redirects** (`page.tsx:25`, `return null`). On a cold load
  to `/`, the user sees a blank white (or, with dark theme, dark) screen with no spinner
  until hydration completes and the `router.replace` fires. `ProtectedRoute` does better
  (it shows `LoadingState` "Checking session…", `protected-route.tsx:35-40`); the root page
  should match that for consistency.
- **Brief authenticated-content paint is possible.** When `hydrated && !token`,
  `ProtectedRoute` returns `null` (`:43`) and fires the redirect in an effect. That's fine.
  But the dashboard layout's chrome (`Sidebar`, `Topbar`) is *inside* `ProtectedRoute`, so it
  won't paint. Good. The only paint risk is the server-rendered shell from §1.4 before
  hydration — short, but real on slow connections.
- **`router.replace` in an effect with `token` in deps** (`page.tsx:20-23`): if `token`
  changes (e.g. login in another tab broadcasting via the persist `storage` event — though
  note zustand persist does **not** cross-tab sync by default here), the effect re-fires.
  Low risk given no cross-tab sync, but the dependency array invites surprises.

**Low/Medium.** No correctness bug, but the blank-screen flash on `/` is a visible UX defect.

### 1.7 `safeRedirect` open-redirect guard — correct and worth calling out positively

`login-form.tsx:44-49` guards `?next=` against `//evil.com` and `/\evil.com`
protocol-relative tricks and requires a leading single `/`. This is a genuinely good,
security-conscious touch. One edge: it does not reject `next` values pointing at the auth
group itself (e.g. `?next=/login`), which could loop, but `router.replace` to `/login` from
`/login` is benign. **Info (positive).**

---

## Concern 2 — SDK Wiring (`lib/api.ts` + `client.ts` transport)

### 2.1 Base URL resolution — fine, with a silent-default caveat

`api.ts:7`:
```ts
const baseURL = process.env.NEXT_PUBLIC_API_URL?.replace(/\/$/, '') ?? 'http://localhost:8080';
```
Trailing-slash trim is good (and the SDK trims again at `client.ts:143`, harmlessly
redundant). The concern is the **hard-coded `http://localhost:8080` fallback**: if a
production build is shipped without `NEXT_PUBLIC_API_URL` set, the client silently points at
`localhost` and every request fails with an opaque network error rather than failing loudly
at build/startup. Because `NEXT_PUBLIC_*` is inlined at build time, this misconfiguration is
baked into the bundle and undetectable at runtime config. Consider throwing in production if
the env is unset. **Low.**

### 2.2 `getToken` reads the store on every request — correct

`api.ts:16` `getToken: () => useAuthStore.getState().token` reads fresh state per request
(not a captured snapshot), so logout/refresh propagate without rebuilding the client. This is
the right pattern and is well-commented. The previously-fixed `fetch.bind(globalThis)`
(`client.ts:151`) is correct. **Info (positive).**

### 2.3 Error handling in `request()` — solid shape, one fragility

`client.ts:196-208`: reads text, `safeParse`, extracts `error` field for the message, throws
`SdkError(message, status, parsed)`. Good: status + parsed body are preserved for callers to
branch on, and `204` is handled (`:192`). Fragilities:

- The message extraction assumes the API error envelope is `{ "error": string }`
  (`:201-203`). If the API ever returns `{ "message": … }`, `{ "errors": [...] }`, or a
  validation map, the message degrades to `HTTP 4xx`. There's no handling of structured
  field-validation errors, so forms can't map server validation to specific fields (see §4).
- On a non-JSON error body (e.g. an HTML 502 from a proxy), `safeParse` presumably returns
  null and the message becomes `HTTP 502` — acceptable, but the raw body is lost.

**Low.**

### 2.4 **No 401 handling in the transport** — the central data-layer defect

`request()` throws `SdkError` for any non-OK status and **does nothing special for 401**.
There is no interceptor, no `onUnauthorized` hook, no automatic `clearSession()`/redirect.
A repo grep for `401` finds exactly one site: `login-form.tsx:88`, which maps 401 to "Invalid
tenant, email, or password." **Every other 401 in the app** (expired token, server-side
session revocation, password-reset-triggered session purge) surfaces only as a generic query
error on whatever page made the call. The user remains "authenticated" (token still in
`localStorage`, guard still passes), staring at error states, with no path back to login
except manually clicking sign-out. This is the most impactful data-layer gap. **High** — see
also Concern 4.4. The standard fix is a global 401 handler (in the SDK config or a
QueryClient `QueryCache.onError`) that calls `clearSession()` and redirects to `/login`.

---

## Concern 3 — TanStack Query Setup (`providers.tsx`)

### 3.1 Retry policy — correct and thoughtful

`providers.tsx:18-24`: the `retry` predicate inspects `error.status`, refuses to retry 4xx
(`>= 400 && < 500`), and retries others up to twice. Mutations don't retry (`:27`). This is
exactly right — retrying a 401/403/422 is pointless and harmful. The cast
`(error as { status?: number } | null)` (`:21`) is loose but safe given the SDK always throws
`SdkError`. **Info (positive)**, minor: could narrow via `instanceof SdkError`.

### 3.2 `staleTime` / `gcTime` — reasonable defaults

`staleTime: 30s`, `gcTime: 5m`, `refetchOnWindowFocus: false` (`:15-17`). Sensible for a
dashboard; per-query overrides exist (`use-permissions.ts:21` uses 60s). The disabled
window-focus refetch is a deliberate trade-off (fewer requests vs. staleness) and is fine
given 30s staleness. **Info.**

### 3.3 **No global query/mutation error handling, no error boundary**

The `QueryClient` is created with **no `QueryCache`/`MutationCache` `onError`** callback. So:

- There is **no single chokepoint** to (a) force-logout on 401 (§2.4), (b) report unexpected
  errors to Sentry, or (c) surface a global toast. Sentry is initialized (`providers.tsx:51`,
  `sentry.ts`) but **nothing ever calls `Sentry.captureException`** — query/mutation failures
  are never reported. Sentry is effectively decorative until manual capture is added.
- There is **no React error boundary** anywhere in the tree (no `error.tsx`, no
  `global-error.tsx`, no class boundary — verified by file search returning nothing). A
  render-time throw in any provider/page produces the raw Next.js error overlay in dev and a
  blank/unstyled crash in prod. The App Router *strongly* expects at least a root
  `app/global-error.tsx` and per-segment `error.tsx`. Their total absence is a foundational
  gap. **High.**

### 3.4 Query-client singleton lifecycle — correct for SSR/CSR, but never cleared

`getQueryClient()` (`:35-43`) makes a fresh client on the server per request and reuses one in
the browser — the documented Next.js pattern, correct. The browser singleton is the same one
implicated in the logout cache-leak (§1.5); it is never cleared on logout. **(Cross-ref 1.5.)**

### 3.5 Invalidation strategy — ad-hoc, no shared query-key factory

Invalidation is done per-page with string-literal keys (e.g. `profile/page.tsx:40`
`qc.invalidateQueries({ queryKey: ['me', 'sessions'] })`; `topbar.tsx:32` uses
`['stations']`; `use-permissions.ts:18` uses `['me', 'permissions']`). There is **no central
query-key factory**, so keys are stringly-typed and duplicated across ~40 pages — a refactor
landmine (rename a key in one place, silently break invalidation elsewhere) and a frequent
source of "data didn't refresh after mutation" bugs. Not a defect today, but a foundational
weakness. **Medium.**

---

## Concern 4 — Forms & Error UX

### 4.1 Login form: zod schema vs. API contract — matches

`login-form.tsx:25-33` schema (`tenant_slug` regex `^[a-z0-9-]+$`, `email`, `password` min 1,
optional `mfa_code`) aligns with `LoginRequest` in `types.ts:7-12`. The tenant-slug regex
mirrors a sensible slug constraint. `password.min(1)` is appropriate for *login* (don't leak
policy). Uses RHF + `zodResolver` correctly. **Info (positive).**

### 4.2 Login error UX — much improved, "Network error" no longer swallows real errors

The brief's "we saw 'Network error' swallowing" concern is **addressed in the current code**:
`login-form.tsx:86-98` branches on `SdkError` first (401 → bad creds, 429 → rate-limited,
else `err.message`), and only falls through to the generic "Network error…" copy for
*non-`SdkError`* throws (genuine fetch failures). This is correct error triage. The other
auth forms are slightly weaker:

- `forgot-password/page.tsx:42` collapses any non-`SdkError` to "Network error. Try again."
  — fine, but it doesn't distinguish a 429.
- `reset-password/page.tsx:58-62` specially handles 400 (invalid/expired token) — good — but
  otherwise mirrors the same generic fallback.

**Info / Low.**

### 4.3 Inconsistent form stacks — RHF+zod for login only, hand-rolled `useState` everywhere else

The **login form is the only form that uses react-hook-form + zod.** `forgot-password`
(`:22`), `reset-password` (`:26`), the profile password-change (`profile/page.tsx:43`), and
(by the grep) every settings/dashboard form use raw `useState` objects with manual
`if (!field) setError(...)` validation. The repo declares `react-hook-form`, `zod`, and
`@hookform/resolvers` as deps but uses them in exactly one component. This is a **consistency
and correctness** problem:

- Manual validation is error-prone (e.g. `reset-password` checks `length < 12` and match
  client-side `:44-51`, but there's no shared password schema; the "12 char" rule is
  duplicated as prose in `profile/page.tsx:162` "Minimum 12 characters" with **no client
  enforcement at all** on that form — it relies entirely on the server).
- No field-level error mapping from the server (ties to §2.3): forms show one blob error, not
  per-field.

**Medium** (maintainability + UX consistency).

### 4.4 Mutations/queries don't react to auth loss

Reinforcing §2.4: across the dashboard, the universal error pattern is
`onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not …')`
(seen in `operations`, `my-shift`, `reconciliation`, `revenue`, every `settings/*`). None
inspect `e.status === 401`. So an expired session mid-session produces a stream of
"Could not save" toasts rather than a clean re-auth. This is a foundation problem (no global
handler) manifesting in every feature. **High (cross-ref 2.4).**

---

## Concern 5 — PWA / Offline Claims

**The product claims offline-first / PWA. The web client has none of it.** Verified by
filesystem search across `apps/web`:

- **No `manifest.json` / `manifest.ts`** (no web app manifest → not installable).
- **No service worker** (`sw.*`, `service-worker.*`, `workbox*` — none).
- **No `public/` directory at all** (so no icons, no offline fallback, no manifest could even
  be served statically).
- **No `next-pwa` / Serwist / Workbox dependency** in `package.json`.
- `layout.tsx` metadata sets only `title`/`description` — no `manifest`, no `themeColor`, no
  `appleWebApp`, no viewport beyond Next defaults.

There is **zero** offline support, zero installability, zero caching beyond the in-memory
TanStack Query cache (which is wiped on reload). Any documentation or marketing asserting
"offline-first PWA" is **factually false** for the current build. **High** (claim vs. reality
gap; for a field-ops product where attendants log shifts on flaky station Wi-Fi, genuine
offline capture is a headline feature that is entirely absent).

---

## Concern 6 — Env & Build Config

### 6.1 `.env.example` — clean, correctly scoped

`.env.example` documents `NEXT_PUBLIC_API_URL`, `_APP_NAME`, `_APP_ENV`, `_SENTRY_DSN`,
`_SENTRY_TRACES_SAMPLE_RATE`, with an explicit warning "NEXT_PUBLIC_ values are baked into
the client bundle, so never put secrets here." All listed envs are genuinely public-safe (no
secrets). No secret-leak via `NEXT_PUBLIC_` found. **Info (positive).**

### 6.2 Hard-coded localhost fallbacks — two of them

- `api.ts:7` → `http://localhost:8080` (see §2.1).
- `client.ts` has no fallback (it requires `baseURL`), so the only hardcode is in `api.ts`.
- `login-form.tsx:65` and `forgot-password/page.tsx:22` default `tenant_slug: 'demo'`. This
  is a dev convenience that **ships to production**: a real customer's login form pre-fills
  the tenant `demo`. Minor, but unprofessional and a small information leak (confirms a
  `demo` tenant exists). **Low.**

### 6.3 `next.config.ts` — minimal, missing hardening

`reactStrictMode: true` (good) and `transpilePackages` for the workspace packages (correct
and necessary). **Missing:** no `headers()` for security headers — no `Content-Security-Policy`
(critical given the `localStorage`-token model in §1.1, where CSP is the primary XSS
mitigation), no `X-Frame-Options`/`frame-ancestors` (clickjacking), no
`Strict-Transport-Security`, no `X-Content-Type-Options`. For a financial SaaS storing bearer
tokens in `localStorage`, the **absence of a CSP is a High-severity omission** — CSP is the
main thing standing between an injected script and token theft. **High.**

### 6.4 TypeScript config — strict, excellent baseline

`packages/config/tsconfig.base.json` enables `strict`, `noUncheckedIndexedAccess`,
`noImplicitOverride`, `noFallthroughCasesInSwitch`, `noUnusedLocals`, `noUnusedParameters`,
`verbatimModuleSyntax`, `forceConsistentCasingInFileNames`. This is a genuinely strong,
above-average config. The web `tsconfig.json` extends it correctly with the Next plugin and
`@/*` path alias. **Info (positive).**

---

## Concern 7 — Accessibility & UX Foundation

### 7.1 Semantic HTML — mostly good

- Root `<html lang="en">` is set (`layout.tsx:15`). **Good.**
- Auth layout uses `<main>` (`(auth)/layout.tsx:4`); dashboard uses `<aside>`/`<header>`/
  `<main>`/`<nav>` appropriately (`(dashboard)/layout.tsx`, `sidebar.tsx`, `topbar.tsx`,
  `settings/layout.tsx`). **Good.**
- Error/status messages use `role="alert"` (login, forgot, reset) and `role="status"`
  (profile success) — correct ARIA live semantics. **Good.**
- The command palette sets `Dialog.Title`/`Description` as `sr-only` (`command-palette.tsx:43-46`)
  — correct for an accessible dialog. **Good.**

### 7.2 Focus management — weak

- After login success, navigation is via `router.replace` with **no focus management** — focus
  is lost to `<body>`, so keyboard/SR users have no announced landing point. Same for the
  redirect flows. No skip-link to main content exists anywhere. No route-change focus reset
  (a known App Router a11y gap that apps must handle). **Medium.**
- The MFA field appears conditionally (`login-form.tsx:150-161`) but focus is **not** moved to
  it when it appears, so an SR user who submits and gets `mfa_required` has no indication a new
  required field materialized. **Medium.**
- `LoadingState` "Checking session…" in `ProtectedRoute` is good but isn't an `aria-live`
  region, so the transition isn't announced.

### 7.3 Theme — dark default, with a hydration nuance

`providers.tsx:55`: `defaultTheme="dark" enableSystem={false}` with `attribute="class"`, and
`layout.tsx:15` sets `suppressHydrationWarning` on `<html>` — the standard `next-themes`
setup, correct, and matches the "dark default" intent in `globals.css` (`.dark` tokens). The
topbar toggle (`topbar.tsx:50,96`) reads `(theme ?? resolvedTheme)`; since `enableSystem` is
false this is fine, but `resolvedTheme` is `undefined` on first server render, so the
toggle's icon could briefly mismatch before hydration — cosmetic. **Info.**

### 7.4 The native `<select>` station picker

`topbar.tsx:68-80` uses a raw `<select>` (with a correct `aria-label="Active station"`),
inconsistent with the radix/cmdk component system used elsewhere. Functional and accessible,
but stylistically off-system. **Low.**

---

## Concern 8 — General: Dead Code, Type Safety, Consistency

### 8.1 Placeholder routes that 404 — by design but user-hostile

`sidebar.tsx:34-56` lists 21 nav items; the file's own comment (`:58-62`) admits "Most entries
are visual placeholders … clicking them lands on a 404 today, which is intentional." Cross-
referencing the route listing, **Tanks, Pumps, Sales, Reports, Alerts, Assistant** (and others)
have sidebar links but **no `page.tsx`**. So a logged-in user clicking "Sales" or "Reports"
hits Next's default 404 (and there is **no custom `not-found.tsx`**, §3.3). Shipping a nav
that 404s ~6 of its entries with no custom 404 page is a real UX defect regardless of intent.
**Medium.**

### 8.2 `mfa/page.tsx` is a dead route

`(auth)/mfa/page.tsx` renders a "MFA enroll + verify UI lands in Stage 9" placeholder
(`:9-12`) and **nothing routes to it** — MFA is handled inline on the login form
(`login-form.tsx:150`). It's a reachable-by-URL dead page. **Low.**

### 8.3 Type safety — strong overall, isolated loose casts

The strict tsconfig (§6.4) plus a fully-typed SDK means very few `any`s. Loose spots:
- `providers.tsx:21` `(error as { status?: number } | null)` — safe but not narrowed via
  `instanceof SdkError`.
- `profile/page.tsx:78,113` `String((me.error as Error).message)` — casts unknown query error
  to `Error`; works because the SDK throws `Error` subclasses, but fragile.
- `me.data.user_id` / `tenant_id` are raw UUIDs rendered to the user (`profile/page.tsx:85,89`)
  because the `Me` type (`types.ts:20-25`) carries **no human-readable name/email** — the
  profile page literally shows the operator a bare UUID for "User." A data-contract gap that
  surfaces as poor UX. **Low.**

### 8.4 Loading / empty / error states — good *where present*, but uneven

`@fuelgrid/ui` provides `LoadingState`/`EmptyState`/`ErrorState`, and the profile page uses
all three correctly with retry (`profile/page.tsx:73-118`). However the **foundation routes
themselves** lack route-level `loading.tsx` files, so initial route transitions have no Suspense
fallback (the page mounts and each query shows its own spinner). The pattern is good
component-side, absent at the route/segment level. **Low/Medium (cross-ref 3.3).**

### 8.5 Zero frontend test coverage

Repo-wide search finds **no `*.test.*` / `*.spec.*` under `apps/web` or the SDK**. There are
**no tests** for: the SDK transport (including the previously-fixed `fetch.bind` regression,
which had — and still has — no test guarding it), `safeRedirect` (a security-relevant
function that *should* be unit-tested), the auth store hydration/persist logic, the
`ProtectedRoute` guard, the `usePermission` policy mirror, or any form. The brief's note that
the `fetch` binding bug shipped without test coverage generalizes: **the entire frontend
foundation is untested.** For security-relevant code (`safeRedirect`, token handling, the
guard), this is a **High**-leverage gap. **High.**

---

## Per-File Notes (quick reference)

- **`layout.tsx`** — clean; missing `viewport`/`themeColor`/`manifest` metadata (PWA, §5) and
  any `<meta>` security/CSP hooks (§6.3).
- **`page.tsx`** — blank-screen flash on `/` (§1.6); duplicates guard logic (§1.4).
- **`providers.tsx`** — strong query defaults (§3.1/3.2); no global error handling, Sentry
  never fed (§3.3); singleton never cleared on logout (§1.5/3.4).
- **`(auth)/*`** — solid forms, good `role=alert`; only login uses RHF/zod (§4.3); `mfa` is dead
  (§8.2); `tenant_slug: 'demo'` prefilled in prod (§6.2).
- **`(dashboard)/layout.tsx`** — guards correctly client-side (§1.4); no `error.tsx`/`loading.tsx`
  siblings (§3.3).
- **`settings/layout.tsx`** — fine; tab `active` logic correct.
- **`login-form.tsx`** — best file in scope: `safeRedirect` (§1.7), good error triage (§4.2),
  proper RHF/zod (§4.1). MFA field appears without focus move (§7.2).
- **`protected-route.tsx`** — correct hydration gate + `LoadingState`; client-only (§1.4); no
  expiry check (§1.2).
- **`sidebar.tsx`** — 6+ links 404 (§8.1).
- **`topbar.tsx`** — logout solid but doesn't clear query cache (§1.5); native `<select>`
  off-system (§7.4); no 401 awareness on the `stations` query.
- **`command-palette.tsx` / `right-panel.tsx`** — honest skeletons, accessible dialog; no
  functionality yet (acceptable placeholders).
- **`api.ts`** — correct `getToken`/bind; localhost fallback (§2.1); no 401 hook (§2.4).
- **`sentry.ts`** — correct lazy init; **never actually captures anything** (§3.3).
- **`auth-store.ts`** — `localStorage` token (§1.1); `expiresAt` dead (§1.2); no cross-tab
  logout broadcast.
- **`tenant-store.ts`** — fine; reset wired into logout (good).
- **`use-permissions.ts`** — well-designed; correctly documents "UX hints only, backend
  authoritative"; `enabled: Boolean(token)` avoids unauth fetch. Good.

---

## Findings Table

| ID | Severity | File:Line | Issue | Fix |
|---|---|---|---|---|
| WEB-001 | High | `stores/auth-store.ts:33-39` | Bearer JWT persisted to `localStorage` → full session theft on any XSS; no `httpOnly`-cookie path (`client.ts:189` `credentials:'omit'`) | Document risk + ship CSP/SRI now; plan migration to `httpOnly` cookie + server session |
| WEB-002 | High | (no `middleware.ts`) + `(dashboard)/layout.tsx:15` | Route protection is purely client-side; unauth users receive dashboard HTML/JS; guard duplicated in `page.tsx` | Add server middleware gate (requires a server-readable session signal; ties to WEB-001) |
| WEB-003 | High | `packages/sdk/src/client.ts:196-208` + `lib/api.ts` | Transport has **no 401 handling**; expired/revoked tokens don't force logout; user stuck "logged in" with broken pages | Add 401 interceptor / `QueryCache.onError` → `clearSession()` + redirect to `/login` |
| WEB-004 | High | `app/providers.tsx:9-31` (no `error.tsx`/`global-error.tsx`) | No global query/mutation error handling and **no React error boundary** anywhere; Sentry inited but never fed | Add `QueryCache`/`MutationCache` `onError` (→ Sentry + 401 logout) and `app/global-error.tsx` + segment `error.tsx` |
| WEB-005 | High | `apps/web` (filesystem) | "Offline-first PWA" claim is false: no manifest, no service worker, no `public/`, no PWA dep | Either implement (manifest + Serwist/Workbox + offline capture) or correct the product claims |
| WEB-006 | High | `next.config.ts:3-12` | No security headers — **no CSP**, no `X-Frame-Options`/`frame-ancestors`, no HSTS/`X-Content-Type-Options` (CSP is the primary XSS mitigation given WEB-001) | Add `headers()` with a strict CSP and the standard hardening headers |
| WEB-007 | High | `apps/web` + `packages/sdk` (no tests) | Zero frontend test coverage incl. security-relevant `safeRedirect`, token store, `ProtectedRoute`, SDK transport (the fixed `fetch.bind` bug still has no regression test) | Add unit tests for `safeRedirect`, auth store, guard, and SDK `request()`/401 paths |
| WEB-008 | Medium | `stores/auth-store.ts:27` + repo-wide | `expiresAt` stored but never read; no client expiry check; `client.ts:253 refresh()` never called → sessions silently rot | Check `expiresAt` in guard; schedule pre-expiry `refresh()`, `clearSession()` on failure |
| WEB-009 | Medium | `components/layout/topbar.tsx:45` + `providers.tsx:33` | Logout doesn't clear the module-singleton query cache → prior user's cached data can flash for the next login on the same tab | Call `queryClient.clear()` in `handleLogout` (or do a full reload) |
| WEB-010 | Medium | `app/providers.tsx` + ~40 pages | No central query-key factory; stringly-typed keys duplicated everywhere → fragile invalidation | Introduce a typed query-key factory module |
| WEB-011 | Medium | `(auth)/reset-password/page.tsx:26`, `profile/page.tsx:43`, settings/* | Only login uses RHF+zod; all other forms hand-rolled `useState`; password rules duplicated/inconsistently enforced (profile has no client check) | Standardize forms on RHF+zod with shared schemas |
| WEB-012 | Medium | `components/auth/login-form.tsx:85`, `protected-route.tsx`, route changes | No focus management on navigation / when MFA field appears; no skip-link | Move focus to `<main>`/MFA field on transitions; add skip-link |
| WEB-013 | Medium | `components/layout/sidebar.tsx:34-56` | ~6 nav items (Sales, Reports, Alerts, Pumps, Tanks, Assistant) link to routes with no `page.tsx` → default 404; no custom `not-found.tsx` | Gate placeholder links or add a custom `not-found.tsx` + "coming soon" pages |
| WEB-014 | Medium | `app/page.tsx:25` | Root `/` renders `null` then redirects → blank flash with no spinner | Render `LoadingState` like `ProtectedRoute` does |
| WEB-015 | Low | `lib/api.ts:7` | Hard-coded `http://localhost:8080` fallback baked into prod bundle if env unset; fails silently | Throw if `NEXT_PUBLIC_API_URL` unset in production |
| WEB-016 | Low | `login-form.tsx:65`, `forgot-password/page.tsx:22` | `tenant_slug: 'demo'` prefilled in production; minor info leak | Empty default outside dev |
| WEB-017 | Low | `packages/sdk/src/client.ts:201-203` | Error-message extraction assumes `{error: string}` envelope; structured/field validation errors degrade to `HTTP 4xx` | Handle `{message}`/field-error shapes; expose field errors to forms |
| WEB-018 | Low | `(auth)/mfa/page.tsx` | Dead route — nothing links to it; MFA handled inline on login | Remove or repurpose for Stage 9 |
| WEB-019 | Low | `profile/page.tsx:85,89` + `types.ts:20-25` | `Me` lacks name/email; profile shows raw UUIDs to the user | Extend `Me` contract with display name/email |
| WEB-020 | Low | `components/layout/topbar.tsx:68` | Native `<select>` station picker inconsistent with radix/cmdk system | Replace with system Select component |
| WEB-021 | Info | `lib/sentry.ts` | Sentry inits but no `captureException` call exists anywhere | Wire into the global error handler (WEB-004) |
| WEB-022 | Info | `components/auth/login-form.tsx:44-49` | `safeRedirect` open-redirect guard — correct, security-conscious (positive; needs a test, see WEB-007) | Keep; add unit test |

---

## Severity Counts

| Severity | Count |
|---|---:|
| Critical | 0 |
| High | 7 |
| Medium | 7 |
| Low | 6 |
| Info | 2 |
| **Total** | **22** |

> No *Critical* assigned: the `localStorage` token model and client-only routing are
> serious (High) but are industry-common, acknowledged in code comments, and do not by
> themselves leak data to an unauthenticated party. They become Critical-adjacent **in
> combination** with the missing CSP (WEB-006) — that pairing is the thing to fix first.

## Top 5 Risks

1. **No CSP + `localStorage` bearer token (WEB-006 × WEB-001)** — `next.config.ts:3-12` ships
   no Content-Security-Policy while `auth-store.ts:33` keeps the session token in
   `localStorage`. Any XSS = total session theft with nothing to stop the exfil. Fix CSP
   first; it's the cheapest, highest-leverage mitigation.
2. **No 401 handling in the data layer (WEB-003)** — `client.ts:196-208` never reacts to 401,
   so an expired/revoked session traps the user in a broken-but-"logged-in" state with no
   route back to login. Pervasive UX + security gap.
3. **No error boundaries / global error handling, Sentry never fed (WEB-004)** — `providers.tsx`
   has no `QueryCache.onError` and the tree has no `error.tsx`/`global-error.tsx`; a single
   render throw blanks the app in prod and is never reported.
4. **PWA / offline-first is entirely fictional (WEB-005)** — no manifest, service worker, or
   `public/` exist; a headline capability for flaky-connectivity field ops is absent.
5. **Purely client-side route protection (WEB-002)** — with no `middleware.ts`, the dashboard
   shell is served to anyone; real server enforcement is blocked by the `localStorage`-only
   model, making this the architectural knot to resolve alongside WEB-001. (Honorable mention:
   **zero frontend tests, WEB-007**, leaves all of the above unguarded against regression.)
