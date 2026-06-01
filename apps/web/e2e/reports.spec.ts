import { test, expect } from '@playwright/test';

import { authedSession, json, STATION } from './helpers/journey';

/**
 * Reports & Intelligence Center journey (QA-7): the hub renders its live
 * category cards from the structured reports overview, and a signature report
 * view can drive a unified export. The backend is mocked: we return a
 * ReportsOverview for the hub, a ReportEnvelope for the reconciliation view, and
 * the unified export endpoint (POST) → file URL (GET) chain that the
 * ExportButtonGroup walks. We assert (a) the hub shows a category, (b) the
 * export POST fires, and (c) the browser download event fires with the expected
 * filename.
 */

const OVERVIEW = {
  generated_at: '2026-06-01T00:00:00Z',
  categories: [
    {
      key: 'inventory-reconciliation',
      title: 'Inventory Reconciliation',
      description: 'Per-tank book-vs-physical waterfall, variance, and tolerance breaches.',
      headline: '2',
      headline_unit: 'open alerts',
      alert_count: 2,
      href: '/api/v1/reports/inventory/reconciliation',
    },
    {
      key: 'station-close',
      title: 'Daily Station Close',
      description: 'Sales, stock variance, cash position, deliveries, and approval status.',
      headline: '',
      headline_unit: '',
      alert_count: 0,
      href: '/api/v1/reports/station-close',
    },
  ],
};

const RECON_ENVELOPE = {
  metadata: {
    report_key: 'inventory-reconciliation',
    title: 'Inventory Reconciliation',
    generated_at: '2026-06-01T00:00:00Z',
    station_id: STATION.id,
    period: 'current',
  },
  filters_used: { station_id: STATION.id, period: 'current' },
  data_quality: [],
  summary: [
    { label: 'Tanks reconciled', value: '1', unit: 'count' },
    { label: 'Over-tolerance tanks', value: '0', unit: 'count' },
  ],
  chart_data: [
    {
      tank: 'TANK-01',
      opening: '10000.000',
      deliveries: '5000.000',
      sales: '4000.000',
      adjustments: '0.000',
      expected_closing: '11000.000',
      actual_closing: '10950.000',
      variance: '-50.000',
      variance_pct: '-0.45',
      tolerance: '0.50',
      sealed: false,
    },
  ],
  table: {
    columns: ['tank', 'opening', 'expected_closing', 'actual_closing', 'variance'],
    rows: [['TANK-01', '10000.000', '11000.000', '10950.000', '-50.000']],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [
    { format: 'csv', url: `/api/v1/stations/${STATION.id}/reports/reconciliation.csv` },
  ],
};

test.describe('reports', () => {
  test('hub lists categories and a report view exports CSV', async ({ page }) => {
    await authedSession(page);

    await page.route('**/api/bff/api/v1/reports/overview', (route) => json(route, OVERVIEW));
    await page.route('**/api/bff/api/v1/reports/inventory/reconciliation**', (route) =>
      json(route, RECON_ENVELOPE),
    );

    // The unified export endpoint audits the request and returns the file URL.
    let exportCalls = 0;
    await page.route('**/api/bff/api/v1/reports/export', async (route) => {
      exportCalls += 1;
      await json(route, {
        report_key: 'reconciliation',
        format: 'csv',
        url: `/api/v1/stations/${STATION.id}/reports/reconciliation.csv`,
      });
    });

    // The file URL the export points at, streamed through the BFF.
    await page.route('**/api/bff/api/v1/stations/*/reports/reconciliation.csv**', (route) =>
      route.fulfill({
        status: 200,
        headers: {
          'Content-Type': 'text/csv',
          'Content-Disposition': 'attachment; filename="reconciliation.csv"',
          'X-Request-Id': 'e2e-report',
        },
        body: 'tank,variance\nTANK-01,-50.000\n',
      }),
    );

    // ---- Hub ----
    await page.goto('/reports');
    await expect(page.getByRole('heading', { name: 'Reporting hub', exact: true })).toBeVisible();
    await expect(page.getByText('Inventory Reconciliation', { exact: true })).toBeVisible();

    // ---- Signature reconciliation view ----
    await page.goto('/reports/inventory/reconciliation');
    await expect(
      page.getByRole('heading', { name: 'Inventory Reconciliation', exact: true }),
    ).toBeVisible();
    await expect(page.getByText('Per-tank reconciliation waterfall')).toBeVisible();

    // The export group's CSV button POSTs the unified export then downloads.
    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('button', { name: 'CSV' }).first().click();

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toContain('reconciliation');
    expect(exportCalls).toBe(1);
  });
});
