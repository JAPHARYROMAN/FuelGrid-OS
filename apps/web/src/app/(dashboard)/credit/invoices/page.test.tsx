import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type Customer, type CustomerInvoice } from '@fuelgrid/sdk';

const listCustomerInvoices = vi.fn();
const listCustomers = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listCustomerInvoices: (...args: unknown[]) => listCustomerInvoices(...args),
    listCustomers: (...args: unknown[]) => listCustomers(...args),
    getCustomerInvoice: vi.fn(),
    issueCustomerInvoice: vi.fn(),
    customerInvoicePdf: vi.fn(),
    postCustomerPayment: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import CreditInvoicesPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CreditInvoicesPage />
    </QueryClientProvider>,
  );
}

const customer: Customer = { id: 'cust-1', name: 'Acme Logistics' } as Customer;

const draftInvoice: CustomerInvoice = {
  id: 'inv-1',
  customer_id: 'cust-1',
  invoice_number: 'INV-0001',
  invoice_date: '2026-02-01',
  due_date: '2026-03-01',
  amount: '1200.00',
  outstanding_amount: '1200.00',
  source_type: 'manual',
  status: 'draft',
} as CustomerInvoice;

describe('CreditInvoicesPage', () => {
  beforeEach(() => {
    permitted = true;
    listCustomerInvoices.mockReset();
    listCustomers.mockReset();
    listCustomers.mockResolvedValue({ items: [customer], count: 1, has_more: false });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders invoices with customer name, amount, and status', async () => {
    listCustomerInvoices.mockResolvedValue({ items: [draftInvoice], count: 1, has_more: false });
    renderPage();

    expect(await screen.findByText('INV-0001')).toBeInTheDocument();
    // Name appears both as a filter button and in the invoice row.
    expect(screen.getAllByText('Acme Logistics').length).toBeGreaterThan(0);
    // 1,200.00 appears in both the amount and outstanding columns.
    expect(screen.getAllByText('1,200.00').length).toBeGreaterThan(0);
    expect(screen.getByText('draft')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^issue$/i })).toBeEnabled();
  });

  it('shows the empty state when there are no invoices', async () => {
    listCustomerInvoices.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No invoices')).toBeInTheDocument();
  });

  it('disables allocate + issue controls when the user lacks permissions', async () => {
    permitted = false;
    listCustomerInvoices.mockResolvedValue({ items: [draftInvoice], count: 1, has_more: false });
    renderPage();

    await screen.findByText('INV-0001');
    expect(screen.getByRole('button', { name: /allocate payment/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^issue$/i })).toBeDisabled();
  });

  it('shows a no-access error when the list 403s', async () => {
    listCustomerInvoices.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
