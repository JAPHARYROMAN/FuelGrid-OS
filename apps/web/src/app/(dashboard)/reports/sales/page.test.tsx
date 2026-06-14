import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getSalesReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getSalesReport: (...args: unknown[]) => getSalesReport(...args),
    exportReport: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import SalesReportPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <SalesReportPage />
    </QueryClientProvider>,
  );
}

// A §5.2 Sales envelope: KPI hero, the per-dimension chart_data, the payment
// tender_mix, and the by-product drillable table. Money/litres are decimal
// strings (the numeric->text contract). margin_shown:true exercises the margin
// column on the dimension tables.
const envelope = {
  metadata: {
    report_key: 'sales',
    title: 'Sales',
    generated_at: '2026-06-13T00:00:00Z',
    station_id: 'st-1',
    period: 'this-month',
  },
  filters_used: { station_id: 'st-1', period: 'this-month' },
  data_quality: [],
  summary: [
    { label: 'Litres sold', value: '25800.000', unit: 'L' },
    { label: 'Revenue', value: '4200000.00', unit: 'TZS', delta: '+12.4%', direction: 'up' },
    { label: 'Average selling price', value: '162.79', unit: 'TZS' },
    { label: 'Transactions', value: '48', unit: 'count' },
    { label: 'Gross margin', value: '520000.00', unit: 'TZS' },
  ],
  chart_data: {
    trend: [
      { date: '2026-06-01', gross: '1200000.00', litres: '7400.000' },
      { date: '2026-06-02', gross: '1300000.00', litres: '8100.000' },
    ],
    by_product: [
      {
        key: 'p-1',
        label: 'Petrol',
        color: '#f97316',
        litres: '15000.000',
        gross: '2500000.00',
        net: '2400000.00',
        margin: '300000.00',
        txn_count: 30,
      },
      {
        key: 'p-2',
        label: 'Diesel',
        color: '#3b82f6',
        litres: '10800.000',
        gross: '1700000.00',
        net: '1650000.00',
        margin: '220000.00',
        txn_count: 18,
      },
    ],
    by_shift: [
      {
        key: 'sh-1',
        label: 'Morning · 2026-06-02',
        litres: '13000.000',
        gross: '2100000.00',
        net: '2050000.00',
        margin: '260000.00',
        txn_count: 24,
      },
    ],
    by_attendant: [
      {
        key: 'u-1',
        label: 'Asha Mwinyi',
        litres: '9000.000',
        gross: '1500000.00',
        net: '1450000.00',
        margin: '190000.00',
        txn_count: 16,
      },
    ],
    by_nozzle: [
      {
        key: 'n-1',
        label: 'Pump 1 · Nozzle 1',
        litres: '6000.000',
        gross: '1000000.00',
        net: '980000.00',
        margin: '120000.00',
        txn_count: 10,
      },
    ],
    by_hour: [
      { hour: 8, gross: '900000.00', litres: '5500.000', txn: 12 },
      { hour: 17, gross: '1100000.00', litres: '6800.000', txn: 14 },
    ],
    stations: [],
    margin_shown: true,
  },
  tender_mix: {
    cash: '2500000.00',
    mobile_money: '1200000.00',
    card: '500000.00',
    credit: '0',
    voucher: '0',
    total: '4200000.00',
  },
  table: {
    columns: ['product', 'litres', 'gross', 'net', 'margin', 'txn'],
    rows: [
      ['Petrol', '15000.000', '2500000.00', '2400000.00', '300000.00', '30'],
      ['Diesel', '10800.000', '1700000.00', '1650000.00', '220000.00', '18'],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/stations/st-1/reports/revenue.csv' }],
};

describe('SalesReportPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    getSalesReport.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the KPI hero, product mix, payment mix and dimension tables', async () => {
    getSalesReport.mockResolvedValue(envelope);
    renderPage();

    // KPI hero metrics.
    expect(await screen.findByText('Litres sold')).toBeInTheDocument();
    expect(screen.getByText('Average selling price')).toBeInTheDocument();
    expect(screen.getByText('Transactions')).toBeInTheDocument();

    // The signature visuals render their card headers.
    expect(screen.getByText('Revenue trend')).toBeInTheDocument();
    expect(screen.getByText('Product mix')).toBeInTheDocument();
    expect(screen.getByText('Payment mix')).toBeInTheDocument();
    expect(screen.getByText('Sales by hour')).toBeInTheDocument();

    // Dimension drill-downs surface their rows.
    expect(screen.getByText('Top attendants')).toBeInTheDocument();
    expect(screen.getByText('Asha Mwinyi')).toBeInTheDocument();
    expect(screen.getByText('Pump 1 · Nozzle 1')).toBeInTheDocument();
  });

  it('hides the margin column when the actor lacks margin.view', async () => {
    const gated = {
      ...envelope,
      summary: envelope.summary.filter((m) => m.label !== 'Gross margin'),
      chart_data: {
        ...envelope.chart_data,
        margin_shown: false,
        by_attendant: [{ ...envelope.chart_data.by_attendant[0], margin: undefined }],
      },
      data_quality: [
        {
          level: 'info',
          message: 'Margin and cost are hidden — they require the margin.view permission.',
        },
      ],
    };
    getSalesReport.mockResolvedValue(gated);
    renderPage();

    expect(await screen.findByText('Top attendants')).toBeInTheDocument();
    // The margin-hidden data-quality note is surfaced; the Gross margin KPI is gone.
    expect(screen.getByText(/require the margin\.view permission/)).toBeInTheDocument();
    expect(screen.queryByText('Gross margin')).toBeNull();
  });

  it('shows a no-access error when the report 403s', async () => {
    getSalesReport.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });

  it('shows an empty state when there are no stations', async () => {
    listStations.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No stations yet')).toBeInTheDocument();
  });
});
