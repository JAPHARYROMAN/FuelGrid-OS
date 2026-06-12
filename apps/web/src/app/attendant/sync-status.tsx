'use client';

import { useEffect, useState } from 'react';
import { AlertTriangle, Check, CloudOff, Loader2, RefreshCw, Trash2, Wifi, X } from 'lucide-react';

import { Button } from '@fuelgrid/ui';

import {
  deriveSyncSummary,
  getSyncEngine,
  useSyncEngineState,
  type QueuedAction,
  type SyncStatusSummary,
} from '@/lib/offline';

/**
 * The attendant shell's sync indicator (PRD §9.3/§9.7): Online / Offline /
 * Syncing… / N waiting / Sync failed / Needs attention / All changes synced.
 * Tapping it opens the sync details sheet listing every queued action with
 * its status, error message, "Try again", and — for failed/conflict rows
 * only — an explicitly confirmed "Discard".
 */
export function SyncStatusChip() {
  const state = useSyncEngineState();
  const [open, setOpen] = useState(false);

  // Wire the engine to the connectivity events + first sync on app open.
  useEffect(() => {
    const engine = getSyncEngine();
    void engine.init();
    engine.setOnline(navigator.onLine);
    const up = () => engine.setOnline(true);
    const down = () => engine.setOnline(false);
    window.addEventListener('online', up);
    window.addEventListener('offline', down);
    return () => {
      window.removeEventListener('online', up);
      window.removeEventListener('offline', down);
    };
  }, []);

  const summary = deriveSyncSummary(state);
  const { label, tone, icon } = chipPresentation(summary);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={`flex items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${tone}`}
        aria-haspopup="dialog"
      >
        <span role="status">
          <span className="flex items-center gap-1.5">
            {icon}
            {label}
          </span>
        </span>
      </button>
      {open ? <SyncDetailsSheet onClose={() => setOpen(false)} /> : null}
    </>
  );
}

function chipPresentation(summary: SyncStatusSummary): {
  label: string;
  tone: string;
  icon: React.ReactNode;
} {
  switch (summary.kind) {
    case 'offline':
      return {
        label: summary.waiting > 0 ? `Offline — ${summary.waiting} to sync` : 'Offline',
        tone: 'bg-danger/10 text-danger',
        icon: <CloudOff className="size-3.5" aria-hidden />,
      };
    case 'syncing':
      return {
        label: 'Syncing…',
        tone: 'bg-accent/10 text-accent',
        icon: <Loader2 className="size-3.5 animate-spin" aria-hidden />,
      };
    case 'auth_required':
      return {
        label: 'Sign in to finish syncing',
        tone: 'bg-warning/10 text-warning',
        icon: <AlertTriangle className="size-3.5" aria-hidden />,
      };
    case 'conflict':
      return {
        label: 'Needs attention',
        tone: 'bg-warning/10 text-warning',
        icon: <AlertTriangle className="size-3.5" aria-hidden />,
      };
    case 'failed':
      return {
        label: 'Sync failed',
        tone: 'bg-danger/10 text-danger',
        icon: <AlertTriangle className="size-3.5" aria-hidden />,
      };
    case 'waiting':
      return {
        label: `${summary.waiting} waiting to sync`,
        tone: 'bg-warning/10 text-warning',
        icon: <RefreshCw className="size-3.5" aria-hidden />,
      };
    case 'synced':
      return {
        label: 'All changes synced',
        tone: 'bg-success/10 text-success',
        icon: <Check className="size-3.5" aria-hidden />,
      };
    case 'online':
      return {
        label: 'Online',
        tone: 'bg-success/10 text-success',
        icon: <Wifi className="size-3.5" aria-hidden />,
      };
  }
}

/** The honest offline strip shown under the header on every attendant screen. */
export function OfflineHint() {
  const state = useSyncEngineState();
  if (state.online) return null;
  return (
    <div className="mx-auto w-full max-w-md px-4 pt-2">
      <p className="rounded-md bg-warning/10 px-3 py-2 text-sm text-warning" role="status">
        You are offline — showing the last synced info. Anything you submit is saved on this phone
        and will sync when you are back online.
      </p>
    </div>
  );
}

