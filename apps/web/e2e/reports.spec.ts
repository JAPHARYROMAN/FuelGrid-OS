import { test, expect } from '@playwright/test';

import { authedSession, json, STATION } from './helpers/journey';

/**
 * Reports & Intelligence Center journey (QA-7): the Reports Center home renders
 * its permission-filtered category cards from the structured reports CATALOG
 * (getReportCatalog → /reports/catalog), and a signature report view can drive a
 * unified export. The backend is mocked: we return a ReportCatalog for the hub, a
 * ReportEnvelope for the reconciliation view, and the unified export endpoint
 * (POST) → file URL (GET) chain that the ExportButtonGroup walks. We assert (a)
 * the hub heading + the Inventory category card and that it links to its report
 * view, (b) the export POST fires, and (c) the browser download event fires with
 * the expected filename.
 */

// A realistic catalog: the blueprint categories as DATA (see migration 0105 and
// the ReportCatalog type in packages/sdk/src/types.ts). We include the live
// `inventory` category — name "Inventory", target_route the real reconciliation
// page, a non-null key metric, an alert pill, and its reports[] entry — plus a
// live `finance` and a `partial` `sales` so the hub reads like the real thing.
const CATALOG = {
  generated_at: '2026-06-01T00:00:00Z',
  categories: [
    {
      key: 'inventory',
      name: 'Inventory',
      description:
        'Per-tank book-vs-physical reconciliation waterfall, variance and tolerance breaches.',
      icon: 'layers',
      sort_order: 30,
      required_permission: 'reconciliation.read',
      availability: 'live',
      target_route: '/reports/inventory/reconciliation',
      metric: { label: 'Over-tolerance tanks', value: '2', unit: 'count' },
      alert_count: 2,
      reports: [
        {
          key: 'inventory-reconciliation',
          name: 'Inventory Reconciliation',
          description: 'Per-tank book-vs-physical waterfall for a station day.',
          endpoint: '/api/v1/reports/inventory/reconciliation',
          required_permission: 'reconciliation.read',
          availability: 'live',
        },
      ],
    },
    {
      key: 'finance',
      name: 'Finance',
      description: 'Profit & loss, trial balance, expenses and the financial statement.',
      icon: 'banknote',
      sort_order: 90,
      required_permission: 'finance.read',
      availability: 'live',
      target_route: '/reports/profitability',
      metric: { label: 'Net operating result', value: '1250000.00', unit: 'TZS' },
      alert_count: 0,
      reports: [
        {
          key: 'profitability',
          name: 'Profitability',
          description: 'Revenue, COGS, gross margin, expenses and net operating result.',
          endpoint: '/api/v1/reports/profitability',
          required_permission: 'revenue.read',
          availability: 'live',
        },
      ],
    },
    {
      key: 'sales',
      name: 'Sales',
      description: 'Revenue, litres, product mix and tender breakdown across stations and days.',
      icon: 'trending-up',
      sort_order: 20,
      required_permission: 'revenue.read',
      availability: 'partial',
      target_route: '/reports/sales-summary',
      metric: {
        label: 'Latest gross',
        value: null,
        reason: 'A tenant-wide sales figure is not wired yet; open the station report.',
      },
      alert_count: 0,
      reports: [],
    },
  ],
  data_quality: [],
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

    await page.route('**/api/bff/api/v1/reports/catalog', (route) => json(route, CATALOG));
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

    // ---- Reports Center home ----
    await page.goto('/reports');
    // PageHeader renders its `title` prop in an <h1> (role=heading, level 1).
    await expect(page.getByRole('heading', { name: 'Reports center', exact: true })).toBeVisible();
    // The hub now shows CATEGORY cards. The live Inventory category renders as a
    // card titled by the category name, whose title links to its report view.
    // Scope to the categories section so the locator never collides with the
    // sidebar's "Inventory" nav link (href="/inventory") in the dashboard chrome.
    const categories = page.getByRole('region', { name: 'Report categories' });
    const inventoryCard = categories.getByRole('link', { name: 'Inventory', exact: true });
    await expect(inventoryCard).toBeVisible();
    await expect(inventoryCard).toHaveAttribute('href', '/reports/inventory/reconciliation');
    // And it is navigable to the signature report view.
    await inventoryCard.click();
    await expect(page).toHaveURL(/\/reports\/inventory\/reconciliation$/);

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
