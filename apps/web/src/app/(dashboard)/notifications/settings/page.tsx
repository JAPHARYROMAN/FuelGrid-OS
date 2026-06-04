'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { BellRing } from 'lucide-react';

import {
  SdkError,
  type NotificationPreference,
  type UpsertNotificationPreferenceRequest,
} from '@fuelgrid/sdk';
import {
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

const CATEGORY_LABELS: Record<string, string> = {
  revenue: 'Revenue recognized',
  shift: 'Shift closed',
  risk: 'Risk alert raised',
  incident: 'Incident opened',
  approval: 'Approval requested',
};

const CHANNEL_LABELS: Record<string, string> = {
  in_app: 'In-app',
  email: 'Email',
};

function label(map: Record<string, string>, key: string): string {
  return map[key] ?? key.replace(/[_-]+/g, ' ');
}

/** A preference keyed by `${category}:${channel}` for quick lookup. */
type PrefMap = Map<string, NotificationPreference>;

function prefKey(category: string, channel: string): string {
  return `${category}:${channel}`;
}

export default function NotificationSettingsPage() {
  const qc = useQueryClient();

  const prefs = useQuery({
    queryKey: ['notification-preferences'],
    queryFn: ({ signal }) => api.listNotificationPreferences(signal),
  });

  const upsert = useMutation({
    mutationFn: (req: UpsertNotificationPreferenceRequest) => api.upsertNotificationPreference(req),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ['notification-preferences'] });
    },
    onError: (err) => {
      toast.error('Could not save preference', err instanceof SdkError ? err.message : undefined);
    },
  });

  const categories = prefs.data?.categories ?? [];
  const channels = prefs.data?.channels ?? [];

  const byKey: PrefMap = React.useMemo(() => {
    const m: PrefMap = new Map();
    for (const p of prefs.data?.items ?? []) {
      m.set(prefKey(p.category, p.channel), p);
    }
    return m;
  }, [prefs.data]);

  // Default: a channel/category with no stored row is treated as enabled (the
  // feed delivers by default until the user opts out).
  function isEnabled(category: string, channel: string): boolean {
    const p = byKey.get(prefKey(category, channel));
    return p ? p.enabled : true;
  }

  function toggle(category: string, channel: string) {
    const current = byKey.get(prefKey(category, channel));
    upsert.mutate({
      category,
      channel,
      enabled: !(current ? current.enabled : true),
      quiet_hours_start: current?.quiet_hours_start ?? null,
      quiet_hours_end: current?.quiet_hours_end ?? null,
    });
  }

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Monitoring"
        title="Notification settings"
        description="Choose which operational events notify you and on which channels. Set quiet hours to suppress non-critical delivery overnight."
        actions={
          <Button asChild variant="secondary" size="sm">
            <a href="/notifications">Back to feed</a>
          </Button>
        }
      />

      {prefs.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 5 }).map((_, i) => (
            <Skeleton key={i} className="h-16 rounded-lg" />
          ))}
        </div>
      ) : prefs.isError ? (
        <ErrorState
          title="Couldn't load notification settings"
          description={String((prefs.error as Error).message)}
          onRetry={() => prefs.refetch()}
        />
      ) : (
        <>
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Delivery by category</CardTitle>
            </CardHeader>
            <CardContent className="flex flex-col gap-1 text-sm">
              <div className="flex items-center gap-3 border-b border-border pb-2 text-xs uppercase tracking-wider text-muted-foreground">
                <span className="flex-1">Event</span>
                {channels.map((ch) => (
                  <span key={ch} className="w-20 text-center">
                    {label(CHANNEL_LABELS, ch)}
                  </span>
                ))}
              </div>
              {categories.map((cat) => (
                <div key={cat} className="flex items-center gap-3 py-2">
                  <span className="flex-1 font-medium text-foreground">
                    {label(CATEGORY_LABELS, cat)}
                  </span>
                  {channels.map((ch) => {
                    const checked = isEnabled(cat, ch);
                    const inputId = `pref-${cat}-${ch}`;
                    return (
                      <label
                        key={ch}
                        htmlFor={inputId}
                        className="flex w-20 cursor-pointer items-center justify-center"
                      >
                        <input
                          id={inputId}
                          type="checkbox"
                          className="size-4 cursor-pointer accent-accent"
                          checked={checked}
                          disabled={upsert.isPending}
                          onChange={() => toggle(cat, ch)}
                          aria-label={`${label(CATEGORY_LABELS, cat)} via ${label(CHANNEL_LABELS, ch)}`}
                        />
                      </label>
                    );
                  })}
                </div>
              ))}
            </CardContent>
          </Card>

          <QuietHoursCard
            categories={categories}
            channels={channels}
            byKey={byKey}
            isEnabled={isEnabled}
            saving={upsert.isPending}
            onSave={(req) => upsert.mutate(req)}
          />
        </>
      )}
    </div>
  );
}

