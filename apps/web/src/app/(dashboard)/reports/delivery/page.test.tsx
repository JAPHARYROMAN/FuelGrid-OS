import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getDeliveryReport = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...args: unknown[]) => listStations(...args),
    getDeliveryReport: (...args: unknown[]) => getDeliveryReport(...args),
    exportReport: vi.fn(),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import DeliveryReportPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <DeliveryReportPage />
    </QueryClientProvider>,
  );
}

// A §5.7 Delivery & Procurement envelope: ordered/received KPI hero, the
// per-product comparison, per-delivery variance, supplier scorecards (with cost
// shown so the price dimension rides), the procurement pipeline, and the delivery
// receipts table. Money/litres are decimal strings (the numeric->text contract).
const envelope = {
  metadata: {
    report_key: 'delivery',
    title: 'Delivery & Procurement',
    generated_at: '2026-06-13T00:00:00Z',
    station_id: 'st-1',
    period: 'this-month',
  },
  filters_used: { station_id: 'st-1', period: 'this-month' },
  data_quality: [],
  summary: [
    { label: 'Ordered', value: '40000.000', unit: 'L' },
    { label: 'Received', value: '39200.000', unit: 'L' },
    { label: 'Delivery variance', value: '-800.000', unit: 'L', direction: 'down' },
    { label: 'Deliveries', value: '6', unit: 'count' },
    { label: 'Late deliveries', value: '1', unit: 'count' },
    { label: 'Fuel cost', value: '6100000.00', unit: 'TZS' },
    { label: 'Avg cost / litre', value: '155.6122', unit: 'TZS' },
  ],
  chart_data: {
    comparison: [
      {
        key: 'p-1',
        label: 'Petrol',
        color: '#f97316',
        ordered: '24000.000',
        loaded: '24000.000',
        received: '23600.000',
      },
      {
        key: 'p-2',
        label: 'Diesel',
        color: '#3b82f6',
        ordered: '16000.000',
        loaded: '16000.000',
        received: '15600.000',
      },
    ],
    deliveries: [
      {
        key: 'd-1',
        received_at: '2026-06-02T08:00:00Z',
        supplier: 'Acme Petroleum',
        product: 'Petrol',
        volume: '12000.000',
        dip_variance: '-30.000',
        match_status: 'matched',
        late: false,
        landed_cost: '1860000.00',
      },
      {
        key: 'd-2',
        received_at: '2026-06-05T08:00:00Z',
        supplier: 'Risky Fuels Ltd',
        product: 'Diesel',
        volume: '8000.000',
        dip_variance: '120.000',
        match_status: 'short',
        late: true,
        landed_cost: '1240000.00',
      },
    ],
    scorecards: [
      {
        supplier_id: 'sup-risky',
        supplier_name: 'Risky Fuels Ltd',
        score: 44,
        band: 'At risk',
        tone: 'critical',
        grade: 'D',
        on_time_score: 50,
        quantity_score: 10,
        dispute_score: 0,
        document_score: 0,
        variance_score: 60,
        price_score: 40,
        price_included: true,
        delivery_count: 3,
        dispute_count: 2,
      },
      {
        supplier_id: 'sup-acme',
        supplier_name: 'Acme Petroleum',
        score: 95,
        band: 'Excellent',
        tone: 'low',
        grade: 'A',
        on_time_score: 100,
        quantity_score: 98,
        dispute_score: 100,
        document_score: 100,
        variance_score: 90,
        price_score: 92,
        price_included: true,
        delivery_count: 3,
        dispute_count: 0,
      },
    ],
    pipeline: [
      { status: 'confirmed', count: 2 },
      { status: 'received', count: 3 },
      { status: 'partially_received', count: 1 },
    ],
    cost_shown: true,
  },
  table: {
    columns: [
      'received_at',
      'supplier',
      'product',
      'volume',
      'dip_variance',
      'status',
      'late',
      'landed_cost',
    ],
    rows: [
      [
        '2026-06-02T08:00:00Z',
        'Acme Petroleum',
        'Petrol',
        '12000.000',
        '-30.000',
        'matched',
        'false',
        '1860000.00',
      ],
      [
        '2026-06-05T08:00:00Z',
        'Risky Fuels Ltd',
        'Diesel',
        '8000.000',
        '120.000',
        'short',
        'true',
        '1240000.00',
      ],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [{ format: 'csv', url: '/api/v1/stations/st-1/reports/inventory.csv' }],
};

describe('DeliveryReportPage', () => {
  beforeEach(() => {
    permitted = true;
    listStations.mockReset();
    getDeliveryReport.mockReset();
    listStations.mockResolvedValue({
      items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
      count: 1,
      has_more: false,
    });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders the KPI hero, comparison, variance, scorecard and pipeline', async () => {
    getDeliveryReport.mockResolvedValue(envelope);
    renderPage();

    // KPI hero metrics. "Ordered" / "Received" also appear as the comparison
    // chart's legend labels, so they match more than once.
    expect((await screen.findAllByText('Ordered')).length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Received').length).toBeGreaterThanOrEqual(1);
    // "Delivery variance" is both a KPI label and the variance card title.
    expect(screen.getAllByText('Delivery variance').length).toBeGreaterThanOrEqual(2);
    // Cost KPI surfaces when cost_shown.
    expect(screen.getByText('Fuel cost')).toBeInTheDocument();

    // The signature visuals render their card headers.
    expect(screen.getByText('Ordered vs loaded vs received')).toBeInTheDocument();
    expect(screen.getByText('Supplier scorecard')).toBeInTheDocument();
    expect(screen.getByText('Procurement pipeline')).toBeInTheDocument();

    // The scorecard surfaces each supplier with its band word (text, not colour).
    // Supplier names also appear in the receipts table, so they match more than once.
    expect(screen.getAllByText('Risky Fuels Ltd').length).toBeGreaterThanOrEqual(1);
    expect(screen.getAllByText('Acme Petroleum').length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText('D · At risk')).toBeInTheDocument();
    expect(screen.getByText('A · Excellent')).toBeInTheDocument();
  });

  it('hides the cost KPI and price dimension when the actor lacks margin.view', async () => {
    const gated = {
      ...envelope,
      summary: envelope.summary.filter(
        (m) => m.label !== 'Fuel cost' && m.label !== 'Avg cost / litre',
      ),
      chart_data: {
        ...envelope.chart_data,
        cost_shown: false,
        scorecards: envelope.chart_data.scorecards.map((s) => ({
          ...s,
          price_included: false,
          price_score: null,
        })),
      },
      data_quality: [
        {
          level: 'info',
          message:
            'Supplier cost and price competitiveness are hidden — they require the margin.view permission.',
        },
      ],
    };
    getDeliveryReport.mockResolvedValue(gated);
    renderPage();

    expect(await screen.findByText('Supplier scorecard')).toBeInTheDocument();
    // The cost-hidden data-quality note is surfaced; the Fuel cost KPI is gone.
    expect(screen.getByText(/require the margin\.view permission/)).toBeInTheDocument();
    expect(screen.queryByText('Fuel cost')).toBeNull();
    // The scorecard still scores its suppliers without the price dimension.
    expect(screen.getAllByText('Risky Fuels Ltd').length).toBeGreaterThanOrEqual(1);
    expect(screen.queryByText('Price')).toBeNull();
  });

  it('shows a no-access error when the report 403s', async () => {
    getDeliveryReport.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();

    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });

  it('shows an empty state when there are no stations', async () => {
    listStations.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No stations yet')).toBeInTheDocument();
  });
});
