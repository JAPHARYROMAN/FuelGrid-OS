import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import * as React from 'react';

import { SdkError } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const checkInToShift = vi.fn();
const checkOutOfShift = vi.fn();
const confirmNozzleAssignment = vi.fn();
const captureMeterReading = vi.fn();
const submitCash = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    checkInToShift: (...args: unknown[]) => checkInToShift(...args),
    checkOutOfShift: (...args: unknown[]) => checkOutOfShift(...args),
    confirmNozzleAssignment: (...args: unknown[]) => confirmNozzleAssignment(...args),
    captureMeterReading: (...args: unknown[]) => captureMeterReading(...args),
    submitCash: (...args: unknown[]) => submitCash(...args),
  },
}));

import { resetSyncEngineForTests, type QueuedAction } from '@/lib/offline';

import { SyncStatusChip } from './sync-status';

const offlineError = () => new SdkError('network request failed: fetch failed', 0, null);

/** Seed the durable queue (localStorage fallback store in jsdom) directly. */
function seedQueue(rows: Array<Partial<QueuedAction>>) {
  const stamp = '2026-06-12T08:00:00.000Z';
  localStorage.setItem(
    'fg.attendant.offline-queue',
    JSON.stringify(
      rows.map((r, i) => ({
        local_action_id: `seed-${i}`,
        seq: i + 1,
        shift_id: 'shift-1',
        label: `Action ${i + 1}`,
        created_at_local: stamp,
        retry_count: 0,
        sync_status: 'pending',
        action_type: 'check_in',
        payload: {},
        ...r,
      })),
    ),
  );
}

describe('SyncStatusChip + sync details sheet', () => {
  beforeEach(() => {
    localStorage.clear();
    resetSyncEngineForTests();
    attendantCurrentShift.mockReset();
    checkInToShift.mockReset();
    checkOutOfShift.mockReset();
    confirmNozzleAssignment.mockReset();
    captureMeterReading.mockReset();
    submitCash.mockReset();
  });

  it('shows Online with an empty queue', async () => {
    render(<SyncStatusChip />);
    expect(await screen.findByText('Online')).toBeInTheDocument();
  });

  it('shows the offline count when replay cannot reach the server', async () => {
    seedQueue([{ action_type: 'check_in' }]);
    checkInToShift.mockRejectedValue(offlineError());
    render(<SyncStatusChip />);

    expect(await screen.findByText('Offline — 1 to sync')).toBeInTheDocument();
  });

  it('replays the queue on app open and lands on All changes synced', async () => {
    seedQueue([{ action_type: 'check_in' }, { action_type: 'check_out' }]);
    checkInToShift.mockResolvedValue({});
    checkOutOfShift.mockResolvedValue({});
    render(<SyncStatusChip />);

    expect(await screen.findByText('All changes synced')).toBeInTheDocument();
    expect(checkInToShift).toHaveBeenCalledTimes(1);
    expect(checkOutOfShift).toHaveBeenCalledTimes(1);
  });

  it('shows Sync failed when the server rejects an action', async () => {
    seedQueue([{ action_type: 'check_in' }]);
    checkInToShift.mockRejectedValue(new SdkError('shift is not open', 409, null));
    render(<SyncStatusChip />);

    expect(await screen.findByText('Sync failed')).toBeInTheDocument();
  });

  it('pauses with "Sign in to finish syncing" on a 401 — nothing discarded', async () => {
    seedQueue([{ action_type: 'check_in' }]);
    checkInToShift.mockRejectedValue(new SdkError('authentication required', 401, null));
    render(<SyncStatusChip />);

    expect(await screen.findByText('Sign in to finish syncing')).toBeInTheDocument();
    // The queued action survives the auth pause.
    expect(localStorage.getItem('fg.attendant.offline-queue')).toContain('seed-0');
  });

  it('surfaces a conflict: chip, sheet row, server message, and double-confirm discard', async () => {
    seedQueue([
      {
        action_type: 'closing_reading',
        payload: { nozzle_id: 'n1', reading: '1500.250' },
        label: 'Closing reading 1500.250 — pump 1 · nozzle 1',
        sync_status: 'conflict',
        error_message:
          'The server already has a different closing reading (1600.000) for this nozzle. Your figure is kept here — show it to your supervisor.',
        server_value: '1600.000',
      },
    ]);
    render(<SyncStatusChip />);

    // Chip state.
    expect(await screen.findByText('Needs attention')).toBeInTheDocument();

    // Open the sheet: the action, when, status, and the error are listed.
    await userEvent.click(screen.getByText('Needs attention'));
    expect(await screen.findByRole('dialog', { name: /sync details/i })).toBeInTheDocument();
    expect(screen.getByText('Closing reading 1500.250 — pump 1 · nozzle 1')).toBeInTheDocument();
    expect(screen.getByText('Needs supervisor attention')).toBeInTheDocument();
    expect(
      screen.getByText(/server already has a different closing reading \(1600\.000\)/i),
    ).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /try again/i })).toBeInTheDocument();

    // Discard requires an explicit second tap — the only data-drop path.
    await userEvent.click(screen.getByRole('button', { name: /^discard$/i }));
    expect(localStorage.getItem('fg.attendant.offline-queue')).toContain('1500.250');
    await userEvent.click(screen.getByRole('button', { name: /tap again to discard/i }));
    await waitFor(() =>
      expect(localStorage.getItem('fg.attendant.offline-queue')).not.toContain('1500.250'),
    );
    expect(await screen.findByText(/nothing waiting to sync/i)).toBeInTheDocument();
  });

  it('Try again re-arms a failed action and syncs it', async () => {
    seedQueue([
      {
        action_type: 'check_in',
        label: 'Check in',
        sync_status: 'failed',
        error_message: 'shift is not open',
      },
    ]);
    checkInToShift.mockResolvedValue({});
    render(<SyncStatusChip />);

    expect(await screen.findByText('Sync failed')).toBeInTheDocument();
    await userEvent.click(screen.getByText('Sync failed'));
    await userEvent.click(await screen.findByRole('button', { name: /try again/i }));

    expect(await screen.findByText('Synced')).toBeInTheDocument();
    expect(checkInToShift).toHaveBeenCalledTimes(1);
  });
});
