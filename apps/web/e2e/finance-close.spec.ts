import { test, expect, type Page } from '@playwright/test';

import { authedSession, json } from './helpers/journey';

/**
 * Finance period-close write-journey (QA-7): open the close checklist (all
 * blockers clear) then transition a period open → closing → closed → locked.
 * Each transition re-fetches the checklist; we flip the period's status so the
 * row's action button advances through the close states.
 */

const PERIOD_ID = 'per-1';

function checklist(periodStatus: string) {
  return {
    can_close: true,
    blockers: 0,
    checks: {
      unposted_cash_reconciliations: 0,
      open_deposits: 0,
      unmatched_bank_lines: 0,
      expenses_awaiting_posting: 0,
      unissued_customer_invoices: 0,
      open_payables: 2,
    },
    periods: [
      { id: PERIOD_ID, start_date: '2026-05-01', end_date: '2026-05-31', status: periodStatus },
    ],
  };
}

const NEXT_STATUS: Record<string, string> = {
  'start-close': 'closing',
  close: 'closed',
  lock: 'locked',
};

async function mockChecklist(page: Page, getState: () => string) {
  await page.route('**/api/bff/api/v1/finance/close-checklist', (route) =>
    json(route, checklist(getState())),
  );
}

test.describe('finance period close', () => {
  test('checklist → start close → close → lock', async ({ page }) => {
    await authedSession(page);

    let periodStatus = 'open';
    await mockChecklist(page, () => periodStatus);

    // POST /accounting-periods/:id/:action — derive the action from the URL tail.
    await page.route('**/api/bff/api/v1/accounting-periods/*/*', async (route) => {
      const url = route.request().url();
      const action = url.split('/').pop() ?? '';
      if (NEXT_STATUS[action]) periodStatus = NEXT_STATUS[action];
      await json(route, {
        id: PERIOD_ID,
        start_date: '2026-05-01',
        end_date: '2026-05-31',
        status: periodStatus,
      });
    });

    await page.goto('/finance/close');
    await expect(page.getByRole('heading', { name: 'Period close' })).toBeVisible();
    // Checklist is ready (no blocking checks).
    await expect(page.getByText('Ready to close')).toBeVisible();
    // The period row shows its open state.
    await expect(page.getByText('2026-05-01 → 2026-05-31')).toBeVisible();

    // --- START CLOSE ---
    await page.getByRole('button', { name: 'Start close' }).click();
    await expect(page.getByText('closing')).toBeVisible();

    // --- CLOSE ---
    await page.getByRole('button', { name: 'Close', exact: true }).click();
    await expect(page.getByText('closed', { exact: true })).toBeVisible();

    // --- LOCK --- enabled because can_close is true.
    const lockBtn = page.getByRole('button', { name: 'Lock' });
    await expect(lockBtn).toBeEnabled();
    await lockBtn.click();
    await expect(page.getByText('locked')).toBeVisible();
    // No further action once locked.
    await expect(page.getByRole('button', { name: 'Lock' })).toHaveCount(0);
  });
});
