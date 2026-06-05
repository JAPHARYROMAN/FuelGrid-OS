'use client';

import * as React from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { ArrowLeft, Check, RefreshCw, Save } from 'lucide-react';

import { SdkError, type OpeningStockRequest, type StockMovement, type Tank } from '@fuelgrid/sdk';
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
import { usePermission, usePermissions } from '@/hooks/use-permissions';
import { api } from '@/lib/api';
import { formatLitres } from '@/lib/money';
import { toast } from '@/lib/toast';

// A tank's opening lifecycle state, derived from its latest opening-stock
// request plus any directly-posted opening movement (legacy seed path).
type OpeningState = 'none' | 'draft' | 'locked' | 'rejected';

interface OpeningStockRow {
  tank: Tank;
  // The tank's directly-posted opening movement (legacy seed), if any.
  opening?: StockMovement;
  // The tank's latest opening-stock lifecycle request, if any.
  request?: OpeningStockRequest;
  state: OpeningState;
}

function isOpeningMovement(m: StockMovement) {
  return (
    m.movement_type === 'opening' &&
    m.status === 'posted' &&
    (!m.source_ref_type || m.source_ref_type !== 'correction')
  );
}

function stateBadge(state: OpeningState): { tone: BadgeProps['tone']; label: string } {
  switch (state) {
    case 'locked':
      return { tone: 'success', label: 'Approved & locked' };
    case 'draft':
      return { tone: 'warning', label: 'Submitted' };
    case 'rejected':
      return { tone: 'danger', label: 'Rejected' };
    default:
      return { tone: 'neutral', label: 'Missing' };
  }
}

