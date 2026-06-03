import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { Product, Station, Tank } from '@fuelgrid/sdk';

// usePermission backs PermissionGate; mocking it lets us flip the manage
// permission deterministically without wiring the /me/permissions query.
const usePermission = vi.fn<(code: string) => boolean | null>();
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string) => usePermission(code),
}));

// next/link renders an <a>; stub it so the row "Calibration" link doesn't need
// the Next router.
vi.mock('next/link', () => ({
  default: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

const listStations = vi.fn();
const listProducts = vi.fn();
const listTanks = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...a: unknown[]) => listStations(...a),
    listProducts: (...a: unknown[]) => listProducts(...a),
    listTanks: (...a: unknown[]) => listTanks(...a),
    createTank: vi.fn(),
    updateTank: vi.fn(),
  },
}));

import TanksPage from './page';

const station: Station = {
  id: 'st-1',
  tenant_id: 't-1',
  company_id: 'c-1',
  name: 'Main Depot',
  code: 'MD',
  timezone: 'UTC',
  status: 'active',
};

const product: Product = {
  id: 'p-1',
  tenant_id: 't-1',
  code: 'D2',
  name: 'Diesel',
  category: 'fuel',
  unit: 'litre',
  default_price: '1.50',
  tax_rate: '0',
  loss_tolerance_percent: '0.5',
  color: '#123456',
  status: 'active',
};

const tank: Tank = {
  id: 'tk-1',
  tenant_id: 't-1',
  station_id: 'st-1',
  product_id: 'p-1',
  name: 'Tank 1',
  code: 'T1',
  capacity_litres: '10000',
  safe_min_litres: '500',
  safe_max_litres: '9500',
  dead_stock_litres: '100',
  has_water_sensor: false,
  has_temp_sensor: false,
  status: 'active',
};

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <TanksPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  usePermission.mockReturnValue(true);
  listStations.mockResolvedValue({ items: [station], count: 1 });
  listProducts.mockResolvedValue({ items: [product], count: 1 });
  listTanks.mockResolvedValue({ items: [tank], count: 1 });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('TanksPage', () => {
  it('renders the tank list from mocked data', async () => {
    renderPage();

    expect(await screen.findByText('Tank 1')).toBeInTheDocument();
    expect(screen.getByText('T1')).toBeInTheDocument();
    // Product cell resolves via the product lookup.
    expect(screen.getByText('Diesel')).toBeInTheDocument();
  });

  it('shows the empty state when the station has no tanks', async () => {
    listTanks.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    expect(await screen.findByText('No tanks at this station')).toBeInTheDocument();
  });

  it('disables the New tank control when the user lacks tanks.manage', async () => {
    usePermission.mockReturnValue(false);
    renderPage();

    // Wait for the list (and thus the header action) to settle.
    await screen.findByText('Tank 1');

    const newTank = screen.getByRole('button', { name: /new tank/i });
    expect(newTank).toBeDisabled();

    // Per-row Edit is gated too.
    const edit = screen.getByRole('button', { name: 'Edit' });
    expect(edit).toBeDisabled();
  });

  it('enables the New tank control when the user holds tanks.manage', async () => {
    usePermission.mockReturnValue(true);
    renderPage();

    await screen.findByText('Tank 1');
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /new tank/i })).not.toBeDisabled(),
    );
  });
});
