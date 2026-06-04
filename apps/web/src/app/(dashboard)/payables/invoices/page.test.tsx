import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import {
  SdkError,
  type PurchaseOrder,
  type Station,
  type Supplier,
  type SupplierInvoice,
} from '@fuelgrid/sdk';

const listSupplierInvoices = vi.fn();
const getSupplierInvoice = vi.fn();
const getPurchaseOrder = vi.fn();
const approveSupplierInvoice = vi.fn();
const listStations = vi.fn();
const listSuppliers = vi.fn();
const listProducts = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listSupplierInvoices: (...args: unknown[]) => listSupplierInvoices(...args),
    getSupplierInvoice: (...args: unknown[]) => getSupplierInvoice(...args),
    getPurchaseOrder: (...args: unknown[]) => getPurchaseOrder(...args),
    approveSupplierInvoice: (...args: unknown[]) => approveSupplierInvoice(...args),
    listStations: (...args: unknown[]) => listStations(...args),
    listSuppliers: (...args: unknown[]) => listSuppliers(...args),
    listProducts: (...args: unknown[]) => listProducts(...args),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

vi.mock('@/lib/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import PayablesInvoicesPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <PayablesInvoicesPage />
    </QueryClientProvider>,
  );
}

const supplier: Supplier = { id: 'sup-1', name: 'Total Energies' } as Supplier;
const station: Station = { id: 'stn-1', name: 'Westlands' } as Station;

const matchedInvoice: SupplierInvoice = {
  id: 'inv-1',
  tenant_id: 't',
  supplier_id: 'sup-1',
  purchase_order_id: 'po-1',
  station_id: 'stn-1',
  invoice_number: 'SINV-0001',
  status: 'matched',
  received_at: '2026-05-01T08:00:00Z',
  due_date: '2026-05-15',
  total_amount: '12500000.00',
  recorded_by: 'u-1',
  lines: [
    {
      id: 'l-1',
      tenant_id: 't',
      supplier_invoice_id: 'inv-1',
      purchase_order_id: 'po-1',
      po_line_id: 'pol-1',
      delivery_id: 'del-1',
      product_id: 'prod-1',
      invoiced_litres: 5000,
      unit_price: '2500.00',
      amount: '12500000.00',
    },
  ],
  discrepancies: [],
} as SupplierInvoice;

const purchaseOrder: PurchaseOrder = {
  id: 'po-1',
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
} as PurchaseOrder;

describe('PayablesInvoicesPage', () => {
  beforeEach(() => {
    permitted = true;
    listSupplierInvoices.mockReset();
    listStations.mockResolvedValue({ items: [station], count: 1, has_more: false });
    listSuppliers.mockResolvedValue({ items: [supplier], count: 1, has_more: false });
    listProducts.mockResolvedValue({
      items: [{ id: 'prod-1', name: 'AGO' }],
      count: 1,
      has_more: false,
    });
    getSupplierInvoice.mockResolvedValue(matchedInvoice);
    getPurchaseOrder.mockResolvedValue(purchaseOrder);
    approveSupplierInvoice.mockResolvedValue({ ...matchedInvoice, status: 'approved' });
  });

  afterEach(() => vi.clearAllMocks());

  it('lists supplier invoices with number, supplier, amount, and status', async () => {
    listSupplierInvoices.mockResolvedValue({
      items: [matchedInvoice],
      count: 1,
      has_more: false,
    });
    renderPage();

    expect(await screen.findByText('SINV-0001')).toBeInTheDocument();
    // Supplier appears in the filter dropdown and the row.
    expect(screen.getAllByText('Total Energies').length).toBeGreaterThan(0);
    expect(screen.getByText('12,500,000.00')).toBeInTheDocument();
    expect(screen.getByText('matched')).toBeInTheDocument();
  });

  it('shows the empty state when there are no invoices', async () => {
    listSupplierInvoices.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No supplier invoices')).toBeInTheDocument();
  });

  it('shows a no-access error when the list 403s', async () => {
    listSupplierInvoices.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });

  it('opens the detail dialog showing line variance against ordered/received litres', async () => {
    listSupplierInvoices.mockResolvedValue({
      items: [matchedInvoice],
      count: 1,
      has_more: false,
    });
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: /details/i }));

    // Invoiced 5000 vs received 4800 (line is delivery-attributed) -> +200 variance.
    expect(await screen.findByText('AGO')).toBeInTheDocument();
    await waitFor(() => expect(screen.getByText('+200')).toBeInTheDocument());
    // The approve action is offered for a matched invoice.
    expect(screen.getByRole('button', { name: /approve invoice/i })).toBeEnabled();
  });

  it('disables the approve action when the user lacks invoice.approve', async () => {
    permitted = false;
    listSupplierInvoices.mockResolvedValue({
      items: [matchedInvoice],
      count: 1,
      has_more: false,
    });
    renderPage();

    fireEvent.click(await screen.findByRole('button', { name: /details/i }));
    await screen.findByText('AGO');
    expect(screen.getByRole('button', { name: /approve invoice/i })).toBeDisabled();
  });
});
