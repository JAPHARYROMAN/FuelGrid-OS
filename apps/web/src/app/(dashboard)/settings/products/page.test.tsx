import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { Product } from '@fuelgrid/sdk';

// The page calls the SDK client through @/lib/api and reads permissions
// through usePermission. Mock both so the test is deterministic and offline.
const listProducts = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listProducts: (...args: unknown[]) => listProducts(...args),
    productsPdf: vi.fn(),
    createProduct: vi.fn(),
    updateProduct: vi.fn(),
    deleteProduct: vi.fn(),
  },
}));

let permitted = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import ProductsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ProductsPage />
    </QueryClientProvider>,
  );
}

const sampleProduct: Product = {
  id: 'p1',
  code: 'PMS',
  name: 'Premium Motor Spirit',
  category: 'fuel',
  unit: 'litre',
  default_price: '900',
  tax_rate: '0',
  density_kg_m3: null,
  loss_tolerance_percent: '0',
  color: '#f97316',
  status: 'active',
} as unknown as Product;

describe('ProductsPage', () => {
  beforeEach(() => {
    permitted = true;
    listProducts.mockReset();
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the product list with mocked data', async () => {
    listProducts.mockResolvedValue({ items: [sampleProduct], count: 1 });
    renderPage();

    expect(await screen.findByText('Premium Motor Spirit')).toBeInTheDocument();
    expect(screen.getByText('PMS')).toBeInTheDocument();
  });

  it('shows the empty state when there are no products', async () => {
    listProducts.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    expect(await screen.findByText('No products yet')).toBeInTheDocument();
  });

  it('disables the New product control when the user lacks products.manage', async () => {
    permitted = false;
    listProducts.mockResolvedValue({ items: [sampleProduct], count: 1 });
    renderPage();

    await screen.findByText('Premium Motor Spirit');
    const newBtn = screen.getByRole('button', { name: /new product/i });
    expect(newBtn).toBeDisabled();

    // Row mutation controls are gated too.
    expect(screen.getByRole('button', { name: /^edit$/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^delete$/i })).toBeDisabled();
  });

  it('enables the New product control when the user has products.manage', async () => {
    permitted = true;
    listProducts.mockResolvedValue({ items: [sampleProduct], count: 1 });
    renderPage();

    await screen.findByText('Premium Motor Spirit');
    expect(screen.getByRole('button', { name: /new product/i })).toBeEnabled();
  });
});
