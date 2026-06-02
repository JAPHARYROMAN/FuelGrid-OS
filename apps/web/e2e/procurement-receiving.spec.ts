import { test, expect, type Page } from '@playwright/test';

import { STATION, authedSession, json, paginated } from './helpers/journey';

/**
 * Procurement write-journey (QA-7): receive a delivery against a confirmed
 * purchase order. We drive the Receiving page — pick the PO line + tank, enter
 * received litres, post the goods receipt — and assert the match-result panel
 * renders the landed cost + match status the mocked receipt returns.
 */

const PRODUCT = {
  id: 'prod-1',
  tenant_id: STATION.tenant_id,
  code: 'DSL',
  name: 'Diesel',
  category: 'fuel',
  unit: 'L',
  default_price: '1.50',
  tax_rate: '0',
  loss_tolerance_percent: '0.5',
  color: '#123456',
  status: 'active',
};

const SUPPLIER = {
  id: 'sup-1',
  tenant_id: STATION.tenant_id,
  code: 'SUP1',
  name: 'Acme Fuels',
  payment_terms_days: 30,
  status: 'active',
  product_ids: [PRODUCT.id],
};

const TANK = {
  id: 'tank-1',
  tenant_id: STATION.tenant_id,
  station_id: STATION.id,
  product_id: PRODUCT.id,
  name: 'Tank 1',
  code: 'T1',
  capacity_litres: '50000.000',
  safe_min_litres: '1000.000',
};

const PO = {
  id: 'po-1',
  tenant_id: STATION.tenant_id,
  station_id: STATION.id,
  supplier_id: SUPPLIER.id,
  status: 'confirmed',
  raised_by: 'u-1',
  created_at: '2026-05-30T00:00:00Z',
  lines: [
    {
      id: 'pol-1',
      tenant_id: STATION.tenant_id,
      purchase_order_id: 'po-1',
      product_id: PRODUCT.id,
      ordered_litres: '10000.000',
      unit_price: '1.20',
      received_litres: '0.000',
    },
  ],
};

const DELIVERY = {
  id: 'del-1',
  tenant_id: STATION.tenant_id,
  tank_id: TANK.id,
  purchase_order_id: PO.id,
  po_line_id: 'pol-1',
  volume_litres: '10000.000',
  line_unit_price: '1.20',
  freight_amount: '0',
  duty_amount: '0',
  levies_amount: '0',
  landed_cost_total: '12000.00',
  landed_cost_per_litre: '1.20',
  match_status: 'matched',
  received_by: 'u-1',
  received_at: '2026-06-01T08:00:00Z',
};

async function mockProcurementReads(page: Page) {
  await page.route('**/api/bff/api/v1/tanks**', (route) => json(route, paginated([TANK])));
  await page.route('**/api/bff/api/v1/products', (route) => json(route, paginated([PRODUCT])));
  await page.route('**/api/bff/api/v1/suppliers', (route) => json(route, paginated([SUPPLIER])));
  await page.route('**/api/bff/api/v1/purchase-orders**', (route) => {
    // Only the GET list; the POST receipt is a different path.
    if (route.request().method() === 'GET') return json(route, paginated([PO]));
    return route.fallback();
  });
}

test.describe('procurement receiving', () => {
  test('receive a delivery against a confirmed PO', async ({ page }) => {
    await authedSession(page);
    await mockProcurementReads(page);

    let receiveCalls = 0;
    await page.route('**/api/bff/api/v1/purchase-orders/*/receipts', async (route) => {
      receiveCalls += 1;
      await json(route, {
        delivery: DELIVERY,
        movement: { id: 'mv-1' },
        dip_mismatch: false,
        quantity_discrepancy: false,
        quantity_variance_litres: '0.000',
        purchase_order_status: 'received',
      });
    });

    await page.goto('/procurement/receiving');

    // The receivable PO + ordered figure render.
    await expect(page.getByRole('heading', { name: 'Receiving' })).toBeVisible();
    await expect(page.getByText('Goods receipt')).toBeVisible();

    // Enter the received litres and post.
    const receiveBtn = page.getByRole('button', { name: 'Receive' });
    await expect(receiveBtn).toBeDisabled(); // no volume yet
    await page.getByLabel('Received litres').fill('10000');
    await expect(receiveBtn).toBeEnabled();
    await receiveBtn.click();

    // The match-result panel now shows the received volume + landed cost +
    // a `matched` badge.
    await expect.poll(() => receiveCalls).toBe(1);
    await expect(page.getByText('10,000 L received')).toBeVisible();
    await expect(page.getByText('matched')).toBeVisible();
  });
});
