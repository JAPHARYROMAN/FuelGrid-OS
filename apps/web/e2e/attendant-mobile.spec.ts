import { expect, test } from '@playwright/test';

import { json, mockBaseline } from './helpers/journey';

const SNAPSHOT = {
  status: 'on_shift',
  next_action: 'check_in',
  user_message: 'Your shift is open. Check in to start working.',
  station: { id: 'station-1', name: 'Itemba Mpemba' },
  shift: {
    id: 'shift-1',
    tenant_id: 'tenant-1',
    station_id: 'station-1',
    operating_day_id: 'day-1',
    name: 'Day',
    status: 'open',
    opened_by: 'manager-1',
    opened_at: '2026-08-21T05:00:00Z',
    slot: 'morning',
  },
  attendance: { status: 'not_checked_in' },
  assignments: [
    {
      assignment_id: 'assignment-1',
      nozzle_id: 'nozzle-1',
      pump_number: 1,
      nozzle_number: 1,
      product_name: 'Petrol',
      product_color: '#ef4444',
      meter_decimal_places: 2,
      assigned_at: '2026-08-21T05:00:00Z',
    },
  ],
  readings: [],
  expected_openings_available: false,
};

test('attendant installs, signs in, renders without overflow, and can sign out', async ({
  page,
}) => {
  await mockBaseline(page);
  await page.route('**/api/bff/api/v1/me/permissions', (route) =>
    json(route, {
      permissions: [
        'cash.submit',
        'incidents.report',
        'payment.record',
        'pricing.read',
        'reading.edit',
        'shift.open',
      ].map((code) => ({ code, station_scoped: true })),
      station_ids: ['station-1'],
      roles: ['attendant'],
      tenant_wide: false,
      is_system_admin: false,
      is_attendant_only: true,
    }),
  );
  await page.route('**/api/bff/api/v1/attendant/current-shift', (route) => json(route, SNAPSHOT));
  await page.route('**/api/bff/api/v1/auth/logout', (route) => route.fulfill({ status: 204 }));

  await page.goto('/attendant');
  await expect(page).toHaveURL(/\/login\?next=%2Fattendant$/);
  await page.getByLabel('Tenant').fill('demo');
  await page.getByLabel('Email').fill('attendant@example.com');
  await page.getByLabel('Password').fill('e2e-only-password');
  await page.getByRole('button', { name: 'Sign in' }).click();

  await expect(page).toHaveURL(/\/attendant$/);
  await expect(page.getByText('Itemba Mpemba')).toBeVisible();
  await expect(page.getByRole('button', { name: /check in/i })).toBeVisible();
  expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
    true,
  );

  await page.goto('/command-center');
  await expect(page).toHaveURL(/\/attendant$/);
  await expect(page.getByText('Itemba Mpemba')).toBeVisible();
  await expect(
    page.getByRole('heading', { name: 'How is my fuel business performing right now?' }),
  ).not.toBeVisible();

  await page.getByRole('button', { name: 'Display & language' }).click();
  await page.getByRole('button', { name: 'Sign out' }).click();
  await expect(page).toHaveURL(/\/login(?:\?next=%2Fattendant)?$/);
});
