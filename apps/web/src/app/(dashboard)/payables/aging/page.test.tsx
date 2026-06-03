import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const getApAging = vi.fn();
const listSuppliers = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    getApAging: (...args: unknown[]) => getApAging(...args),
    listSuppliers: (...args: unknown[]) => listSuppliers(...args),
    supplierBalancesPdf: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import PayablesAgingPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <PayablesAgingPage />
    </QueryClientProvider>,
  );
}

describe('PayablesAgingPage', () => {
  beforeEach(() => {
    permitted = true;
    getApAging.mockReset();
    listSuppliers.mockReset();
    listSuppliers.mockResolvedValue({
      items: [{ id: 'sup-1', code: 'ACME', name: 'Acme Fuels Ltd' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  const bucketRow = {
    supplier_id: 'sup-1',
    outstanding: '1500.00',
    open_count: 3,
    current: '150.00',
    d1_30: '200.00',
    d31_60: '300.00',
    d61_90: '400.00',
    d90_plus: '450.00',
  };
  const totalsRow = {
    supplier_id: '00000000-0000-0000-0000-000000000000',
    outstanding: '1500.00',
    open_count: 3,
    current: '150.00',
    d1_30: '200.00',
    d31_60: '300.00',
    d61_90: '400.00',
    d90_plus: '450.00',
  };

  it('renders day-aged bucket columns and a server-computed totals row', async () => {
    getApAging.mockResolvedValue({
      items: [bucketRow],
      count: 1,
      has_more: false,
      totals: totalsRow,
    });
    renderPage();

    expect(await screen.findByText('Acme Fuels Ltd')).toBeInTheDocument();
    expect(screen.getByText('Aging by supplier')).toBeInTheDocument();
    // Bucket column headers are present.
    expect(screen.getByRole('columnheader', { name: 'Current' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: '1–30' })).toBeInTheDocument();
    expect(screen.getByRole('columnheader', { name: '90+' })).toBeInTheDocument();

    // The totals row reflects the server totals (not re-summed client-side).
    const totals = screen.getByText('Totals').closest('tr')!;
    expect(within(totals).getByText('300.00')).toBeInTheDocument();
    expect(within(totals).getByText('1,500.00')).toBeInTheDocument();
  });

  it('expands a supplier row to show the bucket drilldown', async () => {
    getApAging.mockResolvedValue({
      items: [bucketRow],
      count: 1,
      has_more: false,
      totals: totalsRow,
    });
    renderPage();

    const toggle = await screen.findByRole('button', {
      name: /Toggle bucket detail for Acme Fuels Ltd/,
    });
    expect(toggle).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(toggle);
    expect(toggle).toHaveAttribute('aria-expanded', 'true');
    // Drilldown surfaces the supplier code + open invoice count.
    expect(screen.getByText('ACME')).toBeInTheDocument();
    expect(screen.getByText('Open invoices')).toBeInTheDocument();
  });

  it('shows the empty state when no supplier owes money', async () => {
    getApAging.mockResolvedValue({
      items: [],
      count: 0,
      has_more: false,
      totals: {
        ...totalsRow,
        outstanding: '0',
        open_count: 0,
        current: '0',
        d1_30: '0',
        d31_60: '0',
        d61_90: '0',
        d90_plus: '0',
      },
    });
    renderPage();

    expect(await screen.findByText('No outstanding payables')).toBeInTheDocument();
  });

  it('shows a no-access error when the aging query 403s', async () => {
    getApAging.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
