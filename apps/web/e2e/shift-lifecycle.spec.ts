import { test, expect, type Page } from '@playwright/test';

import { STATION, authedSession, json } from './helpers/journey';

/**
 * Shift lifecycle write-journey (QA-7): a supervisor opens a shift for the
 * scheduled team, the attendant captures meter + dip readings, then the
 * supervisor closes and approves it — each step a mocked API call plus the
 * follow-up operations-overview GET that re-renders the new state.
 *
 * The Operations page (open/close/approve) and the My Shift attendant console
 * (capture readings) are the two surfaces; we drive both.
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

const TEAM = {
  id: 'team-1',
  tenant_id: STATION.tenant_id,
  station_id: STATION.id,
  name: 'Team A',
  rotation_order: 0,
  member_count: 1,
};

const SCHEDULED_TEAM = {
  date: DAY.business_date,
  slot: 'morning',
  team: TEAM,
  members: [
    {
      id: 'emp-1',
      tenant_id: STATION.tenant_id,
      station_id: STATION.id,
      full_name: 'Ada Attendant',
      role: 'pump_attendant',
      status: 'active',
      team_id: TEAM.id,
      created_at: '2026-01-01T00:00:00Z',
      updated_at: '2026-01-01T00:00:00Z',
    },
  ],
};

function shift(status: string, extra: Record<string, unknown> = {}) {
  return {
    id: 'shift-1',
    tenant_id: STATION.tenant_id,
    station_id: STATION.id,
    operating_day_id: DAY.id,
    name: 'Morning',
    status,
    slot: 'morning',
    team_id: TEAM.id,
    opened_by: 'u-1',
    opened_at: '2026-06-01T06:00:00Z',
    attendants: [{ user_id: 'u-2', full_name: 'Ada Attendant', email: 'ada@demo.local' }],
    nozzle_assignments: [],
    expected_cash: '1000.00',
    litres_sold: '500.000',
    cash_submission: null,
    exceptions: [],
    open_exception_count: 0,
    ...extra,
  };
}

function overview(shifts: ReturnType<typeof shift>[]) {
  return { station: STATION, day: DAY, shifts };
}

/** Route the operations-overview GET to whatever the current `state` is. */
async function mockOperationsOverview(page: Page, getState: () => unknown) {
  await page.route('**/api/bff/api/v1/stations/*/operations-overview', (route) =>
    json(route, getState()),
  );
}

