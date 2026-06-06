import { expect, type Page, type Route } from '@playwright/test';

/**
 * Shared helpers for the write-journey e2e specs (QA-7).
 *
 * The BACKEND IS NEVER REAL. Every spec intercepts the same-origin BFF proxy
 * (`/api/bff/api/v1/**`) with Playwright route mocking and returns bodies
 * shaped like the real Go DTOs (see packages/sdk/src/types.ts). The mocks are
 * deterministic — no Postgres/Redis/API — so the specs prove the UI logic, not
 * the server. The pattern mirrors e2e/auth-smoke.spec.ts.
 *
 * Each journey:
 *   1. authenticates (cookie + the `authed` localStorage hint the client guards
 *      read — both are needed: middleware gates on the httpOnly cookie, the
 *      client ProtectedRoute gates on the hint);
 *   2. grants the actor every permission so PermissionGate-wrapped controls are
 *      enabled (the backend stays authoritative in prod — here it's mocked);
 *   3. silences the dashboard chrome (notifications) so its background fetches
 *      don't 404-noise the run;
 *   4. drives the ACTUAL UI controls and asserts state transitions.
 */

export const SESSION = 'e2e-mock-session-token';

/** A /me body that satisfies the SDK's runtime meSchema (SDK-01). */
export const ME_BODY = {
  user_id: '11111111-1111-1111-1111-111111111111',
  tenant_id: '22222222-2222-2222-2222-222222222222',
  session_id: '33333333-3333-3333-3333-333333333333',
  mfa_satisfied: true,
};

const LOGIN = {
  tenant: 'demo',
  email: 'demo@fuelgrid.local',
  password: 'e2e-only-password',
};

/** Reusable station the journeys operate on. */
export const STATION = {
  id: 'st-1111-1111-1111-111111111111',
  tenant_id: ME_BODY.tenant_id,
  company_id: 'co-1',
  name: 'Demo Station',
  code: 'DS1',
  timezone: 'UTC',
  status: 'active',
};

export function paginated<T>(items: T[]) {
  return { items, count: items.length, limit: items.length, offset: 0, has_more: false };
}

/** Convenience: fulfill a route with a JSON body + the X-Request-Id the SDK reads. */
export async function json(route: Route, body: unknown, status = 200) {
  await route.fulfill({
    status,
    contentType: 'application/json',
    headers: { 'X-Request-Id': 'e2e' },
    body: JSON.stringify(body),
  });
}

/**
 * The full permission set with every code the journeys touch. They are marked
 * station_scoped:false so usePermission returns true immediately regardless of
 * whether the calling PermissionGate passed a stationId — some gates (e.g.
 * Teams' station.manage) intentionally omit it. tenant_wide is also set so the
 * station-scoped branch would pass too. The backend stays authoritative in
 * prod; here permissions are mocked purely to un-gate the controls.
 */
export function permissionsBody() {
  const codes = [
    'operations.manage_day',
    'shift.open',
    'shift.close',
    'shift.approve',
    'shift.assign',
    'reading.override',
    'cash.override',
    'station.manage',
    'station.read',
    'period.close',
    'period.lock',
    'revenue.read',
    'inventory.read',
    'reconciliation.read',
    'finance.read',
    'customer.read',
  ];
  return {
    tenant_wide: true,
    station_ids: [STATION.id],
    permissions: codes.map((code) => ({ code, station_scoped: false })),
  };
}

/**
 * Mock the auth + dashboard-chrome endpoints every protected page needs:
 * login, /me, /me/permissions, and the notification bell's background polls.
 * Returns nothing; call before navigating to a protected route.
 */
export async function mockBaseline(page: Page) {
  // Lowest-priority catch-all (Playwright matches the most-recently-registered
  // route first, so this — registered first — only fires when nothing more
  // specific matches). It keeps any un-mocked v1 GET from hanging on the
  // unreachable upstream: the flagship Command Center landing page fans out
  // across many reads, and a spec only mocks the routes its own page needs, so
  // the rest must fail fast (aborted) instead of leaving the page's network
  // pending and blocking the next navigation. Non-GETs fall through so a
  // spec's own mutation mocks (always more specific) stay authoritative.
  await page.route('**/api/bff/api/v1/**', (route) =>
    route.request().method() === 'GET' ? route.abort() : route.continue(),
  );
  await page.route('**/api/bff/api/v1/auth/login', (route) =>
    route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: {
        'X-Request-Id': 'e2e-login',
        'Set-Cookie': `fg_session=${SESSION}; Path=/; HttpOnly; SameSite=Lax`,
      },
      body: JSON.stringify({ expires_at: '2099-01-01T00:00:00Z', mfa_required: false }),
    }),
  );
  await page.route('**/api/bff/api/v1/me', (route) => json(route, ME_BODY));
  await page.route('**/api/bff/api/v1/me/permissions', (route) => json(route, permissionsBody()));
  await page.route('**/api/bff/api/v1/notifications/unread-count', (route) =>
    json(route, { unread_count: 0 }),
  );
  await page.route('**/api/bff/api/v1/notifications**', (route) => json(route, paginated([])));
}

/** Fill + submit the login form (same selectors as auth-smoke). */
export async function login(page: Page) {
  await page.goto('/login');
  await page.getByLabel('Tenant').fill(LOGIN.tenant);
  await page.getByLabel('Email').fill(LOGIN.email);
  await page.getByLabel('Password').fill(LOGIN.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/command-center(\?|$)/);
}

/**
 * Mock the station list (the topbar + every journey page fetches it) and log in.
 * After this the dashboard chrome is up; the caller mocks its journey routes
 * then navigates to the page under test.
 */
export async function authedSession(page: Page) {
  await mockBaseline(page);
  await page.route('**/api/bff/api/v1/stations', (route) => json(route, paginated([STATION])));
  await page.route('**/api/bff/api/v1/stations?**', (route) => json(route, paginated([STATION])));
  await login(page);
}
