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
      availability: 'live',
      target_route: '/reports/sales',
      metric: { label: 'Gross revenue (30d)', value: '4200000.00', unit: 'TZS' },
      alert_count: 0,
      reports: [
        {
          key: 'sales',
          name: 'Sales',
          description:
            'Litres, revenue, average selling price, transaction count and growth, with product mix and drill-down.',
          endpoint: '/api/v1/reports/sales',
          required_permission: 'revenue.read',
          availability: 'live',
        },
      ],
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
    { label: 'Total variance', value: '-50.000', unit: 'L' },
    { label: 'Variance %', value: '-0.45' },
    { label: 'Over-tolerance tanks', value: '0', unit: 'count' },
    { label: 'Tanks reconciled', value: '1', unit: 'count' },
    { label: 'Variance value', value: '145000.00', unit: 'TZS' },
  ],
  chart_data: [
    {
      tank: 'PMS-01',
      product: 'Petrol',
      product_color: '#f97316',
      opening: '10000.000',
      deliveries: '5000.000',
      sales: '4000.000',
      adjustments: '0.000',
      expected_closing: '11000.000',
      actual_closing: '10950.000',
      variance: '-50.000',
      variance_pct: '-0.45',
      variance_value: '145000.00',
      priced: true,
      tolerance: '0.50',
      over_tolerance: false,
      sealed: false,
    },
  ],
  table: {
    columns: ['tank', 'product', 'opening', 'expected_closing', 'actual_closing', 'variance'],
    rows: [['PMS-01', 'Petrol', '10000.000', '11000.000', '10950.000', '-50.000']],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [
    { format: 'csv', url: `/api/v1/stations/${STATION.id}/reports/reconciliation.csv` },
  ],
};

// A §20.5 cash-reconciliation envelope: KPI hero, the per-reconciliation flow,
// and the settlement-status board (the net-new signature visual).
const CASH_ENVELOPE = {
  metadata: {
    report_key: 'cash-reconciliation',
    title: 'Cash Reconciliation',
    generated_at: '2026-06-01T00:00:00Z',
    station_id: STATION.id,
    period: 'current',
  },
  filters_used: { station_id: STATION.id, period: 'current' },
  data_quality: [],
  summary: [
    { label: 'Expected cash', value: '600000.00', unit: 'TZS' },
    { label: 'Submitted cash', value: '595000.00', unit: 'TZS' },
    { label: 'Deposited cash', value: '500000.00', unit: 'TZS' },
    { label: 'Net variance', value: '-5000.00', unit: 'TZS' },
    { label: 'Total shortage', value: '5000.00', unit: 'TZS' },
    { label: 'Total excess', value: '0.00', unit: 'TZS' },
    { label: 'Variance status', value: 'Shortage' },
    { label: 'Reconciliations', value: '1', unit: 'count' },
  ],
  chart_data: {
    flow: [
      {
        created_at: '2026-06-01T08:00:00Z',
        status: 'submitted',
        expected: '600000.00',
        submitted: '595000.00',
        variance: '-5000.00',
        shortage: '5000.00',
        excess: '0',
      },
    ],
    settlement: [
      {
        key: 'cash',
        label: 'Cash',
        status: 'Pending',
        tone: 'pending',
        amount: '595000.00',
        detail: 'Submitted, awaiting approval/posting',
      },
      {
        key: 'mobile_money',
        label: 'Mobile money',
        status: 'Settled',
        tone: 'settled',
        amount: '250000.00',
        detail: 'Day locked — tenders confirmed',
      },
      {
        key: 'card',
        label: 'Card',
        status: 'None',
        tone: 'neutral',
        amount: '0',
        detail: 'No Card tendered',
      },
      {
        key: 'bank_deposit',
        label: 'Bank deposit',
        status: 'Not banked',
        tone: 'at_risk',
        amount: '100000.00',
        detail: '1 deposit(s) prepared, not yet banked',
      },
    ],
  },
  tender_mix: {
    cash: '600000.00',
    mobile_money: '250000.00',
    card: '0',
    credit: '150000.00',
    voucher: '0',
    total: '1000000.00',
  },
  table: {
    columns: ['created_at', 'status', 'expected', 'submitted', 'variance', 'shortage', 'excess'],
    rows: [
      ['2026-06-01T08:00:00Z', 'submitted', '600000.00', '595000.00', '-5000.00', '5000.00', '0'],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  // The cash-reconciliation handler returns NO export options in production
  // (there is no cash CSV exporter wired), so the mock omits them too — the
  // export assertion runs on the reconciliation report, whose handler does emit
  // real options. This keeps the e2e honest about what the cash page can do.
  export_options: [],
};

test.describe('reports', () => {
  test('hub lists categories and a report view exports CSV', async ({ page }) => {
    await authedSession(page);

    await page.route('**/api/bff/api/v1/reports/catalog', (route) => json(route, CATALOG));
    await page.route('**/api/bff/api/v1/reports/inventory/reconciliation**', (route) =>
      json(route, RECON_ENVELOPE),
    );
    await page.route('**/api/bff/api/v1/reports/cash-reconciliation**', (route) =>
      json(route, CASH_ENVELOPE),
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
    // The signature §20.3 layout also renders the new variance heatmap.
    await expect(page.getByRole('heading', { name: 'Variance heatmap' })).toBeVisible();

    // The reconciliation handler returns real export options, so its CSV button
    // POSTs the unified export then downloads — exercise it here (the cash report
    // has no exporter, so we never click a CSV button on that page).
    const downloadPromise = page.waitForEvent('download');
    await page.getByRole('button', { name: 'CSV' }).first().click();

    const download = await downloadPromise;
    expect(download.suggestedFilename()).toContain('reconciliation');
    expect(exportCalls).toBe(1);

    // ---- Signature §20.5 cash-reconciliation view ----
    await page.goto('/reports/cash-reconciliation');
    await expect(
      page.getByRole('heading', { name: 'Cash Reconciliation', exact: true }),
    ).toBeVisible();
    await expect(page.getByText('Cash reconciliation flow')).toBeVisible();
    // The net-new settlement-status board renders a chip per medium with a TEXT
    // status (colour is never the only signal).
    await expect(page.getByRole('heading', { name: 'Settlement status' })).toBeVisible();
    await expect(page.getByText('Bank deposit', { exact: true })).toBeVisible();
    await expect(page.getByText('Not banked', { exact: true })).toBeVisible();
  });
});
