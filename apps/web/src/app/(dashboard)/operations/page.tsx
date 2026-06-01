'use client';

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type OperationsShift } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  Input,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';
import { CalendarClock } from 'lucide-react';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';
import { formatLitres, formatMoney, parseDecimal } from '@/lib/money';

// Money/litre figures are exact decimal strings from the server.
function fmtMoney(n: number | string) {
  return formatMoney(n, { fallback: '0.00' });
}

function fmtLitres(n: number | string) {
  return formatLitres(n, { maximumFractionDigits: 3, fallback: '0' });
}

function shiftTone(status: string): 'success' | 'neutral' | 'warning' {
  if (status === 'open') return 'success';
  if (status === 'approved') return 'neutral';
  return 'warning';
}

export default function OperationsPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState<string>('');
  const [actionError, setActionError] = useState<string | null>(null);
  const [newShiftName, setNewShiftName] = useState('');
  const [slot, setSlot] = useState<'morning' | 'evening'>('morning');

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  // Default to the first accessible station once the list loads.
  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) {
      setStationID(first.id);
    }
  }, [stationID, stations.data]);

  const overviewKey = ['operations-overview', stationID];
  const overview = useQuery({
    queryKey: overviewKey,
    queryFn: ({ signal }) => api.getOperationsOverview(stationID, signal),
    enabled: !!stationID,
  });

  const approve = useMutation({
    mutationFn: (shiftID: string) => api.approveShift(shiftID),
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: overviewKey });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not approve shift'),
  });

  const resolve = useMutation({
    mutationFn: (exceptionID: string) => api.resolveShiftException(exceptionID),
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: overviewKey });
    },
    onError: (e) =>
      setActionError(e instanceof SdkError ? e.message : 'Could not resolve exception'),
  });

  const openDay = useMutation({
    mutationFn: () => api.openOperatingDay(stationID, {}),
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: overviewKey });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not open day'),
  });

  // The team scheduled for the selected slot on the active operating day's
  // business date — shown before opening so the supervisor sees who'll staff it.
  const businessDate = overview.data?.day?.business_date;
  const scheduledTeam = useQuery({
    queryKey: ['scheduled-team', stationID, businessDate, slot],
    queryFn: ({ signal }) =>
      api.getScheduledTeam(stationID, { slot, date: businessDate }, signal),
    enabled: !!stationID && !!businessDate,
  });

  const openShift = useMutation({
    mutationFn: (dayID: string) =>
      api.openShift(stationID, { operating_day_id: dayID, name: newShiftName.trim(), slot }),
    onSuccess: () => {
      setActionError(null);
      setNewShiftName('');
      qc.invalidateQueries({ queryKey: overviewKey });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not open shift'),
  });

  const closeShift = useMutation({
    mutationFn: (shiftID: string) => api.closeShift(shiftID),
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: overviewKey });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not close shift'),
  });

  const stationSelect =
    (stations.data?.items?.length ?? 0) > 0 ? (
      <label className="flex items-center gap-2 text-sm">
        <span className="text-muted-foreground">Station</span>
        <select
          className="h-9 rounded-md border border-border bg-background px-2 text-sm"
          value={stationID}
          onChange={(e) => setStationID(e.target.value)}
        >
          {stations.data!.items.map((s) => (
            <option key={s.id} value={s.id}>
              {s.name} ({s.code})
            </option>
          ))}
        </select>
      </label>
    ) : null;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Operations"
        title="Operations"
        description="Run the day: active shifts, cash status, approvals, and exceptions."
        actions={stationSelect}
      />

      {actionError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {actionError}
        </p>
      ) : null}

      {stations.isPending ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-28 rounded-xl" />
          <div className="grid gap-4 md:grid-cols-2">
            <Skeleton className="h-64 rounded-xl" />
            <Skeleton className="h-64 rounded-xl" />
          </div>
        </div>
      ) : stations.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((stations.error as Error).message)}
          onRetry={() => stations.refetch()}
        />
      ) : (stations.data?.items?.length ?? 0) === 0 ? (
        <EmptyState title="No stations" description="You don't have access to any stations yet." />
      ) : overview.isPending ? (
        <div className="flex flex-col gap-4">
          <Skeleton className="h-28 rounded-xl" />
          <div className="grid gap-4 md:grid-cols-2">
            <Skeleton className="h-64 rounded-xl" />
            <Skeleton className="h-64 rounded-xl" />
          </div>
        </div>
      ) : overview.isError ? (
        (() => {
          const err = overview.error;
          const forbidden = err instanceof SdkError && err.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to this station' : "Couldn't load operations"}
              description={
                forbidden
                  ? "You don't have permission to view this station."
                  : String((err as Error).message)
              }
              onRetry={forbidden ? undefined : () => overview.refetch()}
            />
          );
        })()
      ) : !overview.data.day ? (
        <EmptyState
          title="No active operating day"
          description="Open a day for this station to start running shifts."
          action={
            <PermissionGate permission="operations.manage_day" stationId={stationID}>
              <Button disabled={openDay.isPending} onClick={() => openDay.mutate()}>
                {openDay.isPending ? 'Opening…' : 'Open operating day'}
              </Button>
            </PermissionGate>
          }
        />
      ) : (
        <>
          <Card>
            <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
              <CardTitle className="flex items-center gap-2.5 text-base">
                <span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                  <CalendarClock className="size-4" />
                </span>
                Operating day · {overview.data.day.business_date}
              </CardTitle>
              <Badge tone={overview.data.day.status === 'open' ? 'success' : 'warning'}>
                {overview.data.day.status}
              </Badge>
            </CardHeader>
            <CardContent className="flex flex-col gap-3 text-sm text-muted-foreground">
              <span>
                {overview.data.shifts.length} shift
                {overview.data.shifts.length === 1 ? '' : 's'} · opened{' '}
                {new Date(overview.data.day.opened_at).toLocaleString()}
              </span>
              {overview.data.day.status === 'open' ? (
                <div className="flex flex-col gap-2">
                  <div className="flex flex-wrap items-center gap-2">
                    <Input
                      className="h-9 flex-1 min-w-40"
                      placeholder="New shift name (e.g. Morning)"
                      value={newShiftName}
                      onChange={(e) => setNewShiftName(e.target.value)}
                    />
                    <select
                      className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                      value={slot}
                      onChange={(e) => setSlot(e.target.value as 'morning' | 'evening')}
                    >
                      <option value="morning">Morning</option>
                      <option value="evening">Evening</option>
                    </select>
                    <PermissionGate permission="shift.open" stationId={stationID}>
                      <Button
                        size="sm"
                        disabled={
                          !newShiftName.trim() ||
                          openShift.isPending ||
                          !scheduledTeam.data?.team
                        }
                        onClick={() => openShift.mutate(overview.data.day!.id)}
                      >
                        {openShift.isPending ? 'Opening…' : 'Open shift'}
                      </Button>
                    </PermissionGate>
                  </div>
                  <div className="text-xs">
                    {scheduledTeam.isPending ? (
                      <span className="text-muted-foreground">Resolving scheduled team…</span>
                    ) : scheduledTeam.data?.team ? (
                      <span className="text-muted-foreground">
                        Scheduled team for {slot}:{' '}
                        <Badge tone="accent">{scheduledTeam.data.team.name}</Badge>{' '}
                        {scheduledTeam.data.members.length} member
                        {scheduledTeam.data.members.length === 1 ? '' : 's'}
                        {scheduledTeam.data.members.length > 0
                          ? ` · ${scheduledTeam.data.members.map((m) => m.full_name).join(', ')}`
                          : ''}
                      </span>
                    ) : (
                      <span className="text-warning">
                        No team scheduled for {slot}. Configure teams + the rotation anchor under
                        Teams &amp; Rotation before opening a shift.
                      </span>
                    )}
                  </div>
                </div>
              ) : null}
            </CardContent>
          </Card>

          {overview.data.shifts.length === 0 ? (
            <p className="text-sm text-muted-foreground">No shifts opened on this day yet.</p>
          ) : (
            <div className="grid gap-4 md:grid-cols-2">
              {overview.data.shifts.map((shift) => (
                <ShiftCard
                  key={shift.id}
                  shift={shift}
                  stationID={stationID}
                  onApprove={() => approve.mutate(shift.id)}
                  onResolve={(id) => resolve.mutate(id)}
                  onClose={() => closeShift.mutate(shift.id)}
                  approving={approve.isPending && approve.variables === shift.id}
                  resolvingExceptionID={resolve.isPending ? (resolve.variables ?? null) : null}
                  closing={closeShift.isPending && closeShift.variables === shift.id}
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

function ShiftCard({
  shift,
  stationID,
  onApprove,
  onResolve,
  onClose,
  approving,
  resolvingExceptionID,
  closing,
}: {
  shift: OperationsShift;
  stationID: string;
  onApprove: () => void;
  onResolve: (exceptionID: string) => void;
  onClose: () => void;
  approving: boolean;
  resolvingExceptionID: string | null;
  closing: boolean;
}) {
  const cash = shift.cash_submission;
  const canApprove = shift.status === 'closed' && shift.open_exception_count === 0;

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base">{shift.name}</CardTitle>
        <Badge tone={shiftTone(shift.status)}>{shift.status}</Badge>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {/* Attendants */}
        <div className="flex flex-col gap-1">
          <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            Attendants
          </span>
          {shift.attendants.length === 0 ? (
            <span className="text-muted-foreground">None assigned</span>
          ) : (
            <div className="flex flex-wrap gap-1.5">
              {shift.attendants.map((a) => (
                <span key={a.user_id} className="rounded-full bg-muted px-2 py-0.5 text-[12px]">
                  {a.full_name}
                </span>
              ))}
            </div>
          )}
        </div>

        {/* Figures */}
        <div className="flex flex-col gap-1">
          <Row label="Nozzles assigned" value={String(shift.nozzle_assignments.length)} />
          <Row label="Litres sold" value={fmtLitres(shift.litres_sold)} />
          <Row label="Expected cash" value={fmtMoney(shift.expected_cash)} />
          {cash ? (
            <>
              <Row label="Submitted" value={fmtMoney(cash.submitted_total)} />
              <Row
                label="Variance"
                value={fmtMoney(cash.variance)}
                tone={parseDecimal(cash.variance) < 0 ? 'danger' : 'success'}
              />
            </>
          ) : (
            <Row label="Cash" value={shift.status === 'open' ? 'shift open' : 'not submitted'} />
          )}
        </div>

        {/* Exceptions */}
        {shift.exceptions.length > 0 ? (
          <div className="flex flex-col gap-1.5">
            <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
              Exceptions
            </span>
            {shift.exceptions.map((exc) => (
              <div
                key={exc.id}
                className="flex items-center justify-between gap-2 rounded-md bg-muted px-2 py-1.5"
              >
                <div className="flex flex-col">
                  <span className="font-medium capitalize">{exc.type.replace(/_/g, ' ')}</span>
                  {exc.detail ? (
                    <span className="text-[12px] text-muted-foreground">{exc.detail}</span>
                  ) : null}
                </div>
                {exc.status === 'open' ? (
                  <PermissionGate permission="shift.approve" stationId={stationID}>
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={resolvingExceptionID === exc.id}
                      onClick={() => onResolve(exc.id)}
                    >
                      {resolvingExceptionID === exc.id ? 'Resolving…' : 'Resolve'}
                    </Button>
                  </PermissionGate>
                ) : (
                  <Badge tone="neutral">resolved</Badge>
                )}
              </div>
            ))}
          </div>
        ) : null}

        {/* Lifecycle actions */}
        {shift.status === 'open' ? (
          <PermissionGate permission="shift.close" stationId={stationID}>
            <Button className="h-10" disabled={closing} onClick={onClose}>
              {closing ? 'Closing…' : 'Close shift'}
            </Button>
          </PermissionGate>
        ) : null}
        {shift.status === 'closed' ? (
          <PermissionGate permission="shift.approve" stationId={stationID}>
            <Button className="h-10" disabled={!canApprove || approving} onClick={onApprove}>
              {approving
                ? 'Approving…'
                : canApprove
                  ? 'Approve shift'
                  : 'Resolve exceptions to approve'}
            </Button>
          </PermissionGate>
        ) : null}
      </CardContent>
    </Card>
  );
}

function Row({
  label,
  value,
  tone,
}: {
  label: string;
  value: string;
  tone?: 'danger' | 'success';
}) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span
        className={
          'font-mono text-sm font-medium tabular-nums' +
          (tone === 'danger' ? ' text-danger' : tone === 'success' ? ' text-success' : '')
        }
      >
        {value}
      </span>
    </div>
  );
}
