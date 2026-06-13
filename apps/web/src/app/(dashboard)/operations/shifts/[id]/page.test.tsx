import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import {
  SdkError,
  type CollectionReceipt,
  type MeterReading,
  type ReadingVerification,
  type ShiftDetail,
} from '@fuelgrid/sdk';

const getShift = vi.fn();
const listMeterReadings = vi.fn();
const listAuditLogs = vi.fn();
const listReadingVerifications = vi.fn();
const listShiftAttendance = vi.fn();
const listEmployees = vi.fn();
const getStationOverview = vi.fn();
const listProducts = vi.fn();
const getCloseSummary = vi.fn();
const getCollectionReceipt = vi.fn();
const listShiftExceptions = vi.fn();
const verifyShiftReadings = vi.fn();
const verifyCorrectReading = vi.fn();
const approveReading = vi.fn();
const rejectReading = vi.fn();
const flagReading = vi.fn();
const confirmCashSubmission = vi.fn();
const approveShift = vi.fn();
const assignAttendant = vi.fn();
const assignNozzle = vi.fn();
const unassignNozzle = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    getShift: (...a: unknown[]) => getShift(...a),
    listMeterReadings: (...a: unknown[]) => listMeterReadings(...a),
    listAuditLogs: (...a: unknown[]) => listAuditLogs(...a),
    listReadingVerifications: (...a: unknown[]) => listReadingVerifications(...a),
    listShiftAttendance: (...a: unknown[]) => listShiftAttendance(...a),
    listEmployees: (...a: unknown[]) => listEmployees(...a),
    getStationOverview: (...a: unknown[]) => getStationOverview(...a),
    listProducts: (...a: unknown[]) => listProducts(...a),
    getCloseSummary: (...a: unknown[]) => getCloseSummary(...a),
    getCollectionReceipt: (...a: unknown[]) => getCollectionReceipt(...a),
    listShiftExceptions: (...a: unknown[]) => listShiftExceptions(...a),
    verifyShiftReadings: (...a: unknown[]) => verifyShiftReadings(...a),
    verifyCorrectReading: (...a: unknown[]) => verifyCorrectReading(...a),
    approveReading: (...a: unknown[]) => approveReading(...a),
    rejectReading: (...a: unknown[]) => rejectReading(...a),
    flagReading: (...a: unknown[]) => flagReading(...a),
    confirmCashSubmission: (...a: unknown[]) => confirmCashSubmission(...a),
    approveShift: (...a: unknown[]) => approveShift(...a),
    assignAttendant: (...a: unknown[]) => assignAttendant(...a),
    assignNozzle: (...a: unknown[]) => assignNozzle(...a),
    unassignNozzle: (...a: unknown[]) => unassignNozzle(...a),
  },
}));

// Permission map per test: usePermission(code) → perms[code] ?? false. The
// real PermissionGate runs against this mock, so gating is exercised for real.
let perms: Record<string, boolean> = {};
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string) => perms[code] ?? false,
}));

vi.mock('next/navigation', () => ({ useParams: () => ({ id: 'sh-1' }) }));

import ShiftReviewPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ShiftReviewPage />
    </QueryClientProvider>,
  );
}

function shift(overrides: Partial<ShiftDetail> = {}): ShiftDetail {
  return {
    id: 'sh-1',
    tenant_id: 't-1',
    station_id: 'st-1',
    operating_day_id: 'day-1',
    name: 'Morning',
    status: 'open',
    opened_by: 'u-sup',
    opened_at: '2026-06-01T06:00:00Z',
    attendants: [
      {
        shift_id: 'sh-1',
        user_id: 'u-att',
        assigned_by: 'u-sup',
        assigned_at: '2026-06-01T06:00:00Z',
      },
    ],
    nozzle_assignments: [
      {
        id: 'na-1',
        shift_id: 'sh-1',
        nozzle_id: 'noz-1',
        attendant_id: 'u-att',
        assigned_at: '2026-06-01T06:01:00Z',
      },
    ],
    ...overrides,
  } as ShiftDetail;
}

