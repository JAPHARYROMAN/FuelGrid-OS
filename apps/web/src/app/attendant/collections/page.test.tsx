import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { AttendantCurrentShift, CashSubmission, CollectionReceipt } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const submitCash = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    submitCash: (...args: unknown[]) => submitCash(...args),
  },
}));

import CollectionsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <CollectionsPage />
    </QueryClientProvider>,
  );
}

/** A closed shift ready for collections: 500 L + 300 L = 2,321,000.00 expected. */
const closedSnapshot: AttendantCurrentShift = {
  status: 'on_shift',
  next_action: 'submit_collections',
  user_message: 'Readings verified. Submit your shift collections.',
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
  assignments: [],
  readings: [],
  expected_openings_available: true,
  expected_cash: '2321000.00',
  close_lines: [
    {
      nozzle_id: 'noz-1',
      pump_number: 1,
      nozzle_number: 1,
      product_name: 'Premium',
      product_color: '#f97316',
      opening_reading: '1000.000',
      closing_reading: '1500.000',
      litres_sold: '500.000',
      unit_price: '2950.00',
      expected_value: '1475000.00',
    },
    {
      nozzle_id: 'noz-2',
      pump_number: 1,
      nozzle_number: 2,
      product_name: 'Diesel',
      product_color: '#3b82f6',
      opening_reading: '2000.000',
      closing_reading: '2300.000',
      litres_sold: '300.000',
      unit_price: '2820.00',
      expected_value: '846000.00',
    },
  ],
};

const submission: CashSubmission = {
  id: 'cs-1',
  shift_id: 'shift-1',
  expected_cash: '2321000.00',
  cash_amount: '2000000.00',
  mobile_money_amount: '320000.00',
  card_amount: '0.00',
  credit_amount: '0.00',
  submitted_total: '2320000.00',
  variance: '-1000.00',
  submitted_by: 'u-att',
  submitted_at: '2026-06-11T18:00:00Z',
  notes: 'A customer drove off without paying',
};

const receiptBase: CollectionReceipt = {
  id: 'cr-1',
  tenant_id: 't-1',
  station_id: 'st-1',
  shift_id: 'shift-1',
  cash_submission_id: 'cs-1',
  expected_amount: '2321000.00',
  attendant_submitted_total: '2320000.00',
  supervisor_received_total: '2320000.00',
  difference: '-1000.00',
  status: 'received',
  received_by: 'u-sup',
  received_at: '2026-06-11T19:00:00Z',
};

