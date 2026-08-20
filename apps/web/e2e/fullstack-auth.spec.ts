import { expect, test } from '@playwright/test';

const isFullStack = process.env.FULLSTACK_E2E === '1';

test.describe('real-stack authentication', () => {
  test.skip(!isFullStack, 'Runs only in the full-stack E2E job.');

  test('logs in through the BFF and reads the real authenticated identity', async ({ page }) => {
    const password = process.env.DEMO_USER_PASSWORD;
    expect(password, 'DEMO_USER_PASSWORD must be set by the full-stack job').toBeTruthy();

    await page.goto('/login');
    await page.getByLabel('Tenant').fill('demo');
    await page.getByLabel('Email').fill('demo@fuelgrid.local');
    await page.getByLabel('Password').fill(password!);
    await page.getByRole('button', { name: 'Sign in' }).click();

    await expect(page).toHaveURL(/\/command-center(\?|$)/);
    await expect(
      page.getByRole('heading', { name: 'How is my fuel business performing right now?' }),
    ).toBeVisible();

    const session = (await page.context().cookies()).find((cookie) => cookie.name === 'fg_session');
    expect(session?.httpOnly).toBe(true);
    expect(session?.value).toBeTruthy();

    // Use the browser fetch path here. Chromium treats loopback as a secure
    // context and sends the production Secure cookie, while Playwright's
    // standalone request client correctly refuses Secure cookies over HTTP.
    const meResponse = await page.evaluate(async () => {
      const response = await fetch('/api/bff/api/v1/me', { credentials: 'same-origin' });
      return {
        status: response.status,
        body: (await response.json()) as Record<string, unknown>,
      };
    });
    expect(meResponse.status).toBe(200);
    const me = meResponse.body;
    expect(me.user_id).toBeTruthy();
    expect(me.tenant_id).toBeTruthy();

    const readableCookies = await page.evaluate(() => document.cookie);
    expect(readableCookies).not.toContain('fg_session');
  });
});
