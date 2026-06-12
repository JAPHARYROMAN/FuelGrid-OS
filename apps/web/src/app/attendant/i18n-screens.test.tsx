import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { AttendantCurrentShift } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const listExpectedOpeningReadings = vi.fn();
const captureMeterReading = vi.fn();
const checkInToShift = vi.fn();
const checkOutOfShift = vi.fn();
const confirmNozzleAssignment = vi.fn();
const submitCash = vi.fn();
const createIncident = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    listExpectedOpeningReadings: (...args: unknown[]) => listExpectedOpeningReadings(...args),
    captureMeterReading: (...args: unknown[]) => captureMeterReading(...args),
    checkInToShift: (...args: unknown[]) => checkInToShift(...args),
    checkOutOfShift: (...args: unknown[]) => checkOutOfShift(...args),
    confirmNozzleAssignment: (...args: unknown[]) => confirmNozzleAssignment(...args),
    submitCash: (...args: unknown[]) => submitCash(...args),
    createIncident: (...args: unknown[]) => createIncident(...args),
  },
}));

vi.mock('@/hooks/use-permissions', () => ({ usePermission: () => false }));

const push = vi.fn();
vi.mock('next/navigation', () => ({ useRouter: () => ({ push }) }));

import { AttendantPrefsProvider, LOCALE_STORAGE_KEY } from '@/lib/i18n';
import { resetSyncEngineForTests } from '@/lib/offline';

import AttendantHomePage from './page';
import ClosingReadingsPage from './closing-readings/page';
import CollectionsPage from './collections/page';
import OpeningReadingsPage from './opening-readings/page';
import ReviewStatusPage from './review-status/page';
import ShiftCompletePage from './shift-complete/page';
import { SyncStatusChip } from './sync-status';

/**
 * Language-switch coverage per screen (Phase 6b): with the persisted locale
 * set to Swahili, each key attendant screen renders its critical workflow
 * strings in Swahili — and the same fixtures render English by default.
 * Status text+colour pairing is asserted where the PRD requires it.
 */

function renderSw(ui: React.ReactElement) {
  localStorage.setItem(LOCALE_STORAGE_KEY, 'sw');
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AttendantPrefsProvider>{ui}</AttendantPrefsProvider>
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
  readings: [],
  expected_openings_available: false,
};

beforeEach(() => {
  localStorage.clear();
  resetSyncEngineForTests();
  attendantCurrentShift.mockReset();
  listExpectedOpeningReadings.mockReset();
  captureMeterReading.mockReset();
  submitCash.mockReset();
});

