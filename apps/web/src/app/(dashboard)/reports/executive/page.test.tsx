import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const getExecutiveReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    getExecutiveReport: (...args: unknown[]) => getExecutiveReport(...args),
  },
}));

import ExecutiveReportPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ExecutiveReportPage />
    </QueryClientProvider>,
  );
}

/** A margin-holder envelope: margin + loss value present, full narrative. */
const fullEnvelope = {
  metadata: {
    report_key: 'executive',
    title: 'Executive Business Report',
    generated_at: '2026-06-14T00:00:00Z',
    period: 'this-month',
  },
  filters_used: { period: 'this-month', stations_in_scope: '2' },
  data_quality: [],
  summary: [
    { label: 'Total revenue', value: '1500.00', unit: 'TZS' },
    { label: 'Total litres', value: '600.000', unit: 'L' },
    { label: 'Gross margin', value: '500.00', unit: 'TZS' },
    { label: 'Net margin', value: '350.00', unit: 'TZS' },
    { label: 'Total loss litres', value: '120.000', unit: 'L' },
    { label: 'Loss value', value: '354000.00', unit: 'TZS' },
    { label: 'Open risk alerts', value: '1', unit: 'count' },
    { label: 'Open investigations', value: '0', unit: 'count' },
    { label: 'Stations in scope', value: '2', unit: 'count' },
    { label: 'Top station', value: 'MIK-01' },
    { label: 'Underperforming station', value: 'MSA-01' },
  ],
  chart_data: {
    narrative: {
      sentences: [
        'Your network sold 600.000 L this period across 2 stations.',
        'Revenue rose 50.0% vs the prior period (1500.00 vs 1000.00 TZS).',
      ],
      focus: 'Recommended focus: clear the 1 open risk alert.',
    },
    stations: [
      {
        station: 'MIK-01',
        revenue: '1000.00',
        litres: '400.000',
        net_operating: '200.00',
        risk_alerts: 0,
      },
      {
        station: 'MSA-01',
        revenue: '500.00',
        litres: '200.000',
        net_operating: '150.00',
        risk_alerts: 1,
      },
    ],
    waterfall: [
      { key: 'revenue', label: 'Net revenue', value: '1500.00', kind: 'base' },
      { key: 'net_operating', label: 'Net operating result', value: '350.00', kind: 'total' },
    ],
    comparison: [
      {
        key: 'revenue',
        label: 'Revenue',
        current: '1500.00',
        prior: '1000.00',
        delta_pct: '+50.0',
        unit: 'TZS',
      },
      {
        key: 'litres',
        label: 'Litres',
        current: '600.000',
        prior: '500.000',
        delta_pct: '+20.0',
        unit: 'L',
      },
    ],
    loss_summary: {
      loss_litres: '120.000',
      loss_value: '354000.00',
      stock_variance: '300.000',
      value_shown: true,
    },
    margin_shown: true,
  },
  table: {
    columns: [
      'station',
      'revenue',
      'litres',
      'net_operating',
      'stock_variance',
      'risk_alerts',
      'collections',
    ],
    rows: [
      ['MIK-01', '1000.00', '400.000', '200.00', '0', '0', '0'],
      ['MSA-01', '500.00', '200.000', '150.00', '0', '1', '0'],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [],
};

describe('ExecutiveReportPage', () => {
  beforeEach(() => {
    getExecutiveReport.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the KPI hero, management narrative and station ranking', async () => {
    getExecutiveReport.mockResolvedValue(fullEnvelope);
    renderPage();

    expect(await screen.findByText('Management summary')).toBeInTheDocument();
    // The deterministic narrative band carries the computed sentences.
    expect(
      screen.getByText(/Your network sold 600.000 L this period across 2 stations/),
    ).toBeInTheDocument();
    expect(screen.getByText(/Recommended focus: clear the 1 open risk alert/)).toBeInTheDocument();
    // KPI hero + the station league table.
    expect(screen.getByText('Total revenue')).toBeInTheDocument();
    expect(screen.getByText('Network league table')).toBeInTheDocument();
    expect(screen.getByText('Network profit & loss')).toBeInTheDocument();
  });

  it('omits margin and loss value for a non-margin holder (omit, not zero)', async () => {
    getExecutiveReport.mockResolvedValue({
      ...fullEnvelope,
      summary: [
        { label: 'Total revenue', value: '1500.00', unit: 'TZS' },
        { label: 'Total litres', value: '600.000', unit: 'L' },
        { label: 'Total loss litres', value: '120.000', unit: 'L' },
        { label: 'Stations in scope', value: '2', unit: 'count' },
      ],
      data_quality: [
        {
          level: 'warning',
          message: 'Gross and net margin are hidden — they require the margin.view permission.',
        },
      ],
      chart_data: {
        ...fullEnvelope.chart_data,
        waterfall: [],
        comparison: [
          {
            key: 'revenue',
            label: 'Revenue',
            current: '1500.00',
            prior: '1000.00',
            delta_pct: '+50.0',
            unit: 'TZS',
          },
        ],
        loss_summary: { loss_litres: '120.000', stock_variance: '300.000', value_shown: false },
        margin_shown: false,
      },
    });
    renderPage();

    expect(await screen.findByText('Total revenue')).toBeInTheDocument();
    // Margin KPIs are absent (omitted, not zeroed).
    expect(screen.queryByText('Gross margin')).not.toBeInTheDocument();
    expect(screen.queryByText('Net margin')).not.toBeInTheDocument();
    // The loss-value line reads as permission-gated, and the P&L waterfall is gone.
    expect(screen.getByText('requires margin permission')).toBeInTheDocument();
    expect(screen.queryByText('Network profit & loss')).not.toBeInTheDocument();
    // A data-quality note explains the omission.
    expect(screen.getByText(/require the margin.view permission/)).toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getExecutiveReport.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access to the executive report')).toBeInTheDocument();
  });

  it('renders an empty station ranking when no stations are in scope', async () => {
    getExecutiveReport.mockResolvedValue({
      ...fullEnvelope,
      chart_data: { ...fullEnvelope.chart_data, stations: [] },
      table: { columns: fullEnvelope.table.columns, rows: [] },
    });
    renderPage();

    expect(await screen.findByText('No stations in scope')).toBeInTheDocument();
  });
});
