import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { fireEvent, render, screen, waitFor, within } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type OperatingDay, type Sale, type Shift, type Station } from '@fuelgrid/sdk';

// The page reads sales (station + operating day scoped) and reference data
// through @/lib/api, and the read gate through usePermission. Mock both so the
// test is deterministic and offline.
const listStations = vi.fn();
const listOperatingDays = vi.fn();
const listShifts = vi.fn();
const listProducts = vi.fn();
const listStationSales = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    listOperatingDays: (...args: unknown[]) => listOperatingDays(...args),
    listShifts: (...args: unknown[]) => listShifts(...args),
    listProducts: (...args: unknown[]) => listProducts(...args),
    listStationSales: (...args: unknown[]) => listStationSales(...args),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import SalesPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SalesPage />
    </QueryClientProvider>,
  );
}

const station: Station = { id: 'st-1', name: 'Westlands', code: 'WL' } as Station;
const day: OperatingDay = {
  id: 'day-1',
  station_id: 'st-1',
  business_date: '2026-06-01',
  status: 'open',
} as OperatingDay;
const morning: Shift = { id: 'sh-1', name: 'Morning', station_id: 'st-1' } as Shift;
const evening: Shift = { id: 'sh-2', name: 'Evening', station_id: 'st-1' } as Shift;

function sale(overrides: Partial<Sale>): Sale {
  return {
    id: 'sale-1',
    shift_id: 'sh-1',
    station_id: 'st-1',
    operating_day_id: 'day-1',
    nozzle_id: 'noz-1',
    product_id: 'prod-1',
    tank_id: 'tank-1',
    litres: 40,
    unit_price: '180.00',
    gross_amount: '7200.00',
    tax_rate: '0.16',
    tax_amount: '992.00',
    net_amount: '6208.00',
    margin_amount: '800.00',
    recorded_at: '2026-06-01T08:30:00Z',
    ...overrides,
  } as Sale;
}

const products = {
  items: [
    { id: 'prod-1', code: 'PMS', name: 'Petrol' },
    { id: 'prod-2', code: 'AGO', name: 'Diesel' },
  ],
  count: 2,
  has_more: false,
};

describe('SalesPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockResolvedValue({ items: [station], count: 1, has_more: false });
    listOperatingDays.mockResolvedValue({ items: [day], count: 1, has_more: false });
    listShifts.mockResolvedValue({ items: [morning, evening], count: 2, has_more: false });
    listProducts.mockResolvedValue(products);
    listStationSales.mockResolvedValue({
      items: [sale({})],
      count: 1,
      limit: 50,
      offset: 0,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders recognized sales with valued amounts', async () => {
    renderPage();

    // The clickable row only renders inside the table, so awaiting it proves
    // the sales table (not just the filter dropdowns) has rendered.
    const row = await screen.findByRole('button', { name: /sale sale-1/i });
    // Shift + gross amount are read from within the row (the shift name also
    // appears in the filter dropdown, so scope the lookup to the row).
    expect(within(row).getByText('Morning')).toBeInTheDocument();
    expect(within(row).getByText(/7,?200/)).toBeInTheDocument();
  });

  it('shows the empty state when the day has no recognized sales', async () => {
    listStationSales.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No recognized sales for this day')).toBeInTheDocument();
  });

  it('filters the loaded page by shift', async () => {
    listStationSales.mockResolvedValue({
      items: [sale({ id: 'sale-1', shift_id: 'sh-1' }), sale({ id: 'sale-2', shift_id: 'sh-2' })],
      count: 2,
      has_more: false,
    });
    renderPage();

    // Both sales load as two clickable rows.
    await waitFor(() =>
      expect(screen.getAllByRole('button', { name: /^sale sale-/i })).toHaveLength(2),
    );

    // Narrow to the evening shift — only that row survives.
    fireEvent.change(screen.getByLabelText('Shift filter'), { target: { value: 'sh-2' } });
    await waitFor(() =>
      expect(screen.getAllByRole('button', { name: /^sale sale-/i })).toHaveLength(1),
    );
    // The surviving row is the evening sale; the morning sale is gone from the
    // table (Morning still exists as a filter option, so scope to the row).
    const surviving = screen.getAllByRole('button', { name: /^sale sale-/i })[0]!;
    expect(within(surviving).getByText('Evening')).toBeInTheDocument();
    expect(within(surviving).queryByText('Morning')).not.toBeInTheDocument();
  });

  it('pages forward with Next when has_more is true', async () => {
    listStationSales.mockResolvedValue({
      items: [sale({})],
      count: 1,
      limit: 50,
      offset: 0,
      has_more: true,
    });
    renderPage();

    await screen.findByRole('button', { name: /sale sale-1/i });
    const next = screen.getByRole('button', { name: /^next$/i });
    expect(next).toBeEnabled();
    fireEvent.click(next);

    // Refetch happens at offset 50.
    await waitFor(() =>
      expect(listStationSales).toHaveBeenCalledWith(
        'st-1',
        'day-1',
        expect.objectContaining({ offset: 50 }),
        expect.anything(),
      ),
    );
  });

  it('shows a no-access state when the read permission is denied', async () => {
    permitted = false;
    renderPage();

    expect(await screen.findByText('No access to sales')).toBeInTheDocument();
  });

  it('surfaces a server error from the sales query', async () => {
    listStationSales.mockRejectedValue(new SdkError('boom', 500, { error: 'boom' }));
    renderPage();

    expect(await screen.findByText("Couldn't load sales")).toBeInTheDocument();
  });
});
