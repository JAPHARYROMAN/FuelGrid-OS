import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type PettyCashFloat } from '@fuelgrid/sdk';

const listPettyCashFloats = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listPettyCashFloats: (...args: unknown[]) => listPettyCashFloats(...args),
    listPettyCashTransactions: vi.fn(),
    createPettyCashFloat: vi.fn(),
    recordPettyCashTransaction: vi.fn(),
    reconcilePettyCash: vi.fn(),
    listStations: vi.fn(),
  },
}));

vi.mock('@/lib/toast', () => ({
  toast: { success: vi.fn(), error: vi.fn(), info: vi.fn() },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import PettyCashPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <PettyCashPage />
    </QueryClientProvider>,
  );
}

const activeFloat: PettyCashFloat = {
  id: 'float-1',
  station_id: 'st-1',
  name: 'Front desk float',
  balance: '12000.00',
  status: 'active',
};

describe('PettyCashPage', () => {
  beforeEach(() => {
    permitted = true;
    listPettyCashFloats.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders floats with balance, status and action controls', async () => {
    listPettyCashFloats.mockResolvedValue({ items: [activeFloat], count: 1, has_more: false });
    renderPage();

    expect(await screen.findByText('Front desk float')).toBeInTheDocument();
    expect(screen.getByText(/12,?000/)).toBeInTheDocument();
    expect(screen.getByText('active')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /record movement/i })).toBeEnabled();
    expect(screen.getByRole('button', { name: /^reconcile$/i })).toBeEnabled();
  });

  it('shows the empty state when there are no floats', async () => {
    listPettyCashFloats.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No petty cash floats')).toBeInTheDocument();
  });

  it('disables movement + reconcile controls when the user lacks petty-cash permissions', async () => {
    permitted = false;
    listPettyCashFloats.mockResolvedValue({ items: [activeFloat], count: 1, has_more: false });
    renderPage();

    await screen.findByText('Front desk float');
    expect(screen.getByRole('button', { name: /new float/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /record movement/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^reconcile$/i })).toBeDisabled();
  });

  it('shows a no-access error when the list 403s', async () => {
    listPettyCashFloats.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
