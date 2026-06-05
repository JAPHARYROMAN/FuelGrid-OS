import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { Nozzle, Product, Pump, Station, Tank } from '@fuelgrid/sdk';

// usePermission backs PermissionGate; mocking it flips pumps.manage
// deterministically.
const usePermission = vi.fn<(code: string, opts?: unknown) => boolean | null>();
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string, opts?: unknown) => usePermission(code, opts),
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
const createPump = vi.fn();
const deletePump = vi.fn();
const createNozzle = vi.fn();
const deleteNozzle = vi.fn();
const setNozzleInitialMeter = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...a: unknown[]) => listStations(...a),
    listProducts: (...a: unknown[]) => listProducts(...a),
    listTanks: (...a: unknown[]) => listTanks(...a),
    listPumps: (...a: unknown[]) => listPumps(...a),
    listNozzles: (...a: unknown[]) => listNozzles(...a),
    createPump: (...a: unknown[]) => createPump(...a),
    deletePump: (...a: unknown[]) => deletePump(...a),
    createNozzle: (...a: unknown[]) => createNozzle(...a),
    deleteNozzle: (...a: unknown[]) => deleteNozzle(...a),
    setNozzleInitialMeter: (...a: unknown[]) => setNozzleInitialMeter(...a),
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
  initial_meter_reading: '500.5',
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
  createPump.mockResolvedValue(pump);
  createNozzle.mockResolvedValue({ ...nozzle, id: 'nz-2', number: 2 });
  setNozzleInitialMeter.mockResolvedValue({ ...nozzle, initial_meter_reading: '501' });
  deletePump.mockResolvedValue(undefined);
  deleteNozzle.mockResolvedValue(undefined);
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

  it('shows the nozzle initial meter and can adjust it', async () => {
    const user = userEvent.setup();
    renderPage();

    await user.click(await screen.findByRole('button', { name: /Pump 1/i }));

    expect(await screen.findByText(/Initial 500.5/i)).toBeInTheDocument();
    await user.click(screen.getByRole('button', { name: /Adjust meter/i }));
    await user.clear(screen.getByLabelText(/Meter reading/i));
    await user.type(screen.getByLabelText(/Meter reading/i), '501.00');
    await user.type(screen.getByLabelText('Note'), 'meter serviced');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() =>
      expect(setNozzleInitialMeter).toHaveBeenCalledWith('nz-1', {
        reading: '501',
        note: 'meter serviced',
      }),
    );
  });

  it('sends an initial meter reading when creating a nozzle', async () => {
    const user = userEvent.setup();
    renderPage();

    await screen.findByText('Pump 1');
    await user.click(screen.getByRole('button', { name: 'Nozzle' }));
    await user.selectOptions(screen.getByLabelText('Tank'), 'tk-1');
    await user.type(screen.getByLabelText(/Initial meter$/i), '1000.25');
    await user.type(screen.getByLabelText(/Initial meter note/i), 'physical install');
    await user.click(screen.getByRole('button', { name: 'Save' }));

    await waitFor(() =>
      expect(createNozzle).toHaveBeenCalledWith(
        expect.objectContaining({
          pump_id: 'pm-1',
          tank_id: 'tk-1',
          initial_meter_reading: '1000.25',
          initial_meter_note: 'physical install',
        }),
      ),
    );
  });
});
