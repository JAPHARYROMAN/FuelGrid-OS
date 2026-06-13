import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const getAttendanceReport = vi.fn();
const listStations = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    getAttendanceReport: (...args: unknown[]) => getAttendanceReport(...args),
    listStations: (...args: unknown[]) => listStations(...args),
  },
}));

vi.mock('@/hooks/use-permissions', () => ({ usePermission: () => true }));

import AttendanceReportPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AttendanceReportPage />
    </QueryClientProvider>,
  );
}

const stations = {
  items: [{ id: 'st-1', code: 'MIK-01', name: 'Mikocheni' }],
  count: 1,
};

const envelope = {
  metadata: {
    report_key: 'attendance',
    title: 'Attendance',
    generated_at: '2026-06-13T00:00:00Z',
    station_id: 'st-1',
    period: '2026-05-15..2026-06-13',
  },
  filters_used: { station_id: 'st-1', from: '2026-05-15', to: '2026-06-13' },
  data_quality: [],
  summary: [
    { label: 'Rostered', value: '4', unit: 'count' },
    { label: 'Present', value: '2', unit: 'count' },
    { label: 'Late', value: '1', unit: 'count' },
    { label: 'No-shows', value: '1', unit: 'count' },
  ],
  chart_data: [{ date: '2026-06-12', present: 2, late: 1, no_show: 1, not_checked_in: 0 }],
  table: {
    columns: [
      'shift',
      'slot',
      'shift_status',
      'attendant',
      'email',
      'attendance_status',
      'check_in_at',
      'check_out_at',
    ],
    rows: [
      ['Morning', 'morning', 'open', 'Asha M', 'asha@x.io', 'present', '2026-06-12T05:05:00Z', ''],
      ['Morning', 'morning', 'open', 'Juma K', 'juma@x.io', 'late', '2026-06-12T05:25:00Z', ''],
    ],
  },
  insights: [
    {
      severity: 'warning',
      message: '1 late check-in(s) and 1 no-show(s) in the window.',
      recommended_action: 'Review the flagged shifts.',
    },
  ],
  recommended_actions: ['Review the flagged shifts.'],
  drilldown: [{ label: 'Operations overview', href: '/api/v1/stations/st-1/operations/overview' }],
  export_options: [],
};

describe('AttendanceReportPage', () => {
  beforeEach(() => {
    getAttendanceReport.mockReset();
    listStations.mockReset();
    listStations.mockResolvedValue(stations);
  });
  afterEach(() => vi.clearAllMocks());

  it('renders the roster table and the summary metrics for the station', async () => {
    getAttendanceReport.mockResolvedValue(envelope);
    renderPage();

    expect(await screen.findByText('Roster vs attendance')).toBeInTheDocument();
    expect(screen.getByText('Rostered')).toBeInTheDocument();
    expect(screen.getByText('No-shows')).toBeInTheDocument();
    expect(screen.getByText('Asha M')).toBeInTheDocument();
    expect(screen.getByText('Juma K')).toBeInTheDocument();
  });

  it('exposes the station + from/to date-window filters', async () => {
    getAttendanceReport.mockResolvedValue(envelope);
    renderPage();

    await screen.findByText('Roster vs attendance');
    expect(screen.getByLabelText('Station')).toBeInTheDocument();
    expect(screen.getByLabelText('From date')).toBeInTheDocument();
    expect(screen.getByLabelText('To date')).toBeInTheDocument();
  });

  it('shows a no-access error when the dataset 403s', async () => {
    getAttendanceReport.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    renderPage();
    expect(await screen.findByText('No access to this station')).toBeInTheDocument();
  });
});
