import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import {
  SdkError,
  type AttendantCurrentShift,
  type ExpectedOpeningReadingList,
} from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const listExpectedOpeningReadings = vi.fn();
const captureMeterReading = vi.fn();
const reportIncident = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    listExpectedOpeningReadings: (...args: unknown[]) => listExpectedOpeningReadings(...args),
    captureMeterReading: (...args: unknown[]) => captureMeterReading(...args),
    reportIncident: (...args: unknown[]) => reportIncident(...args),
  },
}));

let incidentsPermitted: boolean | null = false;
vi.mock('@/hooks/use-permissions', () => ({ usePermission: () => incidentsPermitted }));

const push = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ push }) }));

import { resetSyncEngineForTests } from '@/lib/offline';

import OpeningReadingsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <OpeningReadingsPage />
    </QueryClientProvider>,
  );
}

const snapshot: AttendantCurrentShift = {
  status: 'on_shift',
  next_action: 'verify_opening_readings',
  user_message: 'Verify the opening reading on each of your nozzles.',
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
    {
      assignment_id: 'as-2',
      nozzle_id: 'noz-2',
      pump_number: 1,
      nozzle_number: 2,
      product_name: 'Diesel',
      product_color: '#16a34a',
      meter_decimal_places: 2,
      assigned_at: '2026-06-11T05:05:00Z',
      confirmed_at: '2026-06-11T05:06:00Z',
    },
  ],
  readings: [],
  expected_openings_available: true,
};

const expectedList: ExpectedOpeningReadingList = {
  items: [
    {
      assignment_id: 'as-1',
      nozzle_id: 'noz-1',
      attendant_id: 'u-2',
      expected_opening_reading: '1500.250',
      source: 'verified',
    },
    {
      assignment_id: 'as-2',
      nozzle_id: 'noz-2',
      attendant_id: 'u-2',
      expected_opening_reading: '2000.000',
      source: 'verified',
    },
  ],
  count: 2,
};

// The Premium nozzle's input, prefilled with its expected reading.
async function premiumInput() {
  return screen.findByLabelText(/meter reading/i, { selector: '#opening-noz-1' });
}

