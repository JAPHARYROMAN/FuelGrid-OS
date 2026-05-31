'use client';

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type ReconciliationOverviewTank } from '@fuelgrid/sdk';
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
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres, formatMoney, parseDecimal } from '@/lib/money';

// Litre figures arrive as decimal strings (book_balance, reconciliation
// totals) or display numbers (Math.abs of a parsed total); accept both.
function fmtLitres(n: number | string) {
  return formatLitres(n, { maximumFractionDigits: 1, fallback: '0' });
}

export default function ReconciliationPage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState<string>('');
  const [actionError, setActionError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const overviewKey = ['reconciliation-overview', stationID];
  const overview = useQuery({
    queryKey: overviewKey,
    queryFn: ({ signal }) => api.getReconciliationOverview(stationID, {}, signal),
    enabled: !!stationID,
  });

  const dayID = overview.data?.day?.id;
  const invalidate = () => qc.invalidateQueries({ queryKey: overviewKey });
  const onErr = (fallback: string) => (e: unknown) =>
    setActionError(e instanceof SdkError ? e.message : fallback);

  const run = useMutation({
    mutationFn: (tankID: string) => api.runReconciliation(tankID, dayID!),
    onSuccess: () => {
      setActionError(null);
      invalidate();
    },
    onError: onErr('Could not run reconciliation'),
  });

  const adjust = useMutation({
    mutationFn: (v: { reconciliationID: string; litres: number; reason: string }) =>
      api.adjustReconciliation(v.reconciliationID, { litres: v.litres, reason: v.reason }),
    onSuccess: () => {
      setActionError(null);
      invalidate();
    },
    onError: onErr('Could not record adjustment'),
  });

  const seal = useMutation({
    mutationFn: (reconciliationID: string) => api.sealReconciliation(reconciliationID),
    onSuccess: () => {
      setActionError(null);
      invalidate();
    },
    onError: onErr('Could not seal reconciliation'),
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Reconciliation</h1>
          <p className="text-sm text-muted-foreground">
            Review each tank&apos;s book vs physical, record adjustments, and seal the day.
          </p>
        </div>
        {(stations.data?.items?.length ?? 0) > 0 ? (
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
        ) : null}
      </header>

      {actionError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {actionError}
        </p>
      ) : null}

      {stations.isPending ? (
        <LoadingState />
      ) : stations.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((stations.error as Error).message)}
          onRetry={() => stations.refetch()}
        />
      ) : (stations.data?.items?.length ?? 0) === 0 ? (
        <EmptyState title="No stations" description="You don't have access to any stations yet." />
      ) : overview.isPending ? (
        <LoadingState />
      ) : overview.isError ? (
        (() => {
          const err = overview.error;
          const forbidden = err instanceof SdkError && err.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to this station' : "Couldn't load reconciliation"}
              description={
                forbidden
                  ? "You don't have permission to reconcile this station."
                  : String((err as Error).message)
              }
              onRetry={forbidden ? undefined : () => overview.refetch()}
            />
          );
        })()
      ) : !overview.data.day ? (
        <EmptyState
          title="No active operating day"
          description="Open and run a day for this station before reconciling its tanks."
        />
      ) : (
        <>
          <Card>
            <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
              <CardTitle className="text-base">
                Operating day · {overview.data.day.business_date}
              </CardTitle>
              <Badge tone={overview.data.all_shifts_approved ? 'success' : 'warning'}>
                {overview.data.all_shifts_approved ? 'shifts approved' : 'shifts pending'}
              </Badge>
            </CardHeader>
            <CardContent className="text-sm text-muted-foreground">
              {overview.data.all_shifts_approved
                ? 'All shifts are approved — tanks can be reconciled and sealed.'
                : 'Approve all of the day’s shifts (Operations) before running reconciliation.'}
            </CardContent>
          </Card>

          {overview.data.tanks.length === 0 ? (
            <EmptyState title="No tanks" description="This station has no tanks configured yet." />
          ) : (
            <div className="grid gap-4 md:grid-cols-2">
              {overview.data.tanks.map((t) => (
                <ReconCard
                  key={t.tank.id}
                  t={t}
                  allShiftsApproved={overview.data!.all_shifts_approved}
                  onRun={() => run.mutate(t.tank.id)}
                  onAdjust={(litres, reason) =>
                    adjust.mutate({ reconciliationID: t.reconciliation!.id!, litres, reason })
                  }
                  onSeal={() => seal.mutate(t.reconciliation!.id!)}
                  busy={run.isPending || adjust.isPending || seal.isPending}
                />
              ))}
            </div>
          )}
        </>
      )}
    </div>
  );
}

