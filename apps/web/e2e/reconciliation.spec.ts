import { test, expect, type Page } from '@playwright/test';

import { STATION, authedSession, json } from './helpers/journey';

/**
 * Reconciliation write-journey (QA-7): run a tank reconciliation, record a
 * reasoned adjustment that pulls the variance back within tolerance, then seal.
 * Each mutation re-fetches the reconciliation-overview; we flip the mocked
 * state so the UI shows: not-run → over-tolerance draft → within-tolerance →
 * sealed.
 */

const DAY = {
  id: 'day-1',
  tenant_id: STATION.tenant_id,
  station_id: STATION.id,
  business_date: '2026-06-01',
  status: 'open',
  opened_by: 'u-1',
  opened_at: '2026-06-01T05:00:00Z',
};

const TANK = {
  id: 'tank-1',
  tenant_id: STATION.tenant_id,
  station_id: STATION.id,
  product_id: 'prod-1',
  name: 'Tank 1',
  code: 'T1',
  capacity_litres: '50000.000',
  safe_min_litres: '1000.000',
};

function recon(extra: Record<string, unknown>) {
  return {
    id: 'rec-1',
    tank_id: TANK.id,
    operating_day_id: DAY.id,
    opening_book: '10000.000',
    deliveries_total: '0.000',
    sales_total: '5000.000',
    adjustments_total: '0.000',
    closing_book: '5000.000',
    closing_physical: '4900.000',
    variance_litres: '-100.000',
    variance_percent: '-2.00',
    tolerance_percent: '0.50',
    over_tolerance: true,
    status: 'exception',
    ...extra,
  };
}

function overview(tank: { reconciliation?: unknown }) {
  return {
    station: STATION,
    day: DAY,
    all_shifts_approved: true,
    tanks: [{ tank: TANK, book_balance: '5000.000', latest_physical: 4900, ...tank }],
  };
}

async function mockOverview(page: Page, getState: () => unknown) {
  await page.route('**/api/bff/api/v1/stations/*/reconciliation-overview**', (route) =>
    json(route, getState()),
  );
}

test.describe('reconciliation', () => {
  test('run → adjust (with reason) → seal', async ({ page }) => {
    await authedSession(page);

    // Start: not yet run.
    let state = overview({});
    await mockOverview(page, () => state);

    // POST run -> a draft over tolerance.
    await page.route('**/api/bff/api/v1/tanks/*/reconciliations', async (route) => {
      const r = recon({});
      state = overview({ reconciliation: r });
      await json(route, r);
    });

    // POST adjustment (carries the reason) -> variance pulled within tolerance.
    let adjustBody: { litres?: number; reason?: string } = {};
    await page.route('**/api/bff/api/v1/reconciliations/*/adjustments', async (route) => {
      adjustBody = route.request().postDataJSON();
      const r = recon({
        adjustments_total: '100.000',
        closing_book: '4900.000',
        variance_litres: '0.000',
        variance_percent: '0.00',
        over_tolerance: false,
        status: 'draft',
      });
      state = overview({ reconciliation: r });
      await json(route, r);
    });

    // POST seal -> sealed.
    await page.route('**/api/bff/api/v1/reconciliations/*/seal', async (route) => {
      const r = recon({
        adjustments_total: '100.000',
        variance_litres: '0.000',
        over_tolerance: false,
        status: 'sealed',
        sealed_at: '2026-06-01T15:00:00Z',
      });
      state = overview({ reconciliation: r });
      await json(route, r);
    });

    await page.goto('/reconciliation');
    await expect(page.getByText(`Operating day · ${DAY.business_date}`)).toBeVisible();
    await expect(page.getByText('shifts approved')).toBeVisible();

    // --- RUN --- (enabled because all_shifts_approved)
    const runBtn = page.getByRole('button', { name: 'Run reconciliation' });
    await expect(runBtn).toBeEnabled();
    await runBtn.click();

    // Now over tolerance: the seal button is blocked, the variance badge is danger.
    await expect(page.getByText('over tolerance')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Resolve variance to seal' })).toBeDisabled();

    // --- ADJUST --- litres + reason both required to enable Add.
    const addBtn = page.getByRole('button', { name: 'Add' });
    await expect(addBtn).toBeDisabled();
    await page.getByPlaceholder('± litres').fill('100');
    await expect(addBtn).toBeDisabled(); // reason still empty
    await page.getByPlaceholder('Reason (e.g. evaporation, leak)').fill('evaporation');
    await expect(addBtn).toBeEnabled();
    await addBtn.click();

    // The reason rode along on the request, and the variance is now within tolerance.
    await expect.poll(() => adjustBody.reason).toBe('evaporation');
    expect(adjustBody.litres).toBe(100);
    await expect(page.getByText('within tolerance')).toBeVisible();

    // --- SEAL --- now enabled.
    const sealBtn = page.getByRole('button', { name: 'Seal reconciliation' });
    await expect(sealBtn).toBeEnabled();
    await sealBtn.click();
    await expect(page.getByText('sealed', { exact: true })).toBeVisible();
    // The adjustment controls are gone once sealed.
    await expect(page.getByRole('button', { name: 'Add' })).toHaveCount(0);
  });
});
