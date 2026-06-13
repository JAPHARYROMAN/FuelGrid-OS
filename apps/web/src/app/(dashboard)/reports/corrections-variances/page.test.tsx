import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const getCorrectionsVariancesReport = vi.fn();
const listStations = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    getCorrectionsVariancesReport: (...args: unknown[]) => getCorrectionsVariancesReport(...args),
    listStations: (...args: unknown[]) => listStations(...args),
  },
}));

vi.mock('@/hooks/use-permissions', () => ({ usePermission: () => true }));

import CorrectionsVariancesReportPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CorrectionsVariancesReportPage />
    </QueryClientProvider>,
  );
}

const stations = {
  items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
  count: 1,
};

const envelope = {
  metadata: {
    report_key: 'corrections-variances',
    title: 'Corrections & Variances',
    generated_at: '2026-06-13T00:00:00Z',
    station_id: 'st-1',
    period: '2026-05-15..2026-06-13',
  },
  filters_used: { station_id: 'st-1' },
  data_quality: [],
  summary: [
    { label: 'Reading corrections', value: '1', unit: 'count' },
    { label: 'Collection receipts', value: '1', unit: 'count' },
    { label: 'Total shortage', value: '1500.00', unit: 'TZS' },
    { label: 'Total excess', value: '0.00', unit: 'TZS' },
  ],
  chart_data: {
    corrections: [
      {
        shift: 'Morning',
        attendant: 'Asha M',
        submitted_reading: '1600.000',
        final_reading: '1550.000',
        delta_litres: '-50.000',
        status: 'corrected',
      },
    ],
    collections: [
      {
        shift: 'Morning',
        submitted_by: 'Asha M',
        expected_amount: '232000.00',
        submitted_total: '231000.00',
        received_total: '230500.00',
        difference: '-1500.00',
        status: 'shortage',
      },
    ],
  },
  table: {
    columns: [
      'kind',
      'shift',
      'attendant',
      'reference',
      'submitted',
      'final_or_received',
      'expected',
      'variance',
      'reason',
      'decided_by',
      'decided_at',
    ],
    rows: [
      [
        'reading_corrected',
        'Morning',
        'Asha M',
        'pump 1 / nozzle 1',
        '1600.000',
        '1550.000',
        '',
        '-50.000',
        'meter overrun',
        'Sup R',
        '2026-06-12T18:00:00Z',
      ],
      [
        'collection_shortage',
        'Morning',
        'Asha M',
        'cash handover',
        '231000.00',
        '230500.00',
        '232000.00',
        '-1500.00',
        'short change',
        '',
        '2026-06-12T18:30:00Z',
      ],
    ],
  },
  insights: [],
  recommended_actions: [],
  drilldown: [],
  export_options: [],
};

describe('CorrectionsVariancesReportPage', () => {
  beforeEach(() => {
    getCorrectionsVariancesReport.mockReset();
    listStations.mockReset();
    listStations.mockResolvedValue(stations);
  });
  afterEach(() => vi.clearAllMocks());

  it('renders both datasets — corrections rows and collection variances — with the summary', async () => {
    getCorrectionsVariancesReport.mockResolvedValue(envelope);
    renderPage();

    expect(await screen.findByText('Corrections & collection variances')).toBeInTheDocument();
    expect(screen.getByText('Reading corrections')).toBeInTheDocument();
    expect(screen.getByText('Collection receipts')).toBeInTheDocument();
    // A correction row (dual-value: submitted vs final) and a collection row.
    expect(screen.getByText('reading_corrected')).toBeInTheDocument();
    expect(screen.getByText('collection_shortage')).toBeInTheDocument();
    expect(screen.getByText('meter overrun')).toBeInTheDocument();
  });

  it('exposes the station + from/to date-window filters', async () => {
    getCorrectionsVariancesReport.mockResolvedValue(envelope);
    renderPage();

    await screen.findByText('Corrections & collection variances');
    expect(screen.getByLabelText('Station')).toBeInTheDocument();
    expect(screen.getByLabelText('From date')).toBeInTheDocument();
    expect(screen.getByLabelText('To date')).toBeInTheDocument();
  });

  it('shows a no-access error when the dataset 403s', async () => {
    getCorrectionsVariancesReport.mockRejectedValue(
      new SdkError('forbidden', 403, { error: 'forbidden' }),
    );
    renderPage();
    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });
});