function closedShift(overrides: Partial<ShiftDetail> = {}): ShiftDetail {
  return shift({ status: 'closed', closed_at: '2026-06-01T14:00:00Z', ...overrides });
}

function reading(overrides: Partial<MeterReading> = {}): MeterReading {
  return {
    id: 'mr-close',
    tenant_id: 't-1',
    shift_id: 'sh-1',
    nozzle_id: 'noz-1',
    reading_type: 'closing',
    reading: '1500.00',
    recorded_by: 'u-att',
    recorded_at: '2026-06-01T13:50:00Z',
    status: 'active',
    ...overrides,
  };
}

const openingReading = reading({
  id: 'mr-open',
  reading_type: 'opening',
  reading: '1000.00',
  recorded_at: '2026-06-01T06:10:00Z',
});

function verification(overrides: Partial<ReadingVerification> = {}): ReadingVerification {
  return {
    id: 'rv-1',
    tenant_id: 't-1',
    station_id: 'st-1',
    shift_id: 'sh-1',
    nozzle_id: 'noz-1',
    reading_id: 'mr-close',
    attendant_submitted_reading: '1500.00',
    final_approved_reading: '1500.00',
    status: 'approved',
    verified_by: 'u-sup',
    verified_at: '2026-06-01T14:05:00Z',
    ...overrides,
  };
}

const cashSubmission = {
  id: 'cs-1',
  shift_id: 'sh-1',
  expected_cash: '1475000.00',
  cash_amount: '1400000.00',
  mobile_money_amount: '70000.00',
  card_amount: '0.00',
  credit_amount: '0.00',
  submitted_total: '1470000.00',
  variance: '-5000.00',
  submitted_by: 'u-att',
  submitted_at: '2026-06-01T14:10:00Z',
  notes: '5,000 short — drive-off at pump 1',
};

function receipt(overrides: Partial<CollectionReceipt> = {}): CollectionReceipt {
  return {
    id: 'cr-1',
    tenant_id: 't-1',
    station_id: 'st-1',
    shift_id: 'sh-1',
    cash_submission_id: 'cs-1',
    expected_amount: '1475000.00',
    attendant_submitted_total: '1470000.00',
    supervisor_received_total: '1470000.00',
    difference: '-5000.00',
    status: 'approved_with_difference',
    reason: 'drive-off confirmed',
    received_by: 'u-sup',
    received_at: '2026-06-01T15:00:00Z',
    ...overrides,
  };
}

const notFound404 = () => new SdkError('not found', 404, { error: 'not found' });