export default function OpeningStockPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = React.useState('');
  const [enterFor, setEnterFor] = React.useState<Tank | null>(null);
  const [decideFor, setDecideFor] = React.useState<{
    request: OpeningStockRequest;
    action: 'approve' | 'reject';
  } | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const products = useQuery({
    queryKey: ['products'],
    queryFn: ({ signal }) => api.listProducts(signal),
  });

  const effectiveStation = stationID || stations.data?.items[0]?.id || '';
  // The approve/reject controls require stock.approve_adjustment at the row's
  // station; entering a draft requires stock.adjust. Use the effective station
  // shown in the dropdown, including the initial first station selection.
  const canEnter = usePermission('stock.adjust', { stationID: effectiveStation || undefined });
  const canApprove = usePermission('stock.approve_adjustment', {
    stationID: effectiveStation || undefined,
  });
  const permissions = usePermissions();
  const canAdminOverride = permissions.data?.is_system_admin === true;
  const me = useQuery({
    queryKey: ['me'],
    queryFn: ({ signal }) => api.me(signal),
  });

  const rows = useQuery({
    queryKey: ['opening-stock', effectiveStation],
    enabled: Boolean(effectiveStation),
    queryFn: async ({ signal }) => {
      const tankPage = await api.listTanks({ stationID: effectiveStation }, signal);
      const items = await Promise.all(
        tankPage.items.map(async (tank) => {
          const [ledger, reqs] = await Promise.all([
            api.listTankLedger(tank.id, { limit: 100, offset: 0 }, signal),
            api.listOpeningStockRequests({ tank_id: tank.id, limit: 20, offset: 0 }, signal),
          ]);
          const opening = ledger.items.find(isOpeningMovement);
          // Requests come back newest-first; the live one (draft/approved) wins,
          // else the most recent (a rejection) drives the state.
          const live = reqs.items.find((r) => r.status === 'draft' || r.status === 'approved');
          const request = live ?? reqs.items[0];
          let state: OpeningState = 'none';
          if (opening || request?.status === 'approved') state = 'locked';
          else if (request?.status === 'draft') state = 'draft';
          else if (request?.status === 'rejected') state = 'rejected';
          return { tank, opening, request, state } satisfies OpeningStockRow;
        }),
      );
      return items;
    },
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['opening-stock', effectiveStation] });
    void qc.invalidateQueries({ queryKey: ['setup-checklist'] });
  }

  const productLookup = React.useMemo(
    () => new Map((products.data?.items ?? []).map((p) => [p.id, p])),
    [products.data],
  );

  const lockedCount = rows.data?.filter((r) => r.state === 'locked').length ?? 0;
  const totalTanks = rows.data?.length ?? 0;
  const allLocked = totalTanks > 0 && lockedCount === totalTanks;
  const noStations = (stations.data?.items.length ?? 0) === 0;

  const reviewOpeningStock = useMutation({
    mutationFn: () =>
      api.updateSetupStep(
        { step_code: 'opening_stock', status: 'completed' },
        { stationID: effectiveStation },
      ),
    onSuccess: (checklist) => {
      qc.setQueryData(['setup-checklist', effectiveStation], checklist);
    },
  });

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Setup"
        title="Opening stock"
        description="Enter each tank's opening balance as a draft; a supervisor approves it to post the ledger and lock it."
        actions={
          <>
            <div className="flex flex-col gap-1.5">
              <Label htmlFor="station">Station</Label>
              <select
                id="station"
                className="h-10 min-w-56 rounded-md border border-border bg-background px-3 text-sm"
                value={effectiveStation}
                onChange={(e) => setStationID(e.target.value)}
                disabled={noStations}
              >
                {(stations.data?.items ?? []).map((st) => (
                  <option key={st.id} value={st.id}>
                    {st.name} ({st.code})
                  </option>
                ))}
              </select>
            </div>
            <Badge tone={allLocked ? 'success' : 'neutral'}>
              {lockedCount} / {totalTanks} locked
            </Badge>
            <Button asChild variant="ghost">
              <Link href="/setup">
                <ArrowLeft className="size-4" />
                Setup
              </Link>
            </Button>
          </>
        }
      />

      {allLocked ? (
        <Card>
          <CardContent className="flex flex-wrap items-center gap-3 py-5">
            <span className="flex size-10 items-center justify-center rounded-full bg-success/15 text-success">
              <Check className="size-5" />
            </span>
            <div className="flex min-w-0 flex-1 flex-col">
              <p className="font-medium text-foreground">Opening stock is complete</p>
              <p className="text-sm text-muted-foreground">
                Every tank at this station has an approved, locked opening movement.
              </p>
            </div>
            <Button
              variant="ghost"
              onClick={() => reviewOpeningStock.mutate()}
              disabled={reviewOpeningStock.isPending}
            >
              <Check className="size-4" />
              {reviewOpeningStock.isPending ? 'Saving...' : 'Review step'}
            </Button>
          </CardContent>
        </Card>
      ) : null}

      {noStations ? (
        <EmptyState
          title="No stations yet"
          description="Create a station before setting opening stock."
        />
      ) : rows.isPending || stations.isPending ? (
        <Card>
          <CardContent className="flex flex-col gap-2 p-4">
            {Array.from({ length: 4 }).map((_, i) => (
              <Skeleton key={i} className="h-14 rounded-lg" />
            ))}
          </CardContent>
        </Card>
      ) : rows.isError ? (
        <ErrorState
          title="Couldn't load opening stock"
          description={String((rows.error as Error).message)}
          onRetry={() => rows.refetch()}
        />
      ) : totalTanks === 0 ? (
        <EmptyState
          title="No tanks at this station"
          description="Create tanks before setting opening stock."
          action={
            <Button asChild>
              <Link href="/settings/tanks">Add tanks</Link>
            </Button>
          }
        />
      ) : (
        <Card>
          <CardContent className="p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Tank</TableHead>
                  <TableHead>Product</TableHead>
                  <TableHead className="text-right">Opening litres</TableHead>
                  <TableHead>State</TableHead>
                  <TableHead>Notes</TableHead>
                  <TableHead className="text-right">Action</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.data!.map((row) => {
                  const product = productLookup.get(row.tank.product_id);
                  const badge = stateBadge(row.state);
                  const litres = row.request?.litres ?? row.opening?.litres;
                  const requestedByMe =
                    row.request?.requested_by != null &&
                    row.request.requested_by === me.data?.user_id;
                  const selfApprovalBlocked = requestedByMe && !canAdminOverride;
                  return (
                    <TableRow key={row.tank.id}>
                      <TableCell>
                        <div className="flex flex-col">
                          <span className="font-medium">{row.tank.name}</span>
                          <span className="font-mono text-xs text-muted-foreground">
                            {row.tank.code}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <span className="inline-flex items-center gap-2">
                          <span
                            className="inline-block size-3 rounded-full border border-border"
                            style={{ backgroundColor: product?.color ?? '#64748b' }}
                            aria-hidden
                          />
                          {product?.name ?? 'Product'}
                        </span>
                      </TableCell>
                      <TableCell className="text-right font-mono tabular-nums">
                        {litres != null ? formatLitres(litres) : '—'}
                      </TableCell>
                      <TableCell>
                        <Badge tone={badge.tone}>{badge.label}</Badge>
                      </TableCell>
                      <TableCell className="min-w-52 text-sm text-muted-foreground">
                        {row.state === 'draft' && selfApprovalBlocked
                          ? 'Submitted by you; another user must approve.'
                          : row.state === 'draft' && requestedByMe
                            ? 'Submitted by you; admin override available.'
                            : row.state === 'rejected' && row.request?.decision_note
                              ? `Rejected: ${row.request.decision_note}`
                              : (row.request?.notes ?? row.opening?.notes ?? '—')}
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex justify-end gap-2">
                          {row.state === 'draft' && row.request ? (
                            <>
                              <PermissionGate
                                permission="stock.approve_adjustment"
                                stationId={effectiveStation}
                              >
                                <Button
                                  size="sm"
                                  variant="secondary"
                                  disabled={selfApprovalBlocked}
                                  title={
                                    selfApprovalBlocked
                                      ? 'Another user must approve a request you submitted'
                                      : undefined
                                  }
                                  onClick={() =>
                                    setDecideFor({ request: row.request!, action: 'approve' })
                                  }
                                >
                                  Approve
                                </Button>
                              </PermissionGate>
                              <PermissionGate
                                permission="stock.approve_adjustment"
                                stationId={effectiveStation}
                              >
                                <Button
                                  size="sm"
                                  variant="ghost"
                                  disabled={selfApprovalBlocked}
                                  title={
                                    selfApprovalBlocked
                                      ? 'Another user must reject a request you submitted'
                                      : undefined
                                  }
                                  onClick={() =>
                                    setDecideFor({ request: row.request!, action: 'reject' })
                                  }
                                >
                                  Reject
                                </Button>
                              </PermissionGate>
                            </>
                          ) : row.state === 'locked' ? (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => rows.refetch()}
                              title="Refresh"
                            >
                              <RefreshCw className="size-4" />
                            </Button>
                          ) : (
                            <PermissionGate permission="stock.adjust" stationId={effectiveStation}>
                              <Button size="sm" onClick={() => setEnterFor(row.tank)}>
                                <Save className="size-4" />
                                {row.state === 'rejected' ? 'Re-enter' : 'Enter'}
                              </Button>
                            </PermissionGate>
                          )}
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

      {canEnter === false && canApprove === false ? (
        <p className="text-xs text-muted-foreground">
          You have read-only access to opening stock at this station. Entering a draft requires
          stock.adjust and approving requires stock.approve_adjustment.
        </p>
      ) : null}

      <EnterOpeningDialog
        tank={enterFor}
        onOpenChange={(open) => {
          if (!open) setEnterFor(null);
        }}
        canEnter={canEnter === true}
        onSaved={() => {
          setEnterFor(null);
          invalidate();
        }}
      />
      <DecideDialog
        decision={decideFor}
        onOpenChange={(open) => {
          if (!open) setDecideFor(null);
        }}
        canApprove={canApprove === true}
        canAdminOverride={canAdminOverride}
        currentUserID={me.data?.user_id}
        onSaved={() => {
          setDecideFor(null);
          invalidate();
        }}
      />
    </div>
  );
}

function EnterOpeningDialog({
  tank,
  onOpenChange,
  canEnter,
  onSaved,
}: {
  tank: Tank | null;
  onOpenChange: (open: boolean) => void;
  canEnter: boolean;
  onSaved: () => void;
}) {
  const [litres, setLitres] = React.useState('');
  const [notes, setNotes] = React.useState('');
  const [error, setError] = React.useState<string | null>(null);

  const open = tank !== null;
  React.useEffect(() => {
    if (open) {
      setLitres('');
      setNotes('');
      setError(null);
    }
  }, [open]);

  const enter = useMutation({
    mutationFn: () =>
      api.requestOpeningStock({
        tank_id: tank!.id,
        litres: litres.trim(),
        notes: notes.trim() || undefined,
      }),
    onSuccess: () => {
      toast.success('Opening stock submitted for approval');
      onSaved();
    },
    onError: (err) =>
      toast.error(
        'Could not submit opening stock',
        err instanceof SdkError ? err.message : undefined,
      ),
  });

  function submit() {
    const v = litres.trim();
    if (!v || Number.isNaN(Number(v)) || Number(v) < 0) {
      setError('Litres must be a non-negative decimal');
      return;
    }
    setError(null);
    enter.mutate();
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Enter opening stock</DialogTitle>
          <DialogDescription>
            {tank ? `${tank.name} (${tank.code})` : ''} — submitted as a draft for supervisor
            approval.
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            submit();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="opening-litres">Opening litres</Label>
            <Input
              id="opening-litres"
              type="number"
              min="0"
              step="0.001"
              value={litres}
              onChange={(e) => setLitres(e.target.value)}
              placeholder="0.000"
            />
            {error ? <span className="text-xs text-danger">{error}</span> : null}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="opening-notes">Notes</Label>
            <Input
              id="opening-notes"
              value={notes}
              onChange={(e) => setNotes(e.target.value)}
              placeholder="Optional"
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!canEnter || enter.isPending}>
              {enter.isPending ? 'Submitting…' : 'Submit for approval'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

function DecideDialog({
  decision,
  onOpenChange,
  canApprove,
  canAdminOverride,
  currentUserID,
  onSaved,
}: {
  decision: { request: OpeningStockRequest; action: 'approve' | 'reject' } | null;
  onOpenChange: (open: boolean) => void;
  canApprove: boolean;
  canAdminOverride: boolean;
  currentUserID?: string;
  onSaved: () => void;
}) {
  const [note, setNote] = React.useState('');
  const open = decision !== null;
  const isReject = decision?.action === 'reject';
  const requestedByMe =
    decision?.request.requested_by != null && decision.request.requested_by === currentUserID;
  const selfApprovalBlocked = requestedByMe && !canAdminOverride;

  React.useEffect(() => {
    if (open) setNote('');
  }, [open]);

  const decide = useMutation({
    mutationFn: () => {
      const id = decision!.request.id;
      const body = note.trim() ? { note: note.trim() } : {};
      return isReject ? api.rejectOpeningStock(id, body) : api.approveOpeningStock(id, body);
    },
    onSuccess: () => {
      toast.success(isReject ? 'Opening stock rejected' : 'Opening stock approved & locked');
      onSaved();
    },
    onError: (err) =>
      toast.error('Could not decide', err instanceof SdkError ? err.message : undefined),
  });

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{isReject ? 'Reject opening stock' : 'Approve opening stock'}</DialogTitle>
          <DialogDescription>
            {selfApprovalBlocked
              ? 'Another user must approve or reject a request you submitted.'
              : requestedByMe
                ? 'Approving uses your system admin override and records that in audit.'
                : isReject
                  ? 'Record a reason; the tank can then have a corrected figure re-entered.'
                  : 'Approving posts the genesis opening movement and locks the request.'}
          </DialogDescription>
        </DialogHeader>
        <form
          className="flex flex-col gap-4"
          onSubmit={(e) => {
            e.preventDefault();
            decide.mutate();
          }}
        >
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="decide-note">{isReject ? 'Reason' : 'Note (optional)'}</Label>
            <Input
              id="decide-note"
              required={isReject}
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder={isReject ? 'Why is this rejected?' : 'Optional'}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button
              type="submit"
              variant={isReject ? 'secondary' : 'primary'}
              disabled={
                selfApprovalBlocked || !canApprove || decide.isPending || (isReject && !note.trim())
              }
            >
              {decide.isPending ? 'Saving…' : isReject ? 'Reject' : 'Approve & lock'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
