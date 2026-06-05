import { test, expect, type Page } from '@playwright/test';

/**
 * Auth flow e2e smoke (QA-7 / CICD-3) — httpOnly-cookie BFF (WEB-001 / Wave-10).
 *
 * The BACKEND IS NEVER REAL. The browser now talks to the same-origin BFF proxy
 * (`/api/bff/**`) instead of the Go API directly, so the mocks intercept those
 * proxy URLs. The proxy normally forwards server-side and moves the token into
 * an httpOnly cookie; here the mock short-circuits that by returning the
 * already-stripped body PLUS a `Set-Cookie: fg_session=...; HttpOnly` header,
 * exactly as the real proxy would. The three load-bearing transitions:
 *
 *   1. unauthenticated visit to a protected route -> redirect to /login
 *      (Next middleware reads the httpOnly `fg_session` cookie, absent -> bounce)
 *   2. successful login (mock 200 + Set-Cookie, then mock /me) -> command center
 *   3. a 401 on a protected fetch -> session-expiry redirect back to /login
 *      (the BFF clears the cookie on 401; the SDK backstop clears the client
 *       hint + redirects)
 *
 * The session contract under test:
 *   - the token lives ONLY in the httpOnly `fg_session` cookie — never in
 *     localStorage and never in a client-readable cookie;
 *   - the non-sensitive `authed` hint in localStorage drives the client guards;
 *   - any 401 from the SDK transport clears the session and bounces to /login.
 */

const LOGIN = {
  tenant: 'demo',
  email: 'demo@fuelgrid.local',
  password: 'e2e-only-password',
};

const SESSION = 'e2e-mock-session-token';

/** A /me body that satisfies the SDK's runtime meSchema (SDK-01). */
const ME_BODY = {
  user_id: '11111111-1111-1111-1111-111111111111',
  tenant_id: '22222222-2222-2222-2222-222222222222',
  session_id: '33333333-3333-3333-3333-333333333333',
  mfa_satisfied: true,
};

/**
 * Mock the BFF login endpoint. The real proxy strips the token from the body
 * and sets it in an httpOnly cookie; we reproduce both: the body carries no
 * token, and a Set-Cookie plants `fg_session` as HttpOnly so the Next
 * middleware (which runs server-side) gates protected routes on it.
 */
async function mockLoginSuccess(page: Page) {
  await page.route('**/api/bff/api/v1/auth/login', async (route) => {
    await route.fulfill({
      status: 200,
      contentType: 'application/json',
      headers: {
        'X-Request-Id': 'e2e-login',
        'Set-Cookie': `fg_session=${SESSION}; Path=/; HttpOnly; SameSite=Lax`,
      },
      body: JSON.stringify({
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
    // No fg_session cookie -> Next middleware should server-side redirect.
    await page.goto('/command-center');

    await expect(page).toHaveURL(/\/login(\?|$)/);
    // The login surface is rendered.
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();
    // The intended destination is preserved for post-login navigation.
    expect(new URL(page.url()).searchParams.get('next')).toBe('/command-center');
  });

  test('successful login lands on the command center', async ({ page }) => {
    await mockLoginSuccess(page);
    // The command center fetches /me on mount via the BFF; return a valid body.
    await page.route('**/api/bff/api/v1/me', async (route) => {
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
    // The flagship Executive Command Center renders its hero immediately,
    // independent of the (here-unmocked) dashboard data fan-out.
    await expect(
      page.getByRole('heading', { name: 'How is my fuel business performing right now?' }),
    ).toBeVisible();
    await expect(page.getByText('Executive command center')).toBeVisible();

    // The session token lives in an httpOnly cookie — present, and NOT
    // exposed to client JS.
    const cookies = await page.context().cookies();
    const session = cookies.find((c) => c.name === 'fg_session');
    expect(session?.value).toBe(SESSION);
    expect(session?.httpOnly).toBe(true);
    // It must never be readable from document.cookie.
    const readable = await page.evaluate(() => document.cookie);
    expect(readable).not.toContain('fg_session');
    // And the legacy localStorage token must be gone entirely.
    const lsToken = await page.evaluate(() => {
      const raw = window.localStorage.getItem('fuelgrid.auth');
      return raw ?? '';
    });
    expect(lsToken).not.toContain(SESSION);
  });

  test('a 401 on a protected fetch forces redirect back to /login (session expiry)', async ({
    page,
  }) => {
    await mockLoginSuccess(page);

    // First /me succeeds so login lands on the command center...
    let meCalls = 0;
    await page.route('**/api/bff/api/v1/me', async (route) => {
      meCalls += 1;
      if (meCalls === 1) {
        await route.fulfill({
          status: 200,
          contentType: 'application/json',
          body: JSON.stringify(ME_BODY),
        });
        return;
      }
      // ...then the session "expires": a later /me returns 401. The real BFF
      // also clears the cookie on a 401; reproduce that with an expiring
      // Set-Cookie so the middleware no longer sees a session.
      await route.fulfill({
        status: 401,
        contentType: 'application/json',
        headers: { 'Set-Cookie': 'fg_session=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0' },
        body: JSON.stringify({ error: 'unauthorized' }),
      });
    });

    await page.goto('/login');
    await submitLogin(page);
    await expect(page).toHaveURL(/\/command-center(\?|$)/);

    // Force a re-fetch of /me (now 401): a reload re-runs the query.
    await page.evaluate(() => window.location.reload());

    // The 401 backstop clears the session and bounces to /login.
    await expect(page).toHaveURL(/\/login(\?|$)/);
    await expect(page.getByRole('button', { name: 'Sign in' })).toBeVisible();

    // Session was cleared: the httpOnly cookie is gone.
    const cookies = await page.context().cookies();
    expect(cookies.find((c) => c.name === 'fg_session')?.value).toBeFalsy();
  });
});
