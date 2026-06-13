'use client';

import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { Bell } from 'lucide-react';

import { api } from '@/lib/api';
import { useT } from '@/lib/i18n';

const UNREAD_KEY = ['attendant-notifications', 'unread-count'];

/**
 * The attendant header bell (PRD §6.11). Polls the cheap unread-count endpoint
 * — the same pattern as the rest of the workflow queries, no push/websocket —
 * and links to the full /attendant/notifications screen. The count badge is a
 * visual cue; the accessible name always states the count in words so the
 * status is conveyed by text, not colour alone (PRD §15.1).
 */
export function AttendantNotificationBell() {
  const t = useT();
  const unread = useQuery({
    queryKey: UNREAD_KEY,
    queryFn: ({ signal }) => api.notificationUnreadCount(signal),
    refetchInterval: 30_000,
  });

  const count = unread.data?.unread_count ?? 0;

  return (
    <Link
      href="/attendant/notifications"
      aria-label={count > 0 ? t.home.bellUnread(count) : t.home.bellNoUnread}
      className="relative flex size-11 items-center justify-center rounded-full text-foreground hover:bg-accent/10"
    >
      <Bell className="size-5" aria-hidden />
      {count > 0 ? (
        <span className="absolute right-1 top-1 inline-flex min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-semibold leading-4 text-danger-foreground">
          {count > 99 ? '99+' : count}
        </span>
      ) : null}
    </Link>
  );
}
