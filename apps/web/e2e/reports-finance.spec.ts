import { test, expect } from '@playwright/test';

import { authedSession, json, STATION } from './helpers/journey';

/**
 * Finance P&L (§5.8) signature report view. The backend is mocked: the view
 * fetches a ReportEnvelope. We assert the page renders its KPI hero, the
 * signature P&L waterfall (revenue → COGS → gross margin → expenses → net), the
 * accounting-period settlement chips and the embedded financial statements,
 * exactly as the other signature views do.
 */

const FINANCE_ENVELOPE = {
  metadata: {
    report_key: 'finance',
    title: 'Finance',
    generated_at: '2026-06-14T00:00:00Z',
    station_id: STATION.id,
    period: 'this-month',
  },
  filters_used: { station_id: STATION.id, period: 'this-month' },
  data_quality: [],
  summary: [
    { label: 'Net revenue', value: '1000.00', unit: 'TZS' },
    { label: 'Operating expenses', value: '100.00', unit: 'TZS' },
    { label: 'Cash position', value: '900.00', unit: 'TZS' },
    { label: 'Gross margin', value: '300.00', unit: 'TZS' },
    { label: 'Net operating result', value: '200.00', unit: 'TZS' },
    { label: 'Net margin %', value: '20.0' },
  ],
  chart_data: {
    waterfall: [
      { key: 'revenue', label: 'Net revenue', value: '1000.00', kind: 'base' },
      { key: 'cogs', label: 'COGS', value: '700.00', kind: 'delta', negative: true },
      { key: 'gross_margin', label: 'Gross margin', value: '300.00', kind: 'total' },
      {
        key: 'expenses',
        label: 'Operating expenses',
        value: '100.00',
        kind: 'delta',
        negative: true,
      },
      { key: 'net_operating', label: 'Net operating result', value: '200.00', kind: 'total' },
    ],
    by_product: [
      {
        product: 'Premium',
        litres: '400.000',
        revenue: '1000.00',
        cogs: '700.00',
        margin: '300.00',
      },
    ],
    settlements: [{ key: 'p1', label: '2026-06-01 – 2026-06-30', status: 'Open', tone: 'pending' }],
    statements: [
      {
        key: 'profit-loss',
        label: 'Profit & Loss statement',
        endpoint: '/api/v1/finance/reports/profit-loss',
        permission: 'finance.read',
      },
      {
        key: 'trial-balance',
        label: 'Trial Balance',
        endpoint: '/api/v1/finance/reports/trial-balance',
        permission: 'finance.read',
      },
    ],
    cost_shown: true,
  },
  table: {
    columns: ['product', 'litres', 'revenue', 'cogs', 'gross_margin'],
    rows: [['Premium', '400.000', '1000.00', '700.00', '300.00']],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/reports/financials.csv?period=this-month' }],
};

test.describe('reports — finance (P&L)', () => {
  test('finance view renders the KPI hero, P&L waterfall, settlement chips and statements', async ({
    page,
  }) => {
    await authedSession(page);
    await page.route('**/api/bff/api/v1/reports/finance**', (route) =>
      json(route, FINANCE_ENVELOPE),
    );

    await page.goto('/reports/finance');
    await expect(page.getByRole('heading', { name: 'Finance', exact: true })).toBeVisible();
    // KPI hero.
    await expect(page.getByText('Cash position').first()).toBeVisible();
    await expect(page.getByText('Net operating result').first()).toBeVisible();
    // The signature P&L waterfall card + its COGS step.
    await expect(page.getByText('Profit & loss waterfall').first()).toBeVisible();
    await expect(page.getByText('COGS').first()).toBeVisible();
    // Accounting-period settlement chips + the embedded financial statements.
    await expect(page.getByText('Accounting periods').first()).toBeVisible();
    await expect(page.getByText('Financial statements').first()).toBeVisible();
    await expect(page.getByText('Profit & Loss statement').first()).toBeVisible();
    // Per-product table.
    await expect(page.getByText('Per-product profitability').first()).toBeVisible();
    await expect(page.getByText('Premium').first()).toBeVisible();
  });
});
