import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { AuditLogEntry } from '@fuelgrid/sdk';

const listAuditLogs = vi.fn();
const exportAuditLogs = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listAuditLogs: (...args: unknown[]) => listAuditLogs(...args),
    exportAuditLogs: (...args: unknown[]) => exportAuditLogs(...args),
  },
}));

// triggerDownload touches URL.createObjectURL which jsdom lacks; stub it.
vi.mock('@/components/document-actions', () => ({
  triggerDownload: vi.fn(),
}));

let permitted: boolean | null = true;
vi.mock('@/hooks/use-permissions', () => ({
  usePermission: () => permitted,
}));

import AuditLogPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AuditLogPage />
    </QueryClientProvider>,
  );
}

const sampleEntry: AuditLogEntry = {
  id: 'log-1',
  actor_id: 'actor-1',
  action: 'expense.approved',
  entity_type: 'expense',
  entity_id: 'exp-1',
  occurred_at: '2026-02-01T10:00:00Z',
} as AuditLogEntry;

describe('AuditLogPage', () => {
  beforeEach(() => {
    permitted = true;
    listAuditLogs.mockReset();
    exportAuditLogs.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders audit entries from the API', async () => {
    listAuditLogs.mockResolvedValue({ items: [sampleEntry], count: 1, has_more: false });
    renderPage();

    expect(await screen.findByText('expense.approved')).toBeInTheDocument();
    expect(screen.getByText('exp-1')).toBeInTheDocument();
  });

  it('shows the empty state when no entries match', async () => {
    listAuditLogs.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No matching audit entries')).toBeInTheDocument();
  });

  it('shows a no-access state and gates export when the user lacks audit.read', async () => {
    permitted = false;
    listAuditLogs.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No access')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /export csv/i })).toBeDisabled();
  });
});
