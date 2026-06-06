'use client';

import { useEffect, useMemo, useState } from 'react';
import Link from 'next/link';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type DipReading, type MeterReading, type OperationsShift } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  Input,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';
import { CalendarClock, CheckCircle2, Lock, Plus, Trash2 } from 'lucide-react';

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

function todayLocalDate() {
  const now = new Date();
  now.setMinutes(now.getMinutes() - now.getTimezoneOffset());
  return now.toISOString().slice(0, 10);
}

interface NozzleChoice {
  id: string;
  label: string;
  tankID: string;
  tankLabel: string;
  meterDecimalPlaces: number;
}

interface AssignmentDraft {
  nozzleID: string;
  attendantID: string;
}

export default function OperationsPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState<string>('');
  const [actionError, setActionError] = useState<string | null>(null);
  const [openDayDate, setOpenDayDate] = useState(todayLocalDate);
  const [newShiftName, setNewShiftName] = useState('');
  const [slot, setSlot] = useState<'morning' | 'evening'>('morning');
  const [assignmentDrafts, setAssignmentDrafts] = useState<Record<string, AssignmentDraft>>({});

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

  const stationOverview = useQuery({
    queryKey: ['station-overview', stationID, 'operations'],
    queryFn: ({ signal }) => api.getStationOverview(stationID, signal),
    enabled: !!stationID && (overview.data?.shifts.some((s) => s.status === 'open') ?? false),
  });

  const nozzleChoices = useMemo<NozzleChoice[]>(() => {
    const tanksByID = new Map((stationOverview.data?.tanks ?? []).map((t) => [t.id, t]));
    return (stationOverview.data?.pumps ?? []).flatMap((pump) =>
      pump.nozzles
        .filter((nozzle) => nozzle.status === 'active')
        .map((nozzle) => {
          const tank = tanksByID.get(nozzle.tank_id);
          return {
            id: nozzle.id,
            label: `P${pump.number}·N${nozzle.number} · ${tank?.code ?? 'tank'}`,
            tankID: nozzle.tank_id,
            tankLabel: tank?.code ?? 'tank',
            meterDecimalPlaces: nozzle.meter_decimal_places,
          };
        }),
    );
  }, [stationOverview.data]);

  function invalidateOperations() {
    qc.invalidateQueries({ queryKey: overviewKey });
    qc.invalidateQueries({ queryKey: ['station-overview', stationID, 'operations'] });
  }

  const approve = useMutation({
    mutationFn: (shiftID: string) => api.approveShift(shiftID),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not approve shift'),
  });

  const resolve = useMutation({
    mutationFn: (exceptionID: string) => api.resolveShiftException(exceptionID),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
    },
    onError: (e) =>
      setActionError(e instanceof SdkError ? e.message : 'Could not resolve exception'),
  });

  const openDay = useMutation({
    mutationFn: (businessDate: string) =>
      api.openOperatingDay(stationID, { business_date: businessDate }),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not open day'),
  });

  const closeDay = useMutation({
    mutationFn: (dayID: string) =>
      api.updateOperatingDayStatus(dayID, 'closed', 'Closed from operations console'),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not close day'),
  });

  const reopenDay = useMutation({
    mutationFn: (dayID: string) =>
      api.updateOperatingDayStatus(dayID, 'open', 'Reopened from operations console'),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not reopen day'),
  });

  const lockDay = useMutation({
    mutationFn: (dayID: string) => api.lockOperatingDay(dayID, 'Locked from operations console'),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not lock day'),
  });

  // The team scheduled for the selected slot on the active operating day's
  // business date — shown before opening so the supervisor sees who'll staff it.
  const businessDate = overview.data?.day?.business_date;
  const scheduledTeam = useQuery({
    queryKey: ['scheduled-team', stationID, businessDate, slot],
    queryFn: ({ signal }) => api.getScheduledTeam(stationID, { slot, date: businessDate }, signal),
    enabled: !!stationID && !!businessDate,
  });

  const openShift = useMutation({
    mutationFn: (dayID: string) =>
      api.openShift(stationID, { operating_day_id: dayID, name: newShiftName.trim(), slot }),
    onSuccess: () => {
      setActionError(null);
      setNewShiftName('');
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not open shift'),
  });

  const assignNozzle = useMutation({
    mutationFn: ({
      shiftID,
      nozzleID,
      attendantID,
    }: {
      shiftID: string;
      nozzleID: string;
      attendantID: string;
    }) => api.assignNozzle(shiftID, { nozzle_id: nozzleID, attendant_id: attendantID }),
    onSuccess: (_data, vars) => {
      setActionError(null);
      setAssignmentDrafts((prev) => ({
        ...prev,
        [vars.shiftID]: { nozzleID: '', attendantID: '' },
      }));
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not assign nozzle'),
  });

  const unassignNozzle = useMutation({
    mutationFn: ({ shiftID, assignmentID }: { shiftID: string; assignmentID: string }) =>
      api.unassignNozzle(shiftID, assignmentID),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not unassign nozzle'),
  });

  const closeShift = useMutation({
    mutationFn: (shiftID: string) => api.closeShift(shiftID),
    onSuccess: () => {
      setActionError(null);
      invalidateOperations();
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
              <div className="flex flex-wrap items-center justify-center gap-2">
                <Input
                  aria-label="Operating day date"
                  className="w-40"
                  type="date"
                  value={openDayDate}
                  onChange={(e) => setOpenDayDate(e.target.value)}
                />
                <Button
                  disabled={!openDayDate || openDay.isPending}
                  onClick={() => openDay.mutate(openDayDate)}
                >
                  <CalendarClock className="size-4" />
                  {openDay.isPending ? 'Opening…' : 'Open operating day'}
                </Button>
              </div>
            </PermissionGate>
          }
        />
      ) : (
        <>
          <Card>
            <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
              <div className="flex flex-col gap-1">
                <CardTitle className="flex items-center gap-2.5 text-base">
                  <span className="flex size-9 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                    <CalendarClock className="size-4" />
                  </span>
                  Operating day · {overview.data.day.business_date}
                </CardTitle>
                <CardDescription>
                  {overview.data.shifts.length} shift
                  {overview.data.shifts.length === 1 ? '' : 's'} · opened{' '}
                  {new Date(overview.data.day.opened_at).toLocaleString()}
                </CardDescription>
              </div>
              <div className="flex flex-wrap items-center justify-end gap-2">
                <Badge tone={overview.data.day.status === 'open' ? 'success' : 'warning'}>
                  {overview.data.day.status}
                </Badge>
                <DayActions
                  status={overview.data.day.status}
                  shifts={overview.data.shifts}
                  stationID={stationID}
                  closing={closeDay.isPending}
                  reopening={reopenDay.isPending}
                  locking={lockDay.isPending}
                  onClose={() => closeDay.mutate(overview.data.day!.id)}
                  onReopen={() => reopenDay.mutate(overview.data.day!.id)}
                  onLock={() => lockDay.mutate(overview.data.day!.id)}
                />
              </div>
            </CardHeader>
            <CardContent className="flex flex-col gap-3 text-sm text-muted-foreground">
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
                          !newShiftName.trim() || openShift.isPending || !scheduledTeam.data?.team
                        }
                        onClick={() => openShift.mutate(overview.data.day!.id)}
                      >
                        <Plus className="size-4" />
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
                  nozzles={nozzleChoices}
                  nozzleLookupPending={stationOverview.isPending}
                  assignmentDraft={assignmentDrafts[shift.id] ?? { nozzleID: '', attendantID: '' }}
                  onAssignmentDraftChange={(draft) =>
                    setAssignmentDrafts((prev) => ({ ...prev, [shift.id]: draft }))
                  }
                  onAssignNozzle={(nozzleID, attendantID) =>
                    assignNozzle.mutate({ shiftID: shift.id, nozzleID, attendantID })
                  }
                  onUnassignNozzle={(assignmentID) =>
                    unassignNozzle.mutate({ shiftID: shift.id, assignmentID })
                  }
                  onRefresh={invalidateOperations}
                  onApprove={() => approve.mutate(shift.id)}
                  onResolve={(id) => resolve.mutate(id)}
                  onClose={() => closeShift.mutate(shift.id)}
                  assigning={assignNozzle.isPending && assignNozzle.variables?.shiftID === shift.id}
                  unassigningAssignmentID={
                    unassignNozzle.isPending && unassignNozzle.variables?.shiftID === shift.id
                      ? unassignNozzle.variables.assignmentID
                      : null
                  }
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
  nozzles,
  nozzleLookupPending,
  assignmentDraft,
  onAssignmentDraftChange,
  onAssignNozzle,
  onUnassignNozzle,
  onRefresh,
  onApprove,
  onResolve,
  onClose,
  assigning,
  unassigningAssignmentID,
  approving,
  resolvingExceptionID,
  closing,
}: {
  shift: OperationsShift;
  stationID: string;
  nozzles: NozzleChoice[];
  nozzleLookupPending: boolean;
  assignmentDraft: AssignmentDraft;
  onAssignmentDraftChange: (draft: AssignmentDraft) => void;
  onAssignNozzle: (nozzleID: string, attendantID: string) => void;
  onUnassignNozzle: (assignmentID: string) => void;
  onRefresh: () => void;
  onApprove: () => void;
  onResolve: (exceptionID: string) => void;
  onClose: () => void;
  assigning: boolean;
  unassigningAssignmentID: string | null;
  approving: boolean;
  resolvingExceptionID: string | null;
  closing: boolean;
}) {
  const qc = useQueryClient();
  const cash = shift.cash_submission;
  const [readingInputs, setReadingInputs] = useState<Record<string, string>>({});
  const [dipInputs, setDipInputs] = useState<Record<string, string>>({});
  const [cashInputs, setCashInputs] = useState({
    cash: '',
    mobile: '',
    card: '',
    credit: '',
  });
  const canApprove = shift.status === 'closed' && shift.open_exception_count === 0 && !!cash;
  const approveLabel = approving
    ? 'Approving…'
    : !cash
      ? 'Submit cash to approve'
      : canApprove
        ? 'Approve shift'
        : 'Resolve exceptions to approve';
  const assignedNozzleIDs = new Set(shift.nozzle_assignments.map((a) => a.nozzle_id));
  const availableNozzles = nozzles.filter((n) => !assignedNozzleIDs.has(n.id));
  const assignedNozzles = nozzles.filter((n) => assignedNozzleIDs.has(n.id));
  const effectiveNozzleID =
    assignmentDraft.nozzleID || (availableNozzles.length === 1 ? availableNozzles[0]!.id : '');
  const effectiveAttendantID =
    assignmentDraft.attendantID ||
    (shift.attendants.length === 1 ? shift.attendants[0]!.user_id : '');
  const attendantName = new Map(shift.attendants.map((a) => [a.user_id, a.full_name]));
  const nozzleName = new Map(nozzles.map((n) => [n.id, n.label]));
  const assignedTanks = Array.from(
    new Map(assignedNozzles.map((nozzle) => [nozzle.tankID, nozzle])).values(),
  );

  const meterReadings = useQuery({
    queryKey: ['shift-meter-readings', shift.id],
    queryFn: ({ signal }) => api.listMeterReadings(shift.id, signal),
    enabled: shift.status === 'open' && shift.nozzle_assignments.length > 0,
  });

  const dipReadings = useQuery({
    queryKey: ['shift-dip-readings', shift.id],
    queryFn: ({ signal }) => api.listDipReadings(shift.id, signal),
    enabled: shift.status === 'open' && shift.nozzle_assignments.length > 0,
  });

  function refreshShiftFacts() {
    qc.invalidateQueries({ queryKey: ['shift-meter-readings', shift.id] });
    qc.invalidateQueries({ queryKey: ['shift-dip-readings', shift.id] });
    onRefresh();
  }

  const captureMeter = useMutation({
    mutationFn: ({
      nozzleID,
      type,
      reading,
    }: {
      nozzleID: string;
      type: 'opening' | 'closing';
      reading: string;
    }) => api.captureMeterReading(shift.id, { nozzle_id: nozzleID, reading_type: type, reading }),
    onSuccess: (_data, vars) => {
      setReadingInputs((prev) => ({ ...prev, [`${vars.nozzleID}:${vars.type}`]: '' }));
      refreshShiftFacts();
    },
  });

  const captureDip = useMutation({
    mutationFn: ({
      tankID,
      type,
      dipMM,
    }: {
      tankID: string;
      type: 'opening' | 'closing';
      dipMM: string;
    }) => api.captureDipReading(shift.id, { tank_id: tankID, reading_type: type, dip_mm: dipMM }),
    onSuccess: (_data, vars) => {
      setDipInputs((prev) => ({ ...prev, [`${vars.tankID}:${vars.type}`]: '' }));
      refreshShiftFacts();
    },
  });

  const submitCash = useMutation({
    mutationFn: () =>
      api.submitCash(shift.id, {
        cash_amount: cashInputs.cash || '0',
        mobile_money_amount: cashInputs.mobile || '0',
        card_amount: cashInputs.card || '0',
        credit_amount: cashInputs.credit || '0',
      }),
    onSuccess: () => {
      setCashInputs({ cash: '', mobile: '', card: '', credit: '' });
      onRefresh();
    },
  });

  const meterByNozzle = new Map<string, Map<string, MeterReading>>();
  for (const reading of meterReadings.data?.items ?? []) {
    const byType = meterByNozzle.get(reading.nozzle_id) ?? new Map<string, MeterReading>();
    byType.set(reading.reading_type, reading);
    meterByNozzle.set(reading.nozzle_id, byType);
  }

  const dipByTank = new Map<string, Map<string, DipReading>>();
  for (const reading of dipReadings.data?.items ?? []) {
    const byType = dipByTank.get(reading.tank_id) ?? new Map<string, DipReading>();
    byType.set(reading.reading_type, reading);
    dipByTank.set(reading.tank_id, byType);
  }

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base">
          <Link
            href={`/operations/shifts/${shift.id}`}
            className="hover:text-accent hover:underline"
          >
            {shift.name}
          </Link>
        </CardTitle>
        <div className="flex items-center gap-2">
          <Button asChild variant="ghost" size="sm">
            <Link href={`/operations/shifts/${shift.id}`}>Timeline</Link>
          </Button>
          <Badge tone={shiftTone(shift.status)}>{shift.status}</Badge>
        </div>
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

        {shift.status === 'open' ? (
          <div className="flex flex-col gap-2 rounded-lg border border-border/70 bg-muted/20 p-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Nozzle assignment
              </span>
              {nozzleLookupPending ? (
                <span className="text-xs text-muted-foreground">Loading nozzles…</span>
              ) : null}
            </div>
            {shift.nozzle_assignments.length > 0 ? (
              <div className="flex flex-col gap-1.5">
                {shift.nozzle_assignments.map((assignment) => (
                  <div
                    key={assignment.id}
                    className="flex flex-wrap items-center justify-between gap-2 rounded-md bg-background/60 px-2 py-1.5"
                  >
                    <span className="text-[12px] text-muted-foreground">
                      <span className="font-mono text-foreground">
                        {nozzleName.get(assignment.nozzle_id) ?? assignment.nozzle_id.slice(0, 8)}
                      </span>{' '}
                      · {attendantName.get(assignment.attendant_id) ?? 'attendant'}
                    </span>
                    <PermissionGate permission="shift.assign" stationId={stationID}>
                      <Button
                        aria-label="Remove nozzle assignment"
                        size="icon"
                        variant="ghost"
                        disabled={unassigningAssignmentID === assignment.id}
                        onClick={() => onUnassignNozzle(assignment.id)}
                      >
                        <Trash2 className="size-4" />
                      </Button>
                    </PermissionGate>
                  </div>
                ))}
              </div>
            ) : null}
            <PermissionGate permission="shift.assign" stationId={stationID}>
              <div className="grid gap-2 sm:grid-cols-[1fr_1fr_auto]">
                <select
                  aria-label="Nozzle"
                  className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                  value={effectiveNozzleID}
                  onChange={(e) =>
                    onAssignmentDraftChange({ ...assignmentDraft, nozzleID: e.target.value })
                  }
                  disabled={availableNozzles.length === 0}
                >
                  <option value="">
                    {availableNozzles.length === 0 ? 'No unassigned nozzles' : 'Nozzle'}
                  </option>
                  {availableNozzles.map((nozzle) => (
                    <option key={nozzle.id} value={nozzle.id}>
                      {nozzle.label}
                    </option>
                  ))}
                </select>
                <select
                  aria-label="Attendant"
                  className="h-9 rounded-md border border-border bg-background px-2 text-sm"
                  value={effectiveAttendantID}
                  onChange={(e) =>
                    onAssignmentDraftChange({ ...assignmentDraft, attendantID: e.target.value })
                  }
                  disabled={shift.attendants.length === 0}
                >
                  <option value="">
                    {shift.attendants.length === 0 ? 'No attendants' : 'Attendant'}
                  </option>
                  {shift.attendants.map((attendant) => (
                    <option key={attendant.user_id} value={attendant.user_id}>
                      {attendant.full_name}
                    </option>
                  ))}
                </select>
                <Button
                  size="sm"
                  disabled={!effectiveNozzleID || !effectiveAttendantID || assigning}
                  onClick={() => onAssignNozzle(effectiveNozzleID, effectiveAttendantID)}
                >
                  <Plus className="size-4" />
                  {assigning ? 'Assigning…' : 'Assign'}
                </Button>
              </div>
            </PermissionGate>
          </div>
        ) : null}

        {shift.status === 'open' && shift.nozzle_assignments.length > 0 ? (
          <div className="flex flex-col gap-3 rounded-lg border border-border/70 bg-muted/20 p-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Meter readings
              </span>
              {meterReadings.isPending ? (
                <span className="text-xs text-muted-foreground">Loading readings…</span>
              ) : null}
            </div>
            {assignedNozzles.map((nozzle) => (
              <div key={nozzle.id} className="grid gap-2 rounded-md bg-background/60 p-2">
                <span className="font-mono text-xs text-foreground">{nozzle.label}</span>
                <MeterCaptureRow
                  nozzle={nozzle}
                  type="opening"
                  current={meterByNozzle.get(nozzle.id)?.get('opening')}
                  value={readingInputs[`${nozzle.id}:opening`] ?? ''}
                  pending={
                    captureMeter.isPending &&
                    captureMeter.variables?.nozzleID === nozzle.id &&
                    captureMeter.variables?.type === 'opening'
                  }
                  stationID={stationID}
                  onChange={(value) =>
                    setReadingInputs((prev) => ({ ...prev, [`${nozzle.id}:opening`]: value }))
                  }
                  onSave={() =>
                    captureMeter.mutate({
                      nozzleID: nozzle.id,
                      type: 'opening',
                      reading: readingInputs[`${nozzle.id}:opening`] ?? '',
                    })
                  }
                />
                <MeterCaptureRow
                  nozzle={nozzle}
                  type="closing"
                  current={meterByNozzle.get(nozzle.id)?.get('closing')}
                  value={readingInputs[`${nozzle.id}:closing`] ?? ''}
                  pending={
                    captureMeter.isPending &&
                    captureMeter.variables?.nozzleID === nozzle.id &&
                    captureMeter.variables?.type === 'closing'
                  }
                  stationID={stationID}
                  onChange={(value) =>
                    setReadingInputs((prev) => ({ ...prev, [`${nozzle.id}:closing`]: value }))
                  }
                  onSave={() =>
                    captureMeter.mutate({
                      nozzleID: nozzle.id,
                      type: 'closing',
                      reading: readingInputs[`${nozzle.id}:closing`] ?? '',
                    })
                  }
                />
              </div>
            ))}
            {captureMeter.isError ? (
              <p className="text-xs text-danger">
                {captureMeter.error instanceof SdkError
                  ? captureMeter.error.message
                  : 'Could not save meter reading'}
              </p>
            ) : null}
          </div>
        ) : null}

        {shift.status === 'open' && assignedTanks.length > 0 ? (
          <div className="flex flex-col gap-3 rounded-lg border border-border/70 bg-muted/20 p-3">
            <div className="flex items-center justify-between gap-2">
              <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                Tank dips
              </span>
              {dipReadings.isPending ? (
                <span className="text-xs text-muted-foreground">Loading dips…</span>
              ) : null}
            </div>
            {assignedTanks.map((tank) => (
              <div key={tank.tankID} className="grid gap-2 rounded-md bg-background/60 p-2">
                <span className="font-mono text-xs text-foreground">{tank.tankLabel}</span>
                <DipCaptureRow
                  tankLabel={tank.tankLabel}
                  type="opening"
                  current={dipByTank.get(tank.tankID)?.get('opening')}
                  value={dipInputs[`${tank.tankID}:opening`] ?? ''}
                  pending={
                    captureDip.isPending &&
                    captureDip.variables?.tankID === tank.tankID &&
                    captureDip.variables?.type === 'opening'
                  }
                  stationID={stationID}
                  onChange={(value) =>
                    setDipInputs((prev) => ({ ...prev, [`${tank.tankID}:opening`]: value }))
                  }
                  onSave={() =>
                    captureDip.mutate({
                      tankID: tank.tankID,
                      type: 'opening',
                      dipMM: dipInputs[`${tank.tankID}:opening`] ?? '',
                    })
                  }
                />
                <DipCaptureRow
                  tankLabel={tank.tankLabel}
                  type="closing"
                  current={dipByTank.get(tank.tankID)?.get('closing')}
                  value={dipInputs[`${tank.tankID}:closing`] ?? ''}
                  pending={
                    captureDip.isPending &&
                    captureDip.variables?.tankID === tank.tankID &&
                    captureDip.variables?.type === 'closing'
                  }
                  stationID={stationID}
                  onChange={(value) =>
                    setDipInputs((prev) => ({ ...prev, [`${tank.tankID}:closing`]: value }))
                  }
                  onSave={() =>
                    captureDip.mutate({
                      tankID: tank.tankID,
                      type: 'closing',
                      dipMM: dipInputs[`${tank.tankID}:closing`] ?? '',
                    })
                  }
                />
              </div>
            ))}
            {captureDip.isError ? (
              <p className="text-xs text-danger">
                {captureDip.error instanceof SdkError
                  ? captureDip.error.message
                  : 'Could not save tank dip'}
              </p>
            ) : null}
          </div>
        ) : null}

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
              {approveLabel}
            </Button>
          </PermissionGate>
        ) : null}
        {shift.status === 'closed' && !cash ? (
          <CashSubmissionPanel
            stationID={stationID}
            inputs={cashInputs}
            pending={submitCash.isPending}
            error={submitCash.error}
            onChange={(field, value) => setCashInputs((prev) => ({ ...prev, [field]: value }))}
            onSubmit={() => submitCash.mutate()}
          />
        ) : null}
      </CardContent>
    </Card>
  );
}