/**
 * QuietHoursCard sets an optional local quiet window per channel. A blank pair
 * clears quiet hours. The window applies to every category on that channel, so
 * the control writes it across all categories the channel covers.
 */
function QuietHoursCard({
  categories,
  channels,
  byKey,
  isEnabled,
  saving,
  onSave,
}: {
  categories: string[];
  channels: string[];
  byKey: PrefMap;
  isEnabled: (category: string, channel: string) => boolean;
  saving: boolean;
  onSave: (req: UpsertNotificationPreferenceRequest) => void;
}) {
  // Seed the inputs from the first category's stored window for each channel.
  const seed = React.useCallback(
    (channel: string): { start: string; end: string } => {
      for (const cat of categories) {
        const p = byKey.get(prefKey(cat, channel));
        if (p?.quiet_hours_start && p?.quiet_hours_end) {
          return { start: p.quiet_hours_start, end: p.quiet_hours_end };
        }
      }
      return { start: '', end: '' };
    },
    [byKey, categories],
  );

  const [draft, setDraft] = React.useState<Record<string, { start: string; end: string }>>({});

  function valueFor(channel: string): { start: string; end: string } {
    return draft[channel] ?? seed(channel);
  }

  function set(channel: string, patch: Partial<{ start: string; end: string }>) {
    setDraft((d) => ({ ...d, [channel]: { ...valueFor(channel), ...patch } }));
  }

  function apply(channel: string) {
    const { start, end } = valueFor(channel);
    const startVal = start.trim() === '' ? null : start.trim();
    const endVal = end.trim() === '' ? null : end.trim();
    // Apply the window to every category on this channel, preserving each
    // category's enabled state.
    for (const cat of categories) {
      onSave({
        category: cat,
        channel,
        enabled: isEnabled(cat, channel),
        quiet_hours_start: startVal,
        quiet_hours_end: endVal,
      });
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base">Quiet hours</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-col gap-4 text-sm">
        <p className="text-muted-foreground">
          Set a local quiet window (24h HH:MM) per channel. Leave both blank to clear it.
        </p>
        {channels.map((ch) => {
          const v = valueFor(ch);
          return (
            <div key={ch} className="flex flex-wrap items-end gap-3">
              <span className="w-24 font-medium text-foreground">{label(CHANNEL_LABELS, ch)}</span>
              <div className="flex flex-col gap-1">
                <Label htmlFor={`quiet-start-${ch}`}>From</Label>
                <Input
                  id={`quiet-start-${ch}`}
                  type="time"
                  className="w-32"
                  value={v.start}
                  onChange={(e) => set(ch, { start: e.target.value })}
                />
              </div>
              <div className="flex flex-col gap-1">
                <Label htmlFor={`quiet-end-${ch}`}>To</Label>
                <Input
                  id={`quiet-end-${ch}`}
                  type="time"
                  className="w-32"
                  value={v.end}
                  onChange={(e) => set(ch, { end: e.target.value })}
                />
              </div>
              <Button
                type="button"
                size="sm"
                variant="secondary"
                disabled={saving}
                onClick={() => apply(ch)}
              >
                <BellRing className="size-4" />
                Save
              </Button>
            </div>
          );
        })}
      </CardContent>
    </Card>
  );
}
