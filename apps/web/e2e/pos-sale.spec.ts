import { test, expect } from '@playwright/test';

import { STATION, authedSession, json, paginated } from './helpers/journey';

/**
 * Point-of-sale write journey (Feature 4.1 / 4.4): an operator opens the till,
 * picks the open shift, enters a sale total, splits it across two tenders that
 * must sum exactly to the total, records it (two POST /shifts/{id}/payments),
 * and gets a printable receipt. The backend is fully mocked (see
 * helpers/journey.ts) — this proves the UI's split-validation, double-submit
 * guard, and receipt flow, not the server.
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

const SHIFT = {
  id: 'shift-1',
  tenant_id: STATION.tenant_id,
  station_id: STATION.id,
  operating_day_id: DAY.id,
  name: 'Morning',
  status: 'open',
  opened_by: 'u-1',
  opened_at: '2026-06-01T06:00:00Z',
  attendants: [],
  nozzle_assignments: [],
  expected_cash: '0.00',
  litres_sold: '0',
  exceptions: [],
  open_exception_count: 0,
};

const OVERVIEW = { station: STATION, day: DAY, shifts: [SHIFT] };

test('records a split-tender sale and shows a receipt', async ({ page }) => {
  await authedSession(page);

  // The POS gates on payment.record (station-scoped). Override the baseline
  // permission set for this spec so the till's Record button is enabled — the
  // shared helper's set deliberately stays minimal to avoid cross-spec drift.
  await page.route('**/api/bff/api/v1/me/permissions', (route) =>
    json(route, {
      tenant_wide: true,
      station_ids: [STATION.id],
      permissions: [{ code: 'payment.record', station_scoped: false }],
    }),
  );

  await page.route('**/api/bff/api/v1/stations/*/operations-overview', (route) =>
    json(route, OVERVIEW),
  );
  await page.route('**/api/bff/api/v1/products', (route) =>
    json(
      route,
      paginated([{ id: 'prod-1', code: 'PMS', name: 'Petrol', default_price: '180.00' }]),
    ),
  );
  await page.route('**/api/bff/api/v1/shifts/shift-1/payment-reconciliation', (route) =>
    json(route, {
      shift_id: 'shift-1',
      tendered: '0.00',
      recognized: '0.00',
      variance: '0.00',
      over_threshold: false,
    }),
  );
  await page.route('**/api/bff/api/v1/shifts/shift-1/payments?**', (route) =>
    json(route, paginated([])),
  );

  // Count tender posts to prove exactly two went out (one per split line).
  let posts = 0;
  await page.route('**/api/bff/api/v1/shifts/shift-1/payments', (route) => {
    if (route.request().method() !== 'POST') return route.continue();
    posts += 1;
    const body = JSON.parse(route.request().postData() ?? '{}');
    return json(
      route,
      {
        id: `pay-${posts}`,
        station_id: STATION.id,
        shift_id: 'shift-1',
        tender_type: body.tender_type,
        amount: body.amount,
        received_by: 'u-1',
        received_at: '2026-06-01T09:00:00Z',
        status: 'recorded',
      },
      201,
    );
  });

  await page.goto('/pos');

  await expect(page.getByRole('heading', { name: 'Point of sale' })).toBeVisible();

  // Enter the sale total and split it 70 + 30 across cash + card.
  await page.getByLabel('Sale total').fill('100.00');
  await page.getByLabel('Tender 1 amount').fill('70.00');

  const record = page.getByRole('button', { name: /record sale/i });
  await expect(record).toBeDisabled(); // under-tendered

  await page.getByRole('button', { name: /add tender/i }).click();
  await page.getByLabel('Tender 2 method').selectOption('card');
  await page.getByLabel('Tender 2 amount').fill('30.00');

  await expect(record).toBeEnabled(); // balanced
  await record.click();

  // Receipt appears; two tenders were posted.
  await expect(page.getByRole('dialog', { name: /sale receipt/i })).toBeVisible();
  expect(posts).toBe(2);
});
