import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { type OpeningStockRequest, type Tank } from '@fuelgrid/sdk';

const listStations = vi.fn();
const listProducts = vi.fn();
const listTanks = vi.fn();
const listTankLedger = vi.fn();
const listOpeningStockRequests = vi.fn();
const me = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    listProducts: (...args: unknown[]) => listProducts(...args),
    listTanks: (...args: unknown[]) => listTanks(...args),
    listTankLedger: (...args: unknown[]) => listTankLedger(...args),
    listOpeningStockRequests: (...args: unknown[]) => listOpeningStockRequests(...args),
    me: (...args: unknown[]) => me(...args),
    requestOpeningStock: vi.fn(),
    approveOpeningStock: vi.fn(),
    rejectOpeningStock: vi.fn(),
    updateSetupStep: vi.fn(),
  },
}));

vi.mock('@/lib/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import OpeningStockPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OpeningStockPage />
    </QueryClientProvider>,
  );
}

const tank = {
  id: 'tank-1',
  tenant_id: 't',
  station_id: 'st-1',
  product_id: 'prod-1',
  name: 'Tank A',
  code: 'TK-A',
  capacity_litres: '30000.000',
  safe_min_litres: '1000.000',
  safe_max_litres: '29000.000',
  dead_stock_litres: '500.000',
  has_water_sensor: false,
  has_temp_sensor: false,
  status: 'active',
} as Tank;

const draftRequest: OpeningStockRequest = {
  id: 'osr-1',
  tank_id: 'tank-1',
  litres: '15000.000',
  status: 'draft',
  requested_by: 'u-1',
  requested_at: '2026-06-01T00:00:00Z',
};

function seedStationAndProducts() {
  listStations.mockResolvedValue({
    items: [{ id: 'st-1', name: 'Main', code: 'MN' }],
    count: 1,
    has_more: false,
  });
  listProducts.mockResolvedValue({
    items: [{ id: 'prod-1', name: 'Diesel', color: '#000' }],
    count: 1,
    has_more: false,
  });
  listTanks.mockResolvedValue({ items: [tank], count: 1, has_more: false });
  listTankLedger.mockResolvedValue({ items: [], count: 0, has_more: false });
}

describe('OpeningStockPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    listProducts.mockReset();
    listTanks.mockReset();
    listTankLedger.mockReset();
    listOpeningStockRequests.mockReset();
    me.mockReset();
    me.mockResolvedValue({
      user_id: 'current-user',
      tenant_id: 't',
      session_id: 's',
      mfa_satisfied: true,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('shows a submitted (draft) request with approve/reject controls', async () => {
    seedStationAndProducts();
    listOpeningStockRequests.mockResolvedValue({
      items: [draftRequest],
      count: 1,
      has_more: false,
    });
    renderPage();

    expect(await screen.findByText('Tank A')).toBeInTheDocument();
    expect(screen.getByText('Submitted')).toBeInTheDocument();
    expect(screen.getByText(/15,?000/)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeEnabled();
    expect(screen.getByRole('button', { name: /^reject$/i })).toBeEnabled();
  });

  it('shows an Enter control for a tank with no opening request', async () => {
    seedStationAndProducts();
    listOpeningStockRequests.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('Tank A')).toBeInTheDocument();
    expect(screen.getByText('Missing')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^enter$/i })).toBeEnabled();
  });

  it('shows a locked badge once a request is approved', async () => {
    seedStationAndProducts();
    listOpeningStockRequests.mockResolvedValue({
      items: [{ ...draftRequest, status: 'approved', balance_after: '15000.000' }],
      count: 1,
      has_more: false,
    });
    renderPage();

    expect(await screen.findByText('Tank A')).toBeInTheDocument();
    expect(screen.getByText('Approved & locked')).toBeInTheDocument();
  });

  it('disables enter/approve controls when the user lacks station permissions', async () => {
    permitted = false;
    seedStationAndProducts();
    listOpeningStockRequests.mockResolvedValue({
      items: [draftRequest],
      count: 1,
      has_more: false,
    });
    renderPage();

    await screen.findByText('Tank A');
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^reject$/i })).toBeDisabled();
  });

  it('disables approval when the current user submitted the draft', async () => {
    seedStationAndProducts();
    me.mockResolvedValue({
      user_id: 'u-1',
      tenant_id: 't',
      session_id: 's',
      mfa_satisfied: true,
    });
    listOpeningStockRequests.mockResolvedValue({
      items: [draftRequest],
      count: 1,
      has_more: false,
    });
    renderPage();

    await screen.findByText('Tank A');
    expect(screen.getByText('Submitted by you; another user must approve.')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^reject$/i })).toBeDisabled();
  });
});
