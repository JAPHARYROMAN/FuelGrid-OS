import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { Delivery, ProcurementOverview, SupplierInvoice } from '@fuelgrid/sdk';

const getProcurementOverview = vi.fn();
const listStations = vi.fn();
const listSuppliers = vi.fn();
const listProducts = vi.fn();
const listStationDeliveries = vi.fn();
const listSupplierInvoices = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    getProcurementOverview: (...a: unknown[]) => getProcurementOverview(...a),
    listStations: (...a: unknown[]) => listStations(...a),
    listSuppliers: (...a: unknown[]) => listSuppliers(...a),
    listProducts: (...a: unknown[]) => listProducts(...a),
    listStationDeliveries: (...a: unknown[]) => listStationDeliveries(...a),
    listSupplierInvoices: (...a: unknown[]) => listSupplierInvoices(...a),
    purchaseOrderPdf: vi.fn(),
    purchaseOrdersPdf: vi.fn(),
    stationDeliveriesPdf: vi.fn(),
  },
}));

const usePermission = vi.fn((_code: string, _opts?: unknown) => true);
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string, opts?: unknown) => usePermission(code, opts),
}));

import ProcurementPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ProcurementPage />
    </QueryClientProvider>,
  );
}

const overview: ProcurementOverview = {
  station: { id: 'stn-1', name: 'Westlands' },
  open_purchase_orders: [
    {
      id: 'po-1',
      tenant_id: 't',
      station_id: 'stn-1',
      supplier_id: 'sup-1',
      status: 'partially_received',
      raised_by: 'u',
      created_at: '2026-05-01T00:00:00Z',
      lines: [
        {
          id: 'pol-1',
          tenant_id: 't',
          purchase_order_id: 'po-1',
          product_id: 'prod-1',
          ordered_litres: '5000.000',
          unit_price: '2500.00',
          received_litres: '4800.000',
        },
      ],
    },
  ],
  recent_receipts: [],
  supplier_balances: [],
  price_trend: [],
} as unknown as ProcurementOverview;

const delivery: Delivery = {
  id: 'del-1',
  tenant_id: 't',
  tank_id: 'tk-1',
  purchase_order_id: 'po-1',
  po_line_id: 'pol-1',
  volume_litres: '4800.000',
  freight_amount: '0',
  duty_amount: '0',
  levies_amount: '0',
  match_status: 'matched',
  received_by: 'u',
  received_at: '2026-05-02T00:00:00Z',
} as Delivery;

const linkedInvoice: SupplierInvoice = {
  id: 'inv-1',
  supplier_id: 'sup-1',
  purchase_order_id: 'po-1',
  station_id: 'stn-1',
  invoice_number: 'SINV-9001',
  status: 'matched',
  received_at: '2026-05-03T00:00:00Z',
  total_amount: '12000000.00',
  recorded_by: 'u',
  lines: [],
  discrepancies: [],
} as unknown as SupplierInvoice;

describe('ProcurementPage linkage (7.2)', () => {
  beforeEach(() => {
    usePermission.mockClear();
    listStations.mockResolvedValue({
      items: [{ id: 'stn-1', name: 'Westlands', code: 'WL' }],
      count: 1,
      has_more: false,
    });
    listSuppliers.mockResolvedValue({
      items: [{ id: 'sup-1', name: 'Total Energies' }],
      count: 1,
      has_more: false,
    });
    listProducts.mockResolvedValue({
      items: [{ id: 'prod-1', name: 'AGO' }],
      count: 1,
      has_more: false,
    });
    getProcurementOverview.mockResolvedValue(overview);
    listStationDeliveries.mockResolvedValue({ items: [delivery], count: 1, has_more: false });
    listSupplierInvoices.mockResolvedValue({ items: [linkedInvoice], count: 1, has_more: false });
  });

  afterEach(() => vi.clearAllMocks());

  it('expands a PO row to surface its linked goods receipts and supplier invoices', async () => {
    renderPage();

    // The open PO renders with its supplier.
    expect(await screen.findByText('Total Energies')).toBeInTheDocument();

    // Expand the linkage row.
    fireEvent.click(screen.getByRole('button', { name: /expand linkage/i }));

    // Linked goods receipts (deliveries) and supplier invoice both appear; the
    // invoice list is fetched filtered to this PO.
    await waitFor(() => expect(screen.getByText('SINV-9001')).toBeInTheDocument());
    expect(listSupplierInvoices).toHaveBeenCalledWith(
      expect.objectContaining({ purchaseOrderID: 'po-1' }),
      expect.anything(),
    );
    expect(screen.getByText('Goods receipts (deliveries)')).toBeInTheDocument();
  });

  it('checks the purchase orders PDF with held purchase_order.read permission', async () => {
    renderPage();

    expect(await screen.findByRole('button', { name: /view pos/i })).toBeEnabled();
    expect(screen.getByRole('button', { name: /pos pdf/i })).toBeEnabled();
    expect(usePermission).toHaveBeenCalledWith(
      'purchase_order.read',
      expect.objectContaining({ mode: 'held' }),
    );
  });
});
