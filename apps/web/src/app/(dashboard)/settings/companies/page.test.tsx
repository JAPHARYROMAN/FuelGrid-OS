import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';

import type { Company } from '@fuelgrid/sdk';

import { useAuthStore } from '@/stores/auth-store';

const listCompanies = vi.fn();
const mePermissions = vi.fn();

// The page talks to the SDK only through the shared api client; usePermissions
// (behind PermissionGate) calls api.mePermissions. Mock both at the api layer
// so the test is deterministic and never hits the network.
vi.mock('@/lib/api', () => ({
  api: {
    listCompanies: (...args: unknown[]) => listCompanies(...args),
    mePermissions: (...args: unknown[]) => mePermissions(...args),
  },
}));

import CompaniesPage from './page';

const sampleCompany: Company = {
  id: 'c1',
  tenant_id: 't1',
  name: 'Acme Fuels',
  legal_name: 'Acme Fuels Ltd',
  currency: 'USD',
  timezone: 'UTC',
  status: 'active',
};

function withPermission(allowed: boolean) {
  mePermissions.mockResolvedValue({
    permissions: allowed ? [{ code: 'companies.manage', station_scoped: false }] : [],
    station_ids: [],
    tenant_wide: true,
  });
}

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CompaniesPage />
    </QueryClientProvider>,
  );
}

describe('CompaniesPage', () => {
  beforeEach(() => {
    listCompanies.mockReset();
    mePermissions.mockReset();
    // usePermissions is gated on an authenticated session.
    useAuthStore.setState({ authed: true, expiresAt: null, hydrated: true });
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it('renders the list with mocked data', async () => {
    listCompanies.mockResolvedValue({ items: [sampleCompany], count: 1 });
    withPermission(true);

    renderPage();

    expect(await screen.findByText('Acme Fuels')).toBeInTheDocument();
    expect(screen.getByText('Acme Fuels Ltd')).toBeInTheDocument();
  });

  it('shows the empty state when there are no companies', async () => {
    listCompanies.mockResolvedValue({ items: [], count: 0 });
    withPermission(true);

    renderPage();

    expect(await screen.findByText('No companies yet')).toBeInTheDocument();
  });

  it('disables the manage control when the user lacks companies.manage', async () => {
    listCompanies.mockResolvedValue({ items: [sampleCompany], count: 1 });
    withPermission(false);

    renderPage();

    // Row renders first, then the permission set resolves and disables Edit.
    await screen.findByText('Acme Fuels');
    await waitFor(() => {
      expect(screen.getByRole('button', { name: 'Edit' })).toBeDisabled();
    });
    expect(screen.getByRole('button', { name: /New company/i })).toBeDisabled();
  });
});
