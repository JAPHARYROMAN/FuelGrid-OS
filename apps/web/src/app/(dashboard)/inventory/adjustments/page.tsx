'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { SlidersHorizontal } from 'lucide-react';

import {
  SdkError,
  type StockAdjustment,
  type StockAdjustmentClassification,
  type Tank,
} from '@fuelgrid/sdk';
import {
  Badge,
  type BadgeProps,
  Button,
  Card,
  CardContent,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  Input,
  Label,
  PageHeader,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { usePermission } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { formatLitres } from '@/lib/money';
import { toast } from '@/lib/toast';

const STATUS_FILTERS = ['', 'requested', 'approved', 'posted', 'rejected'] as const;

const CLASSIFICATIONS: { value: StockAdjustmentClassification; label: string }[] = [
  { value: 'evaporation', label: 'Evaporation' },
  { value: 'measurement_error', label: 'Measurement error' },
  { value: 'theft', label: 'Theft / loss' },
  { value: 'spillage', label: 'Spillage' },
  { value: 'temperature', label: 'Temperature' },
  { value: 'data_entry', label: 'Data entry' },
  { value: 'other', label: 'Other' },
];

const CLASSIFICATION_LABELS: Record<string, string> = Object.fromEntries(
  CLASSIFICATIONS.map((c) => [c.value, c.label]),
);

function statusTone(status: string): BadgeProps['tone'] {
  switch (status) {
    case 'posted':
      return 'success';
    case 'approved':
      return 'info';
    case 'requested':
      return 'warning';
    case 'rejected':
      return 'danger';
    default:
      return 'neutral';
  }
}

// Litre deltas are exact decimal strings; show the sign explicitly so a
// reduction reads unambiguously.
function fmtDelta(litres: string) {
  const positive = !litres.trim().startsWith('-');
  return `${positive ? '+' : ''}${formatLitres(litres, { fallback: '0' })} L`;
}

export default function StockAdjustmentsPage() {
  const qc = useQueryClient();
  const [status, setStatus] = React.useState<string>('');
  const [createOpen, setCreateOpen] = React.useState(false);

  const list = useQuery({
    queryKey: ['stock-adjustments', status],
    queryFn: ({ signal }) => api.listStockAdjustments({ status: status || undefined }, signal),
  });

  // Tanks back the create selector and let us resolve each adjustment's station
  // for the station-scoped permission gates (the API authorizes per tank's
  // station; the UI mirrors that so controls aren't clickable when they'd 403).
  const tanks = useQuery({
    queryKey: ['tanks'],
    queryFn: ({ signal }) => api.listTanks({}, signal),
  });
  const stationByTank = React.useMemo(() => {
    const m = new Map<string, string>();
    for (const t of tanks.data?.items ?? []) m.set(t.id, t.station_id);
    return m;
  }, [tanks.data]);

  // Coarse "can do anything here" hints for the read-only footer. The
  // per-action gates below are station-scoped and authoritative.
  const canRequestAny = usePermission('stock.adjust');
  const canApproveAny = usePermission('stock.approve_adjustment');

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['stock-adjustments'] });
  }

  const approve = useMutation({
    mutationFn: (id: string) => api.approveStockAdjustment(id),
    onSuccess: () => {
      invalidate();
      toast.success('Adjustment approved');
    },
    onError: (err) =>
      toast.error('Could not approve', err instanceof SdkError ? err.message : undefined),
  });

  const reject = useMutation({
    mutationFn: (id: string) => api.rejectStockAdjustment(id),
    onSuccess: () => {
      invalidate();
      toast.success('Adjustment rejected');
    },
    onError: (err) =>
      toast.error('Could not reject', err instanceof SdkError ? err.message : undefined),
  });

  const post = useMutation({
    mutationFn: (id: string) => api.postStockAdjustment(id),
    onSuccess: () => {
      invalidate();
      toast.success('Adjustment posted to ledger');
    },
    onError: (err) =>
      toast.error('Could not post', err instanceof SdkError ? err.message : undefined),
  });

  const items = list.data?.items ?? [];
  const busy = approve.isPending || reject.isPending || post.isPending;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Operations · Inventory"
        title="Stock adjustments"
        description="Manual book-stock corrections under separation of duties: request a signed litre delta with a reason, a different user approves, then it posts to the tank ledger."
        actions={
          <PermissionGate permission="stock.adjust" mode="hide">
            <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
              New adjustment
            </Button>
          </PermissionGate>
        }
      />

      <div className="flex flex-wrap items-center gap-2">
        {STATUS_FILTERS.map((s) => (
          <Button
            key={s || 'all'}
            type="button"
            size="sm"
            variant={status === s ? 'primary' : 'secondary'}
            onClick={() => setStatus(s)}
          >
            {s === '' ? 'All' : s.charAt(0).toUpperCase() + s.slice(1)}
          </Button>
        ))}
      </div>

      {list.isPending ? (
        <div className="flex flex-col gap-2">
          {Array.from({ length: 6 }).map((_, i) => (
            <Skeleton key={i} className="h-14 rounded-lg" />
          ))}
        </div>
      ) : list.isError ? (
        (() => {
          const forbidden = list.error instanceof SdkError && list.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load adjustments"}
              description={
                forbidden
                  ? "You don't have permission to view stock adjustments (stock.adjust)."
                  : String((list.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => list.refetch()}
            />
          );
        })()
      ) : items.length === 0 ? (
        <EmptyState
          title="No adjustments"
          description={
            status ? `No ${status} adjustments.` : 'Requested stock adjustments will appear here.'
          }
          icon={<SlidersHorizontal />}
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Requested</TableHead>
                  <TableHead className="text-right">Delta</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead>Classification</TableHead>
                  <TableHead className="text-right">Book after</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((a: StockAdjustment) => {
                  const stationId = stationByTank.get(a.tank_id) ?? null;
                  return (
                    <TableRow key={a.id}>
                      <TableCell className="whitespace-nowrap font-mono text-xs">
                        {a.requested_at.slice(0, 10)}
                      </TableCell>
                      <TableCell className="text-right font-mono font-medium tabular-nums">
                        {fmtDelta(a.delta_litres)}
                      </TableCell>
                      <TableCell className="max-w-[18rem] truncate" title={a.reason}>
                        {a.reason}
                      </TableCell>
                      <TableCell className="text-muted-foreground">
                        {CLASSIFICATION_LABELS[a.classification] ?? a.classification}
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums text-muted-foreground">
                        {a.balance_after != null
                          ? `${formatLitres(a.balance_after, { fallback: '0' })} L`
                          : '—'}
                      </TableCell>
                      <TableCell>
                        <Badge tone={statusTone(a.status)}>{a.status}</Badge>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          {a.status === 'requested' ? (
                            <>
                              <PermissionGate
                                permission="stock.approve_adjustment"
                                stationId={stationId}
                              >
                                <Button
                                  type="button"
                                  variant="secondary"
                                  size="sm"
                                  disabled={busy}
                                  onClick={() => approve.mutate(a.id)}
                                >
                                  Approve
                                </Button>
                              </PermissionGate>
                              <PermissionGate
                                permission="stock.approve_adjustment"
                                stationId={stationId}
                              >
                                <Button
                                  type="button"
                                  variant="outline"
                                  size="sm"
                                  disabled={busy}
                                  onClick={() => reject.mutate(a.id)}
                                >
                                  Reject
                                </Button>
                              </PermissionGate>
                            </>
                          ) : null}
                          {a.status === 'approved' ? (
                            <PermissionGate
                              permission="stock.approve_adjustment"
                              stationId={stationId}
                            >
                              <Button
                                type="button"
                                size="sm"
                                disabled={busy}
                                onClick={() => post.mutate(a.id)}
                              >
                                Post
                              </Button>
                            </PermissionGate>
                          ) : null}
                          {a.status === 'posted' ? (
                            <span className="text-xs text-muted-foreground">Posted to ledger</span>
                          ) : null}
                          {a.status === 'rejected' ? (
                            <span className="text-xs text-muted-foreground">Rejected</span>
                          ) : null}
                        </div>
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <RequestAdjustmentDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        tanks={tanks.data?.items ?? []}
        onCreated={() => {
          setCreateOpen(false);
          invalidate();
        }}
      />

      {canRequestAny === false && canApproveAny === false ? (
        <p className="text-xs text-muted-foreground">
          You have read-only access to stock adjustments. Requesting needs stock.adjust; approving
          and posting need stock.approve_adjustment at the tank&apos;s station.
        </p>
      ) : null}
    </div>
  );
}

function RequestAdjustmentDialog({
  open,
  onOpenChange,
  tanks,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  tanks: Tank[];
  onCreated: () => void;
}) {
  const [tankID, setTankID] = React.useState('');
  const [delta, setDelta] = React.useState('');
  const [reason, setReason] = React.useState('');
  const [classification, setClassification] =
    React.useState<StockAdjustmentClassification>('evaporation');

  // Resolve the chosen tank's station so the gate matches what the API enforces.
  const stationId = tanks.find((t) => t.id === tankID)?.station_id ?? null;
  const canRequest = usePermission('stock.adjust', { stationID: stationId });

  const create = useMutation({
    mutationFn: () =>
      api.requestStockAdjustment({
        tank_id: tankID,
        delta_litres: delta.trim(),
        reason: reason.trim(),
        classification,
      }),
    onSuccess: () => {
      toast.success('Adjustment requested');
      setDelta('');
      setReason('');
      setTankID('');
      onCreated();
    },
    onError: (err) =>
      toast.error(
        'Could not request adjustment',
        err instanceof SdkError ? err.message : undefined,
      ),
  });

  // delta must be a non-zero signed decimal (mirrors the backend validator).
  const deltaValid = /^-?\d+(\.\d{1,3})?$/.test(delta.trim()) && Number(delta) !== 0;
  const ready = !!tankID && deltaValid && reason.trim().length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New stock adjustment</DialogTitle>
          <DialogDescription>
            Request a signed litre correction to a tank&apos;s book stock. It then moves through
            approve → post by a different user.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (ready) create.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="tank">Tank</Label>
            <select
              id="tank"
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={tankID}
              onChange={(e) => setTankID(e.target.value)}
              required
            >
              <option value="" disabled>
                Select a tank…
              </option>
              {tanks.map((t) => (
                <option key={t.id} value={t.id}>
                  {t.name} ({t.code})
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="delta">Delta litres (signed)</Label>
            <Input
              id="delta"
              inputMode="text"
              placeholder="e.g. -250 or 100"
              required
              value={delta}
              onChange={(e) => setDelta(e.target.value)}
            />
            <span className="text-xs text-muted-foreground">
              Negative reduces book stock, positive increases it.
            </span>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="classification">Classification</Label>
            <select
              id="classification"
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={classification}
              onChange={(e) => setClassification(e.target.value as StockAdjustmentClassification)}
            >
              {CLASSIFICATIONS.map((c) => (
                <option key={c.value} value={c.value}>
                  {c.label}
                </option>
              ))}
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="reason">Reason</Label>
            <Input
              id="reason"
              placeholder="Why is the book stock being corrected?"
              required
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              disabled={!ready || canRequest === false || create.isPending}
              title={
                canRequest === false
                  ? "You don't have permission at this tank's station"
                  : undefined
              }
            >
              {create.isPending ? 'Requesting…' : 'Request adjustment'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
