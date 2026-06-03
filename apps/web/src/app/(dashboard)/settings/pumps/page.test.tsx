import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { Nozzle, Product, Pump, Station, Tank } from '@fuelgrid/sdk';

// usePermission backs PermissionGate; mocking it flips pumps.manage
// deterministically.
const usePermission = vi.fn<(code: string) => boolean | null>();
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string) => usePermission(code),
}));

vi.mock('next/link', () => ({
  default: ({ children, href }: { children: React.ReactNode; href: string }) => (
    <a href={href}>{children}</a>
  ),
}));

const listStations = vi.fn();
const listProducts = vi.fn();
const listTanks = vi.fn();
const listPumps = vi.fn();
const listNozzles = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...a: unknown[]) => listStations(...a),
    listProducts: (...a: unknown[]) => listProducts(...a),
    listTanks: (...a: unknown[]) => listTanks(...a),
    listPumps: (...a: unknown[]) => listPumps(...a),
    listNozzles: (...a: unknown[]) => listNozzles(...a),
    createPump: vi.fn(),
    deletePump: vi.fn(),
    createNozzle: vi.fn(),
    deleteNozzle: vi.fn(),
  },
}));

import PumpsPage from './page';

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

const pump: Pump = {
  id: 'pm-1',
  tenant_id: 't-1',
  station_id: 'st-1',
  number: 1,
  name: 'Forecourt A',
  status: 'active',
};

const nozzle: Nozzle = {
  id: 'nz-1',
  tenant_id: 't-1',
  station_id: 'st-1',
  pump_id: 'pm-1',
  tank_id: 'tk-1',
  product_id: 'p-1',
  number: 1,
  default_price: '1.50',
  meter_decimal_places: 2,
  status: 'active',
};

function renderPage() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <PumpsPage />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  usePermission.mockReturnValue(true);
  listStations.mockResolvedValue({ items: [station], count: 1 });
  listProducts.mockResolvedValue({ items: [product], count: 1 });
  listTanks.mockResolvedValue({ items: [tank], count: 1 });
  listPumps.mockResolvedValue({ items: [pump], count: 1 });
  listNozzles.mockResolvedValue({ items: [nozzle], count: 1 });
});

afterEach(() => {
  vi.clearAllMocks();
});

describe('PumpsPage', () => {
  it('renders the pump list from mocked data', async () => {
    renderPage();

    expect(await screen.findByText('Pump 1')).toBeInTheDocument();
    expect(screen.getByText('Forecourt A')).toBeInTheDocument();
    expect(screen.getByText('1 nozzle')).toBeInTheDocument();
  });

  it('shows the empty state when the station has no pumps', async () => {
    listPumps.mockResolvedValue({ items: [], count: 0 });
    listNozzles.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    expect(await screen.findByText('No pumps at this station')).toBeInTheDocument();
  });

  it('disables the manage controls when the user lacks pumps.manage', async () => {
    usePermission.mockReturnValue(false);
    renderPage();

    await screen.findByText('Pump 1');

    expect(screen.getByRole('button', { name: /new pump/i })).toBeDisabled();
    // The add-nozzle control's accessible name is exactly "Nozzle".
    expect(screen.getByRole('button', { name: 'Nozzle' })).toBeDisabled();
  });

  it('enables the New pump control when the user holds pumps.manage', async () => {
    usePermission.mockReturnValue(true);
    renderPage();

    await screen.findByText('Pump 1');
    await waitFor(() =>
      expect(screen.getByRole('button', { name: /new pump/i })).not.toBeDisabled(),
    );
  });
});
