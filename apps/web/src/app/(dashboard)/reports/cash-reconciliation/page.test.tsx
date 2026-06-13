import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getCashReconciliationReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getCashReconciliationReport: (...args: unknown[]) => getCashReconciliationReport(...args),
    exportReport: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import CashReconciliationPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CashReconciliationPage />
    </QueryClientProvider>,
  );
}

/** A full §20.5 cash-reconciliation envelope: KPI hero, flow + settlement board. */
const envelope = {
  metadata: {
    report_key: 'cash-reconciliation',
    title: 'Cash Reconciliation',
    generated_at: '2026-06-04T00:00:00Z',
    station_id: 'st-1',
    period: 'current',
  },
  filters_used: { station_id: 'st-1', period: 'current' },
  data_quality: [
    {
      level: 'warning',
      message: 'Mobile-money/card settlement is pending — the operating day is not locked.',
    },
  ],
  summary: [
    { label: 'Expected cash', value: '600000.00', unit: 'TZS' },
    { label: 'Submitted cash', value: '595000.00', unit: 'TZS' },
    { label: 'Deposited cash', value: '500000.00', unit: 'TZS' },
    { label: 'Net variance', value: '-5000.00', unit: 'TZS' },
    { label: 'Total shortage', value: '5000.00', unit: 'TZS' },
    { label: 'Total excess', value: '0.00', unit: 'TZS' },
    { label: 'Variance status', value: 'Shortage' },
    { label: 'Reconciliations', value: '1', unit: 'count' },
  ],
  chart_data: {
    flow: [
      {
        created_at: '2026-06-04T08:00:00Z',
        status: 'submitted',
        expected: '600000.00',
        submitted: '595000.00',
        variance: '-5000.00',
        shortage: '5000.00',
        excess: '0',
      },
    ],
    settlement: [
      {
        key: 'cash',
        label: 'Cash',
        status: 'Pending',
        tone: 'pending',
        amount: '595000.00',
        detail: 'Submitted, awaiting approval/posting',
      },
      {
        key: 'mobile_money',
        label: 'Mobile money',
        status: 'Pending',
        tone: 'pending',
        amount: '250000.00',
        detail: 'Day not locked — awaiting settlement',
      },
      {
        key: 'card',
        label: 'Card',
        status: 'None',
        tone: 'neutral',
        amount: '0',
        detail: 'No Card tendered',
      },
      {
        key: 'bank_deposit',
        label: 'Bank deposit',
        status: 'Not posted',
        tone: 'at_risk',
        amount: '100000.00',
        detail: '1 deposit(s) prepared, not yet posted',
      },
    ],
  },
  tender_mix: {
    cash: '600000.00',
    mobile_money: '250000.00',
    card: '0',
    credit: '150000.00',
    voucher: '0',
    total: '1000000.00',
  },
  table: {
    columns: ['created_at', 'status', 'expected', 'submitted', 'variance', 'shortage', 'excess'],
    rows: [
      ['2026-06-04T08:00:00Z', 'submitted', '600000.00', '595000.00', '-5000.00', '5000.00', '0'],
    ],
  },
  insights: [],
  recommended_actions: [
    'Count the drawer and submit the cash reconciliation before locking the day.',
  ],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/stations/st-1/reports/cash-reconciliation.csv' }],
};

describe('CashReconciliationPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    getCashReconciliationReport.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the KPI hero, cash flow bar and settlement-status board', async () => {
    getCashReconciliationReport.mockResolvedValue(envelope);
    renderPage();

    // KPI hero MetricCards (expected / submitted / deposited). These labels also
    // appear in the flow bar, so assert presence via getAllByText.
    expect((await screen.findAllByText('Expected cash')).length).toBeGreaterThan(0);
    expect(screen.getAllByText('Submitted cash').length).toBeGreaterThan(0);
    expect(screen.getAllByText('Deposited cash').length).toBeGreaterThan(0);

    // The signature §20.5 settlement-status board renders a chip per medium with
    // a TEXT status (colour is never the only signal).
    expect(screen.getByText('Settlement status')).toBeInTheDocument();
    expect(screen.getByText('Bank deposit')).toBeInTheDocument();
    expect(screen.getByText('Not posted')).toBeInTheDocument();

    // The cash flow + tender split visuals. "Mobile money" appears both as a
    // settlement chip and as a tender-mix donut legend entry, so assert presence
    // via getAllByText rather than a unique match.
    expect(screen.getByText('Cash reconciliation flow')).toBeInTheDocument();
    expect(screen.getByText('Tender mix')).toBeInTheDocument();
    expect(screen.getAllByText('Mobile money').length).toBeGreaterThan(0);
  });

  it('surfaces the settlement-pending data-quality warning prominently', async () => {
    getCashReconciliationReport.mockResolvedValue(envelope);
    renderPage();

    expect(
      await screen.findByText(
        'Mobile-money/card settlement is pending — the operating day is not locked.',
      ),
    ).toBeInTheDocument();
  });

  it('shows an honest empty state when there are no reconciliations', async () => {
    getCashReconciliationReport.mockResolvedValue({
      ...envelope,
      chart_data: { flow: [], settlement: [] },
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

    expect(await screen.findByText('No cash position')).toBeInTheDocument();
    expect(screen.getByText('No settlement data')).toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getCashReconciliationReport.mockRejectedValue(
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
