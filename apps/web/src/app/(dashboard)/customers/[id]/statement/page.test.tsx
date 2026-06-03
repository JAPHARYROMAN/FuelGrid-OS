import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type CustomerStatement } from '@fuelgrid/sdk';

const getCustomerStatement = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    getCustomerStatement: (...args: unknown[]) => getCustomerStatement(...args),
  },
}));

vi.mock('next/navigation', () => ({
  useParams: () => ({ id: 'cust-1' }),
}));

import CustomerStatementPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CustomerStatementPage />
    </QueryClientProvider>,
  );
}

const statement: CustomerStatement = {
  customer: { id: 'cust-1', name: 'Acme Logistics' },
  balance: '300.00',
  entries: [
    {
      id: 'ar-1',
      customer_id: 'cust-1',
      entry_type: 'charge',
      amount: '500.00',
      balance_after: '500.00',
      recorded_at: '2026-02-01T10:00:00Z',
    },
    {
      id: 'ar-2',
      customer_id: 'cust-1',
      entry_type: 'payment',
      amount: '-200.00',
      balance_after: '300.00',
      recorded_at: '2026-02-05T10:00:00Z',
    },
  ],
} as CustomerStatement;

describe('CustomerStatementPage', () => {
  beforeEach(() => {
    getCustomerStatement.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the statement totals and ledger entries', async () => {
    getCustomerStatement.mockResolvedValue(statement);
    renderPage();

    expect(await screen.findByText('Acme Logistics — statement')).toBeInTheDocument();
    // Opening = first entry balance_after - amount = 500 - 500 = 0.
    expect(screen.getByText('Opening balance')).toBeInTheDocument();
    expect(screen.getByText('Closing balance')).toBeInTheDocument();
    expect(screen.getByText('charge')).toBeInTheDocument();
    expect(screen.getByText('payment')).toBeInTheDocument();
  });

  it('shows the empty state when there are no ledger entries', async () => {
    getCustomerStatement.mockResolvedValue({
      customer: { id: 'cust-1', name: 'Acme Logistics' },
      balance: '0',
      entries: [],
    } as unknown as CustomerStatement);
    renderPage();

    expect(await screen.findByText('No ledger activity')).toBeInTheDocument();
  });

  it('shows a no-access error when the statement 403s', async () => {
    getCustomerStatement.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });

  it('shows a not-found error when the customer is missing', async () => {
    getCustomerStatement.mockRejectedValue(new SdkError('missing', 404, { error: 'missing' }));
    renderPage();

    expect(await screen.findByText('Customer not found')).toBeInTheDocument();
  });
});
