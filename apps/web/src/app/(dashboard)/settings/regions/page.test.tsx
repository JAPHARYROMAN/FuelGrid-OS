import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { Company, Region } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

const listRegions = vi.fn();
const listCompanies = vi.fn();
const mePermissions = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listRegions: (...args: unknown[]) => listRegions(...args),
    listCompanies: (...args: unknown[]) => listCompanies(...args),
    mePermissions: (...args: unknown[]) => mePermissions(...args),
  },
}));

import RegionsPage from './page';

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

function withPermission(allowed: boolean) {
  mePermissions.mockResolvedValue({
    permissions: allowed ? [{ code: 'regions.manage', station_scoped: false }] : [],
    station_ids: [],
    tenant_wide: true,
  });
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RegionsPage />
    </QueryClientProvider>,
  );
}

describe('RegionsPage', () => {
  beforeEach(() => {
    listRegions.mockReset();
    listCompanies.mockReset();
    mePermissions.mockReset();
    listCompanies.mockResolvedValue({ items: [sampleCompany], count: 1 });
    useAuthStore.setState({ authed: true, expiresAt: null, hydrated: true });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the list with mocked data', async () => {
    listRegions.mockResolvedValue({ items: [sampleRegion], count: 1 });
    withPermission(true);

    renderPage();

    expect(await screen.findByText('Coastal')).toBeInTheDocument();
    // Company name resolved via the company lookup.
    expect(screen.getByText('Acme Fuels')).toBeInTheDocument();
  });

  it('shows the empty state when there are no regions', async () => {
    listRegions.mockResolvedValue({ items: [], count: 0 });
    withPermission(true);

    renderPage();

    expect(await screen.findByText('No regions yet')).toBeInTheDocument();
  });

  it('disables the manage control when the user lacks regions.manage', async () => {
    listRegions.mockResolvedValue({ items: [sampleRegion], count: 1 });
    withPermission(false);

    renderPage();

    await screen.findByText('Coastal');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
    });
    expect(screen.getByRole('button', { name: /New region/i })).toBeDisabled();
  });
});
