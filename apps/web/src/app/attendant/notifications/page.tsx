'use client';

import { useEffect, useRef } from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Bell, CheckCheck, Loader2 } from 'lucide-react';

import type { Notification, NotificationSeverity } from '@fuelgrid/sdk';
import { Badge, Button, Card, CardContent, EmptyState, ErrorState, Skeleton } from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { useT, type Messages } from '@/lib/i18n';

const FEED_KEY = ['attendant-notifications'];
const UNREAD_KEY = ['attendant-notifications', 'unread-count'];
const PAGE_SIZE = 30;

/**
 * Status by text + colour, never colour alone (PRD §15.1): the badge carries
 * a translated severity word; the tone only reinforces it.
 */
function severityTone(sev: NotificationSeverity): 'danger' | 'warning' | 'success' | 'info' {
  switch (sev) {
    case 'critical':
      return 'danger';
    case 'warning':
      return 'warning';
    case 'success':
      return 'success';
    default:
      return 'info';
  }
}

function severityLabel(sev: NotificationSeverity, t: Messages): string {
  switch (sev) {
    case 'critical':
      return t.notifications.severityCritical;
    case 'warning':
      return t.notifications.severityWarning;
    case 'success':
      return t.notifications.severitySuccess;
    default:
      return t.notifications.severityInfo;
  }
}

export default function AttendantNotificationsPage() {
  const t = useT();
  const qc = useQueryClient();

  const list = useQuery({
    queryKey: FEED_KEY,
    // Poll like the rest of the workflow (no push/websocket) so newly produced
    // supervisor notifications appear without a manual refresh.
    queryFn: ({ signal }) => api.listNotifications({ limit: PAGE_SIZE }, signal),
    refetchInterval: 30_000,
  });

  const markRead = useMutation({
    mutationFn: (id: string) => api.markNotificationRead(id),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: FEED_KEY });
      void qc.invalidateQueries({ queryKey: UNREAD_KEY });
    },
  });

  const markAll = useMutation({
    mutationFn: () => api.markAllNotificationsRead(),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: FEED_KEY });
      void qc.invalidateQueries({ queryKey: UNREAD_KEY });
    },
  });

  const items = list.data?.items ?? [];
  const unreadCount = items.filter((n) => !n.read_at).length;

  // Mark every unread notification read on open (the page acts as "seen").
  // Run once per distinct set of unread ids so a poll-driven refetch with the
  // same items doesn't re-fire, but a genuinely new unread does.
  const markedRef = useRef<string>('');
  useEffect(() => {
    const unreadIds = items
      .filter((n) => !n.read_at)
      .map((n) => n.id)
      .sort()
      .join(',');
    if (unreadIds === '' || unreadIds === markedRef.current) return;
    markedRef.current = unreadIds;
    markAll.mutate();
    // `markAll` is a stable TanStack mutation handle; depending on `items`
    // alone is intentional so a poll-driven refetch of the same set is a no-op.
  }, [items, markAll]);

  return (
    <div className="flex flex-col gap-4">
      <Button asChild variant="ghost" className="h-12 w-fit -ml-2 text-base">
        <Link href="/attendant">
          <ArrowLeft className="size-5" aria-hidden />
          {t.common.myShift}
        </Link>
      </Button>

      <div className="flex items-start justify-between gap-2">
        <div>
          <h1 className="text-xl font-semibold leading-tight">{t.notifications.title}</h1>
          <p className="text-base text-muted-foreground">{t.notifications.subtitle}</p>
        </div>
        {unreadCount > 0 ? (
          <Button
            type="button"
            variant="outline"
            className="h-11 shrink-0 text-sm"
            disabled={markAll.isPending}
            onClick={() => markAll.mutate()}
          >
            <CheckCheck className="size-4" aria-hidden />
            {t.notifications.markAllRead}
          </Button>
        ) : null}
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-xl" />
          ))}
        </div>
      ) : list.isError ? (
        <ErrorState
          title={t.notifications.errLoadTitle}
          description={String((list.error as Error).message)}
          action={
            <Button variant="secondary" onClick={() => list.refetch()}>
              {t.common.tryAgain}
            </Button>
          }
        />
      ) : items.length === 0 ? (
        <EmptyState
          title={t.notifications.emptyTitle}
          description={t.notifications.emptyBody}
          icon={<Bell />}
        />
      ) : (
        <ul className="flex flex-col gap-2">
          {items.map((n) => (
            <NotificationRow
              key={n.id}
              notification={n}
              onMarkRead={() => markRead.mutate(n.id)}
              marking={markRead.isPending}
              t={t}
            />
          ))}
        </ul>
      )}
    </div>
  );
}

function NotificationRow({
  notification: n,
  onMarkRead,
  marking,
  t,
}: {
  notification: Notification;
  onMarkRead: () => void;
  marking: boolean;
  t: Messages;
}) {
  const unread = !n.read_at;
  return (
    <li>
      <Card className={unread ? 'border-accent/40 bg-accent/[0.04]' : undefined}>
        <CardContent className="flex flex-col gap-2 p-4">
          <div className="flex items-center justify-between gap-2">
            <Badge tone={severityTone(n.severity)}>{severityLabel(n.severity, t)}</Badge>
            {unread ? (
              <span className="text-xs font-medium text-accent" role="status">
                {t.notifications.unread}
              </span>
            ) : (
              <span className="text-xs text-muted-foreground">{t.notifications.markedRead}</span>
            )}
          </div>
          {/* Title + body come from the server in ENGLISH — rendered VERBATIM
              (server prose is never fake-translated; only the chrome is i18n). */}
          <p className="text-base font-semibold leading-snug">{n.title}</p>
          {n.body ? <p className="text-base text-muted-foreground">{n.body}</p> : null}
          <div className="flex items-center justify-between gap-2 pt-0.5">
            <span className="text-xs text-muted-foreground">
              {new Date(n.created_at).toLocaleString([], {
                hour: '2-digit',
                minute: '2-digit',
                day: '2-digit',
                month: 'short',
              })}
            </span>
            {unread ? (
              <Button
                type="button"
                variant="outline"
                className="h-10 text-sm"
                disabled={marking}
                onClick={onMarkRead}
              >
                {marking ? <Loader2 className="size-4 animate-spin" aria-hidden /> : null}
                {t.notifications.markRead}
              </Button>
            ) : null}
          </div>
        </CardContent>
      </Card>
    </li>
  );
}
