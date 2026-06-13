import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { AttendantCurrentShift } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
  },
}));

import ReviewStatusPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ReviewStatusPage />
    </QueryClientProvider>,
  );
}

function assignment(n: number, product: string) {
  return {
    assignment_id: `as-${n}`,
    nozzle_id: `noz-${n}`,
    pump_number: 1,
    nozzle_number: n,
    product_name: product,
    product_color: '#f97316',
    meter_decimal_places: 2,
    assigned_at: '2026-06-11T05:05:00Z',
    confirmed_at: '2026-06-11T05:06:00Z',
  };
}

const snapshot: AttendantCurrentShift = {
  status: 'on_shift',
  next_action: 'await_reading_verification',
  user_message: 'Closing readings submitted. Wait for your supervisor to verify them.',
  station: { id: 'st-1', name: 'Mikocheni' },
  shift: {
    id: 'shift-1',
    tenant_id: 't-1',
    station_id: 'st-1',
    operating_day_id: 'day-1',
    name: 'Morning',
    status: 'open',
    opened_by: 'u-1',
    opened_at: '2026-06-11T05:00:00Z',
    slot: 'morning',
  },
  attendance: { status: 'checked_in', check_in_at: '2026-06-11T05:10:00Z' },
  assignments: [
    assignment(1, 'Premium'),
    assignment(2, 'Diesel'),
    assignment(3, 'Kerosene'),
    assignment(4, 'V-Power'),
  ],
  readings: [
    {
      nozzle_id: 'noz-1',
      opening_reading: '1000.000',
      closing_reading: '1500.000',
      verification_status: 'pending',
    },
    {
      nozzle_id: 'noz-2',
      opening_reading: '2000.000',
      closing_reading: '2300.000',
      verification_status: 'approved',
      final_reading: '2300.000',
    },
    {
      nozzle_id: 'noz-3',
      opening_reading: '3000.000',
      closing_reading: '3500.000',
      verification_status: 'corrected',
      final_reading: '3490.000',
      verification_reason: 'Meter glass misread',
    },
    {
      nozzle_id: 'noz-4',
      opening_reading: '4000.000',
      closing_reading: '4100.000',
      verification_status: 'rejected',
      final_reading: '4100.000',
      verification_reason: 'Photo does not match the meter',
    },
  ],
  expected_openings_available: true,
};

describe('ReviewStatusPage', () => {
  beforeEach(() => {
    // The offline snapshot hook seeds from localStorage; clear it so a prior
    // test's cached shift (e.g. an open shift) can't leak into this one.
    localStorage.clear();
    attendantCurrentShift.mockReset();
    attendantCurrentShift.mockResolvedValue(snapshot);
  });

  it('renders every review state: pending, approved, corrected, and rejected', async () => {
    renderPage();

    expect(await screen.findByText('Pending supervisor review')).toBeInTheDocument();
    expect(screen.getByText('Approved')).toBeInTheDocument();
    expect(screen.getByText('Corrected by supervisor')).toBeInTheDocument();
    expect(screen.getByText('Rejected')).toBeInTheDocument();
    // Header progress: 3 of the 4 submitted readings carry a decision.
    expect(screen.getByText('3 of 4 readings verified by your supervisor.')).toBeInTheDocument();
  });

  it('shows BOTH values, the exact difference, and the reason for a corrected reading', async () => {
    renderPage();

    await screen.findByText('Corrected by supervisor');
    // The dual-value model: submitted preserved, final approved shown next
    // to it, with the exact decimal difference (3490 - 3500 = -10).
    expect(screen.getByText('3500.000')).toBeInTheDocument();
    expect(screen.getByText('3490.000')).toBeInTheDocument();
    expect(screen.getByText('-10')).toBeInTheDocument();
    expect(screen.getByText(/meter glass misread/i)).toBeInTheDocument();
    expect(screen.getAllByText('Supervisor approved')).toHaveLength(1);
    expect(screen.getAllByText('You submitted')).toHaveLength(4);
  });

  it('shows the rejection reason on a rejected reading', async () => {
    renderPage();

    await screen.findByText('Rejected');
    expect(screen.getByText(/photo does not match the meter/i)).toBeInTheDocument();
  });

  it('offers a resubmit CTA back to closing readings on a rejected reading', async () => {
    renderPage();

    await screen.findByText('Rejected');
    expect(screen.getByText('Your supervisor rejected this reading')).toBeInTheDocument();
    const cta = screen.getByRole('link', { name: /resubmit your closing reading/i });
    expect(cta).toHaveAttribute('href', '/attendant/closing-readings');
  });

  it('drops the resubmit CTA once the shift is closed (supervisor clears it then)', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...snapshot,
      shift: { ...snapshot.shift!, status: 'closed' },
      readings: [
        {
          nozzle_id: 'noz-1',
          opening_reading: '1000.000',
          closing_reading: '1500.000',
          verification_status: 'rejected',
          verification_reason: 'Photo does not match the meter',
        },
      ],
      assignments: [assignment(1, 'Premium')],
    } satisfies AttendantCurrentShift);
    renderPage();

    await screen.findByText('Your supervisor rejected this reading');
    // No re-capture path on a closed shift — the supervisor corrects it.
    expect(
      screen.queryByRole('link', { name: /resubmit your closing reading/i }),
    ).not.toBeInTheDocument();
    expect(
      screen.getByText(/the shift is closed, so your supervisor will correct/i),
    ).toBeInTheDocument();
  });

  it('renders a flagged badge, its reason, and an investigation note (no resubmit CTA)', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...snapshot,
      readings: [
        {
          nozzle_id: 'noz-1',
          opening_reading: '1000.000',
          closing_reading: '1500.000',
          verification_status: 'flagged',
          verification_reason: 'Figure looks tampered — escalating',
        },
      ],
      assignments: [assignment(1, 'Premium')],
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('Flagged for investigation')).toBeInTheDocument();
    expect(screen.getByText(/figure looks tampered/i)).toBeInTheDocument();
    expect(screen.getByText(/your supervisor is investigating this reading/i)).toBeInTheDocument();
    // A flag is the supervisor's own hold — the attendant gets no re-capture path.
    expect(
      screen.queryByRole('link', { name: /resubmit your closing reading/i }),
    ).not.toBeInTheDocument();
  });

  it('marks an unsubmitted nozzle and offers the native path back to closing readings', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...snapshot,
      readings: snapshot.readings.slice(0, 3),
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('Not submitted yet')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /finish closing readings/i })).toHaveAttribute(
      'href',
      '/attendant/closing-readings',
    );
  });

  it('explains when there is nothing to review', async () => {
    attendantCurrentShift.mockResolvedValue({
      status: 'off_duty',
      next_action: 'off_duty',
      user_message: 'You are not on a shift today.',
      attendance: { status: 'not_checked_in' },
      assignments: [],
      readings: [],
      expected_openings_available: false,
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('No readings to review')).toBeInTheDocument();
  });
});
