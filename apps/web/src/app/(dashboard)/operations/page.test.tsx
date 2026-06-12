import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type OperationsOverview, type OperationsShift } from '@fuelgrid/sdk';

const listStations = vi.fn();
const getOperationsOverview = vi.fn();
const getStationOverview = vi.fn();
const getScheduledTeam = vi.fn();
const openShift = vi.fn();
const approveShift = vi.fn();
const resolveShiftException = vi.fn();
const closeShift = vi.fn();
const openOperatingDay = vi.fn();
const updateOperatingDayStatus = vi.fn();
const lockOperatingDay = vi.fn();
const assignNozzle = vi.fn();
const unassignNozzle = vi.fn();
const listMeterReadings = vi.fn();
const listDipReadings = vi.fn();
const captureMeterReading = vi.fn();
const captureDipReading = vi.fn();
const submitCash = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listStations: (...a: unknown[]) => listStations(...a),
    getOperationsOverview: (...a: unknown[]) => getOperationsOverview(...a),
    getStationOverview: (...a: unknown[]) => getStationOverview(...a),
    getScheduledTeam: (...a: unknown[]) => getScheduledTeam(...a),
    openShift: (...a: unknown[]) => openShift(...a),
    approveShift: (...a: unknown[]) => approveShift(...a),
    resolveShiftException: (...a: unknown[]) => resolveShiftException(...a),
    closeShift: (...a: unknown[]) => closeShift(...a),
    openOperatingDay: (...a: unknown[]) => openOperatingDay(...a),
    updateOperatingDayStatus: (...a: unknown[]) => updateOperatingDayStatus(...a),
    lockOperatingDay: (...a: unknown[]) => lockOperatingDay(...a),
    assignNozzle: (...a: unknown[]) => assignNozzle(...a),
    unassignNozzle: (...a: unknown[]) => unassignNozzle(...a),
    listMeterReadings: (...a: unknown[]) => listMeterReadings(...a),
    listDipReadings: (...a: unknown[]) => listDipReadings(...a),
    captureMeterReading: (...a: unknown[]) => captureMeterReading(...a),
    captureDipReading: (...a: unknown[]) => captureDipReading(...a),
    submitCash: (...a: unknown[]) => submitCash(...a),
  },
}));

// Permission map per test: usePermission(code) → perms[code] ?? false. The
// real PermissionGate runs against this mock.
let perms: Record<string, boolean> = {};
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: (code: string) => perms[code] ?? false,
}));

import OperationsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OperationsPage />
    </QueryClientProvider>,
  );
}

const station = { id: 'st-1', name: 'Mikocheni', code: 'MKC' };

function closedShift(overrides: Partial<OperationsShift> = {}): OperationsShift {
  return {
    id: 'sh-prior',
    tenant_id: 't-1',
    station_id: 'st-1',
    operating_day_id: 'day-1',
    name: 'Morning',
    status: 'closed',
    opened_by: 'u-sup',
    opened_at: '2026-06-01T06:00:00Z',
    closed_at: '2026-06-01T14:00:00Z',
    attendants: [],
    nozzle_assignments: [],
    expected_cash: '1475000.00',
    litres_sold: '500.000',
    cash_submission: {
      id: 'cs-1',
      shift_id: 'sh-prior',
      expected_cash: '1475000.00',
      cash_amount: '1475000.00',
      mobile_money_amount: '0.00',
      card_amount: '0.00',
      credit_amount: '0.00',
      submitted_total: '1475000.00',
      variance: '0.00',
      submitted_by: 'u-att',
      submitted_at: '2026-06-01T14:10:00Z',
    },
    exceptions: [],
    open_exception_count: 0,
    ...overrides,
  } as OperationsShift;
}

function overview(overrides: Partial<OperationsOverview> = {}): OperationsOverview {
  return {
    station,
    day: {
      id: 'day-1',
      tenant_id: 't-1',
      station_id: 'st-1',
      business_date: '2026-06-01',
      status: 'open',
      opened_by: 'u-sup',
      opened_at: '2026-06-01T05:30:00Z',
    },
    shifts: [closedShift()],
    ...overrides,
  } as OperationsOverview;
}

