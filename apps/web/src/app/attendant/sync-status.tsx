'use client';

import { useEffect, useState } from 'react';
import { AlertTriangle, Check, CloudOff, Loader2, RefreshCw, Trash2, Wifi, X } from 'lucide-react';

import { Button } from '@fuelgrid/ui';

import { useT, type Messages } from '@/lib/i18n';
import {
  deriveSyncSummary,
  getSyncEngine,
  useSyncEngineState,
  type QueuedAction,
  type SyncStatusSummary,
} from '@/lib/offline';

import { useSheetFocusTrap } from './use-focus-trap';

/**
 * The attendant shell's sync indicator (PRD §9.3/§9.7): Online / Offline /
 * Syncing… / N waiting / Sync failed / Needs attention / All changes synced.
 * Tapping it opens the sync details sheet listing every queued action with
 * its status, error message, "Try again", and — for failed/conflict rows
 * only — an explicitly confirmed "Discard".
 *
 * i18n (Phase 6b): every label here renders from the dictionaries at display
 * time. Queue records store action payloads and machine error CODES, never
 * display prose — switching language re-renders the whole queue translated.
 * Raw server error prose (no code) is shown verbatim as the honest fallback.
 */
export function SyncStatusChip() {
  const t = useT();
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
  const { label, tone, icon } = chipPresentation(summary, t);

  return (
    <>
      <button
        type="button"
        onClick={() => setOpen(true)}
        className={`flex min-h-11 items-center gap-1.5 rounded-full px-2.5 py-1 text-xs font-medium ${tone}`}
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

function chipPresentation(
  summary: SyncStatusSummary,
  t: Messages,
): {
  label: string;
  tone: string;
  icon: React.ReactNode;
} {
  switch (summary.kind) {
    case 'offline':
      return {
        label:
          summary.waiting > 0 ? t.sync.chipOfflineWaiting(summary.waiting) : t.sync.chipOffline,
        tone: 'bg-danger/10 text-danger',
        icon: <CloudOff className="size-3.5" aria-hidden />,
      };
    case 'syncing':
      return {
        label: t.sync.chipSyncing,
        tone: 'bg-accent/10 text-accent',
        icon: <Loader2 className="size-3.5 animate-spin" aria-hidden />,
      };
    case 'auth_required':
      return {
        label: t.sync.chipAuth,
        tone: 'bg-warning/10 text-warning',
        icon: <AlertTriangle className="size-3.5" aria-hidden />,
      };
    case 'conflict':
      return {
        label: t.sync.chipConflict,
        tone: 'bg-warning/10 text-warning',
        icon: <AlertTriangle className="size-3.5" aria-hidden />,
      };
    case 'failed':
      return {
        label: t.sync.chipFailed,
        tone: 'bg-danger/10 text-danger',
        icon: <AlertTriangle className="size-3.5" aria-hidden />,
      };
    case 'waiting':
      return {
        label: t.sync.chipWaiting(summary.waiting),
        tone: 'bg-warning/10 text-warning',
        icon: <RefreshCw className="size-3.5" aria-hidden />,
      };
    case 'synced':
      return {
        label: t.sync.chipSynced,
        tone: 'bg-success/10 text-success',
        icon: <Check className="size-3.5" aria-hidden />,
      };
    case 'online':
      return {
        label: t.sync.chipOnline,
        tone: 'bg-success/10 text-success',
        icon: <Wifi className="size-3.5" aria-hidden />,
      };
  }
}

/** The honest offline strip shown under the header on every attendant screen. */
export function OfflineHint() {
  const t = useT();
  const state = useSyncEngineState();
  if (state.online) return null;
  return (
    <div className="mx-auto w-full max-w-md px-4 pt-2">
      <p className="rounded-md bg-warning/10 px-3 py-2 text-sm text-warning" role="status">
        {t.sync.offlineHint}
      </p>
    </div>
  );
}

/**
 * The translated label of a queued action, derived from action_type +
 * payload at display time (the queue stores codes/figures, not prose).
 * Records persisted before Phase 6b may lack payload context — they fall
 * back to their stored legacy label, then to the generic phrasing.
 */
function queuedActionLabel(item: QueuedAction, t: Messages): string {
  switch (item.action_type) {
    case 'check_in':
      return t.sync.actionCheckIn;
    case 'check_out':
      return t.sync.actionCheckOut;
    case 'confirm_assignment': {
      const { pump_number, nozzle_number } = item.payload;
      if (pump_number != null && nozzle_number != null) {
        return t.sync.actionConfirmAssignment(pump_number, nozzle_number);
      }
      return item.label ?? t.sync.actionConfirmAssignmentGeneric;
    }
    case 'opening_reading': {
      const { reading, pump_number, nozzle_number } = item.payload;
      if (pump_number != null && nozzle_number != null) {
        return t.sync.actionOpeningReading(reading, pump_number, nozzle_number);
      }
      return item.label ?? t.sync.actionOpeningReadingGeneric(reading);
    }
    case 'closing_reading': {
      const { reading, pump_number, nozzle_number } = item.payload;
      if (pump_number != null && nozzle_number != null) {
        return t.sync.actionClosingReading(reading, pump_number, nozzle_number);
      }
      return item.label ?? t.sync.actionClosingReadingGeneric(reading);
    }
    case 'collection':
      return t.sync.actionCollection;
  }
}

/**
 * The translated error text for a failed/conflict/transient row: coded
 * (client-classified) messages render from the dictionary; raw server prose
 * has no code and is shown verbatim (deliberately untranslated).
 */
function queueErrorText(item: QueuedAction, t: Messages): string | null {
  switch (item.error_code) {
    case 'opening_below_expected':
      return t.sync.errOpeningBelowExpected;
    case 'assignment_changed':
      return t.sync.errAssignmentChanged;
    case 'reading_conflict':
      return t.sync.errReadingConflict(
        item.error_params?.reading_type ?? 'opening',
        item.error_params?.server_value,
      );
    case 'collection_conflict':
      return t.sync.errCollectionConflict(item.error_params?.server_value ?? '—');
    case 'verify_unavailable':
      return t.sync.errVerifyUnavailable;
    default:
      return item.error_message ?? null;
  }
}

function SyncDetailsSheet({ onClose }: { onClose: () => void }) {
  const t = useT();
  const state = useSyncEngineState();
  const engine = getSyncEngine();
  const items = [...state.items].sort((a, b) => b.seq - a.seq);
  const panelRef = useSheetFocusTrap(onClose);

  return (
    <div
      className="fixed inset-0 z-50 flex items-end justify-center bg-black/50"
      role="dialog"
      aria-modal="true"
      aria-label={t.sync.sheetTitle}
    >
      <div
        ref={panelRef}
        className="flex max-h-[80vh] w-full max-w-md flex-col rounded-t-2xl border border-border bg-background"
      >
        <div className="flex items-center justify-between border-b border-border px-4 py-3">
          <h2 className="text-base font-semibold">{t.sync.sheetTitle}</h2>
          <button
            type="button"
            className="flex size-11 items-center justify-center rounded-md text-muted-foreground"
            onClick={onClose}
            aria-label={t.sync.sheetClose}
          >
            <X className="size-5" aria-hidden />
          </button>
        </div>

        <div className="flex flex-col gap-3 overflow-y-auto px-4 py-3">
          {state.phase === 'auth_required' ? (
            <p className="rounded-md bg-warning/10 px-3 py-2 text-sm text-warning" role="status">
              {t.sync.authNote}
            </p>
          ) : null}

          {items.length === 0 ? (
            <p className="py-6 text-center text-base text-muted-foreground" role="status">
              {t.sync.emptyQueue}
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
            {t.sync.syncNow}
          </Button>
        </div>
      </div>
    </div>
  );
}

function statusBadge(item: QueuedAction, t: Messages): { label: string; tone: string } {
  switch (item.sync_status) {
    case 'pending':
      return { label: t.sync.statusPending, tone: 'bg-warning/10 text-warning' };
    case 'syncing':
      return { label: t.sync.statusSyncing, tone: 'bg-accent/10 text-accent' };
    case 'synced':
      return { label: t.sync.statusSynced, tone: 'bg-success/10 text-success' };
    case 'failed':
      return { label: t.sync.statusFailed, tone: 'bg-danger/10 text-danger' };
    case 'conflict':
      return { label: t.sync.statusConflict, tone: 'bg-warning/10 text-warning' };
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
  const t = useT();
  // Discarding is the ONLY way queued data is ever dropped, so it takes an
  // explicit second tap to confirm (PRD §14.3).
  const [confirmingDiscard, setConfirmingDiscard] = useState(false);
  const badge = statusBadge(item, t);
  const errorText = queueErrorText(item, t);
  const actionable = item.sync_status === 'failed' || item.sync_status === 'conflict';

  return (
    <li className="flex flex-col gap-2 rounded-lg border border-border p-3">
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="text-sm font-medium">{queuedActionLabel(item, t)}</p>
          <p className="text-xs text-muted-foreground">
            {t.sync.savedAt(
              new Date(item.created_at_local).toLocaleString([], {
                hour: '2-digit',
                minute: '2-digit',
                day: '2-digit',
                month: 'short',
              }),
            )}
          </p>
        </div>
        <span
          className={`shrink-0 rounded-full px-2 py-0.5 text-xs font-medium ${badge.tone}`}
          role="status"
        >
          {badge.label}
        </span>
      </div>

      {errorText ? (
        <p className="rounded-md bg-danger/10 px-2.5 py-1.5 text-xs text-danger" role="alert">
          {errorText}
        </p>
      ) : null}

      {actionable ? (
        <div className="flex items-center gap-2">
          <Button variant="outline" className="h-10 flex-1 text-sm" onClick={onRetry}>
            <RefreshCw className="size-4" aria-hidden />
            {t.sync.tryAgain}
          </Button>
          {confirmingDiscard ? (
            <Button
              variant="outline"
              className="h-10 flex-1 border-danger/40 text-sm text-danger"
              onClick={onDiscard}
            >
              <Trash2 className="size-4" aria-hidden />
              {t.sync.discardConfirm}
            </Button>
          ) : (
            <Button
              variant="outline"
              className="h-10 flex-1 text-sm text-muted-foreground"
              onClick={() => setConfirmingDiscard(true)}
            >
              <Trash2 className="size-4" aria-hidden />
              {t.sync.discard}
            </Button>
          )}
        </div>
      ) : null}
    </li>
  );
}