function MeterCaptureRow({
  nozzle,
  type,
  current,
  value,
  pending,
  stationID,
  onChange,
  onSave,
}: {
  nozzle: NozzleChoice;
  type: 'opening' | 'closing';
  current?: MeterReading;
  value: string;
  pending: boolean;
  stationID: string;
  onChange: (value: string) => void;
  onSave: () => void;
}) {
  const step = 1 / Math.pow(10, nozzle.meterDecimalPlaces);

  return (
    <div className="grid gap-2 sm:grid-cols-[6rem_1fr_auto] sm:items-center">
      <span className="text-xs capitalize text-muted-foreground">{type}</span>
      {current ? (
        <span className="font-mono text-sm tabular-nums">{current.reading}</span>
      ) : (
        <>
          <Input
            className="h-8 text-right"
            aria-label={`${type} meter reading for ${nozzle.label}`}
            type="number"
            inputMode="decimal"
            min="0"
            step={step}
            value={value}
            onChange={(e) => onChange(e.target.value)}
          />
          <PermissionGate permission="reading.override" stationId={stationID}>
            <Button
              aria-label={`Save ${type} meter reading for ${nozzle.label}`}
              size="sm"
              variant="secondary"
              disabled={!value || pending}
              onClick={onSave}
            >
              {pending ? 'Saving…' : 'Save'}
            </Button>
          </PermissionGate>
        </>
      )}
    </div>
  );
}