describe('OperationsPage — Phase 5 supervisor gates', () => {
  beforeEach(() => {
    perms = {
      'operations.manage_day': true,
      'shift.open': true,
      'shift.close': true,
      'shift.approve': true,
      'shift.assign': true,
      'reading.override': true,
      'cash.override': true,
    };
    listStations.mockResolvedValue({ items: [station], count: 1, has_more: false });
    getOperationsOverview.mockResolvedValue(overview());
    getStationOverview.mockResolvedValue({
      station,
      tanks: [],
      pumps: [],
      open_shifts: [],
      open_incidents: [],
    });
    getScheduledTeam.mockResolvedValue({
      team: { id: 'team-1', name: 'Team A' },
      members: [{ id: 'm1', full_name: 'Asha Attendant' }],
    });
    listMeterReadings.mockResolvedValue({ items: [], count: 0, dispensed: [] });
    listDipReadings.mockResolvedValue({ items: [], count: 0 });
    openShift.mockResolvedValue({ id: 'sh-new', status: 'open' });
    approveShift.mockResolvedValue(closedShift({ status: 'approved' }));
  });

  afterEach(() => vi.clearAllMocks());

  async function typeShiftNameAndOpen() {
    await userEvent.type(await screen.findByPlaceholderText(/new shift name/i), 'Evening');
    const open = screen.getByRole('button', { name: /open shift/i });
    await waitFor(() => expect(open).toBeEnabled());
    await userEvent.click(open);
  }

  it('maps the approve 409 readings_unverified gate onto a clear message', async () => {
    approveShift.mockRejectedValue(
      new SdkError("verify the shift's closing readings before approving", 409, {
        error: "verify the shift's closing readings before approving",
        code: 'readings_unverified',
        status: 409,
        unverified_count: 2,
      }),
    );
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /approve shift/i }));
    expect(
      await screen.findByText(
        /2 closing readings are awaiting verification — open the shift review/i,
      ),
    ).toBeInTheDocument();
  });

  it('maps the approve 409 collection_unconfirmed gate onto a clear message', async () => {
    approveShift.mockRejectedValue(
      new SdkError("confirm the shift's cash submission before approving", 409, {
        error: "confirm the shift's cash submission before approving",
        code: 'collection_unconfirmed',
        status: 409,
      }),
    );
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: /approve shift/i }));
    expect(
      await screen.findByText(/cash handover has not been confirmed — record the collection/i),
    ).toBeInTheDocument();
  });

  it('renders the prior_shift_unapproved block naming the blocking shift, with the override path', async () => {
    openShift.mockRejectedValue(
      new SdkError('approve the station&apos;s closed shifts before opening a new one', 409, {
        error: 'approve the station’s closed shifts before opening a new one',
        code: 'prior_shift_unapproved',
        status: 409,
        unapproved_shift_ids: ['sh-prior'],
      }),
    );
    renderPage();
    await typeShiftNameAndOpen();

    const block = await screen.findByTestId('handover-block');
    expect(within(block).getByText(/shift handover incomplete/i)).toBeInTheDocument();
    // Names the blocking shift and links to its review page.
    const link = within(block).getByRole('link', { name: 'Morning' });
    expect(link).toHaveAttribute('href', '/operations/shifts/sh-prior');

    // The override path mirrors the server contract: reason mandatory.
    const overrideButton = within(block).getByRole('button', { name: /override and open/i });
    expect(overrideButton).toBeDisabled();
    await userEvent.type(
      within(block).getByLabelText('Handover override reason'),
      'outgoing supervisor unreachable',
    );
    expect(overrideButton).toBeEnabled();

    openShift.mockResolvedValue({ id: 'sh-new', status: 'open' });
    await userEvent.click(overrideButton);
    await waitFor(() =>
      expect(openShift).toHaveBeenLastCalledWith('st-1', {
        operating_day_id: 'day-1',
        name: 'Evening',
        slot: 'morning',
        handover_override_reason: 'outgoing supervisor unreachable',
      }),
    );
  });

  it('hides the override path from users without shift.approve', async () => {
    perms = { ...perms, 'shift.approve': false };
    openShift.mockRejectedValue(
      new SdkError('approve the station closed shifts before opening a new one', 409, {
        error: 'approve the station closed shifts before opening a new one',
        code: 'prior_shift_unapproved',
        status: 409,
        unapproved_shift_ids: ['sh-prior'],
      }),
    );
    renderPage();
    await typeShiftNameAndOpen();

    const block = await screen.findByTestId('handover-block');
    expect(
      within(block).queryByRole('button', { name: /override and open/i }),
    ).not.toBeInTheDocument();
    expect(within(block).queryByLabelText('Handover override reason')).not.toBeInTheDocument();
    expect(within(block).getByText(/requires the shift.approve permission/i)).toBeInTheDocument();
  });
});
