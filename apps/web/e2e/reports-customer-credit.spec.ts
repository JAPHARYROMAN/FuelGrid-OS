import { test, expect } from '@playwright/test';

import { authedSession, json, STATION } from './helpers/journey';

/**
 * Customer Credit (§5.9) signature report view. The backend is mocked: the view
 * fetches a ReportEnvelope (the aging buckets + per-customer rows with gated
 * credit exposure) and, on a row click, a small drilldown payload. We assert the
 * page renders its KPI hero, the aging-bucket chart, the credit-limit utilization
 * meters, the per-customer aging table, and that clicking a customer opens the
 * balance -> invoices -> payments drilldown dialog — mirroring reports-finance.
 */

const CUSTOMER_ID = 'cust-1111-1111-1111-111111111111';

const CUSTOMER_CREDIT_ENVELOPE = {
  metadata: {
    report_key: 'customer-credit',
    title: 'Customer Credit',
    generated_at: '2026-06-14T00:00:00Z',
    station_id: STATION.id,
    period: 'this-month',
  },
  filters_used: { period: 'this-month', as_of: '2026-06-14' },
  data_quality: [],
  summary: [
    { label: 'Total receivable', value: '1500.00', unit: 'TZS' },
    { label: 'Total overdue', value: '1400.00', unit: 'TZS' },
    { label: '% overdue', value: '93.3' },
    { label: 'Customers with balance', value: '1', unit: 'count' },
    { label: 'Customers over limit', value: '1', unit: 'count' },
    { label: 'Customers on hold', value: '0', unit: 'count' },
  ],
  chart_data: {
    buckets: [
      { bucket: 'Current', amount: '100.00' },
      { bucket: '1-30', amount: '200.00' },
      { bucket: '31-60', amount: '300.00' },
      { bucket: '61-90', amount: '400.00' },
      { bucket: '90+', amount: '500.00' },
    ],
    customers: [
      {
        customer_id: CUSTOMER_ID,
        code: 'AGECUST',
        name: 'Aging Customer',
        current: '100.00',
        days_1_30: '200.00',
        days_31_60: '300.00',
        days_61_90: '400.00',
        days_90_plus: '500.00',
        outstanding: '1500.00',
        overdue: '1400.00',
        risk_category: 'standard',
        on_hold: false,
        status: 'active',
        credit_limit: '1000.00',
        exposure: '1200.00',
        available: '-200.00',
        utilization: '120.00',
        warning_pct: '80',
        over_limit: true,
      },
    ],
    exposure_shown: true,
  },
  table: { columns: [], rows: [] },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/reports/ar-aging.csv' }],
};

const DRILLDOWN = {
  customer_id: CUSTOMER_ID,
  invoices: [
    {
      invoice_id: 'inv-1',
      invoice_number: 'INV-0001',
      invoice_date: '2026-02-14',
      due_date: '2026-02-28',
      amount: '500.00',
      outstanding: '500.00',
      days_overdue: 106,
      bucket: '90+',
      status: 'issued',
    },
  ],
  payments: [
    {
      payment_id: 'pay-1',
      payment_date: '2026-03-01',
      method: 'bank',
      reference: 'TXN-1',
      amount: '100.00',
      allocated: '100.00',
      status: 'posted',
    },
  ],
};

test.describe('reports — customer credit (aging)', () => {
  test('customer-credit view renders the hero, aging chart, utilization meters and table', async ({
    page,
  }) => {
    await authedSession(page);
    // Register the broad report route first, then the more-specific drilldown
    // route — Playwright matches the most-recently-registered route first, so the
    // drilldown URL (which also matches the broad pattern) resolves to its own
    // payload.
    await page.route('**/api/bff/api/v1/reports/customer-credit**', (route) =>
      json(route, CUSTOMER_CREDIT_ENVELOPE),
    );
    await page.route('**/api/bff/api/v1/reports/customer-credit/drilldown**', (route) =>
      json(route, DRILLDOWN),
    );

    await page.goto('/reports/customer-credit');
    await expect(page.getByRole('heading', { name: 'Customer Credit', exact: true })).toBeVisible();
    // KPI hero.
    await expect(page.getByText('Total receivable').first()).toBeVisible();
    await expect(page.getByText('Customers over limit').first()).toBeVisible();
    // The signature aging-bucket chart + the credit-limit utilization meters.
    await expect(page.getByText('Receivables by aging bucket').first()).toBeVisible();
    await expect(page.getByText('Credit-limit utilization').first()).toBeVisible();
    // The per-customer aging table.
    await expect(page.getByText('Aging by customer').first()).toBeVisible();
    await expect(page.getByText('Aging Customer').first()).toBeVisible();
  });

  test('clicking a customer opens the balance -> invoices -> payments drilldown', async ({
    page,
  }) => {
    await authedSession(page);
    // Broad route first, specific drilldown route last (most-recently-registered
    // wins) so the drilldown URL resolves to its own payload, not the envelope.
    await page.route('**/api/bff/api/v1/reports/customer-credit**', (route) =>
      json(route, CUSTOMER_CREDIT_ENVELOPE),
    );
    await page.route('**/api/bff/api/v1/reports/customer-credit/drilldown**', (route) =>
      json(route, DRILLDOWN),
    );

    await page.goto('/reports/customer-credit');
    await expect(page.getByText('Aging by customer').first()).toBeVisible();

    // Click the customer's name cell in the aging table (the row's onClick opens
    // the drilldown dialog). Scope to a row cell so the click lands on a real
    // element and bubbles to the clickable <tr>.
    await page.getByRole('cell', { name: 'Aging Customer' }).click();

    const dialog = page.getByRole('dialog');
    await expect(dialog).toBeVisible();
    // Use the section headings (not the prose description, which also says
    // "open invoices and recent payments") to keep the locators unambiguous.
    await expect(dialog.getByRole('heading', { name: 'Open invoices' })).toBeVisible();
    await expect(dialog.getByText('INV-0001')).toBeVisible();
    await expect(dialog.getByRole('heading', { name: 'Recent payments' })).toBeVisible();
  });
});