describe('OpeningReadingsPage', () => {
  beforeEach(() => {
    attendantCurrentShift.mockReset();
    listExpectedOpeningReadings.mockReset();
    captureMeterReading.mockReset();
    reportIncident.mockReset();
    push.mockReset();
    resetSyncEngineForTests();
    localStorage.clear();
    incidentsPermitted = false;
    attendantCurrentShift.mockResolvedValue(snapshot);
    listExpectedOpeningReadings.mockResolvedValue(expectedList);
  });

  it('prefills each nozzle with its expected reading and shows Matched', async () => {
    renderPage();

    const input = await premiumInput();
    expect(input).toHaveValue('1500.250');
    expect(screen.getAllByText(/matched — same as the expected opening/i)).toHaveLength(2);
    expect(screen.getByText('0 of 2 nozzles verified', { exact: false })).toBeInTheDocument();
    // Save is enabled when every nozzle matches.
    expect(screen.getByRole('button', { name: /save opening readings/i })).toBeEnabled();
  });

  it('flags a HIGHER reading as a warning with the exact difference, still submittable', async () => {
    renderPage();

    const input = await premiumInput();
    await userEvent.clear(input);
    await userEvent.type(input, '1510.25');

    expect(screen.getByText(/higher than expected by 10/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /save opening readings/i })).toBeEnabled();
  });

  it('blocks a LOWER reading client-side with the supervisor message', async () => {
    renderPage();

    const input = await premiumInput();
    await userEvent.clear(input);
    await userEvent.type(input, '1499');

    expect(
      screen.getAllByText(/lower than the previous shift's approved closing/i).length,
    ).toBeGreaterThan(0);
    expect(screen.getByRole('button', { name: /save opening readings/i })).toBeDisabled();
    // No capture may be attempted while blocked.
    expect(captureMeterReading).not.toHaveBeenCalled();
  });

  it('rejects readings finer than the nozzle meter precision', async () => {
    renderPage();

    const input = await premiumInput();
    await userEvent.clear(input);
    await userEvent.type(input, '1500.255');

    expect(screen.getByText(/too many decimals/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /save opening readings/i })).toBeDisabled();
  });

  it('says "No previous reading" when the handover chain has no figure', async () => {
    listExpectedOpeningReadings.mockResolvedValue({
      items: [
        { assignment_id: 'as-1', nozzle_id: 'noz-1', attendant_id: 'u-2' },
        { assignment_id: 'as-2', nozzle_id: 'noz-2', attendant_id: 'u-2' },
      ],
      count: 2,
    } satisfies ExpectedOpeningReadingList);
    renderPage();

    expect(await screen.findAllByText('No previous reading')).toHaveLength(2);
    const input = await premiumInput();
    await userEvent.type(input, '100');
    expect(
      screen.getByText(/no previous reading for this nozzle — enter the meter as you see it/i),
    ).toBeInTheDocument();
  });

  it('confirms before submitting, captures every nozzle, and returns home', async () => {
    captureMeterReading.mockResolvedValue({ id: 'mr-1' });
    renderPage();

    await premiumInput();
    await userEvent.click(screen.getByRole('button', { name: /save opening readings/i }));
    // Confirmation step before anything is sent (PRD 15.3).
    expect(await screen.findByText('Confirm your readings')).toBeInTheDocument();
    expect(captureMeterReading).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: /confirm and save/i }));

    await waitFor(() => expect(captureMeterReading).toHaveBeenCalledTimes(2));
    expect(captureMeterReading).toHaveBeenCalledWith('shift-1', {
      nozzle_id: 'noz-1',
      reading_type: 'opening',
      reading: '1500.250',
    });
    expect(captureMeterReading).toHaveBeenCalledWith('shift-1', {
      nozzle_id: 'noz-2',
      reading_type: 'opening',
      reading: '2000.000',
    });
    await waitFor(() => expect(push).toHaveBeenCalledWith('/attendant'));
  });

  it('reports a partial failure honestly, per nozzle, and stays on the screen', async () => {
    captureMeterReading.mockImplementation((_shiftID: string, req: { nozzle_id: string }) =>
      req.nozzle_id === 'noz-1'
        ? Promise.resolve({ id: 'mr-1' })
        : Promise.reject(new SdkError('internal error', 500, { error: 'internal error' })),
    );
    renderPage();

    await premiumInput();
    await userEvent.click(screen.getByRole('button', { name: /save opening readings/i }));
    await userEvent.click(await screen.findByRole('button', { name: /confirm and save/i }));

    expect(await screen.findByText(/saved 1 of 2 readings/i)).toBeInTheDocument();
    expect(screen.getByText(/not saved: internal error/i)).toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
  });

  it("maps the server's 422 opening_below_expected onto the supervisor message", async () => {
    captureMeterReading.mockRejectedValue(
      new SdkError('opening reading is below the previous shift’s approved closing', 422, {
        code: 'opening_below_expected',
        expected_opening_reading: '1500.250',
      }),
    );
    renderPage();

    await premiumInput();
    await userEvent.click(screen.getByRole('button', { name: /save opening readings/i }));
    await userEvent.click(await screen.findByRole('button', { name: /confirm and save/i }));

    expect(await screen.findByText(/saved 0 of 2 readings/i)).toBeInTheDocument();
    expect(
      screen.getAllByText(/lower than the previous shift's approved closing/i).length,
    ).toBeGreaterThan(0);
  });

  it('shows the call-supervisor fallback when the attendant cannot file incidents', async () => {
    incidentsPermitted = false;
    renderPage();

    const input = await premiumInput();
    await userEvent.clear(input);
    await userEvent.type(input, '1');

    expect(await screen.findByText(/call your supervisor to resolve this/i)).toBeInTheDocument();
    expect(
      screen.queryByRole('button', { name: /report issue to supervisor/i }),
    ).not.toBeInTheDocument();
  });

  it('files a self-service incident one-tap when the actor holds incidents.report (no station_id, with dedupe_key)', async () => {
    incidentsPermitted = true;
    reportIncident.mockResolvedValue({ id: 'inc-1', dedupe_key: 'k1' });
    renderPage();

    const input = await premiumInput();
    await userEvent.clear(input);
    await userEvent.type(input, '1');

    await userEvent.click(screen.getByRole('button', { name: /report issue to supervisor/i }));

    await waitFor(() => expect(reportIncident).toHaveBeenCalledTimes(1));
    const sent = reportIncident.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(sent).toMatchObject({
      type: 'meter',
      severity: 'high',
      description: expect.stringContaining('pump 1 nozzle 1'),
    });
    expect(typeof sent.dedupe_key).toBe('string');
    // Station is derived server-side from the actor's current shift.
    expect(sent).not.toHaveProperty('station_id');
    expect(await screen.findByText(/issue reported/i)).toBeInTheDocument();
  });

  it('shows already-recorded nozzles as done and the all-done state when complete', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...snapshot,
      readings: [
        { nozzle_id: 'noz-1', opening_reading: '1500.250' },
        { nozzle_id: 'noz-2', opening_reading: '2000.000' },
      ],
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('All opening readings are recorded')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /back to my shift/i })).toHaveAttribute(
      'href',
      '/attendant',
    );
  });
});
