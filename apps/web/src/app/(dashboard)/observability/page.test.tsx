import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { JobRun, ObservabilityHealth } from '@fuelgrid/sdk';

const listJobRuns = vi.fn();
const getObservabilityHealth = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listJobRuns: (...args: unknown[]) => listJobRuns(...args),
    getObservabilityHealth: (...args: unknown[]) => getObservabilityHealth(...args),
  },
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import ObservabilityPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ObservabilityPage />
    </QueryClientProvider>,
  );
}

const sampleRun: JobRun = {
  id: 'run-1',
  job_name: 'risk.detection',
  started_at: '2026-02-01T10:00:00Z',
  finished_at: '2026-02-01T10:00:02Z',
  status: 'success',
  duration_ms: 2000,
};

const sampleHealth: ObservabilityHealth = {
  healthy: true,
  checks: { postgres: 'ok', redis: 'ok' },
  outbox: { backlog: 3, dead_letter: 0 },
  scheduler_last_run: {
    job_name: 'risk.detection',
    status: 'success',
    started_at: '2026-02-01T10:00:00Z',
    finished_at: '2026-02-01T10:00:02Z',
    duration_ms: 2000,
  },
};

describe('ObservabilityPage', () => {
  beforeEach(() => {
    permitted = true;
    listJobRuns.mockReset();
    getObservabilityHealth.mockReset();
    getObservabilityHealth.mockResolvedValue(sampleHealth);
  });

  afterEach(() => vi.clearAllMocks());

  it('renders job runs from the API', async () => {
    listJobRuns.mockResolvedValue({ items: [sampleRun], count: 1 });
    renderPage();

    // risk.detection appears in both the scheduler-last-run row (health) and the
    // jobs table, so assert at least one rendering.
    const matches = await screen.findAllByText('risk.detection');
    expect(matches.length).toBeGreaterThan(0);
  });

  it('renders the health snapshot — dependencies and outbox counts', async () => {
    listJobRuns.mockResolvedValue({ items: [sampleRun], count: 1 });
    renderPage();

    // Outbox backlog stat from the health endpoint.
    expect(await screen.findByText('Outbox backlog')).toBeInTheDocument();
    expect(screen.getByText('Dead-letter')).toBeInTheDocument();
    // Dependency rows.
    expect(screen.getByText('postgres')).toBeInTheDocument();
    expect(screen.getByText('redis')).toBeInTheDocument();
  });

  it('shows the empty state when no job runs are recorded', async () => {
    listJobRuns.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    expect(await screen.findByText('No job runs recorded yet')).toBeInTheDocument();
  });

  it('shows a no-access state when the user lacks audit.read', async () => {
    permitted = false;
    listJobRuns.mockResolvedValue({ items: [], count: 0 });
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
  });
});