describe('ShiftReviewPage', () => {
  beforeEach(() => {
    perms = {
      'reading.override': true,
      'cash.confirm': true,
      'shift.approve': true,
      'shift.assign': true,
    };
    getShift.mockResolvedValue(shift());
    listMeterReadings.mockResolvedValue({ items: [], count: 0, dispensed: [] });
    listAuditLogs.mockResolvedValue({ items: [], count: 0, has_more: false });
    listReadingVerifications.mockResolvedValue({ items: [], count: 0 });
    listShiftAttendance.mockResolvedValue({ items: [], count: 0 });
    listEmployees.mockResolvedValue({
      items: [
        {
          id: 'emp-1',
          tenant_id: 't-1',
          station_id: 'st-1',
          user_id: 'u-att',
          full_name: 'Asha Attendant',
          role: { id: 'r1', name: 'Attendant' },
          status: 'active',
          created_at: '',
          updated_at: '',
        },
        {
          id: 'emp-2',
          tenant_id: 't-1',
          station_id: 'st-1',
          user_id: 'u-sup',
          full_name: 'Said Supervisor',
          role: { id: 'r2', name: 'Manager' },
          status: 'active',
          created_at: '',
          updated_at: '',
        },
      ],
      count: 2,
      has_more: false,
    });
    getStationOverview.mockResolvedValue({
      station: { id: 'st-1', name: 'Mikocheni' },
      tanks: [],
      pumps: [
        {
          id: 'p-1',
          number: 1,
          nozzles: [
            {
              id: 'noz-1',
              number: 2,
              product_id: 'prod-1',
              tank_id: 'tk-1',
              meter_decimal_places: 2,
              status: 'active',
            },
          ],
        },
      ],
      open_shifts: [],
      open_incidents: [],
    });
    listProducts.mockResolvedValue({
      items: [{ id: 'prod-1', name: 'Petrol', code: 'PMS' }],
      count: 1,
      has_more: false,
    });
    getCloseSummary.mockResolvedValue({
      shift: shift({ status: 'closed' }),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: null,
    });
    getCollectionReceipt.mockRejectedValue(notFound404());
    listShiftExceptions.mockResolvedValue({ items: [], count: 0, has_more: false });
    verifyShiftReadings.mockResolvedValue({ items: [verification()], count: 1, newly_verified: 1 });
    verifyCorrectReading.mockResolvedValue(
      verification({ status: 'corrected', supervisor_verified_reading: '1490.00' }),
    );
    approveReading.mockResolvedValue(verification({ status: 'approved' }));
    rejectReading.mockResolvedValue(verification({ status: 'rejected', reason: 'photo mismatch' }));
    flagReading.mockResolvedValue(verification({ status: 'flagged', reason: 'looks tampered' }));
    confirmCashSubmission.mockResolvedValue(receipt());
    approveShift.mockResolvedValue(shift({ status: 'approved' }));
    assignAttendant.mockResolvedValue(undefined);
    assignNozzle.mockResolvedValue({
      id: 'na-2',
      shift_id: 'sh-1',
      nozzle_id: 'noz-1',
      attendant_id: 'u-att',
      assigned_at: '2026-06-01T06:30:00Z',
    });
    unassignNozzle.mockResolvedValue(undefined);
  });

  afterEach(() => vi.clearAllMocks());

  // --- timeline (Feature 3.4, retained) ---

  it('still composes the lifecycle timeline', async () => {
    getShift.mockResolvedValue(
      shift({
        status: 'approved',
        closed_at: '2026-06-01T14:00:00Z',
        approved_at: '2026-06-01T15:00:00Z',
      }),
    );
    renderPage();
    await screen.findByTestId('shift-timeline');
    expect(screen.getByText('Shift opened')).toBeInTheDocument();
    expect(screen.getByText('Shift closed & submitted')).toBeInTheDocument();
    expect(screen.getByText('Shift approved')).toBeInTheDocument();
  });

  it('shows a not-found state for a missing shift', async () => {
    getShift.mockRejectedValue(notFound404());
    renderPage();
    expect(await screen.findByText('Shift not found')).toBeInTheDocument();
  });

  // --- attendance ---

  it('shows checked-in attendants with times and flags rostered no-shows', async () => {
    getShift.mockResolvedValue(
      shift({
        attendants: [
          { shift_id: 'sh-1', user_id: 'u-att', assigned_by: 'u-sup', assigned_at: '' },
          { shift_id: 'sh-1', user_id: 'u-noshow', assigned_by: 'u-sup', assigned_at: '' },
        ],
      }),
    );
    listShiftAttendance.mockResolvedValue({
      items: [
        {
          id: 'a-1',
          tenant_id: 't-1',
          station_id: 'st-1',
          shift_id: 'sh-1',
          attendant_id: 'u-att',
          status: 'checked_in',
          check_in_at: '2026-06-01T06:05:00Z',
        },
      ],
      count: 1,
    });
    renderPage();

    const panel = await screen.findByTestId('attendance-panel');
    const rows = await within(panel).findAllByTestId('attendance-row');
    expect(rows).toHaveLength(2);
    expect(within(rows[0]!).getByText('Asha Attendant')).toBeInTheDocument();
    expect(within(rows[0]!).getByText('Checked in')).toBeInTheDocument();
    expect(within(rows[1]!).getByText('Not checked in')).toBeInTheDocument();
  });

  // --- verification queue ---

  it('renders a pending closing reading with nozzle, product, attendant, litres and Approve all', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    renderPage();

    const queue = await screen.findByTestId('verification-queue');
    const row = await within(queue).findByTestId('verification-row');
    expect(within(row).getByText(/Pump 1 · Nozzle 2/)).toBeInTheDocument();
    expect(within(row).getByText(/Petrol/)).toBeInTheDocument();
    expect(await within(row).findByText(/Asha Attendant/)).toBeInTheDocument();
    expect(within(row).getByText('1000.00 → 1500.00')).toBeInTheDocument();
    expect(within(row).getByText('500 L')).toBeInTheDocument();
    expect(within(row).getByText('Pending verification')).toBeInTheDocument();
    expect(within(queue).getByRole('button', { name: /approve all \(1\)/i })).toBeInTheDocument();
  });

  it('shows a corrected verification with both values and the reason', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    listReadingVerifications.mockResolvedValue({
      items: [
        verification({
          status: 'corrected',
          supervisor_verified_reading: '1490.00',
          final_approved_reading: '1490.00',
          reason: 'pump display misread',
        }),
      ],
      count: 1,
    });
    renderPage();

    const queue = await screen.findByTestId('verification-queue');
    const row = await within(queue).findByTestId('verification-row');
    expect(within(row).getByText('Corrected')).toBeInTheDocument();
    expect(within(row).getByText('1500.00')).toBeInTheDocument();
    expect(within(row).getByText('1490.00')).toBeInTheDocument();
    expect(within(row).getByText(/pump display misread/)).toBeInTheDocument();
    // Verified readings have no further actions and no batch button.
    expect(within(queue).queryByRole('button', { name: /approve all/i })).not.toBeInTheDocument();
    expect(within(queue).queryByRole('button', { name: /correct/i })).not.toBeInTheDocument();
  });

  it('approve-all asks for confirmation, then batch-verifies', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /approve all \(1\)/i }));
    expect(await screen.findByText('Approve all pending readings?')).toBeInTheDocument();
    expect(verifyShiftReadings).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: /approve all as submitted/i }));
    await waitFor(() => expect(verifyShiftReadings).toHaveBeenCalledWith('sh-1'));
  });

  it('surfaces the separation-of-duties 403 from batch verify cleanly', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    verifyShiftReadings.mockRejectedValue(
      new SdkError('separation of duties: you cannot verify readings you recorded', 403, {
        error: 'separation of duties: you cannot verify readings you recorded',
      }),
    );
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /approve all \(1\)/i }));
    await userEvent.click(screen.getByRole('button', { name: /approve all as submitted/i }));
    expect(
      await screen.findByText(/separation of duties: you cannot verify readings you recorded/i),
    ).toBeInTheDocument();
  });

  it('correct modal validates meter scale and requires a reason before submitting', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /correct…/i }));
    const valueInput = await screen.findByLabelText('Verified reading');
    const submit = screen.getByRole('button', { name: /verify with correction/i });

    // Too many decimals for a 2dp meter — blocked with the scale message.
    await userEvent.type(valueInput, '1490.555');
    expect(await screen.findByText(/too many decimals/i)).toBeInTheDocument();
    expect(submit).toBeDisabled();

    // Valid figure but no reason — still blocked.
    await userEvent.clear(valueInput);
    await userEvent.type(valueInput, '1490.55');
    expect(submit).toBeDisabled();

    // Below the opening — blocked with the rollback message.
    await userEvent.clear(valueInput);
    await userEvent.type(valueInput, '900');
    expect(await screen.findByText(/cannot be below the opening reading/i)).toBeInTheDocument();

    await userEvent.clear(valueInput);
    await userEvent.type(valueInput, '1490.55');
    await userEvent.type(screen.getByLabelText('Correction reason'), 'misread display');
    expect(submit).toBeEnabled();
    await userEvent.click(submit);

    await waitFor(() =>
      expect(verifyCorrectReading).toHaveBeenCalledWith('sh-1', 'mr-close', {
        verified_reading: '1490.55',
        reason: 'misread display',
      }),
    );
  });

  it('hides verification actions without reading.override (read-only)', async () => {
    perms = {
      'reading.override': false,
      'cash.confirm': true,
      'shift.approve': true,
      'shift.assign': true,
    };
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    renderPage();

    const queue = await screen.findByTestId('verification-queue');
    await within(queue).findByTestId('verification-row');
    expect(within(queue).queryByRole('button', { name: /approve all/i })).not.toBeInTheDocument();
    expect(within(queue).queryByRole('button', { name: /correct/i })).not.toBeInTheDocument();
    expect(within(queue).queryByRole('button', { name: /reject/i })).not.toBeInTheDocument();
    expect(
      within(queue).queryByRole('button', { name: /flag for investigation/i }),
    ).not.toBeInTheDocument();
    expect(within(queue).getByText('Pending verification')).toBeInTheDocument();
  });

  it('reject modal requires a reason, then calls rejectReading', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /^reject…$/i }));
    const reasonInput = await screen.findByLabelText('Rejection reason');
    const submit = screen.getByRole('button', { name: /^reject reading$/i });

    // No reason — blocked.
    expect(submit).toBeDisabled();
    expect(rejectReading).not.toHaveBeenCalled();

    await userEvent.type(reasonInput, 'photo does not match the meter');
    expect(submit).toBeEnabled();
    await userEvent.click(submit);

    await waitFor(() =>
      expect(rejectReading).toHaveBeenCalledWith('sh-1', 'mr-close', {
        reason: 'photo does not match the meter',
      }),
    );
  });

  it('flag modal requires a reason, then calls flagReading', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /flag for investigation…/i }));
    const reasonInput = await screen.findByLabelText('Flag reason');
    const submit = screen.getByRole('button', { name: /^flag reading$/i });

    expect(submit).toBeDisabled();
    expect(flagReading).not.toHaveBeenCalled();

    await userEvent.type(reasonInput, 'figure looks tampered');
    expect(submit).toBeEnabled();
    await userEvent.click(submit);

    await waitFor(() =>
      expect(flagReading).toHaveBeenCalledWith('sh-1', 'mr-close', {
        reason: 'figure looks tampered',
      }),
    );
  });

  it('surfaces the separation-of-duties 403 from reject cleanly', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    rejectReading.mockRejectedValue(
      new SdkError('separation of duties: you cannot verify readings you recorded', 403, {
        error: 'separation of duties: you cannot verify readings you recorded',
      }),
    );
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /^reject…$/i }));
    await userEvent.type(await screen.findByLabelText('Rejection reason'), 'mismatch');
    await userEvent.click(screen.getByRole('button', { name: /^reject reading$/i }));

    expect(
      await screen.findByText(/separation of duties: you cannot verify readings you recorded/i),
    ).toBeInTheDocument();
  });

  it('shows a rejected verification with its reason and resubmit note', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    listReadingVerifications.mockResolvedValue({
      items: [verification({ status: 'rejected', reason: 'blurry meter photo' })],
      count: 1,
    });
    renderPage();

    const queue = await screen.findByTestId('verification-queue');
    const row = await within(queue).findByTestId('verification-row');
    expect(within(row).getByText('Rejected')).toBeInTheDocument();
    expect(within(row).getByText(/blurry meter photo/)).toBeInTheDocument();
    expect(within(row).getByText(/sent back to the attendant to re-capture/i)).toBeInTheDocument();
    // A rejected reading is a HOLD: it offers clear actions but not another Reject.
    expect(within(queue).queryByRole('button', { name: /^reject…$/i })).not.toBeInTheDocument();
    expect(
      within(queue).getByRole('button', { name: /approve as submitted/i }),
    ).toBeInTheDocument();
  });

  it('shows a flagged verification with its reason', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    listReadingVerifications.mockResolvedValue({
      items: [verification({ status: 'flagged', reason: 'escalated to manager' })],
      count: 1,
    });
    renderPage();

    const queue = await screen.findByTestId('verification-queue');
    const row = await within(queue).findByTestId('verification-row');
    expect(within(row).getByText('Flagged for investigation')).toBeInTheDocument();
    expect(within(row).getByText(/escalated to manager/)).toBeInTheDocument();
  });

  it('clears a FLAGGED reading by approving it as-submitted (per-reading approve path)', async () => {
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    listReadingVerifications.mockResolvedValue({
      items: [verification({ status: 'flagged', reason: 'looked off — investigated' })],
      count: 1,
    });
    renderPage();

    const queue = await screen.findByTestId('verification-queue');
    // Wait for the flagged verdict to render before asserting its clear actions.
    await within(queue).findByText('Flagged for investigation');
    // The hold offers Approve/Correct/Reject (the supervisor can re-decide it).
    const approve = within(queue).getByRole('button', { name: /approve as submitted/i });
    expect(within(queue).getByRole('button', { name: /^correct…$/i })).toBeInTheDocument();
    expect(within(queue).getByRole('button', { name: /^reject…$/i })).toBeInTheDocument();

    await userEvent.click(approve);
    await waitFor(() => expect(approveReading).toHaveBeenCalledWith('sh-1', 'mr-close'));
  });

  it('lets the supervisor re-confirm a FLAGGED collection receipt to clear the hold', async () => {
    getShift.mockResolvedValue(closedShift());
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    getCollectionReceipt.mockResolvedValue(
      receipt({ status: 'flagged', reason: 'cash count disputed', difference: '0.00' }),
    );
    confirmCashSubmission.mockResolvedValue(receipt({ status: 'received', difference: '0.00' }));
    renderPage();

    const panel = await screen.findByTestId('collection-receipt-panel');
    // A held receipt still shows its status AND a re-confirm form.
    expect(await within(panel).findByText(/re-confirm the handover below/i)).toBeInTheDocument();
    await userEvent.type(within(panel).getByLabelText('Received total'), '1475000');
    await userEvent.click(within(panel).getByRole('button', { name: /confirm receipt/i }));

    await waitFor(() =>
      expect(confirmCashSubmission).toHaveBeenCalledWith('sh-1', { received_total: '1475000' }),
    );
  });

  it('keeps a TERMINAL collection receipt read-only (no re-confirm form)', async () => {
    getShift.mockResolvedValue(closedShift());
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    getCollectionReceipt.mockResolvedValue(receipt({ status: 'received', difference: '0.00' }));
    renderPage();

    const panel = await screen.findByTestId('collection-receipt-panel');
    await within(panel).findByTestId('receipt-status');
    expect(within(panel).queryByText(/re-confirm the handover below/i)).not.toBeInTheDocument();
    expect(within(panel).queryByLabelText('Received total')).not.toBeInTheDocument();
  });

  // --- ad-hoc attendant assignment (PRD gap #5) ---

  it('assigns a rostered attendant to a nozzle without re-adding them to the roster', async () => {
    // noz-1 is already assigned in the fixture; expose a second free nozzle.
    getStationOverview.mockResolvedValue({
      station: { id: 'st-1', name: 'Mikocheni' },
      tanks: [],
      pumps: [
        {
          id: 'p-1',
          number: 1,
          nozzles: [
            {
              id: 'noz-1',
              number: 2,
              product_id: 'prod-1',
              tank_id: 'tk-1',
              meter_decimal_places: 2,
              status: 'active',
            },
            {
              id: 'noz-2',
              number: 3,
              product_id: 'prod-1',
              tank_id: 'tk-1',
              meter_decimal_places: 2,
              status: 'active',
            },
          ],
        },
      ],
      open_shifts: [],
      open_incidents: [],
    });
    getShift.mockResolvedValue(shift({ nozzle_assignments: [] }));
    renderPage();

    const panel = await screen.findByTestId('adhoc-assignment-panel');
    // Wait for the station overview to load the selectable nozzle options.
    await within(panel).findByRole('option', { name: /Pump 1 · Nozzle 2/ });
    await userEvent.selectOptions(within(panel).getByLabelText('Nozzle'), 'noz-1');
    await userEvent.selectOptions(within(panel).getByLabelText('Attendant'), 'u-att');
    await userEvent.click(within(panel).getByRole('button', { name: /^assign$/i }));

    await waitFor(() =>
      expect(assignNozzle).toHaveBeenCalledWith('sh-1', {
        nozzle_id: 'noz-1',
        attendant_id: 'u-att',
      }),
    );
    // u-att is already on the roster — no extra assignAttendant call.
    expect(assignAttendant).not.toHaveBeenCalled();
  });

  it('adds an off-roster substitute to the shift before assigning the nozzle', async () => {
    getShift.mockResolvedValue(shift({ nozzle_assignments: [] }));
    renderPage();

    const panel = await screen.findByTestId('adhoc-assignment-panel');
    // u-sup is a station employee but NOT on this shift's roster — a substitute.
    await within(panel).findByRole('option', { name: /Pump 1 · Nozzle 2/ });
    await userEvent.selectOptions(within(panel).getByLabelText('Nozzle'), 'noz-1');
    await userEvent.selectOptions(within(panel).getByLabelText('Attendant'), 'u-sup');
    await userEvent.click(within(panel).getByRole('button', { name: /^assign$/i }));

    await waitFor(() => expect(assignAttendant).toHaveBeenCalledWith('sh-1', 'u-sup'));
    await waitFor(() =>
      expect(assignNozzle).toHaveBeenCalledWith('sh-1', {
        nozzle_id: 'noz-1',
        attendant_id: 'u-sup',
      }),
    );
  });

  it('confirms before unassigning a nozzle, then calls unassignNozzle', async () => {
    renderPage();

    const panel = await screen.findByTestId('adhoc-assignment-panel');
    const row = await within(panel).findByTestId('adhoc-assignment-row');
    await userEvent.click(within(row).getByRole('button', { name: /unassign/i }));

    // A confirmation dialog appears before anything is called.
    expect(await screen.findByText('Unassign this nozzle?')).toBeInTheDocument();
    expect(unassignNozzle).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: /^unassign$/i }));
    await waitFor(() => expect(unassignNozzle).toHaveBeenCalledWith('sh-1', 'na-1'));
  });

  it('disables assignment controls without shift.assign', async () => {
    perms = {
      'reading.override': true,
      'cash.confirm': true,
      'shift.approve': true,
      'shift.assign': false,
    };
    renderPage();

    const panel = await screen.findByTestId('adhoc-assignment-panel');
    // The assign form is withheld entirely; the per-row unassign is rendered
    // but disabled (PermissionGate disable mode, matching the operations page).
    expect(within(panel).queryByLabelText('Nozzle')).not.toBeInTheDocument();
    expect(within(panel).getByRole('button', { name: /unassign/i })).toBeDisabled();
  });

  // --- collection receipt ---

  it('requires a reason only when the received total differs from expected', async () => {
    getShift.mockResolvedValue(closedShift());
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    renderPage();

    const panel = await screen.findByTestId('collection-receipt-panel');
    // Expected vs submitted with the tender breakdown + the attendant's note.
    expect(await within(panel).findByText('Expected collection')).toBeInTheDocument();
    expect(within(panel).getByText('Mobile money')).toBeInTheDocument();
    expect(within(panel).getByText(/drive-off at pump 1/)).toBeInTheDocument();

    const total = within(panel).getByLabelText('Received total');
    const submit = within(panel).getByRole('button', { name: /confirm receipt/i });

    // Balanced: no reason needed.
    await userEvent.type(total, '1475000.00');
    expect(submit).toBeEnabled();

    // Short: a reason becomes mandatory.
    await userEvent.clear(total);
    await userEvent.type(total, '1470000');
    expect(await within(panel).findByText(/a reason is required/i)).toBeInTheDocument();
    expect(submit).toBeDisabled();

    await userEvent.type(within(panel).getByLabelText('Receipt reason'), 'drive-off confirmed');
    expect(submit).toBeEnabled();
    await userEvent.click(submit);

    await waitFor(() =>
      expect(confirmCashSubmission).toHaveBeenCalledWith('sh-1', {
        received_total: '1470000',
        reason: 'drive-off confirmed',
      }),
    );
  });

  it('shows the recorded receipt status instead of the form', async () => {
    getShift.mockResolvedValue(closedShift());
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    getCollectionReceipt.mockResolvedValue(receipt());
    renderPage();

    const status = await screen.findByTestId('receipt-status');
    expect(within(status).getByText('Approved with difference')).toBeInTheDocument();
    expect(within(status).getByText(/drive-off confirmed/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Received total')).not.toBeInTheDocument();
  });

  it('shows a read-only waiting note without cash.confirm', async () => {
    perms = { 'reading.override': true, 'cash.confirm': false, 'shift.approve': true };
    getShift.mockResolvedValue(closedShift());
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    renderPage();

    expect(await screen.findByTestId('receipt-readonly')).toBeInTheDocument();
    expect(screen.queryByLabelText('Received total')).not.toBeInTheDocument();
  });

  // --- approval readiness ---

  it('renders the 409 gates as a human checklist', async () => {
    getShift.mockResolvedValue(closedShift());
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading(), reading({ id: 'mr-close-2', nozzle_id: 'noz-2' })],
      count: 3,
      dispensed: [],
    });
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    renderPage();

    const readiness = await screen.findByTestId('approval-readiness');
    expect(
      await within(readiness).findByText('2 readings awaiting verification'),
    ).toBeInTheDocument();
    expect(within(readiness).getByText('Collection not confirmed')).toBeInTheDocument();
    expect(within(readiness).getByText('No open exceptions')).toBeInTheDocument();
    expect(within(readiness).getByRole('button', { name: /approve shift/i })).toBeDisabled();
  });

  it('maps a 409 readings_unverified from approve onto a clear message', async () => {
    getShift.mockResolvedValue(closedShift());
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    listReadingVerifications.mockResolvedValue({ items: [verification()], count: 1 });
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    getCollectionReceipt.mockResolvedValue(receipt());
    // The server still refuses (e.g. a correction superseded a reading since
    // the page loaded) — the gate code must surface, not the raw error.
    approveShift.mockRejectedValue(
      new SdkError("verify the shift's closing readings before approving", 409, {
        error: "verify the shift's closing readings before approving",
        code: 'readings_unverified',
        status: 409,
        unverified_count: 1,
      }),
    );
    renderPage();

    const readiness = await screen.findByTestId('approval-readiness');
    const approve = await within(readiness).findByRole('button', { name: /approve shift/i });
    await waitFor(() => expect(approve).toBeEnabled());
    await userEvent.click(approve);

    expect(
      await screen.findByText(/1 closing reading is still awaiting verification/i),
    ).toBeInTheDocument();
  });

  it('maps a 409 collection_unconfirmed from approve onto a clear message', async () => {
    getShift.mockResolvedValue(closedShift());
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    listReadingVerifications.mockResolvedValue({ items: [verification()], count: 1 });
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    getCollectionReceipt.mockResolvedValue(receipt());
    approveShift.mockRejectedValue(
      new SdkError("confirm the shift's cash submission before approving", 409, {
        error: "confirm the shift's cash submission before approving",
        code: 'collection_unconfirmed',
        status: 409,
      }),
    );
    renderPage();

    const readiness = await screen.findByTestId('approval-readiness');
    const approve = await within(readiness).findByRole('button', { name: /approve shift/i });
    await waitFor(() => expect(approve).toBeEnabled());
    await userEvent.click(approve);

    expect(await screen.findByText(/collection has not been confirmed/i)).toBeInTheDocument();
  });

  it('approves when every gate is green', async () => {
    getShift.mockResolvedValue(closedShift());
    listMeterReadings.mockResolvedValue({
      items: [openingReading, reading()],
      count: 2,
      dispensed: [],
    });
    listReadingVerifications.mockResolvedValue({ items: [verification()], count: 1 });
    getCloseSummary.mockResolvedValue({
      shift: closedShift(),
      lines: [],
      expected_cash: '1475000.00',
      cash_submission: cashSubmission,
    });
    getCollectionReceipt.mockResolvedValue(receipt());
    renderPage();

    const readiness = await screen.findByTestId('approval-readiness');
    expect(await within(readiness).findByText('All closing readings verified')).toBeInTheDocument();
    expect(within(readiness).getByText('Collection receipt recorded')).toBeInTheDocument();
    const approve = within(readiness).getByRole('button', { name: /approve shift/i });
    await waitFor(() => expect(approve).toBeEnabled());
    await userEvent.click(approve);
    await waitFor(() => expect(approveShift).toHaveBeenCalledWith('sh-1'));
  });
});
