import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type Customer, type CustomerInvoice, type CustomerPayment } from '@fuelgrid/sdk';

const listCustomerInvoices = vi.fn();
const listCustomers = vi.fn();
const listCustomerPayments = vi.fn();
const createCustomerInvoice = vi.fn();
const reverseCustomerPayment = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listCustomerInvoices: (...args: unknown[]) => listCustomerInvoices(...args),
    listCustomers: (...args: unknown[]) => listCustomers(...args),
    listCustomerPayments: (...args: unknown[]) => listCustomerPayments(...args),
    getCustomerInvoice: vi.fn(),
    issueCustomerInvoice: vi.fn(),
    customerInvoicePdf: vi.fn(),
    postCustomerPayment: vi.fn(),
    createCustomerInvoice: (...args: unknown[]) => createCustomerInvoice(...args),
    reverseCustomerPayment: (...args: unknown[]) => reverseCustomerPayment(...args),
  },
}));

vi.mock('@/lib/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn() },
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

const postedPayment: CustomerPayment = {
  id: 'pay-1',
  customer_id: 'cust-1',
  payment_date: '2026-02-10',
  method: 'bank_transfer',
  reference: 'REF-9',
  amount: '500.00',
  source_account_key: 'bank',
  status: 'posted',
} as CustomerPayment;

describe('CreditInvoicesPage', () => {
  beforeEach(() => {
    permitted = true;
    listCustomerInvoices.mockReset();
    listCustomers.mockReset();
    listCustomerPayments.mockReset();
    createCustomerInvoice.mockReset();
    reverseCustomerPayment.mockReset();
    listCustomers.mockResolvedValue({ items: [customer], count: 1, has_more: false });
    listCustomerPayments.mockResolvedValue({ items: [], count: 0, has_more: false });
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

  it('enables the New invoice button and submits a draft invoice with lines', async () => {
    const user = userEvent.setup();
    listCustomerInvoices.mockResolvedValue({ items: [], count: 0, has_more: false });
    createCustomerInvoice.mockResolvedValue({ ...draftInvoice });
    renderPage();

    await screen.findByText('No invoices');
    const newBtn = screen.getByRole('button', { name: /new invoice/i });
    expect(newBtn).toBeEnabled();
    await user.click(newBtn);

    // Pick the customer, fill the first line amount, and submit.
    await user.selectOptions(screen.getByLabelText('Customer'), 'cust-1');
    await user.type(screen.getByLabelText('Line 1 amount'), '1200');
    await user.click(screen.getByRole('button', { name: /create invoice/i }));

    await waitFor(() => expect(createCustomerInvoice).toHaveBeenCalledTimes(1));
    const arg = createCustomerInvoice.mock.calls[0]![0] as {
      customer_id: string;
      lines: Array<{ description?: string; amount: string }>;
    };
    expect(arg.customer_id).toBe('cust-1');
    // Money is sent as a decimal string, never a float.
    expect(arg.lines).toEqual([{ description: undefined, amount: '1200' }]);
  });

  it('lists posted payments and reverses one through the confirm dialog', async () => {
    const user = userEvent.setup();
    listCustomerInvoices.mockResolvedValue({ items: [draftInvoice], count: 1, has_more: false });
    listCustomerPayments.mockResolvedValue({ items: [postedPayment], count: 1, has_more: false });
    reverseCustomerPayment.mockResolvedValue({
      payment_id: 'pay-1',
      status: 'voided',
      reversal_entry_id: 'je-1',
    });
    renderPage();

    expect(await screen.findByText('REF-9')).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /^reverse$/i }));

    // Confirm dialog → confirm the reversal.
    await user.click(screen.getByRole('button', { name: /reverse payment/i }));

    await waitFor(() => expect(reverseCustomerPayment).toHaveBeenCalledTimes(1));
    expect(reverseCustomerPayment.mock.calls[0]![0]).toBe('pay-1');
  });

  it('hides the Reverse action for an already-voided payment', async () => {
    listCustomerInvoices.mockResolvedValue({ items: [], count: 0, has_more: false });
    listCustomerPayments.mockResolvedValue({
      items: [{ ...postedPayment, status: 'voided' }],
      count: 1,
      has_more: false,
    });
    renderPage();

    expect(await screen.findByText('voided')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /^reverse$/i })).not.toBeInTheDocument();
  });

  it('disables the Reverse action when the user lacks customer_payment.manage', async () => {
    permitted = false;
    listCustomerInvoices.mockResolvedValue({ items: [], count: 0, has_more: false });
    listCustomerPayments.mockResolvedValue({ items: [postedPayment], count: 1, has_more: false });
    renderPage();

    await screen.findByText('REF-9');
    expect(screen.getByRole('button', { name: /^reverse$/i })).toBeDisabled();
  });
});
