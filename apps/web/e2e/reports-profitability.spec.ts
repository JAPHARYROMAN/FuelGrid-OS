import { test, expect } from '@playwright/test';

import { authedSession, json, STATION } from './helpers/journey';

/**
 * Profitability (10.4) + Station Comparison (10.6) signature report views. The
 * backend is mocked: each view fetches a ReportEnvelope. We assert the page
 * renders its headline summary metrics and the drillable table from the
 * envelope, exactly as the other signature views do.
 */

const PROFIT_ENVELOPE = {
  metadata: {
    report_key: 'profitability',
    title: 'Profitability',
    generated_at: '2026-06-01T00:00:00Z',
    station_id: STATION.id,
    period: 'this-month',
  },
  filters_used: { station_id: STATION.id, period: 'this-month' },
  data_quality: [],
  summary: [
    { label: 'Net revenue', value: '1000.00', unit: 'TZS' },
    { label: 'COGS', value: '700.00', unit: 'TZS' },
    { label: 'Gross margin', value: '300.00', unit: 'TZS' },
    { label: 'Operating expenses', value: '100.00', unit: 'TZS' },
    { label: 'Net operating result', value: '200.00', unit: 'TZS' },
    { label: 'Litres sold', value: '400.000', unit: 'L' },
  ],
  chart_data: [
    {
      product: 'Premium',
      litres: '400.000',
      revenue: '1000.00',
      cogs: '700.00',
      gross_margin: '300.00',
    },
  ],
  table: {
    columns: ['product', 'litres', 'revenue', 'cogs', 'gross_margin'],
    rows: [['Premium', '400.000', '1000.00', '700.00', '300.00']],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/reports/financials.csv?period=this-month' }],
};

const COMPARISON_ENVELOPE = {
  metadata: {
    report_key: 'station-comparison',
    title: 'Station Comparison',
    generated_at: '2026-06-01T00:00:00Z',
    period: 'this-month',
  },
  filters_used: { period: 'this-month', stations_in_scope: '2' },
  data_quality: [],
  summary: [
    { label: 'Stations compared', value: '2', unit: 'count' },
    { label: 'Open risk alerts', value: '1', unit: 'count' },
  ],
  chart_data: [
    {
      station: 'DS1',
      revenue: '1000.00',
      litres: '400.000',
      gross_margin: '300.00',
      expenses: '100.00',
      net_operating: '200.00',
      risk_alerts: 0,
    },
    {
      station: 'DS2',
      revenue: '500.00',
      litres: '200.000',
      gross_margin: '200.00',
      expenses: '50.00',
      net_operating: '150.00',
      risk_alerts: 1,
    },
  ],
  table: {
    columns: [
      'station',
      'revenue',
      'litres',
      'gross_margin',
      'expenses',
      'net_operating',
      'stock_variance',
      'risk_alerts',
      'collections',
    ],
    rows: [
      ['DS1', '1000.00', '400.000', '300.00', '100.00', '200.00', '0', '0', '0'],
      ['DS2', '500.00', '200.000', '200.00', '50.00', '150.00', '0', '1', '0'],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [],
};

test.describe('reports — profitability & comparison', () => {
  test('profitability view renders the P&L summary and per-product table', async ({ page }) => {
    await authedSession(page);
    await page.route('**/api/bff/api/v1/reports/profitability**', (route) =>
      json(route, PROFIT_ENVELOPE),
    );

    await page.goto('/reports/profitability');
    await expect(page.getByRole('heading', { name: 'Profitability', exact: true })).toBeVisible();
    await expect(page.getByText('Net operating result')).toBeVisible();
    await expect(page.getByText('Per-product profitability')).toBeVisible();
    await expect(page.getByText('Premium')).toBeVisible();
  });

  test('station comparison view ranks the accessible stations', async ({ page }) => {
    await authedSession(page);
    await page.route('**/api/bff/api/v1/reports/station-comparison**', (route) =>
      json(route, COMPARISON_ENVELOPE),
    );

    await page.goto('/reports/station-comparison');
    await expect(
      page.getByRole('heading', { name: 'Station Comparison', exact: true }),
    ).toBeVisible();
    await expect(page.getByText('Stations compared')).toBeVisible();
    await expect(page.getByRole('heading', { name: 'Station ranking' })).toBeVisible();
    await expect(page.getByText('DS1')).toBeVisible();
    await expect(page.getByText('DS2')).toBeVisible();
  });
});
