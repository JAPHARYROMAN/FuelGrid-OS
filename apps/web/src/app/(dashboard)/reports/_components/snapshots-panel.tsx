'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Camera, Lock, RotateCcw, ShieldCheck } from 'lucide-react';

import { SdkError, type ReportSnapshot } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  Input,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { toast } from '@/lib/toast';

/**
 * SnapshotsPanel (Reports Center Phase 14 — blueprint §15) is the per-report
 * locking surface: it CAPTURES an immutable snapshot of the current report, lists
 * the revision chain, and drives the sign-off / reopen workflow — every button
 * permission-gated by the same `permitted` flag the report page computes (a
 * snapshot must require the same permission as running the report live).
 *
 * It also surfaces the LOCK STATE: when a signed-off snapshot exists for the
 * report/scope, a lock badge is shown. The panel is scope-aware — for a
 * station-scoped report it passes `stationId` so the snapshots, lock state and
 * revision chain all match the report's scope.
 */
export function SnapshotsPanel({
  reportKey,
  stationId,
  filters,
  permitted,
}: {
  reportKey: string;
  /** The station the report is scoped to (omit for tenant-wide reports). */
  stationId?: string;
  /** The report's full filter set, captured verbatim with the snapshot. */
  filters: Record<string, string>;
  /**
   * Whether the actor may run this report live (null = still loading). The same
   * gate the report endpoint enforces; capture/sign-off/reopen are hidden when
   * the actor cannot run the report.
   */
  permitted: boolean | null;
}) {
  const qc = useQueryClient();
  const [reopenTarget, setReopenTarget] = React.useState<ReportSnapshot | null>(null);

  const enabled = permitted !== false && (!stationId || stationId.length > 0);
  const listKey = ['report-snapshots', reportKey, stationId ?? ''];
  const lockKey = ['report-lock-state', reportKey, stationId ?? ''];

  const snapshots = useQuery({
    queryKey: listKey,
    queryFn: ({ signal }) =>
      api.listReportSnapshots(reportKey, stationId ? { stationID: stationId } : undefined, signal),
    enabled,
    retry: false,
  });

  const lock = useQuery({
    queryKey: lockKey,
    queryFn: ({ signal }) =>
      api.getReportLockState(reportKey, stationId ? { stationID: stationId } : undefined, signal),
    enabled,
    retry: false,
  });

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: listKey });
    void qc.invalidateQueries({ queryKey: lockKey });
    void qc.invalidateQueries({ queryKey: ['reports-hub', 'locked'] });
  };

  const capture = useMutation({
    mutationFn: () => api.captureReportSnapshot(reportKey, { filters }),
    onSuccess: (snap) => {
      toast.success(`Snapshot captured (revision ${snap.revision})`);
      invalidate();
    },
    onError: (e) => toast.error('Could not capture snapshot', describeError(e)),
  });

  const signOff = useMutation({
    mutationFn: (id: string) => api.signOffReportSnapshot(id),
    onSuccess: () => {
      toast.success('Snapshot signed off');
      invalidate();
    },
    onError: (e) => toast.error('Could not sign off snapshot', describeError(e)),
  });

  const reopen = useMutation({
    mutationFn: (args: { id: string; note: string }) =>
      api.reopenReportSnapshot(args.id, { correction_note: args.note }),
    onSuccess: () => {
      toast.success('Snapshot reopened — capture a new revision to correct it');
      setReopenTarget(null);
      invalidate();
    },
    onError: (e) => toast.error('Could not reopen snapshot', describeError(e)),
  });

  if (permitted === false) return null;

  const items = snapshots.data?.items ?? [];
  const busy = capture.isPending || signOff.isPending || reopen.isPending;

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="flex items-center gap-2 text-base">
          <Camera className="size-4 text-accent" />
          Snapshots & locking
        </CardTitle>
        {lock.data?.locked ? (
          <Badge tone="info">
            <Lock className="mr-1 size-3" />
            Locked
          </Badge>
        ) : null}
      </CardHeader>
      <CardContent className="flex flex-col gap-3">
        <p className="text-xs text-muted-foreground">
          Capture an immutable copy of this report. A signed-off snapshot is a tamper-evident,
          point-in-time record; corrections create a new revision.
        </p>

        <Button
          size="sm"
          onClick={() => capture.mutate()}
          disabled={busy || permitted === null || (!!stationId && stationId.length === 0)}
        >
          <Camera className="size-3.5" />
          {capture.isPending ? 'Capturing…' : 'Capture snapshot'}
        </Button>

        {snapshots.isPending ? (
          <>
            <Skeleton className="h-10 rounded-lg" />
            <Skeleton className="h-10 rounded-lg" />
          </>
        ) : snapshots.error instanceof SdkError && snapshots.error.status === 403 ? (
          <p className="rounded-lg border border-dashed border-border/70 px-3 py-3 text-xs text-muted-foreground">
            You don&apos;t have access to this report&apos;s snapshots.
          </p>
        ) : items.length === 0 ? (
          <p className="rounded-lg border border-dashed border-border/70 px-3 py-3 text-xs text-muted-foreground">
            No snapshots captured yet.
          </p>
        ) : (
          <ul className="flex flex-col gap-2">
            {items.map((s) => (
              <SnapshotRow
                key={s.id}
                snap={s}
                busy={busy}
                onSignOff={() => signOff.mutate(s.id)}
                onReopen={() => setReopenTarget(s)}
              />
            ))}
          </ul>
        )}
      </CardContent>

      <ReopenDialog
        target={reopenTarget}
        pending={reopen.isPending}
        onCancel={() => setReopenTarget(null)}
        onConfirm={(note) => reopenTarget && reopen.mutate({ id: reopenTarget.id, note })}
      />
    </Card>
  );
}

