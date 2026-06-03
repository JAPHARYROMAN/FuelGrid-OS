import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { Company, Region, Station } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

const listStations = vi.fn();
const listRegions = vi.fn();
const listCompanies = vi.fn();
const mePermissions = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    listRegions: (...args: unknown[]) => listRegions(...args),
    listCompanies: (...args: unknown[]) => listCompanies(...args),
    mePermissions: (...args: unknown[]) => mePermissions(...args),
  },
}));

import StationsPage from './page';

const sampleCompany: Company = {
  id: 'c1',
  tenant_id: 't1',
  name: 'Acme Fuels',
  currency: 'USD',
  timezone: 'UTC',
  status: 'active',
};

const sampleRegion: Region = {
  id: 'r1',
  tenant_id: 't1',
  company_id: 'c1',
  name: 'Coastal',
  code: 'CST',
  status: 'active',
};

const sampleStation: Station = {
  id: 's1',
  tenant_id: 't1',
  company_id: 'c1',
  region_id: 'r1',
  name: 'Mikocheni',
  code: 'MIK-01',
  city: 'Dar es Salaam',
  country: 'TZ',
  timezone: 'Africa/Dar_es_Salaam',
  status: 'active',
};

function withPermission(allowed: boolean) {
  mePermissions.mockResolvedValue({
    permissions: allowed ? [{ code: 'station.manage', station_scoped: false }] : [],
    station_ids: [],
    tenant_wide: true,
  });
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <StationsPage />
    </QueryClientProvider>,
  );
}

describe('StationsPage', () => {
  beforeEach(() => {
    listStations.mockReset();
    listRegions.mockReset();
    listCompanies.mockReset();
    mePermissions.mockReset();
    listCompanies.mockResolvedValue({ items: [sampleCompany], count: 1 });
    listRegions.mockResolvedValue({ items: [sampleRegion], count: 1 });
    useAuthStore.setState({ authed: true, expiresAt: null, hydrated: true });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the list with mocked data', async () => {
    listStations.mockResolvedValue({ items: [sampleStation], count: 1 });
    withPermission(true);

    renderPage();

    expect(await screen.findByText('Mikocheni')).toBeInTheDocument();
    expect(screen.getByText('MIK-01')).toBeInTheDocument();
    expect(screen.getByText('Coastal')).toBeInTheDocument();
  });

  it('shows the empty state when there are no stations', async () => {
    listStations.mockResolvedValue({ items: [], count: 0 });
    withPermission(true);

    renderPage();

    expect(await screen.findByText('No stations yet')).toBeInTheDocument();
  });

  it('disables the manage control when the user lacks station.manage', async () => {
    listStations.mockResolvedValue({ items: [sampleStation], count: 1 });
    withPermission(false);

    renderPage();

    await screen.findByText('Mikocheni');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
    });
    expect(screen.getByRole('button', { name: /New station/i })).toBeDisabled();
  });
});