describe('attendant screens render Swahili when the locale is sw', () => {
  it('home: CTA, checklist, and nozzle card strings flip to Swahili', async () => {
    attendantCurrentShift.mockResolvedValue({ ...baseSnapshot, next_action: 'check_in' });
    renderSw(<AttendantHomePage />);

    expect(await screen.findByRole('button', { name: 'Ingia kazini' })).toBeInTheDocument();
    expect(screen.getByText('Maendeleo ya zamu')).toBeInTheDocument();
    expect(screen.getByText('Nozeli zangu')).toBeInTheDocument();
    expect(screen.getByText('Zamu ya asubuhi', { exact: false })).toBeInTheDocument();
    expect(screen.getByText('Pampu 1 · Nozeli 1')).toBeInTheDocument();
    // Status badge keeps text + colour pairing (never colour-only).
    expect(screen.getByText('Zamu imefunguliwa')).toBeInTheDocument();
  });

  it('opening readings: title, expected row, and save button in Swahili', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'verify_opening_readings',
    });
    listExpectedOpeningReadings.mockResolvedValue({
      items: [{ assignment_id: 'as-1', nozzle_id: 'noz-1', expected_opening_reading: '1000.00' }],
    });
    renderSw(<OpeningReadingsPage />);

    expect(await screen.findByText('Usomaji wa kufungua')).toBeInTheDocument();
    expect(screen.getByText('Usomaji unaotarajiwa')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Hifadhi usomaji wa kufungua' })).toBeInTheDocument();
    // The prefilled match status carries meaning as TEXT with the success tone.
    const matched = screen.getByText('Umelingana — sawa na usomaji unaotarajiwa.');
    expect(matched.className).toContain('text-success');
  });

  it('closing readings: title, opening row, and submit button in Swahili', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'submit_closing_readings',
      readings: [{ nozzle_id: 'noz-1', opening_reading: '1000.00' }],
    });
    renderSw(<ClosingReadingsPage />);

    expect(await screen.findByText('Usomaji wa kufunga', { selector: 'h1' })).toBeInTheDocument();
    expect(
      screen.getByRole('button', { name: 'Wasilisha usomaji wa kufunga' }),
    ).toBeInTheDocument();
  });

  it('collections: form, tenders, and shortage status in Swahili (text + colour)', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'submit_collections',
      shift: { ...baseSnapshot.shift!, status: 'closed' },
      expected_cash: '100000.00',
      close_lines: [],
    });
    renderSw(<CollectionsPage />);

    expect(await screen.findByText('Wasilisha makusanyo yako')).toBeInTheDocument();
    expect(screen.getByText('Taslimu')).toBeInTheDocument();
    expect(screen.getByText('Pesa za simu')).toBeInTheDocument();
    expect(screen.getByText('Jumla inayotarajiwa')).toBeInTheDocument();

    // Enter less than expected: the shortage line must SAY it (Upungufu) and
    // pair the text with the danger tone — never colour alone (PRD §15.1).
    await userEvent.type(screen.getByLabelText('Taslimu'), '60000');
    const shortage = await screen.findByText(/Upungufu wa/);
    expect(shortage.className).toContain('text-danger');
    expect(screen.getByText('Sababu ya tofauti (inahitajika)')).toBeInTheDocument();
  });

  it('review status: title and progress line in Swahili', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      next_action: 'await_reading_verification',
      readings: [
        {
          nozzle_id: 'noz-1',
          opening_reading: '1000.00',
          closing_reading: '1500.00',
          verification_status: 'pending',
        },
      ],
    });
    renderSw(<ReviewStatusPage />);

    expect(await screen.findByText('Hali ya uhakiki wa usomaji')).toBeInTheDocument();
    expect(
      screen.getByText('Usomaji 0 kati ya 1 umehakikiwa na msimamizi wako.'),
    ).toBeInTheDocument();
    expect(screen.getByText('Unasubiri uhakiki wa msimamizi')).toBeInTheDocument();
  });

  it('shift complete: banner and check-out in Swahili', async () => {
    attendantCurrentShift.mockResolvedValue({
      ...baseSnapshot,
      status: 'complete',
      next_action: 'complete',
      user_message: 'Shift complete. Well done.',
      shift: { ...baseSnapshot.shift!, status: 'approved' },
      expected_cash: '100000.00',
    });
    renderSw(<ShiftCompletePage />);

    expect(await screen.findByText('Zamu imekamilika — hongera!')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Toka kazini' })).toBeInTheDocument();
    expect(screen.getByText('Angalia maelezo ya makusanyo')).toBeInTheDocument();
  });

  it('sync chip + queue rows render translated labels derived from payload codes', async () => {
    // A queue record persisted WITHOUT display prose: label must derive from
    // action_type + payload; the conflict message from its stored CODE.
    localStorage.setItem(
      'fg.attendant.offline-queue',
      JSON.stringify([
        {
          local_action_id: 'row-1',
          seq: 1,
          shift_id: 'shift-1',
          created_at_local: '2026-06-12T08:00:00.000Z',
          retry_count: 0,
          sync_status: 'conflict',
          action_type: 'closing_reading',
          payload: { nozzle_id: 'n1', reading: '1500.250', pump_number: 1, nozzle_number: 2 },
          error_message:
            'The server already has a different closing reading (1600.000) for this nozzle. Your figure is kept here — show it to your supervisor.',
          error_code: 'reading_conflict',
          error_params: { reading_type: 'closing', server_value: '1600.000' },
          server_value: '1600.000',
        },
      ]),
    );
    renderSw(<SyncStatusChip />);

    expect(await screen.findByText('Inahitaji kuangaliwa')).toBeInTheDocument();
    await userEvent.click(screen.getByText('Inahitaji kuangaliwa'));

    // Derived, translated row label (no stored prose involved).
    expect(
      await screen.findByText('Usomaji wa kufunga 1500.250 — pampu 1 · nozeli 2'),
    ).toBeInTheDocument();
    // Coded conflict message rendered in Swahili from error_code + params.
    expect(
      screen.getByText(/Seva tayari ina usomaji tofauti wa kufunga \(1600\.000\)/),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Jaribu tena' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Futa' })).toBeInTheDocument();
  });
});

describe('default locale stays English', () => {
  it('home renders English with no stored locale (existing-suite contract)', async () => {
    attendantCurrentShift.mockResolvedValue(baseSnapshot);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    render(
      <QueryClientProvider client={qc}>
        <AttendantPrefsProvider>
          <AttendantHomePage />
        </AttendantPrefsProvider>
      </QueryClientProvider>,
    );
    expect(await screen.findByRole('button', { name: 'Check in' })).toBeInTheDocument();
    expect(screen.getByText('Shift progress')).toBeInTheDocument();
  });
});
