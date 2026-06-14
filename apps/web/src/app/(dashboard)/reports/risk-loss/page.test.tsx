import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getRiskLossReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getRiskLossReport: (...args: unknown[]) => getRiskLossReport(...args),
    exportReport: vi.fn(),
  },
}));

// The page calls usePermission twice (reconciliation.read for exports, risk.read
// for the tuning link); a permissive mock renders every affordance.
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => true,
}));

import RiskLossPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RiskLossPage />
    </QueryClientProvider>,
  );
}

/** A margin.view holder's envelope: loss value present, full §5.11 visuals. */
const valueShownEnvelope = {
  metadata: {
    report_key: 'risk-loss',
    title: 'Risk & Loss Intelligence',
    generated_at: '2026-06-14T00:00:00Z',
    station_id: 'st-1',
    period: 'current',
  },
  filters_used: { station_id: 'st-1', period: 'current' },
  data_quality: [],
  summary: [
    { label: 'Total loss litres', value: '750.000', unit: 'L' },
    { label: 'Loss value', value: '2212500.00', unit: 'TZS' },
    { label: 'Over-tolerance events', value: '3', unit: 'count' },
    { label: 'Repeated-incident tanks', value: '1', unit: 'count' },
    { label: 'Open risk alerts', value: '2', unit: 'count' },
    { label: 'Open investigations', value: '1', unit: 'count' },
    { label: 'Highest-risk station', value: 'Mikocheni' },
  ],
  chart_data: {
    heatmap: [
      {
        station: 'Mikocheni',
        cells: {
          'Variance events': 3,
          'Repeated tanks': 1,
          'Open alerts': 2,
          'Open investigations': 1,
        },
      },
    ],
    heat_types: ['Variance events', 'Repeated tanks', 'Open alerts', 'Open investigations'],
    trend: [
      { date: '2026-05-20', loss_litres: '300.000', loss_value: '885000.00', events: 1 },
      { date: '2026-05-21', loss_litres: '250.000', loss_value: '737500.00', events: 1 },
      { date: '2026-05-22', loss_litres: '200.000', loss_value: '590000.00', events: 1 },
    ],
    ranking: [{ station: 'Mikocheni', score: 75, band: 'high', open_alerts: 2 }],
    distribution: [
      { key: 'Pump:03', label: 'Pump 03', value: '2' },
      { key: 'Shift:Evening', label: 'Shift Evening', value: '2' },
    ],
    alert_board: [
      {
        key: 'high',
        label: 'High',
        status: '2 open',
        tone: 'at_risk',
        count: 2,
        detail: 'High severity alerts',
      },
    ],
    investigations: [
      { title: 'PMS loss', status: 'current', when: '2026-05-22', detail: 'fuel_loss · in_review' },
    ],
    patterns: [
      { dimension: 'shift', label: 'Evening', count: 2, total: 3, share_pct: 67 },
      { dimension: 'pump', label: '03', count: 2, total: 3, share_pct: 67 },
    ],
    rules: [
      {
        code: 'fuel_loss',
        name: 'Fuel variance over tolerance',
        condition: 'fuel_variance_over_tolerance',
        severity: 'high',
        threshold: '',
        enabled: true,
        status: 'active',
      },
    ],
    value_shown: true,
  },
  table: {
    columns: [
      'business_date',
      'product',
      'pump',
      'variance_pct',
      'loss_litres',
      'over_tolerance',
      'shift',
      'attendant',
    ],
    rows: [['2026-05-20', 'Premium', '03', '-6.0', '300.000', 'true', 'Evening', 'Asha']],
  },
  insights: [
    {
      severity: 'warning',
      message: 'Shift "Evening" appeared in 67% of related variance events (2 of 3).',
    },
  ],
  recommended_actions: [],
  drilldown: [{ label: 'Risk alerts', href: '/api/v1/risk/alerts?status=open' }],
  export_options: [{ format: 'csv', url: '/api/v1/stations/st-1/reports/reconciliation.csv' }],
};

/** A non-margin actor's envelope: loss value omitted + a DQ note. */
const valueHiddenEnvelope = {
  ...valueShownEnvelope,
  data_quality: [
    {
      level: 'warning',
      message:
        'Loss value is hidden — it requires the margin.view permission. Loss litres and counts are shown in full.',
    },
  ],
  summary: [
    { label: 'Total loss litres', value: '750.000', unit: 'L' },
    { label: 'Over-tolerance events', value: '3', unit: 'count' },
    { label: 'Repeated-incident tanks', value: '1', unit: 'count' },
    { label: 'Open risk alerts', value: '2', unit: 'count' },
    { label: 'Open investigations', value: '1', unit: 'count' },
  ],
  chart_data: { ...valueShownEnvelope.chart_data, value_shown: false },
};

describe('RiskLossPage', () => {
  beforeEach(() => {
    listStations.mockReset();
    getRiskLossReport.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the KPI hero, the §5.11 pattern intelligence and the reused visuals', async () => {
    getRiskLossReport.mockResolvedValue(valueShownEnvelope);
    renderPage();

    // KPI hero: loss litres + the gated loss value (margin.view holder).
    expect((await screen.findAllByText('Total loss litres')).length).toBeGreaterThan(0);
    expect(screen.getByText('Loss value')).toBeInTheDocument();
    expect(screen.getByText('Highest-risk station')).toBeInTheDocument();

    // The deterministic §5.11 pattern intelligence card + a traceable finding.
    expect(screen.getByText('Pattern intelligence')).toBeInTheDocument();
    expect(screen.getAllByText('67%').length).toBeGreaterThan(0);

    // The reused visuals each render their card title.
    expect(screen.getByText('Risk heatmap')).toBeInTheDocument();
    expect(screen.getByText('Loss trend')).toBeInTheDocument();
    expect(screen.getByText('Root-cause distribution')).toBeInTheDocument();
    expect(screen.getByText('Alert severity')).toBeInTheDocument();
    expect(screen.getByText('Investigations')).toBeInTheDocument();
    expect(screen.getByText('Station risk ranking')).toBeInTheDocument();
    expect(screen.getByText('Rules driving alerts')).toBeInTheDocument();

    // The drillable variance-event history table.
    expect(screen.getByText('Variance event history')).toBeInTheDocument();
  });

  it('omits the loss value and surfaces the margin-hidden note for a non-margin actor', async () => {
    getRiskLossReport.mockResolvedValue(valueHiddenEnvelope);
    renderPage();

    // Loss litres stay; the loss VALUE KPI is omitted (not zeroed).
    expect(await screen.findByText('Total loss litres')).toBeInTheDocument();
    expect(screen.queryByText('Loss value')).not.toBeInTheDocument();
    // The omit-not-zero data-quality note is shown.
    expect(
      screen.getByText(/Loss value is hidden — it requires the margin.view permission/i),
    ).toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getRiskLossReport.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });

  it('shows an empty state when there are no stations', async () => {
    listStations.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No stations yet')).toBeInTheDocument();
  });
});
