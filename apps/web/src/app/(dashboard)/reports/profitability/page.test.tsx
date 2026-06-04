import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getProfitabilityReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getProfitabilityReport: (...args: unknown[]) => getProfitabilityReport(...args),
    exportReport: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import ProfitabilityPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ProfitabilityPage />
    </QueryClientProvider>,
  );
}

const envelope = {
  metadata: {
    report_key: 'profitability',
    title: 'Profitability',
    generated_at: '2026-06-04T00:00:00Z',
    station_id: 'st-1',
    period: 'this-month',
  },
  filters_used: { station_id: 'st-1', period: 'this-month' },
  data_quality: [],
  summary: [
    { label: 'Net revenue', value: '1000.00', unit: 'TZS' },
    { label: 'COGS', value: '700.00', unit: 'TZS' },
    { label: 'Gross margin', value: '300.00', unit: 'TZS' },
    { label: 'Operating expenses', value: '100.00', unit: 'TZS' },
    { label: 'Net operating result', value: '200.00', unit: 'TZS' },
    { label: 'Litres sold', value: '200.000', unit: 'L' },
  ],
  chart_data: [
    {
      product: 'Premium',
      litres: '200.000',
      revenue: '1000.00',
      cogs: '700.00',
      gross_margin: '300.00',
    },
  ],
  table: {
    columns: ['product', 'litres', 'revenue', 'cogs', 'gross_margin'],
    rows: [['Premium', '200.000', '1000.00', '700.00', '300.00']],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/reports/financials.csv?period=this-month' }],
};

describe('ProfitabilityPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    getProfitabilityReport.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the P&L summary metrics and per-product table', async () => {
    getProfitabilityReport.mockResolvedValue(envelope);
    renderPage();

    expect(await screen.findByText('Net operating result')).toBeInTheDocument();
    expect(screen.getByText('Operating expenses')).toBeInTheDocument();
    // Per-product table surfaces the product line.
    expect(screen.getByText('Per-product profitability')).toBeInTheDocument();
    expect(screen.getByText('Premium')).toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getProfitabilityReport.mockRejectedValue(
      new SdkError('forbidden', 403, { error: 'forbidden' }),
    );
    renderPage();

    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });

  it('shows an empty state when there are no stations', async () => {
    listStations.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No stations yet')).toBeInTheDocument();
  });
});
