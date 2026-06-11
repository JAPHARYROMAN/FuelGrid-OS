import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type AttendantCurrentShift } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const captureMeterReading = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    captureMeterReading: (...args: unknown[]) => captureMeterReading(...args),
  },
}));

const push = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ push }) }));

import ClosingReadingsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ClosingReadingsPage />
    </QueryClientProvider>,
  );
}

const snapshot: AttendantCurrentShift = {
  status: 'on_shift',
  next_action: 'working',
  user_message: 'You are set for this shift. Enter closing readings when your shift ends.',
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
      assigned_at: '2026-06-11T05:05:00Z',
      confirmed_at: '2026-06-11T05:06:00Z',
      meter_decimal_places: 2,
    },
  ],
  readings: [
    { nozzle_id: 'noz-1', opening_reading: '1500.250' },
    { nozzle_id: 'noz-2', opening_reading: '2000.000' },
  ],
  expected_openings_available: true,
};

async function premiumInput() {
  return screen.findByLabelText(/closing meter reading/i, { selector: '#closing-noz-1' });
}
async function dieselInput() {
  return screen.findByLabelText(/closing meter reading/i, { selector: '#closing-noz-2' });
}

describe('ClosingReadingsPage', () => {
  beforeEach(() => {
    attendantCurrentShift.mockReset();
    captureMeterReading.mockReset();
    push.mockReset();
    attendantCurrentShift.mockResolvedValue(snapshot);
  });

  it('shows the opening read-only and live-calculates litres sold EXACTLY (no float drift)', async () => {
    renderPage();

    const input = await premiumInput();
    expect(screen.getAllByText('Opening reading')).toHaveLength(2);
    // 1500.35 - 1500.250 = 0.1 — exact decimal-string math; parseFloat would
    // give 0.09999999999999964.
    await userEvent.type(input, '1500.35');
    expect(screen.getByText('Litres sold: 0.1 L')).toBeInTheDocument();

    await userEvent.clear(input);
    await userEvent.type(input, '1620.50');
    expect(screen.getByText('Litres sold: 120.25 L')).toBeInTheDocument();
  });

  it('blocks a closing LOWER than the opening with the exact PRD message', async () => {
    renderPage();

    const input = await premiumInput();
    await userEvent.type(input, '1499');

    expect(
      screen.getByText('Closing reading cannot be lower than opening reading.'),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /submit closing readings/i })).toBeDisabled();
    expect(captureMeterReading).not.toHaveBeenCalled();
  });

  it('rejects readings finer than the nozzle meter precision', async () => {
    renderPage();

    const input = await premiumInput();
    await userEvent.type(input, '1600.255');

    expect(screen.getByText(/too many decimals/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /submit closing readings/i })).toBeDisabled();
  });

  it('requires ALL assigned nozzles before anything can be submitted', async () => {
    renderPage();

    const input = await premiumInput();
    await userEvent.type(input, '1620.50');
    // Premium is valid but Diesel is still empty — submit stays disabled.
    expect(screen.getByText('Litres sold: 120.25 L')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /submit closing readings/i })).toBeDisabled();

    await userEvent.type(await dieselInput(), '2300');
    expect(screen.getByRole('button', { name: /submit closing readings/i })).toBeEnabled();
  });

  it('warns amber (still submittable) on an unusually high delta over the absolute threshold', async () => {
    renderPage();

    // 60,000 L on one nozzle crosses the 50,000 L absolute ceiling.
    await userEvent.type(await premiumInput(), '61500.25');
    await userEvent.type(await dieselInput(), '2300');

    expect(screen.getByText(/this looks unusually high/i)).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /submit closing readings/i })).toBeEnabled();
  });

  it('confirms with the reading count + exact total litres before capturing, then returns home', async () => {
    captureMeterReading.mockResolvedValue({ id: 'mr-1' });
    renderPage();

    await userEvent.type(await premiumInput(), '1620.50'); // 120.25 L
    await userEvent.type(await dieselInput(), '2300'); // 300 L
    await userEvent.click(screen.getByRole('button', { name: /submit closing readings/i }));

    // Confirmation step before anything is sent (PRD §15.3) — with the
    // exact decimal total: 120.25 + 300 = 420.25.
    expect(await screen.findByText(/you are submitting 2 readings totalling/i)).toBeInTheDocument();
    expect(screen.getByText('420.25')).toBeInTheDocument();
    expect(captureMeterReading).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: /confirm and submit/i }));

    await waitFor(() => expect(captureMeterReading).toHaveBeenCalledTimes(2));
    expect(captureMeterReading).toHaveBeenCalledWith('shift-1', {
      nozzle_id: 'noz-1',
      reading_type: 'closing',
      reading: '1620.50',
    });
    expect(captureMeterReading).toHaveBeenCalledWith('shift-1', {
      nozzle_id: 'noz-2',
      reading_type: 'closing',
      reading: '2300',
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

    await userEvent.type(await premiumInput(), '1620.50');
    await userEvent.type(await dieselInput(), '2300');
    await userEvent.click(screen.getByRole('button', { name: /submit closing readings/i }));
    await userEvent.click(await screen.findByRole('button', { name: /confirm and submit/i }));

    expect(await screen.findByText(/saved 1 of 2 readings/i)).toBeInTheDocument();
    expect(screen.getByText(/not saved: internal error/i)).toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
  });

  it("maps the server's 409 closing_already_submitted onto the lock message", async () => {
    captureMeterReading.mockRejectedValue(
      new SdkError('a closing reading was already submitted for this nozzle', 409, {
        code: 'closing_already_submitted',
      }),
    );
    renderPage();

    await userEvent.type(await premiumInput(), '1620.50');
    await userEvent.type(await dieselInput(), '2300');
    await userEvent.click(screen.getByRole('button', { name: /submit closing readings/i }));
    await userEvent.click(await screen.findByRole('button', { name: /confirm and submit/i }));

    expect(await screen.findByText(/saved 0 of 2 readings/i)).toBeInTheDocument();
    expect(
      screen.getAllByText(/already submitted for this nozzle — it is pending supervisor review/i)
        .length,
    ).toBeGreaterThan(0);
  });

  it('locks an already-submitted nozzle read-only with NO edit path', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...snapshot,
      next_action: 'submit_closing_readings',
      readings: [
        {
          nozzle_id: 'noz-1',
          opening_reading: '1500.250',
          closing_reading: '1620.500',
          verification_status: 'pending',
        },
        { nozzle_id: 'noz-2', opening_reading: '2000.000' },
      ],
    } satisfies AttendantCurrentShift);
    renderPage();

    // The submitted nozzle shows its figures and the lock badge, no input.
    expect(await screen.findByText('Submitted — pending supervisor review')).toBeInTheDocument();
    expect(screen.getByText('1620.500')).toBeInTheDocument();
    expect(screen.getByText('Litres sold')).toBeInTheDocument();
    expect(screen.getByText('120.25 L')).toBeInTheDocument();
    expect(document.querySelector('#closing-noz-1')).toBeNull();
    // The remaining nozzle is still capturable.
    expect(await dieselInput()).toBeInTheDocument();
  });

  it('shows the all-submitted state with the review-status path when every nozzle is done', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...snapshot,
      next_action: 'await_reading_verification',
      readings: [
        {
          nozzle_id: 'noz-1',
          opening_reading: '1500.250',
          closing_reading: '1620.500',
          verification_status: 'pending',
        },
        {
          nozzle_id: 'noz-2',
          opening_reading: '2000.000',
          closing_reading: '2300.000',
          verification_status: 'pending',
        },
      ],
    } satisfies AttendantCurrentShift);
    renderPage();

    expect(await screen.findByText('All closing readings are submitted')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /view review status/i })).toHaveAttribute(
      'href',
      '/attendant/review-status',
    );
  });
});
