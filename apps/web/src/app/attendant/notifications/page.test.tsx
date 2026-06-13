import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { QueryClient, QueryClientProvider } from '@tanstack/react-query';
import * as React from 'react';

import type { Notification, Paginated } from '@fuelgrid/sdk';

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

import AttendantNotificationsPage from './page';

function renderPage() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <AttendantNotificationsPage />
    </QueryClientProvider>,
  );
}

function page(items: Notification[]): Paginated<Notification> {
  return { items, count: items.length, limit: 30, offset: 0, has_more: false };
}

const unread: Notification = {
  id: 'n-1',
  type: 'assignment.created',
  title: 'You were assigned pump 1 nozzle 1',
  body: 'Your supervisor assigned you to a nozzle.',
  severity: 'info',
  created_at: '2026-06-13T05:00:00Z',
};
const readOne: Notification = {
  id: 'n-2',
  type: 'reading.rejected',
  title: 'Closing reading rejected',
  body: 'Your supervisor rejected a closing reading.',
  severity: 'critical',
  created_at: '2026-06-13T04:00:00Z',
  read_at: '2026-06-13T04:30:00Z',
};

describe('AttendantNotificationsPage', () => {
  beforeEach(() => {
    listNotifications.mockReset();
    markNotificationRead.mockReset();
    markAllNotificationsRead.mockReset();
    markNotificationRead.mockResolvedValue(undefined);
    markAllNotificationsRead.mockResolvedValue({ marked_read: 1 });
  });

  it('renders the empty state when there are no notifications', async () => {
    listNotifications.mockResolvedValue(page([]));
    renderPage();
    expect(await screen.findByText('No notifications yet')).toBeInTheDocument();
  });

  it('shows the server title/body VERBATIM and a translated severity badge', async () => {
    listNotifications.mockResolvedValue(page([unread, readOne]));
    renderPage();

    // Server prose is rendered as-is (never fake-translated).
    expect(await screen.findByText('You were assigned pump 1 nozzle 1')).toBeInTheDocument();
    expect(screen.getByText('Your supervisor assigned you to a nozzle.')).toBeInTheDocument();
    // Severity badge labels are the i18n chrome (text, not colour alone).
    expect(screen.getByText('Info')).toBeInTheDocument();
    expect(screen.getByText('Urgent')).toBeInTheDocument();
  });

  it('emphasises unread items and marks all read on open', async () => {
    listNotifications.mockResolvedValue(page([unread, readOne]));
    renderPage();

    await screen.findByText('You were assigned pump 1 nozzle 1');
    // On open, the page marks the unread set read (acts as "seen").
    await waitFor(() => expect(markAllNotificationsRead).toHaveBeenCalledTimes(1));
    expect(screen.getByText('Unread')).toBeInTheDocument();
  });

  it('marks a single notification read on tap', async () => {
    // Only read items present so the auto-mark-all does not fire; add one unread.
    listNotifications.mockResolvedValue(page([unread]));
    renderPage();

    await screen.findByText('You were assigned pump 1 nozzle 1');
    const markButton = await screen.findByRole('button', { name: 'Mark as read' });
    await userEvent.click(markButton);
    await waitFor(() => expect(markNotificationRead).toHaveBeenCalledWith('n-1'));
  });

  it('shows an error state when the feed fails to load', async () => {
    listNotifications.mockRejectedValue(new Error('boom'));
    renderPage();
    expect(await screen.findByText("Couldn't load your notifications")).toBeInTheDocument();
  });
});