function DipCaptureRow({
  tankLabel,
  type,
  current,
  value,
  pending,
  stationID,
  onChange,
  onSave,
}: {
  tankLabel: string;
  type: 'opening' | 'closing';
  current?: DipReading;
  value: string;
  pending: boolean;
  stationID: string;
  onChange: (value: string) => void;
  onSave: () => void;
}) {
  return (
    <div className="grid gap-2 sm:grid-cols-[6rem_1fr_auto] sm:items-center">
      <span className="text-xs capitalize text-muted-foreground">{type}</span>
      {current ? (
        <span className="font-mono text-sm tabular-nums">
          {current.dip_mm} mm · {fmtLitres(current.volume_litres)} L
        </span>
      ) : (
        <>
          <Input
            className="h-8 text-right"
            aria-label={`${type} tank dip for ${tankLabel}`}
            type="number"
            inputMode="decimal"
            min="0"
            step="1"
            value={value}
            onChange={(e) => onChange(e.target.value)}
          />
          <PermissionGate permission="reading.override" stationId={stationID}>
            <Button
              aria-label={`Save ${type} tank dip for ${tankLabel}`}
              size="sm"
              variant="secondary"
              disabled={!value || pending}
              onClick={onSave}
            >
              {pending ? 'Saving…' : 'Save'}
            </Button>
          </PermissionGate>
        </>
      )}
    </div>
  );
}