test.describe('shift lifecycle', () => {
  test('open → close → approve via the operations console', async ({ page }) => {
    await authedSession(page);

    // The overview GET is re-fetched after every mutation; flip the state.
    let state = overview([]); // day open, no shifts yet
    await mockOperationsOverview(page, () => state);

    // Scheduled team for the morning slot — the "Open shift" button is disabled
    // until this resolves a team.
    await page.route('**/api/bff/api/v1/stations/*/scheduled-team**', (route) =>
      json(route, SCHEDULED_TEAM),
    );

    // POST open shift -> next overview shows one open shift.
    let openShiftCalls = 0;
    await page.route('**/api/bff/api/v1/stations/*/shifts', async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      openShiftCalls += 1;
      state = overview([shift('open')]);
      await json(route, shift('open'));
    });

    // POST close -> overview shows the shift closed with a submitted cash split.
    await page.route('**/api/bff/api/v1/shifts/*/close', async (route) => {
      state = overview([
        shift('closed', {
          cash_submission: {
            id: 'cs-1',
            shift_id: 'shift-1',
            expected_cash: '1000.00',
            cash_amount: '1000.00',
            mobile_money_amount: '0.00',
            card_amount: '0.00',
            credit_amount: '0.00',
            submitted_total: '1000.00',
            variance: '0.00',
            submitted_by: 'u-2',
            submitted_at: '2026-06-01T14:00:00Z',
          },
        }),
      ]);
      await json(route, {
        shift: shift('closed'),
        lines: [],
        expected_cash: '1000.00',
        cash_submission: null,
      });
    });

    // PATCH status=approved -> overview shows it approved.
    await page.route('**/api/bff/api/v1/shifts/*/status', async (route) => {
      state = overview([shift('approved')]);
      await json(route, shift('approved'));
    });

    await page.goto('/operations');

    // The operating day card is up and the scheduled team is shown.
    await expect(page.getByText(`Operating day · ${DAY.business_date}`)).toBeVisible();
    await expect(page.getByText('Team A')).toBeVisible();

    // --- OPEN ---
    const nameInput = page.getByPlaceholder('New shift name (e.g. Morning)');
    const openBtn = page.getByRole('button', { name: 'Open shift' });
    // Disabled until a name is typed.
    await expect(openBtn).toBeDisabled();
    await nameInput.fill('Morning');
    await expect(openBtn).toBeEnabled();
    await openBtn.click();

    // The shift card appears; its open state is proven by the Close button
    // being present (only rendered when status === 'open').
    await expect(page.getByRole('heading', { name: 'Morning' })).toBeVisible();
    await expect.poll(() => openShiftCalls).toBe(1);

    // --- CLOSE ---
    const closeBtn = page.getByRole('button', { name: 'Close shift' });
    await expect(closeBtn).toBeVisible();
    await closeBtn.click();
    // Closed badge + the submitted cash now renders (variance 0.00).
    await expect(page.getByText('closed', { exact: true })).toBeVisible();

    // --- APPROVE --- enabled because open_exception_count === 0.
    const approveBtn = page.getByRole('button', { name: 'Approve shift' });
    await expect(approveBtn).toBeEnabled();
    await approveBtn.click();
    await expect(page.getByText('approved', { exact: true })).toBeVisible();
    // The lifecycle buttons are gone once approved.
    await expect(page.getByRole('button', { name: 'Approve shift' })).toHaveCount(0);
  });

  test('attendant captures a meter reading on My Shift', async ({ page }) => {
    await authedSession(page);

    // The attendant console fetches /me/active-shift. Start with an opening
    // reading still to capture, then re-fetch shows it captured.
    const nozzle = {
      nozzle_id: 'noz-1',
      pump_number: 1,
      nozzle_number: 1,
      product_name: 'Diesel',
      product_color: '#123456',
      tank_id: 'tank-1',
      tank_code: 'T1',
      default_price: 1.5,
      meter_decimal_places: 3,
    };
    let active: Record<string, unknown> = {
      shift: { id: 'shift-1', status: 'open', name: 'Morning' },
      assigned_nozzles: [{ ...nozzle }],
      assigned_tanks: [],
      expected_cash: '0.00',
    };
    await page.route('**/api/bff/api/v1/me/active-shift', (route) => json(route, active));

    let captured = 0;
    await page.route('**/api/bff/api/v1/shifts/*/meter-readings', async (route) => {
      if (route.request().method() !== 'POST') return route.fallback();
      captured += 1;
      // After capture the re-fetch shows the opening reading as a fixed figure.
      active = {
        ...active,
        assigned_nozzles: [{ ...nozzle, opening_reading: 1000 }],
      };
      await json(route, {
        id: 'mr-1',
        shift_id: 'shift-1',
        nozzle_id: 'noz-1',
        reading_type: 'opening',
        reading: '1000',
        status: 'active',
      });
    });

    await page.goto('/my-shift');
    await expect(page.getByRole('heading', { name: 'My Shift' })).toBeVisible();
    await expect(page.getByText('Diesel')).toBeVisible();

    // The opening row has an input + a Save button (disabled until a value).
    const saveBtn = page.getByRole('button', { name: 'Save' }).first();
    await expect(saveBtn).toBeDisabled();
    // Fill the opening reading input (first number input on the card).
    await page.getByPlaceholder('0').first().fill('1000');
    await expect(saveBtn).toBeEnabled();
    await saveBtn.click();

    // After the POST + re-fetch the captured figure renders read-only (no input).
    await expect.poll(() => captured).toBe(1);
    await expect(page.getByText('1,000')).toBeVisible();
  });
});
