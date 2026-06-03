import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type StockAdjustment } from '@fuelgrid/sdk';

const listStockAdjustments = vi.fn();
const listTanks = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStockAdjustments: (...args: unknown[]) => listStockAdjustments(...args),
    listTanks: (...args: unknown[]) => listTanks(...args),
    requestStockAdjustment: vi.fn(),
    approveStockAdjustment: vi.fn(),
    rejectStockAdjustment: vi.fn(),
    postStockAdjustment: vi.fn(),
  },
}));

// usePermission returns this value; null mimics the still-loading state.
let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import StockAdjustmentsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <StockAdjustmentsPage />
    </QueryClientProvider>,
  );
}

const requestedAdjustment: StockAdjustment = {
  id: 'adj-1',
  tank_id: 'tank-1',
  delta_litres: '-250.000',
  reason: 'evaporation loss',
  classification: 'evaporation',
  status: 'requested',
  requested_by: 'user-1',
  requested_at: '2026-02-01T08:00:00Z',
};

const tanksPage = {
  items: [
    {
      id: 'tank-1',
      tenant_id: 't',
      station_id: 'station-1',
      product_id: 'p',
      name: 'AGO-1',
      code: 'AGO1',
      capacity_litres: '30000.000',
      safe_min_litres: '0',
      safe_max_litres: '30000.000',
      dead_stock_litres: '0',
      has_water_sensor: false,
      has_temp_sensor: false,
      status: 'active',
    },
  ],
  count: 1,
  has_more: false,
};

describe('StockAdjustmentsPage', () => {
  beforeEach(() => {
    permitted = true;
    listStockAdjustments.mockReset();
    listTanks.mockReset();
    listTanks.mockResolvedValue(tanksPage);
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the adjustment list with its signed delta and status', async () => {
    listStockAdjustments.mockResolvedValue({
      items: [requestedAdjustment],
      count: 1,
      has_more: false,
    });
    renderPage();

    expect(await screen.findByText('evaporation loss')).toBeInTheDocument();
    expect(screen.getByText('requested')).toBeInTheDocument();
    // The signed delta renders with an explicit minus sign.
    expect(screen.getByText(/-250/)).toBeInTheDocument();
    // With permission, the approve control is live.
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeEnabled();
  });

  it('shows the empty state when there are no adjustments', async () => {
    listStockAdjustments.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No adjustments')).toBeInTheDocument();
  });

  it('disables approve/reject controls when the user lacks the permission', async () => {
    permitted = false;
    listStockAdjustments.mockResolvedValue({
      items: [requestedAdjustment],
      count: 1,
      has_more: false,
    });
    renderPage();

    await screen.findByText('evaporation loss');
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^reject$/i })).toBeDisabled();
  });

  it('shows a no-access error when the list 403s', async () => {
    listStockAdjustments.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
