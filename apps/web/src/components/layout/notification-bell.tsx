'use client';

import { useEffect, useRef, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Bell, Check, CheckCheck } from 'lucide-react';

import type { Notification, NotificationSeverity } from '@fuelgrid/sdk';
import { Badge, Button, EmptyState, ErrorState, Skeleton, cn } from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { useAuthStore } from '@/stores/auth-store';

const UNREAD_COUNT_KEY = ['notifications', 'unread-count'];
const FEED_KEY = ['notifications', 'feed'];

// Badge tone per severity. 'critical' renders danger; the rest map 1:1.
const severityTone: Record<NotificationSeverity, 'info' | 'success' | 'warning' | 'danger'> = {
  info: 'info',
  success: 'success',
  warning: 'warning',
  critical: 'danger',
};

function relativeTime(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return '';
  const diffSec = Math.round((Date.now() - then) / 1000);
  if (diffSec < 60) return 'just now';
  const diffMin = Math.round(diffSec / 60);
  if (diffMin < 60) return `${diffMin}m ago`;
  const diffHr = Math.round(diffMin / 60);
  if (diffHr < 24) return `${diffHr}h ago`;
  const diffDay = Math.round(diffHr / 24);
  if (diffDay < 7) return `${diffDay}d ago`;
  return new Date(iso).toLocaleDateString();
}

export function NotificationBell() {
  const authed = useAuthStore((s) => s.authed);
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const containerRef = useRef<HTMLDivElement>(null);

  // Poll the unread count for the badge while signed in. Cheap endpoint, so a
  // 30s interval keeps the badge live without hammering the API.
  const unread = useQuery({
    queryKey: UNREAD_COUNT_KEY,
    queryFn: ({ signal }) => api.notificationUnreadCount(signal),
    enabled: authed,
    refetchInterval: 30_000,
  });

  // The feed is only fetched when the dropdown is open.
  const feed = useQuery({
    queryKey: FEED_KEY,
    queryFn: ({ signal }) => api.listNotifications({ limit: 20 }, signal),
    enabled: authed && open,
  });

  const markOne = useMutation({
    mutationFn: (id: string) => api.markNotificationRead(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: UNREAD_COUNT_KEY });
      void qc.invalidateQueries({ queryKey: FEED_KEY });
    },
  });

  const markAll = useMutation({
    mutationFn: () => api.markAllNotificationsRead(),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: UNREAD_COUNT_KEY });
      void qc.invalidateQueries({ queryKey: FEED_KEY });
    },
  });

  // Close on outside click / Escape.
  useEffect(() => {
    if (!open) return;
    function onClick(e: MouseEvent) {
      if (containerRef.current && !containerRef.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    function onKey(e: KeyboardEvent) {
      if (e.key === 'Escape') setOpen(false);
    }
    document.addEventListener('mousedown', onClick);
    document.addEventListener('keydown', onKey);
    return () => {
      document.removeEventListener('mousedown', onClick);
      document.removeEventListener('keydown', onKey);
    };
  }, [open]);

  if (!authed) return null;

  const count = unread.data?.unread_count ?? 0;
  const items = feed.data?.items ?? [];

  return (
    <div ref={containerRef} className="relative">
      <Button
        variant="ghost"
        size="icon"
        aria-label={count > 0 ? `Notifications (${count} unread)` : 'Notifications'}
        aria-haspopup="true"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
        className="relative"
      >
        <Bell className="size-[18px]" />
        {count > 0 ? (
          <span className="absolute -right-0.5 -top-0.5 inline-flex min-w-4 items-center justify-center rounded-full bg-danger px-1 text-[10px] font-semibold leading-4 text-danger-foreground">
            {count > 99 ? '99+' : count}
          </span>
        ) : null}
      </Button>

      {open ? (
        <div
          role="dialog"
          aria-label="Notifications"
          className="absolute right-0 top-12 z-50 w-80 overflow-hidden rounded-xl border border-border bg-card shadow-elev-lg sm:w-96"
        >
          <div className="flex items-center justify-between border-b border-border px-4 py-3">
            <span className="text-sm font-semibold text-foreground">Notifications</span>
            <button
              type="button"
              onClick={() => markAll.mutate()}
              disabled={markAll.isPending || count === 0}
              className="inline-flex items-center gap-1 text-xs text-muted-foreground transition-colors hover:text-foreground disabled:cursor-not-allowed disabled:opacity-50"
            >
              <CheckCheck className="size-3.5" />
              Mark all read
            </button>
          </div>

          <div className="max-h-[28rem] overflow-y-auto">
            {feed.isLoading ? (
              <div className="space-y-3 p-4">
                {[0, 1, 2].map((i) => (
                  <Skeleton key={i} className="h-14 w-full" />
                ))}
              </div>
            ) : feed.isError ? (
              <div className="p-4">
                <ErrorState
                  title="Could not load notifications"
                  description="Please try again."
                  onRetry={() => void feed.refetch()}
                />
              </div>
            ) : items.length === 0 ? (
              <div className="p-6">
                <EmptyState title="You're all caught up" description="No notifications yet." />
              </div>
            ) : (
              <ul className="divide-y divide-border">
                {items.map((n: Notification) => {
                  const isUnread = !n.read_at;
                  return (
                    <li
                      key={n.id}
                      className={cn(
                        'flex items-start gap-3 px-4 py-3 transition-colors',
                        isUnread ? 'bg-accent/5' : 'bg-transparent',
                      )}
                    >
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <Badge tone={severityTone[n.severity] ?? 'neutral'}>{n.severity}</Badge>
                          <span className="truncate text-sm font-medium text-foreground">
                            {n.title}
                          </span>
                        </div>
                        {n.body ? (
                          <p className="mt-1 text-xs text-muted-foreground">{n.body}</p>
                        ) : null}
                        <span className="mt-1 block text-[11px] text-muted-foreground">
                          {relativeTime(n.created_at)}
                        </span>
                      </div>
                      {isUnread ? (
                        <button
                          type="button"
                          aria-label="Mark as read"
                          title="Mark as read"
                          onClick={() => markOne.mutate(n.id)}
                          disabled={markOne.isPending}
                          className="mt-0.5 inline-flex size-7 items-center justify-center rounded-lg text-muted-foreground transition-colors hover:bg-muted hover:text-foreground disabled:opacity-50"
                        >
                          <Check className="size-4" />
                        </button>
                      ) : null}
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        </div>
      ) : null}
    </div>
  );
}