describe('CollectionsPage', () => {
  beforeEach(() => {
    attendantCurrentShift.mockReset();
    submitCash.mockReset();
  });

  it('shows the honest pre-close state while the shift is still open', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...closedSnapshot,
      next_action: 'working',
      shift: { ...closedSnapshot.shift!, status: 'open' },
      expected_cash: undefined,
      close_lines: undefined,
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(
      await screen.findByText(/expected collection is available after the shift closes/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /submit collections/i })).not.toBeInTheDocument();
  });

  it('waits for reading verification before offering the form (basis not final)', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...closedSnapshot,
      next_action: 'await_reading_verification',
      user_message: 'Closing readings submitted. Wait for your supervisor to verify them.',
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(
      await screen.findByText(/supervisor is still verifying your closing readings/i),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /submit collections/i })).not.toBeInTheDocument();
  });

  it('renders the expected total and the per-nozzle litres × price basis', async () => {
    attendantCurrentShift.mockResolvedValue(closedSnapshot);
    renderPage();

    expect(await screen.findByText('Expected collection')).toBeInTheDocument();
    expect(screen.getAllByText('2,321,000.00').length).toBeGreaterThan(0);
    // Per-nozzle basis: litres sold × unit price = expected value.
    expect(screen.getByText('Premium')).toBeInTheDocument();
    expect(screen.getByText('1,475,000.00')).toBeInTheDocument();
    expect(screen.getByText(/500\s*L\s*×\s*2,950\.00/)).toBeInTheDocument();
    expect(screen.getByText('Diesel')).toBeInTheDocument();
    expect(screen.getByText('846,000.00')).toBeInTheDocument();
    expect(screen.getByText(/300\s*L\s*×\s*2,820\.00/)).toBeInTheDocument();
  });

  it('live-calculates a SHORTAGE (text + danger) and requires a reason', async () => {
    attendantCurrentShift.mockResolvedValue(closedSnapshot);
    renderPage();

    const cash = await screen.findByLabelText('Cash');
    await userEvent.type(cash, '2320000');

    expect(screen.getByText(/shortage of 1,000\.00/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/reason for the difference \(required\)/i)).toBeInTheDocument();
    // No reason yet: the primary action stays disabled.
    expect(screen.getByRole('button', { name: /submit collections/i })).toBeDisabled();

    await userEvent.type(
      screen.getByLabelText(/reason for the difference/i),
      'A customer drove off',
    );
    expect(screen.getByRole('button', { name: /submit collections/i })).toBeEnabled();
  });

  it('live-calculates an EXCESS across tenders (text + warning)', async () => {
    attendantCurrentShift.mockResolvedValue(closedSnapshot);
    renderPage();

    await userEvent.type(await screen.findByLabelText('Cash'), '2000000');
    await userEvent.type(screen.getByLabelText('Mobile money'), '321500.50');

    expect(screen.getByText(/excess of 500\.50/i)).toBeInTheDocument();
    expect(screen.getByLabelText(/reason for the difference \(required\)/i)).toBeInTheDocument();
  });

  it('shows BALANCED with no reason needed when the total matches exactly', async () => {
    attendantCurrentShift.mockResolvedValue(closedSnapshot);
    renderPage();

    await userEvent.type(await screen.findByLabelText('Cash'), '2000000');
    await userEvent.type(screen.getByLabelText('Mobile money'), '321000');

    expect(screen.getByText(/balanced — your total matches/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/reason for the difference/i)).not.toBeInTheDocument();
    expect(screen.getByRole('button', { name: /submit collections/i })).toBeEnabled();
  });

  it('guards a ZERO total: even when expected is zero, submitting nothing needs a reason', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...closedSnapshot,
      expected_cash: '0.00',
      close_lines: [],
    } satisfies AttendantCurrentShift);
    renderPage();

    // All tenders empty -> total 0, balanced vs 0 expected, but still guarded.
    await screen.findByLabelText('Cash');
    expect(screen.getByLabelText(/reason for the difference \(required\)/i)).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText(/explain why you are submitting nothing/i),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /submit collections/i })).toBeDisabled();
  });

  it('rejects a malformed tender with a plain-language error', async () => {
    attendantCurrentShift.mockResolvedValue(closedSnapshot);
    renderPage();

    await userEvent.type(await screen.findByLabelText('Cash'), '2,000');
    expect(screen.getByText(/enter a money amount like/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /submit collections/i })).toBeDisabled();
  });

  it('confirms before submitting and posts the tender breakdown with the reason in notes', async () => {
    attendantCurrentShift.mockResolvedValue(closedSnapshot);
    submitCash.mockResolvedValue(submission);
    renderPage();

    await userEvent.type(await screen.findByLabelText('Cash'), '2000000');
    await userEvent.type(screen.getByLabelText('Mobile money'), '320000');
    await userEvent.type(
      screen.getByLabelText(/reason for the difference/i),
      'A customer drove off without paying',
    );
    await userEvent.click(screen.getByRole('button', { name: /submit collections/i }));

    // The confirmation step recaps total, expected, and the signed difference
    // — nothing has been posted yet.
    expect(screen.getByText('Confirm your collections')).toBeInTheDocument();
    expect(screen.getByText(/you can submit collections only once/i)).toBeInTheDocument();
    expect(screen.getByText('-1,000.00')).toBeInTheDocument();
    expect(submitCash).not.toHaveBeenCalled();

    // Going back keeps the entered figures editable.
    await userEvent.click(screen.getByRole('button', { name: /go back and edit/i }));
    expect(screen.getByLabelText('Cash')).toHaveValue('2000000');

    await userEvent.click(screen.getByRole('button', { name: /submit collections/i }));
    await userEvent.click(screen.getByRole('button', { name: /confirm and submit/i }));

    expect(submitCash).toHaveBeenCalledWith('shift-1', {
      cash_amount: '2000000',
      mobile_money_amount: '320000',
      card_amount: '0',
      credit_amount: '0',
      notes: 'A customer drove off without paying',
    });
  });

  it('locks into the read-only submitted view once the one-per-shift submission exists', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...closedSnapshot,
      next_action: 'await_collection_receipt',
      cash_submission: submission,
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('Your submission')).toBeInTheDocument();
    expect(
      screen.getByText(/submitted — waiting for your supervisor to confirm receipt/i),
    ).toBeInTheDocument();
    // Read-only: the tender breakdown is text, not inputs.
    expect(screen.queryByLabelText('Cash')).not.toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /submit collections/i })).not.toBeInTheDocument();
    expect(screen.getByText('2,320,000.00')).toBeInTheDocument();
    expect(screen.getByText(/shortage of 1,000\.00/i)).toBeInTheDocument();
    expect(screen.getByText(/a customer drove off without paying/i)).toBeInTheDocument();
  });

  it('shows the supervisor receipt: received', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...closedSnapshot,
      next_action: 'complete',
      cash_submission: { ...submission, submitted_total: '2321000.00', variance: '0.00' },
      collection_receipt: {
        ...receiptBase,
        supervisor_received_total: '2321000.00',
        difference: '0.00',
        status: 'received',
      },
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('Supervisor receipt')).toBeInTheDocument();
    // Both the status badge and the "Received" amount row are present.
    expect(screen.getAllByText('Received')).toHaveLength(2);
  });

  it('shows the supervisor receipt: approved with difference + the supervisor reason', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...closedSnapshot,
      next_action: 'complete',
      cash_submission: submission,
      collection_receipt: {
        ...receiptBase,
        status: 'approved_with_difference',
        reason: '1,000 short — attendant to repay',
        supervisor_comment: 'counted twice',
      },
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('Approved with difference')).toBeInTheDocument();
    expect(
      screen.getByText(/supervisor reason: 1,000 short — attendant to repay/i),
    ).toBeInTheDocument();
    expect(screen.getByText(/comment: counted twice/i)).toBeInTheDocument();
  });

  it('shows the REJECTED receipt with the blocked-state guidance', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...closedSnapshot,
      next_action: 'blocked',
      blocking_code: 'collection_rejected',
      user_message: 'Your collection was rejected. See your supervisor.',
      cash_submission: submission,
      collection_receipt: {
        ...receiptBase,
        status: 'rejected',
        reason: 'Notes do not match the count',
      },
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('Rejected')).toBeInTheDocument();
    expect(
      screen.getByText('Your collection was rejected. See your supervisor.'),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/supervisor reason: notes do not match the count/i),
    ).toBeInTheDocument();
  });
});
