'use client';

import * as React from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Archive } from 'lucide-react';

import {
  SdkError,
  type AccountingPeriod,
  type ClosedPeriodChangeRequest,
  type ClosedPeriodChangeType,
  type JobRun,
  type RetentionPolicy,
  type RetentionScope,
} from '@fuelgrid/sdk';
import {
  Badge,
  type BadgeProps,
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
import { toast } from '@/lib/toast';

// Real backend permission codes (services/api/migrations/0090 + server_routes.go).
const RETENTION_MANAGE = 'retention.manage';
const CLOSED_PERIOD_CHANGE = 'closed_period.change';

const SCOPES: { value: RetentionScope; label: string }[] = [
  { value: 'audit', label: 'Audit logs' },
  { value: 'session', label: 'Sessions' },
  { value: 'export', label: 'Exports' },
];
const SCOPE_LABELS: Record<string, string> = Object.fromEntries(
  SCOPES.map((s) => [s.value, s.label]),
);

function statusTone(status: string): BadgeProps['tone'] {
  switch (status) {
    case 'active':
    case 'approved':
    case 'success':
      return 'success';
    case 'disabled':
    case 'rejected':
    case 'failure':
      return 'danger';
    case 'requested':
    case 'running':
      return 'warning';
    default:
      return 'neutral';
  }
}

export default function RetentionGovernancePage() {
  const canManage = usePermission(RETENTION_MANAGE);
  const canChangePeriods = usePermission(CLOSED_PERIOD_CHANGE);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Governance"
        title="Data lifecycle & retention"
        description="Declare how long each data scope is kept, watch the retention sweep, and run the maker-checker workflow to reopen or relock a closed accounting period."
      />

      <RetentionPolicies />
      <RetentionJobRuns />
      <ClosedPeriodChanges />

      {canManage === false && canChangePeriods === false ? (
        <p className="text-xs text-muted-foreground">
          You have read-only access here. Managing retention policies needs retention.manage;
          requesting or approving closed-period changes needs closed_period.change.
        </p>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Retention policies
// ---------------------------------------------------------------------------

function RetentionPolicies() {
  const qc = useQueryClient();
  const [createOpen, setCreateOpen] = React.useState(false);

  const list = useQuery({
    queryKey: ['retention-policies'],
    queryFn: ({ signal }) => api.listRetentionPolicies(signal),
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['retention-policies'] });
    void qc.invalidateQueries({ queryKey: ['retention-job-runs'] });
  }

  const remove = useMutation({
    mutationFn: (id: string) => api.deleteRetentionPolicy(id),
    onSuccess: () => {
      invalidate();
      toast.success('Retention policy deleted');
    },
    onError: (err) =>
      toast.error('Could not delete', err instanceof SdkError ? err.message : undefined),
  });

  const toggle = useMutation({
    mutationFn: ({ id, status }: { id: string; status: 'active' | 'disabled' }) =>
      api.updateRetentionPolicy(id, { status }),
    onSuccess: () => {
      invalidate();
      toast.success('Retention policy updated');
    },
    onError: (err) =>
      toast.error('Could not update', err instanceof SdkError ? err.message : undefined),
  });

  const items = (list.data?.items ?? []) as RetentionPolicy[];
  const busy = remove.isPending || toggle.isPending;

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3">
        <CardTitle>Retention policies</CardTitle>
        <PermissionGate permission={RETENTION_MANAGE} mode="hide">
          <Button type="button" size="sm" onClick={() => setCreateOpen(true)}>
            New policy
          </Button>
        </PermissionGate>
      </CardHeader>
      <CardContent className="p-0">
        {list.isPending ? (
          <div className="flex flex-col gap-2 p-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        ) : list.isError ? (
          (() => {
            const forbidden = list.error instanceof SdkError && list.error.status === 403;
            return (
              <ErrorState
                title={forbidden ? 'No access' : "Couldn't load policies"}
                description={
                  forbidden
                    ? "You don't have permission to view retention policies (retention.manage)."
                    : String((list.error as Error).message)
                }
                onRetry={forbidden ? undefined : () => list.refetch()}
              />
            );
          })()
        ) : items.length === 0 ? (
          <EmptyState
            title="No retention policies"
            description="Declare a retention window per data scope (audit, session, export)."
            icon={<Archive />}
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Scope</TableHead>
                <TableHead className="text-right">Retention (days)</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((p) => (
                <TableRow key={p.id}>
                  <TableCell>{SCOPE_LABELS[p.scope] ?? p.scope}</TableCell>
                  <TableCell className="text-right font-mono tabular-nums">
                    {p.retention_days}
                  </TableCell>
                  <TableCell>
                    <Badge tone={statusTone(p.status)}>{p.status}</Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      <PermissionGate permission={RETENTION_MANAGE}>
                        <Button
                          type="button"
                          variant="secondary"
                          size="sm"
                          disabled={busy}
                          onClick={() =>
                            toggle.mutate({
                              id: p.id,
                              status: p.status === 'active' ? 'disabled' : 'active',
                            })
                          }
                        >
                          {p.status === 'active' ? 'Disable' : 'Enable'}
                        </Button>
                      </PermissionGate>
                      <PermissionGate permission={RETENTION_MANAGE}>
                        <Button
                          type="button"
                          variant="outline"
                          size="sm"
                          disabled={busy}
                          onClick={() => remove.mutate(p.id)}
                        >
                          Delete
                        </Button>
                      </PermissionGate>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
      <CreatePolicyDialog
        open={createOpen}
        onOpenChange={setCreateOpen}
        existingScopes={items.map((p) => p.scope)}
        onCreated={() => {
          setCreateOpen(false);
          invalidate();
        }}
      />
    </Card>
  );
}

function CreatePolicyDialog({
  open,
  onOpenChange,
  existingScopes,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  existingScopes: string[];
  onCreated: () => void;
}) {
  const available = SCOPES.filter((s) => !existingScopes.includes(s.value));
  const firstAvailable = available[0]?.value;
  const [scope, setScope] = React.useState<RetentionScope>('audit');
  const [days, setDays] = React.useState('365');
  const canManage = usePermission(RETENTION_MANAGE);

  // Default to the first still-available scope when the dialog opens.
  React.useEffect(() => {
    if (open && firstAvailable) setScope(firstAvailable);
  }, [open, firstAvailable]);

  const create = useMutation({
    mutationFn: () => api.createRetentionPolicy({ scope, retention_days: Number(days) }),
    onSuccess: () => {
      toast.success('Retention policy created');
      setDays('365');
      onCreated();
    },
    onError: (err) =>
      toast.error('Could not create policy', err instanceof SdkError ? err.message : undefined),
  });

  const daysValid = /^\d+$/.test(days.trim()) && Number(days) > 0;
  const ready = !!scope && daysValid && available.length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>New retention policy</DialogTitle>
          <DialogDescription>
            Keep a data scope for a fixed number of days. A tenant has at most one policy per scope.
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
            <Label htmlFor="scope">Scope</Label>
            <select
              id="scope"
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={scope}
              onChange={(e) => setScope(e.target.value as RetentionScope)}
            >
              {available.map((s) => (
                <option key={s.value} value={s.value}>
                  {s.label}
                </option>
              ))}
            </select>
            {available.length === 0 ? (
              <span className="text-xs text-muted-foreground">
                Every scope already has a policy. Edit or delete an existing one.
              </span>
            ) : null}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="days">Retention (days)</Label>
            <Input
              id="days"
              inputMode="numeric"
              placeholder="e.g. 365"
              required
              value={days}
              onChange={(e) => setDays(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!ready || canManage === false || create.isPending}>
              {create.isPending ? 'Creating…' : 'Create policy'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}

// ---------------------------------------------------------------------------
// Retention sweep job-run history
// ---------------------------------------------------------------------------

function RetentionJobRuns() {
  const runs = useQuery({
    queryKey: ['retention-job-runs'],
    queryFn: ({ signal }) => api.listRetentionJobRuns(signal),
  });

  const items = (runs.data?.items ?? []) as JobRun[];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Retention sweep history</CardTitle>
      </CardHeader>
      <CardContent className="p-0">
        {runs.isPending ? (
          <div className="flex flex-col gap-2 p-4">
            {Array.from({ length: 2 }).map((_, i) => (
              <Skeleton key={i} className="h-10 rounded-lg" />
            ))}
          </div>
        ) : runs.isError ? (
          <ErrorState
            title="Couldn't load job history"
            description={String((runs.error as Error).message)}
            onRetry={() => runs.refetch()}
          />
        ) : items.length === 0 ? (
          <EmptyState
            title="No sweep runs yet"
            description="The retention sweep runs on a schedule and records its intent here. It is a dry-run today — it reports purge candidates without deleting."
            icon={<Archive />}
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Started</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Detail</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((r) => (
                <TableRow key={r.id}>
                  <TableCell className="whitespace-nowrap font-mono text-xs">
                    {r.started_at.slice(0, 19).replace('T', ' ')}
                  </TableCell>
                  <TableCell>
                    <Badge tone={statusTone(r.status)}>{r.status}</Badge>
                  </TableCell>
                  <TableCell
                    className="max-w-[28rem] truncate text-muted-foreground"
                    title={r.detail}
                  >
                    {r.detail ?? '—'}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}

// ---------------------------------------------------------------------------
// Closed-period change requests (maker-checker)
// ---------------------------------------------------------------------------

function ClosedPeriodChanges() {
  const qc = useQueryClient();
  const [requestOpen, setRequestOpen] = React.useState(false);

  const list = useQuery({
    queryKey: ['closed-period-change-requests'],
    queryFn: ({ signal }) => api.listClosedPeriodChangeRequests({}, signal),
  });

  function invalidate() {
    void qc.invalidateQueries({ queryKey: ['closed-period-change-requests'] });
  }

  const approve = useMutation({
    mutationFn: (id: string) => api.approveClosedPeriodChange(id),
    onSuccess: () => {
      invalidate();
      toast.success('Change request approved');
    },
    onError: (err) =>
      toast.error('Could not approve', err instanceof SdkError ? err.message : undefined),
  });

  const reject = useMutation({
    mutationFn: (id: string) => api.rejectClosedPeriodChange(id),
    onSuccess: () => {
      invalidate();
      toast.success('Change request rejected');
    },
    onError: (err) =>
      toast.error('Could not reject', err instanceof SdkError ? err.message : undefined),
  });

  const items = (list.data?.items ?? []) as ClosedPeriodChangeRequest[];
  const busy = approve.isPending || reject.isPending;

  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-3">
        <CardTitle>Closed-period change requests</CardTitle>
        <PermissionGate permission={CLOSED_PERIOD_CHANGE} mode="hide">
          <Button type="button" size="sm" onClick={() => setRequestOpen(true)}>
            Request change
          </Button>
        </PermissionGate>
      </CardHeader>
      <CardContent className="p-0">
        {list.isPending ? (
          <div className="flex flex-col gap-2 p-4">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-12 rounded-lg" />
            ))}
          </div>
        ) : list.isError ? (
          (() => {
            const forbidden = list.error instanceof SdkError && list.error.status === 403;
            return (
              <ErrorState
                title={forbidden ? 'No access' : "Couldn't load change requests"}
                description={
                  forbidden
                    ? "You don't have permission to view closed-period change requests (closed_period.change)."
                    : String((list.error as Error).message)
                }
                onRetry={forbidden ? undefined : () => list.refetch()}
              />
            );
          })()
        ) : items.length === 0 ? (
          <EmptyState
            title="No change requests"
            description="Reopening or relocking a closed period needs a request that a different user approves."
            icon={<Archive />}
          />
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Requested</TableHead>
                <TableHead>Change</TableHead>
                <TableHead>Reason</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {items.map((c) => (
                <TableRow key={c.id}>
                  <TableCell className="whitespace-nowrap font-mono text-xs">
                    {c.requested_at.slice(0, 10)}
                  </TableCell>
                  <TableCell className="capitalize">{c.change_type}</TableCell>
                  <TableCell className="max-w-[18rem] truncate" title={c.reason}>
                    {c.reason}
                  </TableCell>
                  <TableCell>
                    <Badge tone={statusTone(c.status)}>{c.status}</Badge>
                  </TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-2">
                      {c.status === 'requested' ? (
                        <>
                          <PermissionGate permission={CLOSED_PERIOD_CHANGE}>
                            <Button
                              type="button"
                              variant="secondary"
                              size="sm"
                              disabled={busy}
                              onClick={() => approve.mutate(c.id)}
                            >
                              Approve
                            </Button>
                          </PermissionGate>
                          <PermissionGate permission={CLOSED_PERIOD_CHANGE}>
                            <Button
                              type="button"
                              variant="outline"
                              size="sm"
                              disabled={busy}
                              onClick={() => reject.mutate(c.id)}
                            >
                              Reject
                            </Button>
                          </PermissionGate>
                        </>
                      ) : (
                        <span className="text-xs text-muted-foreground capitalize">{c.status}</span>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
      <RequestChangeDialog
        open={requestOpen}
        onOpenChange={setRequestOpen}
        onCreated={() => {
          setRequestOpen(false);
          invalidate();
        }}
      />
    </Card>
  );
}

function RequestChangeDialog({
  open,
  onOpenChange,
  onCreated,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  onCreated: () => void;
}) {
  const [periodID, setPeriodID] = React.useState('');
  const [changeType, setChangeType] = React.useState<ClosedPeriodChangeType>('reopen');
  const [reason, setReason] = React.useState('');
  const canChange = usePermission(CLOSED_PERIOD_CHANGE);

  // Only CLOSED/LOCKED periods can be targeted (the backend rejects others).
  const periods = useQuery({
    queryKey: ['accounting-periods'],
    queryFn: ({ signal }) => api.listAccountingPeriods(signal),
    enabled: open,
  });
  const closedPeriods = ((periods.data?.items ?? []) as AccountingPeriod[]).filter(
    (p) => p.status === 'closed' || p.status === 'locked',
  );

  const create = useMutation({
    mutationFn: () =>
      api.requestClosedPeriodChange(periodID, { change_type: changeType, reason: reason.trim() }),
    onSuccess: () => {
      toast.success('Change request opened');
      setReason('');
      setPeriodID('');
      onCreated();
    },
    onError: (err) =>
      toast.error('Could not request change', err instanceof SdkError ? err.message : undefined),
  });

  const ready = !!periodID && reason.trim().length > 0;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>Request a closed-period change</DialogTitle>
          <DialogDescription>
            Open a maker-checker request to reopen or relock a closed/locked accounting period. A
            different user must approve it.
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
            <Label htmlFor="period">Closed period</Label>
            <select
              id="period"
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={periodID}
              onChange={(e) => setPeriodID(e.target.value)}
              required
            >
              <option value="" disabled>
                Select a closed/locked period…
              </option>
              {closedPeriods.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.start_date} → {p.end_date} ({p.status})
                </option>
              ))}
            </select>
            {periods.isSuccess && closedPeriods.length === 0 ? (
              <span className="text-xs text-muted-foreground">
                No closed or locked periods to change.
              </span>
            ) : null}
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="change-type">Change</Label>
            <select
              id="change-type"
              className="h-9 rounded-md border border-border bg-background px-2 text-sm"
              value={changeType}
              onChange={(e) => setChangeType(e.target.value as ClosedPeriodChangeType)}
            >
              <option value="reopen">Reopen</option>
              <option value="relock">Relock</option>
            </select>
          </div>
          <div className="flex flex-col gap-1.5">
            <Label htmlFor="reason">Reason</Label>
            <Input
              id="reason"
              placeholder="Why does this closed period need to change?"
              required
              value={reason}
              onChange={(e) => setReason(e.target.value)}
            />
          </div>
          <DialogFooter>
            <Button type="button" variant="ghost" onClick={() => onOpenChange(false)}>
              Cancel
            </Button>
            <Button type="submit" disabled={!ready || canChange === false || create.isPending}>
              {create.isPending ? 'Requesting…' : 'Request change'}
            </Button>
          </DialogFooter>
        </form>
      </DialogContent>
    </Dialog>
  );
}
