import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getReconciliationReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getReconciliationReport: (...args: unknown[]) => getReconciliationReport(...args),
    exportReport: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import InventoryReconciliationPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <InventoryReconciliationPage />
    </QueryClientProvider>,
  );
}

/** A two-tank reconciliation envelope: one over-tolerance, one within. */
const envelope = {
  metadata: {
    report_key: 'inventory-reconciliation',
    title: 'Inventory Reconciliation',
    generated_at: '2026-06-04T00:00:00Z',
    station_id: 'st-1',
    period: 'current',
  },
  filters_used: { station_id: 'st-1', period: 'current', business_date: '2026-06-04' },
  data_quality: [
    { level: 'warning', message: 'Not all shifts are closed — book balances may still change.' },
  ],
  summary: [
    { label: 'Total variance', value: '-150.000', unit: 'L' },
    { label: 'Variance %', value: '-0.71' },
    { label: 'Over-tolerance tanks', value: '1', unit: 'count' },
    { label: 'Tanks reconciled', value: '2', unit: 'count' },
    { label: 'Variance value', value: '435000.00', unit: 'TZS' },
  ],
  chart_data: [
    {
      tank: 'PMS-01',
      product: 'Petrol',
      product_color: '#f97316',
      opening: '10000.000',
      deliveries: '5000.000',
      sales: '4000.000',
      adjustments: '0.000',
      expected_closing: '11000.000',
      actual_closing: '10870.000',
      variance: '-130.000',
      variance_pct: '-1.18',
      variance_value: '377000.00',
      priced: true,
      tolerance: '0.50',
      over_tolerance: true,
      sealed: false,
    },
    {
      tank: 'AGO-01',
      product: 'Diesel',
      product_color: '#2563eb',
      opening: '8000.000',
      deliveries: '0.000',
      sales: '2000.000',
      adjustments: '0.000',
      expected_closing: '6000.000',
      actual_closing: '5980.000',
      variance: '-20.000',
      variance_pct: '-0.33',
      variance_value: '58000.00',
      priced: true,
      tolerance: '0.50',
      over_tolerance: false,
      sealed: false,
    },
  ],
  table: {
    columns: ['tank', 'product', 'variance', 'variance_pct', 'over_tolerance'],
    rows: [
      ['PMS-01', 'Petrol', '-130.000', '-1.18', 'true'],
      ['AGO-01', 'Diesel', '-20.000', '-0.33', 'false'],
    ],
  },
  insights: [
    {
      severity: 'warning',
      message: 'PMS-01 variance -1.18% exceeds its 0.50% tolerance.',
      recommended_action: 'Investigate possible loss, theft, or a miscalibrated dip.',
    },
  ],
  recommended_actions: ['Investigate possible loss, theft, or a miscalibrated dip.'],
  drilldown: [
    { label: 'Reconciliation console', href: '/api/v1/stations/st-1/reconciliation-overview' },
  ],
  export_options: [{ format: 'csv', url: '/api/v1/stations/st-1/reports/reconciliation.csv' }],
};

describe('InventoryReconciliationPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    getReconciliationReport.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the KPI hero, the waterfall and the variance heatmap', async () => {
    getReconciliationReport.mockResolvedValue(envelope);
    renderPage();

    // KPI hero MetricCards.
    expect(await screen.findByText('Total variance')).toBeInTheDocument();
    expect(screen.getByText('Over-tolerance tanks')).toBeInTheDocument();
    expect(screen.getByText('Variance value')).toBeInTheDocument();

    // The signature waterfall (its heading is asserted by e2e too).
    expect(screen.getByText('Per-tank reconciliation waterfall')).toBeInTheDocument();

    // The variance heatmap renders both tanks; the over-tolerance one is flagged.
    expect(screen.getByText('Variance heatmap')).toBeInTheDocument();
    // The heatmap shows each tank's signed variance % as text (not colour-alone).
    expect(screen.getAllByText('-1.18%').length).toBeGreaterThan(0);
    // The over-tolerance cell carries the textual "Over" chip.
    expect(screen.getAllByText('Over').length).toBeGreaterThan(0);
  });

  it('summarises tolerance status in the right panel', async () => {
    getReconciliationReport.mockResolvedValue(envelope);
    renderPage();

    expect(await screen.findByText('Tolerance status')).toBeInTheDocument();
    expect(screen.getByText(/1 of 2 tank\(s\) breached tolerance/)).toBeInTheDocument();
  });

  it('surfaces the data-quality warning prominently', async () => {
    getReconciliationReport.mockResolvedValue(envelope);
    renderPage();

    expect(
      await screen.findByText('Not all shifts are closed — book balances may still change.'),
    ).toBeInTheDocument();
  });

  it('shows an honest empty state when no tanks are reconciled', async () => {
    getReconciliationReport.mockResolvedValue({
      ...envelope,
      summary: [
        { label: 'Total variance', value: '0.000', unit: 'L' },
        { label: 'Variance %', value: '0' },
        { label: 'Over-tolerance tanks', value: '0', unit: 'count' },
        { label: 'Tanks reconciled', value: '0', unit: 'count' },
      ],
      chart_data: [],
    });
    renderPage();

    expect(await screen.findByText('No tanks reconciled')).toBeInTheDocument();
    expect(screen.getByText('No variance to map')).toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getReconciliationReport.mockRejectedValue(
      new SdkError('forbidden', 403, { error: 'forbidden' }),
    );
    renderPage();

    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });
});
