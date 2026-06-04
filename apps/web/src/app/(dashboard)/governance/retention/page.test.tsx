import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import {
  SdkError,
  type ClosedPeriodChangeRequest,
  type JobRun,
  type RetentionPolicy,
} from '@fuelgrid/sdk';

const listRetentionPolicies = vi.fn();
const listRetentionJobRuns = vi.fn();
const listClosedPeriodChangeRequests = vi.fn();
const listAccountingPeriods = vi.fn();

vi.mock('@/lib/api', () => ({
  api: {
    listRetentionPolicies: (...a: unknown[]) => listRetentionPolicies(...a),
    listRetentionJobRuns: (...a: unknown[]) => listRetentionJobRuns(...a),
    listClosedPeriodChangeRequests: (...a: unknown[]) => listClosedPeriodChangeRequests(...a),
    listAccountingPeriods: (...a: unknown[]) => listAccountingPeriods(...a),
    createRetentionPolicy: vi.fn(),
    updateRetentionPolicy: vi.fn(),
    deleteRetentionPolicy: vi.fn(),
    requestClosedPeriodChange: vi.fn(),
    approveClosedPeriodChange: vi.fn(),
    rejectClosedPeriodChange: vi.fn(),
  },
}));

// usePermission returns this value; null mimics the still-loading state.
let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import RetentionGovernancePage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <RetentionGovernancePage />
    </QueryClientProvider>,
  );
}

const policy: RetentionPolicy = {
  id: 'pol-1',
  scope: 'audit',
  retention_days: 365,
  status: 'active',
  created_at: '2026-01-01T00:00:00Z',
  updated_at: '2026-01-01T00:00:00Z',
};

const jobRun: JobRun = {
  id: 'run-1',
  job_name: 'retention_sweep',
  started_at: '2026-02-01T03:00:00Z',
  finished_at: '2026-02-01T03:00:01Z',
  status: 'success',
  detail: 'policies=1 audit_policies=1 audit_purge_candidates=12 purged=0 (dry-run)',
};

const changeRequest: ClosedPeriodChangeRequest = {
  id: 'cr-1',
  period_id: 'per-1',
  change_type: 'reopen',
  reason: 'month-end correction discovered',
  status: 'requested',
  requested_by: 'user-1',
  requested_at: '2026-02-02T08:00:00Z',
};

describe('RetentionGovernancePage', () => {
  beforeEach(() => {
    permitted = true;
    listRetentionPolicies.mockReset();
    listRetentionJobRuns.mockReset();
    listClosedPeriodChangeRequests.mockReset();
    listAccountingPeriods.mockReset();
    listAccountingPeriods.mockResolvedValue({ items: [], count: 0, has_more: false });
  });

  afterEach(() => vi.clearAllMocks());

  it('renders policies, the sweep run, and the change-request queue', async () => {
    listRetentionPolicies.mockResolvedValue({ items: [policy], count: 1 });
    listRetentionJobRuns.mockResolvedValue({ items: [jobRun], count: 1 });
    listClosedPeriodChangeRequests.mockResolvedValue({
      items: [changeRequest],
      count: 1,
      has_more: false,
    });
    renderPage();

    // Policy row.
    expect(await screen.findByText('Audit logs')).toBeInTheDocument();
    expect(screen.getByText('365')).toBeInTheDocument();
    // Sweep run detail surfaces the dry-run candidate count.
    expect(await screen.findByText(/audit_purge_candidates=12/)).toBeInTheDocument();
    // Change-request row + a live approve control (with permission).
    expect(await screen.findByText('month-end correction discovered')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeEnabled();
  });

  it('shows empty states when there is no data', async () => {
    listRetentionPolicies.mockResolvedValue({ items: [], count: 0 });
    listRetentionJobRuns.mockResolvedValue({ items: [], count: 0 });
    listClosedPeriodChangeRequests.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No retention policies')).toBeInTheDocument();
    expect(await screen.findByText('No sweep runs yet')).toBeInTheDocument();
    expect(await screen.findByText('No change requests')).toBeInTheDocument();
  });

  it('disables approve/reject controls when the user lacks the permission', async () => {
    permitted = false;
    listRetentionPolicies.mockResolvedValue({ items: [policy], count: 1 });
    listRetentionJobRuns.mockResolvedValue({ items: [], count: 0 });
    listClosedPeriodChangeRequests.mockResolvedValue({
      items: [changeRequest],
      count: 1,
      has_more: false,
    });
    renderPage();

    await screen.findByText('month-end correction discovered');
    expect(screen.getByRole('button', { name: /^approve$/i })).toBeDisabled();
    expect(screen.getByRole('button', { name: /^reject$/i })).toBeDisabled();
  });

  it('shows a no-access error when the policy list 403s', async () => {
    listRetentionPolicies.mockRejectedValue(new SdkError('forbidden', 403, { error: 'forbidden' }));
    listRetentionJobRuns.mockResolvedValue({ items: [], count: 0 });
    listClosedPeriodChangeRequests.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
