'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Bell, CheckCheck, ExternalLink } from 'lucide-react';

import { SdkError, type Notification, type NotificationSeverity } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  EmptyState,
  ErrorState,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

const PAGE_SIZE = 50;

type SeverityFilter = 'all' | NotificationSeverity;
type ReadFilter = 'all' | 'unread';

const SEVERITY_OPTIONS: { value: SeverityFilter; label: string }[] = [
  { value: 'all', label: 'All severities' },
  { value: 'critical', label: 'Critical' },
  { value: 'warning', label: 'Warning' },
  { value: 'success', label: 'Success' },
  { value: 'info', label: 'Info' },
];

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

/**
 * Map a notification's related aggregate type + id to an in-app route so the
 * "Open" action can deep-link to the record that triggered it. The aggregate
 * types come from the notifications subscriber (shift, incident, risk alert,
 * approval request, revenue). Unknown types get no link.
 */
function relatedHref(n: Notification): string | null {
  if (!n.related_entity_type) return null;
  const type = n.related_entity_type.toLowerCase();
  if (type.includes('incident')) return '/incidents';
  if (type.includes('risk')) return '/risk';
  if (type.includes('approval')) return '/operations';
  if (type.includes('shift')) return '/operations';
  if (type.includes('revenue')) return '/revenue';
  return null;
}

export default function NotificationsPage() {
  const qc = useQueryClient();
  const [severity, setSeverity] = React.useState<SeverityFilter>('all');
  const [readFilter, setReadFilter] = React.useState<ReadFilter>('all');
  const [offset, setOffset] = React.useState(0);

  const list = useQuery({
    queryKey: ['notifications', readFilter, offset],
    queryFn: ({ signal }) =>
      api.listNotifications({ unread: readFilter === 'unread', limit: PAGE_SIZE, offset }, signal),
  });

  const markRead = useMutation({
    mutationFn: (id: string) => api.markNotificationRead(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notifications'] });
      qc.invalidateQueries({ queryKey: ['notifications', 'unread-count'] });
    },
    onError: (err) => {
      toast.error(
        'Could not mark the notification as read',
        err instanceof SdkError ? err.message : undefined,
      );
    },
  });

  const markAllRead = useMutation({
    mutationFn: () => api.markAllNotificationsRead(),
    onSuccess: (result) => {
      qc.invalidateQueries({ queryKey: ['notifications'] });
      qc.invalidateQueries({ queryKey: ['notifications', 'unread-count'] });
      toast.success('Notifications cleared', `${result.marked_read} marked as read.`);
    },
    onError: (err) => {
      toast.error(
        'Could not mark all notifications as read',
        err instanceof SdkError ? err.message : undefined,
      );
    },
  });

  function resetTo(read: ReadFilter) {
    setReadFilter(read);
    setOffset(0);
  }

  const allItems = list.data?.items ?? [];
  // The feed backend has no severity filter, so narrow client-side.
  const items = severity === 'all' ? allItems : allItems.filter((n) => n.severity === severity);
  const hasMore = list.data?.has_more ?? false;
  const unreadOnPage = allItems.filter((n) => !n.read_at).length;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitoring"
        title="Notifications"
        description="Your in-app feed of operational events — revenue, shift closes, incidents, risk alerts and approvals. Open the linked record or mark items read."
        actions={
          <Button
            type="button"
            variant="secondary"
            size="sm"
            disabled={markAllRead.isPending || unreadOnPage === 0}
            onClick={() => markAllRead.mutate()}
          >
            <CheckCheck className="size-4" />
            {markAllRead.isPending ? 'Marking…' : 'Mark all read'}
          </Button>
        }
      />

      <div className="flex flex-wrap items-center gap-2 rounded-xl border border-border bg-card/40 p-3">
        <div className="flex gap-1" role="group" aria-label="Read filter">
          <Button
            type="button"
            size="sm"
            variant={readFilter === 'all' ? 'primary' : 'ghost'}
            onClick={() => resetTo('all')}
          >
            All
          </Button>
          <Button
            type="button"
            size="sm"
            variant={readFilter === 'unread' ? 'primary' : 'ghost'}
            onClick={() => resetTo('unread')}
          >
            Unread
          </Button>
        </div>
        <div className="ml-auto flex flex-wrap gap-1" role="group" aria-label="Severity filter">
          {SEVERITY_OPTIONS.map((opt) => (
            <Button
              key={opt.value}
              type="button"
              size="sm"
              variant={severity === opt.value ? 'primary' : 'ghost'}
              onClick={() => setSeverity(opt.value)}
            >
              {opt.label}
            </Button>
          ))}
        </div>
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-20 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        <ErrorState
          title="Couldn't load notifications"
          description={String((list.error as Error).message)}
          onRetry={() => list.refetch()}
        />
      ) : items.length === 0 ? (
        <EmptyState
          title={readFilter === 'unread' ? 'No unread notifications' : 'No notifications'}
          description="Operational events show up here as they happen — revenue, shift closes, incidents, risk alerts and approvals."
          icon={<Bell />}
        />
      ) : (
        <>
          <div className="flex flex-col gap-2">
            {items.map((n) => {
              const href = relatedHref(n);
              const unread = !n.read_at;
              return (
                <Card
                  key={n.id}
                  className={unread ? 'border-accent/40 bg-accent/[0.03]' : undefined}
                >
                  <CardContent className="flex items-start gap-4 p-4">
                    <div className="mt-0.5 flex flex-col items-center gap-2">
                      <Badge tone={severityTone(n.severity)}>{n.severity}</Badge>
                      {unread ? (
                        <span
                          className="size-2 rounded-full bg-accent"
                          aria-label="Unread"
                          title="Unread"
                        />
                      ) : null}
                    </div>
                    <div className="flex min-w-0 flex-1 flex-col gap-1">
                      <div className="flex items-center gap-2">
                        <span className="font-medium text-foreground">{n.title}</span>
                        <span className="font-mono text-[11px] text-muted-foreground">
                          {n.type}
                        </span>
                      </div>
                      <p className="text-sm text-muted-foreground">{n.body}</p>
                      <span className="text-xs text-muted-foreground/80">
                        {new Date(n.created_at).toLocaleString()}
                      </span>
                    </div>
                    <div className="flex shrink-0 flex-col items-end gap-2">
                      {href ? (
                        <Button asChild variant="ghost" size="sm">
                          <a href={href}>
                            <ExternalLink className="size-4" />
                            Open
                          </a>
                        </Button>
                      ) : null}
                      {unread ? (
                        <Button
                          type="button"
                          variant="secondary"
                          size="sm"
                          disabled={markRead.isPending}
                          onClick={() => markRead.mutate(n.id)}
                        >
                          Mark read
                        </Button>
                      ) : null}
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          </div>

          <div className="flex items-center justify-between">
            <p className="text-sm text-muted-foreground">
              Showing {offset + 1}–{offset + items.length}
              {severity !== 'all' ? ' (filtered)' : ''}
            </p>
            <div className="flex gap-2">
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={offset === 0 || list.isFetching}
                onClick={() => setOffset(Math.max(0, offset - PAGE_SIZE))}
              >
                Previous
              </Button>
              <Button
                type="button"
                variant="secondary"
                size="sm"
                disabled={!hasMore || list.isFetching}
                onClick={() => setOffset(offset + PAGE_SIZE)}
              >
                Next
              </Button>
            </div>
          </div>
        </>
      )}
    </div>
  );
}
