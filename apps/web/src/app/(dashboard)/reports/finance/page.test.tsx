import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getFinanceReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getFinanceReport: (...args: unknown[]) => getFinanceReport(...args),
    exportReport: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import FinancePage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <FinancePage />
    </QueryClientProvider>,
  );
}

/** A margin.view holder's envelope: full P&L cascade + cost columns. */
const costShownEnvelope = {
  metadata: {
    report_key: 'finance',
    title: 'Finance',
    generated_at: '2026-06-14T00:00:00Z',
    station_id: 'st-1',
    period: 'this-month',
  },
  filters_used: { station_id: 'st-1', period: 'this-month' },
  data_quality: [],
  summary: [
    { label: 'Net revenue', value: '1000.00', unit: 'TZS' },
    { label: 'Operating expenses', value: '100.00', unit: 'TZS' },
    { label: 'Cash position', value: '900.00', unit: 'TZS' },
    { label: 'Gross margin', value: '300.00', unit: 'TZS' },
    { label: 'Net operating result', value: '200.00', unit: 'TZS' },
    { label: 'Net margin %', value: '20.0' },
  ],
  chart_data: {
    waterfall: [
      { key: 'revenue', label: 'Net revenue', value: '1000.00', kind: 'base' },
      { key: 'cogs', label: 'COGS', value: '700.00', kind: 'delta', negative: true },
      { key: 'gross_margin', label: 'Gross margin', value: '300.00', kind: 'total' },
      {
        key: 'expenses',
        label: 'Operating expenses',
        value: '100.00',
        kind: 'delta',
        negative: true,
      },
      { key: 'net_operating', label: 'Net operating result', value: '200.00', kind: 'total' },
    ],
    by_product: [
      {
        product: 'Premium',
        litres: '200.000',
        revenue: '1000.00',
        cogs: '700.00',
        margin: '300.00',
      },
    ],
    settlements: [{ key: 'p1', label: '2026-06-01 – 2026-06-30', status: 'Open', tone: 'pending' }],
    statements: [
      {
        key: 'profit-loss',
        label: 'Profit & Loss statement',
        endpoint: '/api/v1/finance/reports/profit-loss',
        permission: 'finance.read',
      },
    ],
    cost_shown: true,
  },
  table: {
    columns: ['product', 'litres', 'revenue', 'cogs', 'gross_margin'],
    rows: [['Premium', '200.000', '1000.00', '700.00', '300.00']],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/reports/financials.csv?period=this-month' }],
};

/** A non-margin actor's envelope: cost omitted, non-cost waterfall, DQ note. */
const costHiddenEnvelope = {
  ...costShownEnvelope,
  data_quality: [
    {
      level: 'info',
      message:
        'COGS, gross margin and net margin are hidden — they require the margin.view permission.',
    },
  ],
  summary: [
    { label: 'Net revenue', value: '1000.00', unit: 'TZS' },
    { label: 'Operating expenses', value: '100.00', unit: 'TZS' },
    { label: 'Cash position', value: '900.00', unit: 'TZS' },
  ],
  chart_data: {
    waterfall: [
      { key: 'revenue', label: 'Net revenue', value: '1000.00', kind: 'base' },
      {
        key: 'expenses',
        label: 'Operating expenses',
        value: '100.00',
        kind: 'delta',
        negative: true,
      },
      { key: 'net_of_expenses', label: 'Revenue after expenses', value: '900.00', kind: 'total' },
    ],
    by_product: [{ product: 'Premium', litres: '200.000', revenue: '1000.00' }],
    settlements: [],
    statements: [],
    cost_shown: false,
  },
  table: {
    columns: ['product', 'litres', 'revenue'],
    rows: [['Premium', '200.000', '1000.00']],
  },
};

describe('FinancePage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    getFinanceReport.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the P&L KPIs, the waterfall steps and the per-product table', async () => {
    getFinanceReport.mockResolvedValue(costShownEnvelope);
    renderPage();

    // KPI hero (gross margin + net operating visible for a margin.view holder).
    // "Net operating result" / "Gross margin" appear both as a KPI and a
    // waterfall step label, so assert at least one match.
    expect((await screen.findAllByText('Net operating result')).length).toBeGreaterThan(0);
    expect(screen.getByText('Cash position')).toBeInTheDocument();
    expect(screen.getAllByText('Gross margin').length).toBeGreaterThan(0);
    // The signature P&L waterfall card + its COGS step are rendered.
    expect(screen.getByText('Profit & loss waterfall')).toBeInTheDocument();
    expect(screen.getAllByText('COGS').length).toBeGreaterThan(0);
    // The accounting-period settlement chip + the embedded statement link.
    expect(screen.getByText('Accounting periods')).toBeInTheDocument();
    expect(screen.getByText('Financial statements')).toBeInTheDocument();
    expect(screen.getByText('Profit & Loss statement')).toBeInTheDocument();
    // Per-product table.
    expect(screen.getByText('Per-product profitability')).toBeInTheDocument();
    expect(screen.getAllByText('Premium').length).toBeGreaterThan(0);
  });

  it('omits COGS / margin and surfaces the margin-hidden note for a non-margin actor', async () => {
    getFinanceReport.mockResolvedValue(costHiddenEnvelope);
    renderPage();

    // Revenue stays visible; gross margin / net operating are omitted.
    expect(await screen.findByText('Cash position')).toBeInTheDocument();
    expect(screen.queryByText('Gross margin')).not.toBeInTheDocument();
    expect(screen.queryByText('Net operating result')).not.toBeInTheDocument();
    // The margin-hidden data-quality note is shown (the DQ banner wording, which
    // is distinct from the waterfall description paragraph).
    expect(
      screen.getByText(/are hidden — they require the margin.view permission/i),
    ).toBeInTheDocument();
    // The waterfall has no COGS step for a non-margin actor.
    expect(screen.queryByText('COGS')).not.toBeInTheDocument();
  });

  it('shows a no-access error when the report 403s', async () => {
    getFinanceReport.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });

  it('shows an empty state when there are no stations', async () => {
    listStations.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No stations yet')).toBeInTheDocument();
  });
});
