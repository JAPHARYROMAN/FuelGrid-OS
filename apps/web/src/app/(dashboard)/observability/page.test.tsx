import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { JobRun } from '@fuelgrid/sdk';

const listJobRuns = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listJobRuns: (...args: unknown[]) => listJobRuns(...args),
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

describe('ObservabilityPage', () => {
  beforeEach(() => {
    permitted = true;
    listJobRuns.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders job runs from the API', async () => {
    listJobRuns.mockResolvedValue({ items: [sampleRun], count: 1 });
    renderPage();

    expect(await screen.findByText('risk.detection')).toBeInTheDocument();
    expect(screen.getByText('Healthy')).toBeInTheDocument();
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
