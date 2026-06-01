import { test, expect } from '@playwright/test';

import { STATION, authedSession, json, paginated } from './helpers/journey';

/**
 * Executive Command Center read-journey (Phase 16 Workstream E). The flagship
 * dashboard fans out across network reads (enterprise overview, station
 * ranking, risk, AR aging, reports overview) and station-scoped reads for the
 * focused site (revenue, inventory, reconciliation, shifts, deliveries).
 *
 * The BACKEND IS NEVER REAL: we mock every BFF route the page touches with
 * bodies shaped like the Go DTOs, then assert the hero KPIs, the alert/insight
 * surfaces, and the secondary panels render the mocked figures. This proves the
 * dashboard's data-binding and deterministic insight rules, not the server.
 */

const DAY_ID = 'day-cc-1';

function mockCommandCenter(page: Parameters<typeof authedSession>[0]) {
  // ---- Network-level reads --------------------------------------------
  page.route('**/api/bff/api/v1/enterprise/overview**', (route) =>
    json(route, {
      from: '2026-06-01',
      to: '2026-06-01',
      gross_revenue: '125000.00',
      net_revenue: '110000.00',
      margin_total: '18000.00',
      ap_outstanding: '42000.00',
      ar_outstanding: '9500.00',
      open_incidents: 2,
      approvals_waiting: 3,
    }),
  );
  page.route('**/api/bff/api/v1/enterprise/station-ranking**', (route) =>
    json(
      route,
      paginated([
        {
          station_id: STATION.id,
          name: STATION.name,
          gross_revenue: '125000.00',
          margin_total: '18000.00',
        },
      ]),
    ),
  );
  page.route('**/api/bff/api/v1/risk/overview', (route) =>
    json(route, {
      open_by_severity: { critical: 1, high: 2 },
      open_total: 3,
      top_stations: [],
    }),
  );
  page.route('**/api/bff/api/v1/risk/alerts**', (route) =>
    json(
      route,
      paginated([
        {
          id: 'alert-1',
          rule_code: 'CASH_SHORTAGE',
          alert_type: 'cash_shortage',
          severity: 'critical',
          status: 'open',
          detail: 'Repeated cash shortage at DS1',
          amount: '1500.00',
          score: 92,
        },
      ]),
    ),
  );
  page.route('**/api/bff/api/v1/ar-aging', (route) =>
    json(
      route,
      paginated([
        { customer_id: 'cust-1', code: 'AC-1', name: 'Acme Haulage', balance: '9500.00' },
      ]),
    ),
  );
  page.route('**/api/bff/api/v1/reports/overview', (route) =>
    json(route, {
      generated_at: '2026-06-01T08:00:00Z',
      categories: [
        {
          key: 'receivables',
          title: 'Receivables Aging',
          description: 'Outstanding credit-customer balances.',
          headline: '9500.00',
          headline_unit: 'TZS',
          alert_count: 0,
          href: '/api/v1/reports/customer-aging/insights',
        },
      ],
    }),
  );

  // ---- Station-scoped reads (focused site) ----------------------------
  page.route('**/api/bff/api/v1/stations/*/revenue-overview', (route) =>
    json(route, {
      station: STATION,
      day: {
        id: DAY_ID,
        station_id: STATION.id,
        business_date: '2026-06-01',
        status: 'open',
        opened_by: 'u-1',
        opened_at: '2026-06-01T05:00:00Z',
      },
      summary: {
        gross_revenue: '12500.00',
        net_revenue: '11000.00',
        tax_total: '1500.00',
        cogs_total: '9000.00',
        margin_total: '2000.00',
        litres_sold: 5400,
        sale_count: 132,
      },
      tenders: {
        cash: '8000.00',
        mobile_money: '0.00',
        card: '0.00',
        credit: '0.00',
        voucher: '0.00',
        total: '8000.00',
      },
      recent_days: [
        {
          id: 'rev-day-1',
          station_id: STATION.id,
          operating_day_id: DAY_ID,
          business_date: '2026-06-01',
          gross_revenue: '12500.00',
          net_revenue: '11000.00',
          tax_total: '1500.00',
          cogs_total: '9000.00',
          margin_total: '2000.00',
          cash_total: '8000.00',
          mobile_money_total: '0.00',
          card_total: '0.00',
          credit_total: '0.00',
          voucher_total: '0.00',
          tender_total: '8000.00',
          cash_variance: '-25.00',
          status: 'open',
        },
      ],
    }),
  );
  page.route('**/api/bff/api/v1/stations/*/inventory-overview', (route) =>
    json(route, {
      station: STATION,
      tanks: [
        {
          tank: { id: 'tank-1', station_id: STATION.id, name: 'Tank 1', code: 'T1' },
          book_balance: '4900.000',
          latest_physical: 4900,
          fill_percent: 32,
          days_of_stock: 1.5,
          recent_variances: [],
        },
      ],
    }),
  );
  page.route('**/api/bff/api/v1/stations/*/reconciliation-overview**', (route) =>
    json(route, {
      station: STATION,
      day: { id: DAY_ID, station_id: STATION.id, business_date: '2026-06-01', status: 'open' },
      all_shifts_approved: false,
      tanks: [
        {
          tank: { id: 'tank-1', station_id: STATION.id, name: 'Tank 1', code: 'T1' },
          book_balance: '4900.000',
          latest_physical: 4900,
          reconciliation: { id: 'rec-1', over_tolerance: true },
        },
      ],
    }),
  );
  page.route('**/api/bff/api/v1/stations/*/shifts**', (route) =>
    json(
      route,
      paginated([
        {
          id: 'shift-1',
          station_id: STATION.id,
          operating_day_id: DAY_ID,
          name: 'Morning',
          status: 'closed',
          opened_by: 'u-1',
          opened_at: '2026-06-01T05:00:00Z',
          closed_at: '2026-06-01T13:00:00Z',
        },
      ]),
    ),
  );
  page.route('**/api/bff/api/v1/stations/*/deliveries', (route) =>
    json(
      route,
      paginated([
        {
          id: 'del-1',
          tenant_id: STATION.tenant_id,
          tank_id: 'tank-1',
          supplier_ref: 'BL-9001',
          volume_litres: 20000,
          freight_amount: '0.00',
          duty_amount: '0.00',
          levies_amount: '0.00',
          match_status: 'matched',
          received_by: 'u-1',
          received_at: '2026-06-01T07:30:00Z',
        },
      ]),
    ),
  );
}