function SyncDetailsSheet({ onClose }: { onClose: () => void }) {
  const state = useSyncEngineState();
  const engine = getSyncEngine();
  const items = [...state.items].sort((a, b) => b.seq - a.seq);

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/50"
      role="dialog"
      aria-modal="true"
      aria-label="Sync details"
    >
      <div className="flex max-h-[80vh] w-full max-w-md flex-col rounded-t-2xl border border-border bg-background">
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="text-base font-semibold">Sync details</h2>
          <button
            type="button"
            className="flex size-9 items-center justify-center rounded-md text-muted-foreground"
            onClick={onClose}
            aria-label="Close sync details"
          >
            <X className="size-5" aria-hidden />
          </button>
        </div>

        <div className="flex flex-col gap-3 overflow-y-auto px-4 py-3">
          {state.phase === 'auth_required' ? (
            <p className="rounded-md bg-warning/10 px-3 py-2 text-sm text-warning" role="status">
              Your session expired before everything synced. Sign in again to finish syncing —
              nothing has been lost.
            </p>
          ) : null}

          {items.length === 0 ? (
            <p className="py-6 text-center text-base text-muted-foreground" role="status">
              Nothing waiting to sync. Everything you submitted reached the server.
            </p>
          ) : (
            <ul className="flex flex-col gap-2">
              {items.map((item) => (
                <SyncDetailsRow
                  key={item.local_action_id}
                  item={item}
                  onRetry={() => void engine.retry(item.local_action_id)}
                  onDiscard={() => void engine.discard(item.local_action_id)}
                />
              ))}
            </ul>
          )}
        </div>

        <div className="border-t border-border px-4 py-3">
          <Button
            className="h-12 w-full text-base"
            disabled={state.phase === 'syncing' || !state.online}
            onClick={() => void engine.syncNow()}
          >
            {state.phase === 'syncing' ? (
              <Loader2 className="size-5 animate-spin" aria-hidden />
            ) : (
              <RefreshCw className="size-5" aria-hidden />
            )}
            Sync now
          </Button>
        </div>
      </div>
    </div>
  );
}

function statusBadge(item: QueuedAction): { label: string; tone: string } {
  switch (item.sync_status) {
    case 'pending':
      return { label: 'Waiting to sync', tone: 'bg-warning/10 text-warning' };
    case 'syncing':
      return { label: 'Syncing…', tone: 'bg-accent/10 text-accent' };
    case 'synced':
      return { label: 'Synced', tone: 'bg-success/10 text-success' };
    case 'failed':
      return { label: 'Failed', tone: 'bg-danger/10 text-danger' };
    case 'conflict':
      return { label: 'Needs supervisor attention', tone: 'bg-warning/10 text-warning' };
  }
}

function SyncDetailsRow({
  item,
  onRetry,
  onDiscard,
}: {
  item: QueuedAction;
  onRetry: () => void;
  onDiscard: () => void;
}) {
  // Discarding is the ONLY way queued data is ever dropped, so it takes an
  // explicit second tap to confirm (PRD §14.3).
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);
  const badge = statusBadge(item);
  const actionable = item.sync_status === 'failed' || item.sync_status === 'conflict';

  return (
    <li className="flex flex-col gap-2 rounded-lg border border-border p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-medium">{item.label}</p>
          <p className="text-xs text-muted-foreground">
            Saved{' '}
            {new Date(item.created_at_local).toLocaleString([], {
              hour: '2-digit',
              minute: '2-digit',
              day: '2-digit',
              month: 'short',
            })}
          </p>
        </div>
        <span
          className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium ${badge.tone}`}
          role="status"
        >
          {badge.label}
        </span>
      </div>

      {item.error_message ? (
        <p className="rounded-md bg-danger/10 px-2.5 py-1.5 text-xs text-danger" role="alert">
          {item.error_message}
        </p>
      ) : null}

      {actionable ? (
        <div className="flex items-center gap-2">
          <Button variant="outline" className="h-10 flex-1 text-sm" onClick={onRetry}>
            <RefreshCw className="size-4" aria-hidden />
            Try again
          </Button>
          {confirmingDiscard ? (
            <Button
              variant="outline"
              className="h-10 flex-1 border-danger/40 text-sm text-danger"
              onClick={onDiscard}
            >
              <Trash2 className="size-4" aria-hidden />
              Tap again to discard
            </Button>
          ) : (
            <Button
              variant="outline"
              className="h-10 flex-1 text-sm text-muted-foreground"
              onClick={() => setConfirmingDiscard(true)}
            >
              <Trash2 className="size-4" aria-hidden />
              Discard
            </Button>
          )}
        </div>
      ) : null}
    </li>
  );
}
