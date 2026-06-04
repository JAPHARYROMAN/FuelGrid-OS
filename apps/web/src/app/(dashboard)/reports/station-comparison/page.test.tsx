import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const getStationComparisonReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    getStationComparisonReport: (...args: unknown[]) => getStationComparisonReport(...args),
  },
}));

import StationComparisonPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <StationComparisonPage />
    </QueryClientProvider>,
  );
}

const envelope = {
  metadata: {
    report_key: 'station-comparison',
    title: 'Station Comparison',
    generated_at: '2026-06-04T00:00:00Z',
    period: 'this-month',
  },
  filters_used: { period: 'this-month', stations_in_scope: '2' },
  data_quality: [],
  summary: [
    { label: 'Stations compared', value: '2', unit: 'count' },
    { label: 'Open risk alerts', value: '1', unit: 'count' },
  ],
  chart_data: [
    {
      station: 'MIK-01',
      revenue: '1000.00',
      litres: '400.000',
      gross_margin: '300.00',
      expenses: '100.00',
      net_operating: '200.00',
      risk_alerts: 0,
    },
    {
      station: 'MSA-01',
      revenue: '500.00',
      litres: '200.000',
      gross_margin: '200.00',
      expenses: '50.00',
      net_operating: '150.00',
      risk_alerts: 1,
    },
  ],
  table: {
    columns: [
      'station',
      'revenue',
      'litres',
      'gross_margin',
      'expenses',
      'net_operating',
      'stock_variance',
      'risk_alerts',
      'collections',
    ],
    rows: [
      ['MIK-01', '1000.00', '400.000', '300.00', '100.00', '200.00', '0', '0', '0'],
      ['MSA-01', '500.00', '200.000', '200.00', '50.00', '150.00', '0', '1', '0'],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [],
};

describe('StationComparisonPage', () => {
  beforeEach(() => {
    getStationComparisonReport.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the ranking table across the accessible stations', async () => {
    getStationComparisonReport.mockResolvedValue(envelope);
    renderPage();

    expect(await screen.findByText('Station ranking')).toBeInTheDocument();
    expect(screen.getByText('Stations compared')).toBeInTheDocument();
    expect(screen.getByText('MIK-01')).toBeInTheDocument();
    expect(screen.getByText('MSA-01')).toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getStationComparisonReport.mockRejectedValue(
      new SdkError('forbidden', 403, { error: 'forbidden' }),
    );
    renderPage();

    expect(await screen.findByText('No access to station reports')).toBeInTheDocument();
  });

  it('shows an empty state when no stations are in scope', async () => {
    getStationComparisonReport.mockResolvedValue({
      ...envelope,
      chart_data: [],
      table: { columns: envelope.table.columns, rows: [] },
      summary: [{ label: 'Stations compared', value: '0', unit: 'count' }],
    });
    renderPage();

    expect(await screen.findByText('No stations in scope')).toBeInTheDocument();
  });
});
