import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { EnterpriseScopes } from '@fuelgrid/sdk';

const listEnterpriseScopes = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listEnterpriseScopes: (...args: unknown[]) => listEnterpriseScopes(...args),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import { ScopeSwitcher } from './scope-switcher';
import { useTenantStore } from '@/stores/tenant-store';

function renderSwitcher() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ScopeSwitcher />
    </QueryClientProvider>,
  );
}

const companyScope: EnterpriseScopes = {
  tenant_wide: false,
  scopes: [
    { scope_type: 'company', scope_id: 'co-1', label: 'Acme Fuels', station_count: 2 },
    { scope_type: 'station', scope_id: 'st-1', label: 'Highway 9', station_count: 1 },
  ],
};

describe('ScopeSwitcher', () => {
  beforeEach(() => {
    permitted = true;
    listEnterpriseScopes.mockReset();
    useTenantStore.getState().reset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders nothing without the enterprise.scope.switch permission', () => {
    permitted = false;
    const { container } = renderSwitcher();
    expect(container).toBeEmptyDOMElement();
    expect(listEnterpriseScopes).not.toHaveBeenCalled();
  });

  it('renders nothing when the user is tenant-wide', async () => {
    listEnterpriseScopes.mockResolvedValue({ tenant_wide: true, scopes: [] });
    const { container } = renderSwitcher();
    // Wait for the query to resolve past the loading skeleton, then the switcher
    // collapses to nothing because there is nothing to switch between.
    await waitFor(() => expect(container).toBeEmptyDOMElement());
  });

  it('shows the switcher trigger labelled "All stations" when no scope is active', async () => {
    listEnterpriseScopes.mockResolvedValue(companyScope);
    renderSwitcher();

    const trigger = await screen.findByLabelText('Active scope');
    expect(trigger).toHaveTextContent('All stations');
  });

  it('shows the active scope label on the trigger when one is selected', async () => {
    listEnterpriseScopes.mockResolvedValue(companyScope);
    useTenantStore.getState().setActiveScope({ type: 'company', id: 'co-1', label: 'Acme Fuels' });
    renderSwitcher();

    const trigger = await screen.findByLabelText('Active scope');
    expect(trigger).toHaveTextContent('Acme Fuels');
  });
});
