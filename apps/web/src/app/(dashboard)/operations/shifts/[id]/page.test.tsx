import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import { SdkError, type ShiftDetail } from '@fuelgrid/sdk';

const getShift = vi.fn();
const listMeterReadings = vi.fn();
const listAuditLogs = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    getShift: (...a: unknown[]) => getShift(...a),
    listMeterReadings: (...a: unknown[]) => listMeterReadings(...a),
    listAuditLogs: (...a: unknown[]) => listAuditLogs(...a),
  },
}));

let auditPermitted: boolean | null = false;
vi.mock('@/hooks/use-permissions', () => ({ usePermission: () => auditPermitted }));

vi.mock('next/navigation', () => ({ useParams: () => ({ id: 'sh-1' }) }));

import ShiftTimelinePage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ShiftTimelinePage />
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
    opened_by: 'u-1',
    opened_at: '2026-06-01T06:00:00Z',
    attendants: [],
    nozzle_assignments: [],
    ...overrides,
  } as ShiftDetail;
}

describe('ShiftTimelinePage', () => {
  beforeEach(() => {
    auditPermitted = false;
    getShift.mockResolvedValue(shift());
    listMeterReadings.mockResolvedValue({ items: [], count: 0, dispensed: [] });
    listAuditLogs.mockResolvedValue({ items: [], count: 0, has_more: false });
  });

  afterEach(() => vi.clearAllMocks());

  it('shows a loading state then the opened event', async () => {
    renderPage();
    expect(await screen.findByTestId('shift-timeline')).toBeInTheDocument();
    expect(screen.getByText('Shift opened')).toBeInTheDocument();
  });

  it('composes the full lifecycle from the shift fields', async () => {
    getShift.mockResolvedValue(
      shift({
        status: 'approved',
        closed_at: '2026-06-01T14:00:00Z',
        approved_at: '2026-06-01T15:00:00Z',
      }),
    );
    listMeterReadings.mockResolvedValue({
      items: [
        {
          id: 'mr-1',
          tenant_id: 't-1',
          shift_id: 'sh-1',
          nozzle_id: 'noz-1abcdef0',
          reading_type: 'opening',
          reading: '12345.000',
          recorded_by: 'u-1',
          recorded_at: '2026-06-01T06:30:00Z',
          status: 'active',
        },
      ],
      count: 1,
      dispensed: [],
    });
    renderPage();

    await screen.findByTestId('shift-timeline');
    expect(screen.getByText('Shift opened')).toBeInTheDocument();
    expect(screen.getByText('Opening meter reading')).toBeInTheDocument();
    expect(screen.getByText('Shift closed & submitted')).toBeInTheDocument();
    expect(screen.getByText('Shift approved')).toBeInTheDocument();
  });

  it('marks a rejected shift distinctly', async () => {
    getShift.mockResolvedValue(
      shift({
        status: 'rejected',
        closed_at: '2026-06-01T14:00:00Z',
        approved_at: '2026-06-01T15:00:00Z',
      }),
    );
    renderPage();
    await screen.findByTestId('shift-timeline');
    expect(screen.getByText('Shift rejected')).toBeInTheDocument();
  });

  it('does not query the audit trail without audit.read', async () => {
    renderPage();
    await screen.findByTestId('shift-timeline');
    expect(listAuditLogs).not.toHaveBeenCalled();
  });

  it('enriches the timeline from the audit trail when permitted', async () => {
    auditPermitted = true;
    listAuditLogs.mockResolvedValue({
      items: [
        {
          id: 'al-1',
          action: 'shift.nozzle_assigned',
          entity_type: 'shift',
          entity_id: 'sh-1',
          occurred_at: '2026-06-01T06:10:00Z',
        },
      ],
      count: 1,
      has_more: false,
    });
    renderPage();

    await screen.findByTestId('shift-timeline');
    await waitFor(() =>
      expect(listAuditLogs).toHaveBeenCalledWith(
        expect.objectContaining({ entityType: 'shift', entityID: 'sh-1' }),
        expect.anything(),
      ),
    );
    expect(await screen.findByText('Nozzle assigned')).toBeInTheDocument();
  });

  it('shows a not-found state for a missing shift', async () => {
    getShift.mockRejectedValue(new SdkError('not found', 404, { error: 'not found' }));
    renderPage();
    expect(await screen.findByText('Shift not found')).toBeInTheDocument();
  });
});
