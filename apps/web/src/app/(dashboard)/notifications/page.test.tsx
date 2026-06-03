import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { Notification } from '@fuelgrid/sdk';

const listNotifications = vi.fn();
const markNotificationRead = vi.fn();
const markAllNotificationsRead = vi.fn();
vi.mock('@/lib/api', () => ({
  api: {
    listNotifications: (...args: unknown[]) => listNotifications(...args),
    markNotificationRead: (...args: unknown[]) => markNotificationRead(...args),
    markAllNotificationsRead: (...args: unknown[]) => markAllNotificationsRead(...args),
  },
}));

import NotificationsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <NotificationsPage />
    </QueryClientProvider>,
  );
}

const sample: Notification = {
  id: 'notif-1',
  type: 'incident.opened',
  title: 'Incident opened',
  body: 'An incident was opened — check the incidents queue.',
  severity: 'critical',
  related_entity_type: 'incident',
  related_entity_id: 'inc-1',
  created_at: '2026-02-01T10:00:00Z',
};

describe('NotificationsPage', () => {
  beforeEach(() => {
    listNotifications.mockReset();
    markNotificationRead.mockReset();
    markAllNotificationsRead.mockReset();
  });

  afterEach(() => vi.clearAllMocks());

  it('renders notifications from the API', async () => {
    listNotifications.mockResolvedValue({ items: [sample], count: 1, has_more: false });
    renderPage();

    expect(await screen.findByText('Incident opened')).toBeInTheDocument();
    expect(screen.getByText('incident.opened')).toBeInTheDocument();
    // Unread item exposes a mark-read action.
    expect(screen.getByRole('button', { name: /mark read/i })).toBeInTheDocument();
  });

  it('shows the empty state when there are no notifications', async () => {
    listNotifications.mockResolvedValue({ items: [], count: 0, has_more: false });
    renderPage();

    expect(await screen.findByText('No notifications')).toBeInTheDocument();
  });

  it('surfaces the error state when the feed fails to load', async () => {
    listNotifications.mockRejectedValue(new Error('boom'));
    renderPage();

    expect(await screen.findByText("Couldn't load notifications")).toBeInTheDocument();
  });
});