function ReconCard({
  t,
  allShiftsApproved,
  onRun,
  onAdjust,
  onSeal,
  busy,
}: {
  t: ReconciliationOverviewTank;
  allShiftsApproved: boolean;
  onRun: () => void;
  onAdjust: (litres: number, reason: string) => void;
  onSeal: () => void;
  busy: boolean;
}) {
  const [litres, setLitres] = useState('');
  const [reason, setReason] = useState('');
  const rec = t.reconciliation;
  const sealed = rec?.status === 'sealed';

  const varianceTone = !rec
    ? 'neutral'
    : sealed
      ? 'neutral'
      : rec.over_tolerance
        ? 'danger'
        : 'success';
  const varianceLabel = !rec
    ? 'not run'
    : sealed
      ? 'sealed'
      : rec.over_tolerance
        ? 'over tolerance'
        : 'within tolerance';

  const litresNum = Number(litres);
  const canAdjust =
    !busy && reason.trim().length > 0 && Number.isFinite(litresNum) && litresNum !== 0;

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base">
          {t.tank.name} <span className="font-normal text-muted-foreground">({t.tank.code})</span>
        </CardTitle>
        <Badge tone={varianceTone}>{varianceLabel}</Badge>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {!rec ? (
          <>
            <div className="flex flex-col gap-1">
              <Row label="Book stock" value={`${fmtLitres(t.book_balance)} L`} />
              <Row
                label="Physical (dip)"
                value={
                  t.latest_physical != null ? `${fmtLitres(t.latest_physical)} L` : 'no dip yet'
                }
              />
            </div>
            <Button
              className="h-10"
              disabled={!allShiftsApproved || busy}
              onClick={onRun}
              title={allShiftsApproved ? undefined : 'Approve all the day’s shifts first'}
            >
              {allShiftsApproved ? 'Run reconciliation' : 'Approve shifts to reconcile'}
            </Button>
          </>
        ) : (
          <>
            <div className="flex flex-col gap-1">
              <Row label="Opening book" value={`${fmtLitres(rec.opening_book)} L`} />
              <Row label="Deliveries" value={`+${fmtLitres(rec.deliveries_total)} L`} />
              <Row label="Sales" value={`−${fmtLitres(rec.sales_total)} L`} />
              <Row
                label="Adjustments"
                value={`${parseDecimal(rec.adjustments_total) >= 0 ? '+' : '−'}${fmtLitres(
                  Math.abs(parseDecimal(rec.adjustments_total)),
                )} L`}
              />
              <Row label="Closing book" value={`${fmtLitres(rec.closing_book)} L`} />
              <Row label="Physical (dip)" value={`${fmtLitres(rec.closing_physical)} L`} />
              <Row
                label="Variance"
                value={`${parseDecimal(rec.variance_litres) >= 0 ? '+' : '−'}${fmtLitres(
                  Math.abs(parseDecimal(rec.variance_litres)),
                )} L (${formatMoney(rec.variance_percent)}%)`}
                tone={rec.over_tolerance ? 'danger' : undefined}
              />
              <Row label="Tolerance" value={`±${rec.tolerance_percent}%`} />
            </div>

            {!sealed ? (
              <>
                {/* Inline reasoned adjustment. */}
                <div className="flex flex-col gap-2 border-t border-border pt-3">
                  <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
                    Record adjustment
                  </span>
                  <div className="flex flex-col gap-2 sm:flex-row">
                    <Input
                      className="h-9 sm:w-28"
                      type="number"
                      step="0.001"
                      placeholder="± litres"
                      value={litres}
                      onChange={(e) => setLitres(e.target.value)}
                    />
                    <Input
                      className="h-9 flex-1"
                      placeholder="Reason (e.g. evaporation, leak)"
                      value={reason}
                      onChange={(e) => setReason(e.target.value)}
                    />
                    <Button
                      size="sm"
                      variant="outline"
                      disabled={!canAdjust}
                      onClick={() => {
                        onAdjust(litresNum, reason.trim());
                        setLitres('');
                        setReason('');
                      }}
                    >
                      Add
                    </Button>
                  </div>
                </div>

                <Button className="h-10" disabled={rec.over_tolerance || busy} onClick={onSeal}>
                  {rec.over_tolerance ? 'Resolve variance to seal' : 'Seal reconciliation'}
                </Button>
              </>
            ) : (
              <p className="border-t border-border pt-3 text-xs text-muted-foreground">
                Sealed{rec.sealed_at ? ` · ${new Date(rec.sealed_at).toLocaleString()}` : ''}. The
                physical figure carries forward as the next day&apos;s opening book.
              </p>
            )}
          </>
        )}
      </CardContent>
    </Card>
  );
}

function Row({ label, value, tone }: { label: string; value: string; tone?: 'danger' }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span className={'font-medium tabular-nums' + (tone === 'danger' ? ' text-danger' : '')}>
        {value}
      </span>
    </div>
  );
}