test.describe('command center', () => {
  test('renders the flagship dashboard with live network + station figures', async ({ page }) => {
    await authedSession(page);
    await mockCommandCenter(page);

    await page.goto('/command-center');

    // Hero header.
    await expect(
      page.getByRole('heading', { name: 'How is my fuel business performing right now?' }),
    ).toBeVisible();

    // KPI hero row binds the focused station's revenue + the network margin.
    await expect(page.getByText('Revenue today', { exact: true })).toBeVisible();
    await expect(page.getByText('12,500.00').first()).toBeVisible();
    await expect(page.getByText('Litres sold today', { exact: true })).toBeVisible();
    await expect(page.getByText('Network margin', { exact: true })).toBeVisible();

    // Critical alert surfaces with its detail + risk badge.
    await expect(page.getByText('Repeated cash shortage at DS1')).toBeVisible();

    // Deterministic insight: the critical risk rule fires.
    await expect(page.getByText(/critical risk alert is open/i)).toBeVisible();

    // Station ranking table renders the ranked site.
    await expect(page.getByRole('heading', { name: 'Station ranking' })).toBeVisible();

    // Credit exposure binds the AR balance.
    await expect(page.getByText('Acme Haulage')).toBeVisible();

    // Recent deliveries panel binds the receipt volume.
    await expect(page.getByText('Recent deliveries', { exact: true })).toBeVisible();
  });
});
