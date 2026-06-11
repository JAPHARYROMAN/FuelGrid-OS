import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { AttendantCurrentShift } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const checkInToShift = vi.fn();
const checkOutOfShift = vi.fn();
const confirmNozzleAssignment = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    checkInToShift: (...args: unknown[]) => checkInToShift(...args),
    checkOutOfShift: (...args: unknown[]) => checkOutOfShift(...args),
    confirmNozzleAssignment: (...args: unknown[]) => confirmNozzleAssignment(...args),
  },
}));

import AttendantHomePage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AttendantHomePage />
    </QueryClientProvider>,
  );
}

const baseSnapshot: AttendantCurrentShift = {
  status: 'on_shift',
  next_action: 'check_in',
  user_message: 'Your shift is open. Check in to start working.',
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
  attendance: { status: 'not_checked_in' },
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
    },
  ],
  readings: [],
  expected_openings_available: false,
};

describe('AttendantHomePage', () => {
  beforeEach(() => {
    attendantCurrentShift.mockReset();
    checkInToShift.mockReset();
    checkOutOfShift.mockReset();
    confirmNozzleAssignment.mockReset();
  });

  it('renders the off-duty snapshot without shift data', async () => {
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

    expect(await screen.findByText('Off duty')).toBeInTheDocument();
    expect(screen.getByText('You are not on a shift today.')).toBeInTheDocument();
  });

  it('renders the expected-today duty with slot, team, and station', async () => {
    attendantCurrentShift.mockResolvedValue({
      status: 'expected_today',
      next_action: 'await_shift_open',
      user_message:
        'Your team Team A covers the morning shift today at Mikocheni. Wait for your supervisor to open the shift.',
      station: { id: 'st-1', name: 'Mikocheni' },
      expected_today: { slot: 'morning', team_id: 'team-1', team_name: 'Team A' },
      attendance: { status: 'not_checked_in' },
      assignments: [],
      readings: [],
      expected_openings_available: false,
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('You are on duty today')).toBeInTheDocument();
    expect(screen.getByText('morning shift')).toBeInTheDocument();
    expect(screen.getByText('Team A')).toBeInTheDocument();
    expect(screen.getByText('Mikocheni')).toBeInTheDocument();
  });

  it('drives the check-in CTA from next_action and posts the check-in', async () => {
    attendantCurrentShift.mockResolvedValue(baseSnapshot);
    checkInToShift.mockResolvedValue({ id: 'att-1', status: 'checked_in' });
    renderPage();

    const button = await screen.findByRole('button', { name: /check in/i });
    await userEvent.click(button);

    expect(checkInToShift).toHaveBeenCalledWith('shift-1', expect.anything());
    expect(await screen.findByText('You are checked in.')).toBeInTheDocument();
  });

  it('shows the blocked state message with the assignment list', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'blocked',
      blocking_code: 'awaiting_nozzle_assignment',
      user_message: 'You are checked in. Wait for your nozzle assignment.',
      attendance: { status: 'checked_in', check_in_at: '2026-06-11T05:10:00Z' },
      assignments: [],
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(
      await screen.findByText('You are checked in. Wait for your nozzle assignment.'),
    ).toBeInTheDocument();
    // Wait states have no primary CTA.
    expect(screen.queryByRole('button', { name: /check in/i })).not.toBeInTheDocument();
  });

  it('confirms every unconfirmed assignment from the confirm CTA', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'confirm_assignment',
      user_message: 'Confirm your nozzle assignment to continue.',
      attendance: { status: 'checked_in', check_in_at: '2026-06-11T05:10:00Z' },
    } satisfies AttendantCurrentShift);
    confirmNozzleAssignment.mockResolvedValue({ id: 'as-1' });
    renderPage();

    expect(await screen.findByText('Awaiting your confirmation')).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /confirm my nozzles/i }));
    expect(confirmNozzleAssignment).toHaveBeenCalledWith('shift-1', 'as-1');
  });

  it('drives verify_opening_readings to the NATIVE opening-readings screen (no deep-link)', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'verify_opening_readings',
      user_message: 'Verify the opening reading on each of your nozzles.',
      attendance: { status: 'checked_in', check_in_at: '2026-06-11T05:10:00Z' },
      assignments: [
        { ...baseSnapshot.assignments[0]! },
        {
          ...baseSnapshot.assignments[0]!,
          assignment_id: 'as-2',
          nozzle_id: 'noz-2',
          nozzle_number: 2,
          product_name: 'Diesel',
        },
      ],
      readings: [{ nozzle_id: 'noz-1', opening_reading: '1000.000' }],
    } satisfies AttendantCurrentShift);
    renderPage();

    const link = await screen.findByRole('link', { name: /verify opening readings/i });
    expect(link).toHaveAttribute('href', '/attendant/opening-readings');
    // The honest "opens the full site" stub is gone for this stage.
    expect(screen.queryByText(/opens the full site/i)).not.toBeInTheDocument();
    // The checklist stage shows per-nozzle progress: one of two openings done.
    expect(screen.getByText('1 of 2 nozzles verified')).toBeInTheDocument();
  });

  it('deep-links submit_collections to the existing my-shift page', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'submit_collections',
      user_message: 'Readings verified. Submit your shift collections.',
      shift: { ...baseSnapshot.shift!, status: 'closed' },
      attendance: { status: 'checked_in', check_in_at: '2026-06-11T05:10:00Z' },
      expected_cash: '2321000.00',
      readings: [
        {
          nozzle_id: 'noz-1',
          opening_reading: '1000.000',
          closing_reading: '1500.000',
          verification_status: 'approved',
        },
      ],
    } satisfies AttendantCurrentShift);
    renderPage();

    const link = await screen.findByRole('link', { name: /submit collections/i });
    expect(link).toHaveAttribute('href', '/my-shift');
    // Expected collections figure is visible once the shift is closed.
    expect(screen.getByText(/2,321,000/)).toBeInTheDocument();
  });
});
