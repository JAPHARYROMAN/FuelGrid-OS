import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type AttendantCurrentShift } from '@fuelgrid/sdk';

const attendantCurrentShift = vi.fn();
const reportIncident = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: (...args: unknown[]) => attendantCurrentShift(...args),
    reportIncident: (...args: unknown[]) => reportIncident(...args),
  },
}));

import { resetSyncEngineForTests } from '@/lib/offline';

import ReportIssuePage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ReportIssuePage />
    </QueryClientProvider>,
  );
}

const onShift: AttendantCurrentShift = {
  status: 'on_shift',
  next_action: 'working',
  user_message: 'Work the shift.',
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
  assignments: [],
  readings: [],
  expected_openings_available: true,
};

const offShift: AttendantCurrentShift = {
  status: 'off_duty',
  next_action: 'blocked',
  user_message: 'You are off duty.',
  attendance: { status: 'not_checked_in' },
  assignments: [],
  readings: [],
  expected_openings_available: false,
};

describe('ReportIssuePage', () => {
  beforeEach(() => {
    attendantCurrentShift.mockReset();
    reportIncident.mockReset();
    resetSyncEngineForTests();
    localStorage.clear();
    attendantCurrentShift.mockResolvedValue(onShift);
  });

  it('validates that a type and a description are required before confirming', async () => {
    renderPage();

    // No type, no description → submit shows validation, no confirm step.
    await userEvent.click(await screen.findByRole('button', { name: /send to supervisor/i }));
    expect(screen.getByText(/choose what the problem is first/i)).toBeInTheDocument();
    expect(screen.queryByText(/send this to your supervisor\?/i)).not.toBeInTheDocument();

    // Pick a type but still no description.
    await userEvent.click(screen.getByRole('button', { name: 'Pump' }));
    await userEvent.click(screen.getByRole('button', { name: /send to supervisor/i }));
    expect(screen.getByText(/add a short description before you send/i)).toBeInTheDocument();
    expect(reportIncident).not.toHaveBeenCalled();
  });

  it('confirms before sending, posts a dedupe_key and NO station_id, then shows the sent state', async () => {
    reportIncident.mockResolvedValue({ id: 'inc-1', dedupe_key: 'k1' });
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: 'Pump' }));
    await userEvent.type(
      screen.getByLabelText(/describe the problem/i),
      'Pump 1 will not dispense',
    );
    await userEvent.click(screen.getByRole('button', { name: /send to supervisor/i }));

    // Confirmation step before anything is sent (PRD 15.3).
    expect(await screen.findByText(/send this to your supervisor\?/i)).toBeInTheDocument();
    expect(reportIncident).not.toHaveBeenCalled();

    await userEvent.click(screen.getByRole('button', { name: /send now/i }));

    await waitFor(() => expect(reportIncident).toHaveBeenCalledTimes(1));
    const sent = reportIncident.mock.calls[0]?.[0] as Record<string, unknown>;
    expect(sent).toMatchObject({
      type: 'pump',
      description: 'Pump 1 will not dispense',
      severity: 'medium',
    });
    expect(typeof sent.dedupe_key).toBe('string');
    expect((sent.dedupe_key as string).length).toBeGreaterThan(0);
    // The station is derived server-side — never asserted by the client.
    expect(sent).not.toHaveProperty('station_id');

    expect(await screen.findByText('Sent to your supervisor')).toBeInTheDocument();
  });

  it('queues the report on this phone when the network is down, reusing the dedupe_key', async () => {
    reportIncident.mockRejectedValue(new SdkError('network request failed: fetch failed', 0, null));
    renderPage();

    await userEvent.click(await screen.findByRole('button', { name: 'Safety' }));
    await userEvent.type(screen.getByLabelText(/describe the problem/i), 'Fuel spill at pump 2');
    await userEvent.click(screen.getByRole('button', { name: /send to supervisor/i }));
    await userEvent.click(await screen.findByRole('button', { name: /send now/i }));

    expect(await screen.findByText('Saved on this phone')).toBeInTheDocument();

    // The queued record carries the type + the dedupe_key (no station_id).
    const stored = localStorage.getItem('fg.attendant.offline-queue') ?? '';
    expect(stored).toContain('"action_type":"report_issue"');
    expect(stored).toContain('"type":"safety"');
    expect(stored).toContain('"dedupe_key"');
    expect(stored).not.toContain('station_id');
  });

  it('shows the no-shift state when the attendant is off duty (report-issue is shift-linked)', async () => {
    attendantCurrentShift.mockResolvedValue(offShift);
    renderPage();
    expect(await screen.findByText('You are not on a shift')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Pump' })).not.toBeInTheDocument();
  });
});
