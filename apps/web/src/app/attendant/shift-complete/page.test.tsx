import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { AttendantCurrentShift } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const checkOutOfShift = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    checkOutOfShift: (...args: unknown[]) => checkOutOfShift(...args),
  },
}));

import ShiftCompletePage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ShiftCompletePage />
    </QueryClientProvider>,
  );
}

/** A finished shift: readings verified, collections received, ready to check out. */
const completeSnapshot: AttendantCurrentShift = {
  status: 'complete',
  next_action: 'complete',
  user_message: 'Shift complete. Wait for final approval.',
  station: { id: 'st-1', name: 'Mikocheni' },
  shift: {
    id: 'shift-1',
    tenant_id: 't-1',
    station_id: 'st-1',
    operating_day_id: 'day-1',
    name: 'Morning',
    status: 'closed',
    opened_by: 'u-1',
    opened_at: '2026-06-11T05:00:00Z',
    slot: 'morning',
  },
  attendance: { status: 'checked_in', check_in_at: '2026-06-11T05:10:00Z' },
  assignments: [
    {
      assignment_id: 'as-1',
      nozzle_id: 'noz-1',
      pump_number: 1,
      nozzle_number: 1,
      product_name: 'Premium',
      product_color: '#f97316',
      meter_decimal_places: 2,
      assigned_at: '2026-06-11T05:05:00Z',
      confirmed_at: '2026-06-11T05:06:00Z',
    },
  ],
  readings: [
    {
      nozzle_id: 'noz-1',
      opening_reading: '1000.000',
      closing_reading: '1500.000',
      verification_status: 'approved',
      final_reading: '1500.000',
    },
  ],
  expected_openings_available: true,
  expected_cash: '1475000.00',
  cash_submission: {
    id: 'cs-1',
    shift_id: 'shift-1',
    expected_cash: '1475000.00',
    cash_amount: '1470000.00',
    mobile_money_amount: '0.00',
    card_amount: '0.00',
    credit_amount: '0.00',
    submitted_total: '1470000.00',
    variance: '-5000.00',
    submitted_by: 'u-att',
    submitted_at: '2026-06-11T18:00:00Z',
    notes: 'Short due to drive-off',
  },
  collection_receipt: {
    id: 'cr-1',
    tenant_id: 't-1',
    station_id: 'st-1',
    shift_id: 'shift-1',
    cash_submission_id: 'cs-1',
    expected_amount: '1475000.00',
    attendant_submitted_total: '1470000.00',
    supervisor_received_total: '1470000.00',
    difference: '-5000.00',
    status: 'approved_with_difference',
    reason: '5,000 short — attendant to repay',
    received_by: 'u-sup',
    received_at: '2026-06-11T19:00:00Z',
  },
};

describe('ShiftCompletePage', () => {
  beforeEach(() => {
    attendantCurrentShift.mockReset();
    checkOutOfShift.mockReset();
  });

  it('summarizes the finished shift: readings, collections, difference, and completion message', async () => {
    attendantCurrentShift.mockResolvedValue(completeSnapshot);
    renderPage();

    expect(await screen.findByText(/shift complete — well done/i)).toBeInTheDocument();
    // Readings status with native link to the review detail.
    expect(
      screen.getByText('1 of 1 closing readings verified by your supervisor.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /view reading details/i })).toHaveAttribute(
      'href',
      '/attendant/review-status',
    );
    // Collections status: expected, submitted, received, difference, reason.
    expect(screen.getByText('Approved with difference')).toBeInTheDocument();
    expect(screen.getByText('1,475,000.00')).toBeInTheDocument();
    expect(screen.getAllByText('1,470,000.00')).toHaveLength(2);
    expect(screen.getByText('-5,000.00')).toBeInTheDocument();
    expect(screen.getByText(/supervisor reason: 5,000 short/i)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /view collection details/i })).toHaveAttribute(
      'href',
      '/attendant/collections',
    );
  });

  it('offers check-out while still checked in and posts it (Phase 0 endpoint)', async () => {
    attendantCurrentShift.mockResolvedValue(completeSnapshot);
    checkOutOfShift.mockResolvedValue({ id: 'att-1', status: 'checked_out' });
    renderPage();

    const button = await screen.findByRole('button', { name: /check out/i });
    await userEvent.click(button);

    expect(checkOutOfShift).toHaveBeenCalledWith('shift-1');
  });

  it('shows the checked-out confirmation instead of the button after check-out', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...completeSnapshot,
      attendance: {
        status: 'checked_out',
        check_in_at: '2026-06-11T05:10:00Z',
        check_out_at: '2026-06-11T19:30:00Z',
      },
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText(/you are checked out/i)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /check out/i })).not.toBeInTheDocument();
  });

  it('redirects attention home while the shift is not complete yet', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...completeSnapshot,
      status: 'on_shift',
      next_action: 'submit_collections',
      user_message: 'Readings verified. Submit your shift collections.',
      collection_receipt: undefined,
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('Your shift is not complete yet')).toBeInTheDocument();
    expect(
      screen.getByText('Readings verified. Submit your shift collections.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /back to my shift/i })).toHaveAttribute(
      'href',
      '/attendant',
    );
    expect(screen.queryByRole('button', { name: /check out/i })).not.toBeInTheDocument();
  });

  it('explains when there is no shift at all', async () => {
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

    expect(await screen.findByText('No shift to complete')).toBeInTheDocument();
  });
});