function CashSubmissionPanel({
  stationID,
  inputs,
  pending,
  error,
  onChange,
  onSubmit,
}: {
  stationID: string;
  inputs: { cash: string; mobile: string; card: string; credit: string };
  pending: boolean;
  error: unknown;
  onChange: (field: 'cash' | 'mobile' | 'card' | 'credit', value: string) => void;
  onSubmit: () => void;
}) {
  return (
    <div className="flex flex-col gap-3 rounded-lg border border-border/70 bg-muted/20 p-3">
      <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        Submit cash
      </span>
      <div className="grid gap-2 sm:grid-cols-2">
        <MoneyInput
          label="Cash"
          value={inputs.cash}
          onChange={(value) => onChange('cash', value)}
        />
        <MoneyInput
          label="Mobile"
          value={inputs.mobile}
          onChange={(value) => onChange('mobile', value)}
        />
        <MoneyInput
          label="Card"
          value={inputs.card}
          onChange={(value) => onChange('card', value)}
        />
        <MoneyInput
          label="Credit"
          value={inputs.credit}
          onChange={(value) => onChange('credit', value)}
        />
      </div>
      {error ? (
        <p className="text-xs text-danger">
          {error instanceof SdkError ? error.message : 'Could not submit cash'}
        </p>
      ) : null}
      <PermissionGate permission="cash.override" stationId={stationID}>
        <Button className="self-start" size="sm" disabled={pending} onClick={onSubmit}>
          {pending ? 'Submitting…' : 'Submit cash'}
        </Button>
      </PermissionGate>
    </div>
  );
}