/** One snapshot in the revision chain: revision, status, captured time + actions. */
function SnapshotRow({
  snap,
  busy,
  onSignOff,
  onReopen,
}: {
  snap: ReportSnapshot;
  busy: boolean;
  onSignOff: () => void;
  onReopen: () => void;
}) {
  return (
    <li className="flex items-center justify-between gap-2 rounded-lg border border-border/70 px-3 py-2">
      <div className="flex min-w-0 flex-col">
        <span className="flex items-center gap-2 text-sm font-medium text-foreground">
          Revision {snap.revision}
          <StatusBadge status={snap.status} />
        </span>
        <span className="truncate font-mono text-[11px] text-muted-foreground">
          {snap.content_hash.slice(0, 12)}… · {shortTime(snap.captured_at)}
        </span>
      </div>
      <div className="flex shrink-0 items-center gap-1.5">
        {snap.status === 'signed_off' ? (
          <Button size="sm" variant="ghost" disabled={busy} onClick={onReopen}>
            <RotateCcw className="size-3.5" />
            Reopen
          </Button>
        ) : (
          <Button size="sm" variant="outline" disabled={busy} onClick={onSignOff}>
            <ShieldCheck className="size-3.5" />
            Sign off
          </Button>
        )}
      </div>
    </li>
  );
}

function StatusBadge({ status }: { status: ReportSnapshot['status'] }) {
  const tone = status === 'signed_off' ? 'success' : status === 'reopened' ? 'warning' : 'neutral';
  const label =
    status === 'signed_off' ? 'Signed off' : status === 'reopened' ? 'Reopened' : 'Draft';
  return <Badge tone={tone}>{label}</Badge>;
}

/** The reopen dialog: a correction note is required to reopen a signed-off snapshot. */
function ReopenDialog({
  target,
  pending,
  onCancel,
  onConfirm,
}: {
  target: ReportSnapshot | null;
  pending: boolean;
  onCancel: () => void;
  onConfirm: (note: string) => void;
}) {
  const [note, setNote] = React.useState('');
  React.useEffect(() => {
    if (target) setNote('');
  }, [target]);

  return (
    <Dialog open={!!target} onOpenChange={(open) => !open && onCancel()}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Reopen snapshot</DialogTitle>
          <DialogDescription>
            The original snapshot stays immutable. Reopening records a correction note and lets you
            capture a new revision. A note is required.
          </DialogDescription>
        </DialogHeader>
        <Input
          autoFocus
          placeholder="Correction note (e.g. restate the day's cash variance)"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          aria-label="Correction note"
        />
        <DialogFooter>
          <Button variant="ghost" onClick={onCancel} disabled={pending}>
            Cancel
          </Button>
          <Button onClick={() => onConfirm(note.trim())} disabled={pending || note.trim() === ''}>
            {pending ? 'Reopening…' : 'Reopen'}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}

function describeError(e: unknown): string | undefined {
  if (e instanceof SdkError) return e.message;
  if (e instanceof Error) return e.message;
  return undefined;
}

function shortTime(iso: string): string {
  const s = String(iso ?? '');
  return s.length >= 16 ? s.slice(0, 16).replace('T', ' ') : s;
}
