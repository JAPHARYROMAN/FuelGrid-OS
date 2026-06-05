import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { Supplier } from '@fuelgrid/sdk';

const listSuppliers = vi.fn();
const listProducts = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listSuppliers: (...args: unknown[]) => listSuppliers(...args),
    listProducts: (...args: unknown[]) => listProducts(...args),
    suppliersPdf: vi.fn(),
    createSupplier: vi.fn(),
    updateSupplier: vi.fn(),
    deactivateSupplier: vi.fn(),
  },
}));

let permitted = true;
const usePermission = vi.fn((_code: string, _opts?: unknown) => permitted);
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string, opts?: unknown) => usePermission(code, opts),
}));

import SuppliersPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SuppliersPage />
    </QueryClientProvider>,
  );
}

const sampleSupplier: Supplier = {
  id: 's1',
  code: 'ACME',
  name: 'Acme Fuels Ltd',
  contact_name: 'Jane Doe',
  contact_email: null,
  contact_phone: null,
  payment_terms_days: 14,
  product_ids: [],
  status: 'active',
} as unknown as Supplier;

describe('SuppliersPage', () => {
  beforeEach(() => {
    permitted = true;
    usePermission.mockClear();
    listSuppliers.mockReset();
    listProducts.mockReset();
    listProducts.mockResolvedValue({ items: [], count: 0 });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the supplier list with mocked data', async () => {
    listSuppliers.mockResolvedValue({ items: [sampleSupplier], count: 1 });
    renderPage();

    expect(await screen.findByText('Acme Fuels Ltd')).toBeInTheDocument();
    expect(screen.getByText('ACME')).toBeInTheDocument();
  });

  it('shows the empty state when there are no suppliers', async () => {
    listSuppliers.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    expect(await screen.findByText('No suppliers yet')).toBeInTheDocument();
  });

  it('disables the mutation controls when the user lacks supplier.manage', async () => {
    permitted = false;
    listSuppliers.mockResolvedValue({ items: [sampleSupplier], count: 1 });
    renderPage();

    await screen.findByText('Acme Fuels Ltd');
    expect(screen.getByRole('button', { name: /new supplier/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^edit$/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^deactivate$/i })).toBeDisabled();
  });

  it('enables the New supplier control when the user has supplier.manage', async () => {
    permitted = true;
    listSuppliers.mockResolvedValue({ items: [sampleSupplier], count: 1 });
    renderPage();

    await screen.findByText('Acme Fuels Ltd');
    expect(screen.getByRole('button', { name: /new supplier/i })).toBeEnabled();
  });

  it('checks the suppliers PDF controls with held purchase_order.read permission', async () => {
    permitted = true;
    listSuppliers.mockResolvedValue({ items: [sampleSupplier], count: 1 });
    renderPage();

    await screen.findByText('Acme Fuels Ltd');

    expect(screen.getByRole('button', { name: /^view$/i })).toBeEnabled();
    expect(screen.getByRole('button', { name: /^download$/i })).toBeEnabled();
    expect(usePermission).toHaveBeenCalledWith(
      'purchase_order.read',
      expect.objectContaining({ mode: 'held' }),
    );
  });
});
