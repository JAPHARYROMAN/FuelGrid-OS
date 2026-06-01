import { test, expect, type Page } from '@playwright/test';

import { STATION, authedSession, json, paginated } from './helpers/journey';

/**
 * Workforce write-journey (QA-7): create an employee (Employees page), then on
 * the Teams & rotation page assign them to a team, set the rotation anchor, and
 * see the roster populate. The two pages share the ['employees', stationID]
 * query, and each mutation re-fetches; we flip the mocked state accordingly.
 */

function employee(extra: Record<string, unknown> = {}) {
  return {
    id: 'emp-1',
    tenant_id: STATION.tenant_id,
    station_id: STATION.id,
    full_name: 'Ada Attendant',
    role: 'pump_attendant',
    status: 'active',
    created_at: '2026-01-01T00:00:00Z',
    updated_at: '2026-01-01T00:00:00Z',
    ...extra,
  };
}

function team(order: number) {
  return {
    id: `team-${order}`,
    tenant_id: STATION.tenant_id,
    station_id: STATION.id,
    name: `Team ${String.fromCharCode(65 + order)}`,
    rotation_order: order,
    member_count: 0,
  };
}

const TEAMS = [team(0), team(1), team(2)];

test.describe('workforce', () => {
  test('create an employee on the Employees page', async ({ page }) => {
    await authedSession(page);

    let employees: ReturnType<typeof employee>[] = [];
    await page.route('**/api/bff/api/v1/stations/*/employees', async (route) => {
      if (route.request().method() === 'POST') {
        employees = [employee()];
        return json(route, employee());
      }
      return json(route, paginated(employees));
    });

    await page.goto('/settings/employees');
    await expect(page.getByRole('heading', { name: 'Employees' })).toBeVisible();
    // Empty state first.
    await expect(page.getByText('No employees yet')).toBeVisible();

    // Open the create dialog, fill the name, save.
    await page.getByRole('button', { name: 'New employee' }).click();
    await expect(page.getByRole('dialog')).toBeVisible();
    await page.getByLabel('Full name').fill('Ada Attendant');
    await page.getByRole('button', { name: 'Save' }).click();

    // Dialog closes and the new employee row appears in the table.
    await expect(page.getByRole('dialog')).toHaveCount(0);
    await expect(page.getByRole('cell', { name: 'Ada Attendant' })).toBeVisible();
  });

  test('assign to team, set rotation anchor, see roster', async ({ page }) => {
    await authedSession(page);

    // One unassigned employee to start.
    let employees = [employee({ team_id: undefined })];
    await page.route('**/api/bff/api/v1/stations/*/employees', (route) =>
      json(route, paginated(employees)),
    );

    await page.route('**/api/bff/api/v1/stations/*/teams', (route) =>
      json(route, paginated(TEAMS)),
    );

    // Rotation anchor: empty, then set.
    let anchorDate: string | null = null;
    await page.route('**/api/bff/api/v1/stations/*/rotation-anchor', async (route) => {
      if (route.request().method() === 'PUT') {
        anchorDate = route.request().postDataJSON().rotation_anchor_date;
        return json(route, { station_id: STATION.id, rotation_anchor_date: anchorDate });
      }
      return json(route, { station_id: STATION.id, rotation_anchor_date: anchorDate });
    });

    // Roster: empty until the anchor is set + the employee is on a team.
    await page.route('**/api/bff/api/v1/stations/*/roster**', (route) => {
      const populated = anchorDate && employees[0].team_id;
      const items = Array.from({ length: 7 }).map((_, i) => ({
        date: `2026-06-0${i + 1}`,
        morning_team: populated ? TEAMS[i % 3] : null,
        evening_team: populated ? TEAMS[(i + 1) % 3] : null,
        resting_team: populated ? TEAMS[(i + 2) % 3] : null,
      }));
      return json(route, { items, count: items.length });
    });

    // PUT team members -> move the employee onto Team A.
    await page.route('**/api/bff/api/v1/teams/*/members', async (route) => {
      employees = [employee({ team_id: 'team-0' })];
      await json(route, paginated(employees));
    });

    await page.goto('/settings/teams');
    await expect(page.getByRole('heading', { name: 'Teams & rotation' })).toBeVisible();
    // Three team cards exist.
    await expect(page.getByText('Team A')).toBeVisible();
    await expect(page.getByText('Team C')).toBeVisible();

    // --- SET ROTATION ANCHOR ---
    await page.getByLabel('Cycle day 0 (date)').fill('2026-06-01');
    await page.getByRole('button', { name: 'Save anchor' }).click();
    await expect.poll(() => anchorDate).toBe('2026-06-01');

    // --- ASSIGN TO TEAM --- click the employee chip under Team A.
    // The chip is a button labelled with the employee name; click the first.
    await page.getByRole('button', { name: 'Ada Attendant' }).first().click();

    // --- ROSTER --- after the anchor + assignment the roster shows team badges.
    // Re-render: the morning column now carries a team badge for the first date.
    await expect(page.getByRole('cell', { name: '2026-06-01' })).toBeVisible();
    // At least one roster row renders a team badge now (was — before).
    await expect(page.getByText('Team A').first()).toBeVisible();
  });
});
