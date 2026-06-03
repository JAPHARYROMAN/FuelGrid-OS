import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
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

  it('renders supplier aging rows with the joined supplier name', async () => {
    getApAging.mockResolvedValue({
      items: [{ supplier_id: 'sup-1', outstanding: '1500.00', open_count: 3 }],
      count: 1,
      has_more: false,
    });
    renderPage();

    expect(await screen.findByText('Acme Fuels Ltd')).toBeInTheDocument();
    expect(screen.getByText('ACME')).toBeInTheDocument();
    expect(screen.getByText('Outstanding by supplier')).toBeInTheDocument();
  });

  it('shows the empty state when no supplier owes money', async () => {
    getApAging.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No outstanding payables')).toBeInTheDocument();
  });

  it('shows a no-access error when the aging query 403s', async () => {
    getApAging.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