function MoneyInput({
  label,
  value,
  onChange,
}: {
  label: string;
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <label className="flex flex-col gap-1 text-xs text-muted-foreground">
      {label}
      <Input
        className="h-8 text-right"
        type="number"
        inputMode="decimal"
        min="0"
        step="0.01"
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder="0.00"
      />
    </label>
  );
}

function DayActions({
  status,
  shifts,
  stationID,
  closing,
  reopening,
  locking,
  onClose,
  onReopen,
  onLock,
}: {
  status: string;
  shifts: OperationsShift[];
  stationID: string;
  closing: boolean;
  reopening: boolean;
  locking: boolean;
  onClose: () => void;
  onReopen: () => void;
  onLock: () => void;
}) {
  const openShifts = shifts.filter((shift) => shift.status === 'open').length;
  const unapprovedShifts = shifts.filter((shift) => shift.status !== 'approved').length;

  if (status === 'open') {
    return (
      <PermissionGate permission="operations.manage_day" stationId={stationID}>
        <Button
          size="sm"
          variant="secondary"
          disabled={openShifts > 0 || closing}
          title={openShifts > 0 ? 'Close open shifts first' : undefined}
          onClick={onClose}
        >
          <CheckCircle2 className="size-4" />
          {closing ? 'Closing…' : 'Close day'}
        </Button>
      </PermissionGate>
    );
  }

  if (status === 'closed') {
    return (
      <PermissionGate permission="operations.manage_day" stationId={stationID}>
        <div className="flex flex-wrap items-center justify-end gap-2">
          <Button size="sm" variant="ghost" disabled={reopening} onClick={onReopen}>
            {reopening ? 'Reopening…' : 'Reopen'}
          </Button>
          <Button
            size="sm"
            disabled={unapprovedShifts > 0 || locking}
            title={unapprovedShifts > 0 ? 'Approve shifts first' : undefined}
            onClick={onLock}
          >
            <Lock className="size-4" />
            {locking ? 'Locking…' : 'Lock day'}
          </Button>
        </div>
      </PermissionGate>
    );
  }

  return null;
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
