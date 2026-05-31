import { test, expect, type Page } from '@playwright/test';

/**
 * Auth flow e2e smoke (QA-7 / CICD-3).
 *
 * The BACKEND IS NEVER REAL: every `/api/**` call is intercepted with
 * Playwright route mocking, so this runs in CI with no Postgres/Redis/API.
 * It exercises the three load-bearing auth transitions end-to-end through the
 * real Next.js production bundle:
 *
 *   1. unauthenticated visit to a protected route -> redirect to /login
 *   2. successful login (mock 200 + token, then mock /me) -> command center
 *   3. a 401 on a protected fetch -> session-expiry redirect back to /login
 *
 * The session contract under test (see stores/auth-store.ts + middleware.ts):
 *   - login success sets the non-httpOnly `fg_authed=1` presence cookie that
 *     the Next middleware reads to server-side gate protected routes, and the
 *     token in localStorage that the SDK client attaches to API calls;
 *   - any 401 from the SDK transport clears that session and bounces to
 *     /login (the SEC-3 logout backstop in lib/api.ts).
 */

const LOGIN = {
  tenant: 'demo',
  email: 'demo@fuelgrid.local',
  password: 'fuelgrid-demo-password-1234',
};

const TOKEN = 'e2e-mock-session-token';

/** A /me body that satisfies the SDK's runtime meSchema (SDK-01). */
const ME_BODY = {
  user_id: '11111111-1111-1111-1111-111111111111',
  tenant_id: '22222222-2222-2222-2222-222222222222',
  session_id: '33333333-3333-3333-3333-333333333333',
  mfa_satisfied: true,
};

/** Mock the login endpoint to return a 200 with a token (no MFA). */
async function mockLoginSuccess(page: Page) {
  await page.route('**/api/v1/auth/login', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: { 'X-Request-Id': 'e2e-login' },
      body: JSON.stringify({
        token: TOKEN,
        expires_at: '2099-01-01T00:00:00Z',
        mfa_required: false,
      }),
    });
  });
}

/** Fill and submit the login form. */
async function submitLogin(page: Page) {
  await page.getByLabel('Tenant').fill(LOGIN.tenant);
  await page.getByLabel('Email').fill(LOGIN.email);
  await page.getByLabel('Password').fill(LOGIN.password);
  await page.getByRole('button', { name: 'Sign in' }).click();
}

test.describe('auth smoke', () => {
  test('unauthenticated visit to a protected route redirects to /login', async ({ page }) => {
    // No fg_authed cookie -> Next middleware should server-side redirect.
    await page.goto('/command-center');

    await expect(page).toHaveURL(/\/login(\?|$)/);
    // The login surface is rendered.
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
    // The intended destination is preserved for post-login navigation.
    expect(new URL(page.url()).searchParams.get('next')).toBe('/command-center');
  });

  test('successful login lands on the command center', async ({ page }) => {
    await mockLoginSuccess(page);
    // The command center fetches /me on mount; return a schema-valid body.
    await page.route('**/api/v1/me', async (route) => {
      await route.fulfill({
        status: 200,
        contentType: 'application/json',
        headers: { 'X-Request-Id': 'e2e-me' },
        body: JSON.stringify(ME_BODY),
      });
    });

    await page.goto('/login');
    await submitLogin(page);

    // Login success -> router.replace(safeRedirect(next)) -> /command-center.
    await expect(page).toHaveURL(/\/command-center(\?|$)/);
    await expect(page.getByRole('heading', { name: 'Command Center' })).toBeVisible();
    // /me resolved -> the Session card renders the mocked identity.
    await expect(page.getByText(ME_BODY.user_id)).toBeVisible();

    // The presence cookie the middleware relies on was set by login.
    const cookies = await page.context().cookies();
    expect(cookies.find((c) => c.name === 'fg_authed')?.value).toBe('1');
  });

  test('a 401 on a protected fetch forces redirect back to /login (session expiry)', async ({
    page,
  }) => {
    await mockLoginSuccess(page);

    // First /me succeeds so login lands on the command center...
    let meCalls = 0;
    await page.route('**/api/v1/me', async (route) => {
      meCalls += 1;
      if (meCalls === 1) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(ME_BODY),
        });
        return;
      }
      // ...then the session "expires": a later /me returns 401, which the SDK
      // transport turns into a logout + redirect to /login (SEC-3).
      await route.fulfill({
        status: 401,
        contentType: 'application/json',
        body: JSON.stringify({ error: 'unauthorized' }),
      });
    });

    await page.goto('/login');
    await submitLogin(page);
    await expect(page).toHaveURL(/\/command-center(\?|$)/);

    // Force a re-fetch of /me (now 401). React Query refetches on window focus;
    // a navigation back into the route re-runs the query.
    await page.evaluate(() => window.location.reload());

    // The 401 backstop clears the session and bounces to /login.
    await expect(page).toHaveURL(/\/login(\?|$)/);
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();

    // Session was cleared: the presence cookie is gone.
    const cookies = await page.context().cookies();
    expect(cookies.find((c) => c.name === 'fg_authed')?.value).not.toBe('1');
  });
});
