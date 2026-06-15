import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getStationCloseReport = vi.fn();
const listReportSnapshots = vi.fn();
const getReportLockState = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getStationCloseReport: (...args: unknown[]) => getStationCloseReport(...args),
    // The snapshots panel (Phase 14) queries these on the station-close page.
    listReportSnapshots: (...args: unknown[]) => listReportSnapshots(...args),
    getReportLockState: (...args: unknown[]) => getReportLockState(...args),
    exportReport: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import StationClosePage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <StationClosePage />
    </QueryClientProvider>,
  );
}

/** A full station-close envelope including the additive tender_mix breakdown. */
const envelope = {
  metadata: {
    report_key: 'station-close',
    title: 'Daily Station Close',
    generated_at: '2026-06-04T00:00:00Z',
    station_id: 'st-1',
    period: '',
  },
  filters_used: { station_id: 'st-1', business_date: '2026-06-04' },
  data_quality: [
    { level: 'warning', message: 'This day is not locked yet, so its totals are provisional.' },
  ],
  summary: [
    { label: 'Sales value', value: '1000000.00', unit: 'TZS' },
    { label: 'Net revenue', value: '900000.00', unit: 'TZS' },
    { label: 'Margin', value: '150000.00', unit: 'TZS' },
    { label: 'Total tendered', value: '1000000.00', unit: 'TZS' },
    { label: 'Expected cash', value: '600000.00', unit: 'TZS' },
    { label: 'Submitted cash', value: '595000.00', unit: 'TZS' },
    { label: 'Cash variance', value: '-5000.00', unit: 'TZS' },
    { label: 'Open exceptions', value: '1', unit: 'count' },
    { label: 'Approval status', value: 'pending_shifts' },
  ],
  chart_data: [
    {
      date: '2026-06-03',
      gross: '950000.00',
      margin: '140000.00',
      tendered: '950000.00',
      cash_variance: '0',
    },
    {
      date: '2026-06-04',
      gross: '1000000.00',
      margin: '150000.00',
      tendered: '1000000.00',
      cash_variance: '-5000.00',
    },
  ],
  tender_mix: {
    cash: '600000.00',
    mobile_money: '250000.00',
    card: '0',
    credit: '150000.00',
    voucher: '0',
    total: '1000000.00',
  },
  table: {
    columns: ['business_date', 'status', 'gross', 'net', 'margin', 'tendered', 'cash_variance'],
    rows: [
      ['2026-06-04', 'draft', '1000000.00', '900000.00', '150000.00', '1000000.00', '-5000.00'],
    ],
  },
  insights: [],
  recommended_actions: [
    'Reconcile the drawer and confirm tender breakdown before locking the day.',
  ],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/stations/st-1/reports/revenue.csv' }],
};

describe('StationClosePage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    getStationCloseReport.mockReset();
    listReportSnapshots.mockReset();
    getReportLockState.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
    // The snapshots panel queries these; default to no snapshots / unlocked.
    listReportSnapshots.mockResolvedValue({ items: [], count: 0, has_more: false });
    getReportLockState.mockResolvedValue({ report_key: 'station-close', locked: false });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the KPI hero, tender-mix legend and cash-reconciliation panel', async () => {
    getStationCloseReport.mockResolvedValue(envelope);
    renderPage();

    // KPI hero MetricCards (expected vs submitted cash + variance). These labels
    // appear both as a MetricCard and as a CashReconBar row, so assert presence
    // via getAllByText rather than a unique match.
    expect(await screen.findByText('Sales value')).toBeInTheDocument();
    expect(screen.getAllByText('Expected cash').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Submitted cash').length).toBeGreaterThan(0);

    // Tender-mix donut legend surfaces the non-zero tenders and drops zero ones.
    expect(screen.getByText('Tender mix')).toBeInTheDocument();
    expect(screen.getByText('Mobile money')).toBeInTheDocument();
    expect(screen.queryByText('Voucher')).not.toBeInTheDocument();

    // Cash reconciliation panel + the shift-close checklist (right panel).
    expect(screen.getByText('Cash reconciliation')).toBeInTheDocument();
    expect(screen.getByText('Shift close checklist')).toBeInTheDocument();
  });

  it('surfaces the data-quality warning prominently', async () => {
    getStationCloseReport.mockResolvedValue(envelope);
    renderPage();

    expect(
      await screen.findByText('This day is not locked yet, so its totals are provisional.'),
    ).toBeInTheDocument();
  });

  it('shows an honest empty state for the tender mix when no tenders exist', async () => {
    getStationCloseReport.mockResolvedValue({
      ...envelope,
      tender_mix: {
        cash: '0',
        mobile_money: '0',
        card: '0',
        credit: '0',
        voucher: '0',
        total: '0',
      },
    });
    renderPage();

    expect(await screen.findByText('No tenders yet')).toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getStationCloseReport.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });

  it('shows an empty state when there are no stations', async () => {
    listStations.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No stations yet')).toBeInTheDocument();
  });
});
