'use client';

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';

import { SdkError, type RevenueDay } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

function money(n?: string) {
  if (n == null) return '—';
  const v = Number(n);
  return Number.isFinite(v) ? v.toLocaleString(undefined, { minimumFractionDigits: 2 }) : n;
}

export default function RevenuePage() {
  const qc = useQueryClient();
  const [stationID, setStationID] = useState('');
  const [actionError, setActionError] = useState<string | null>(null);

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const overviewKey = ['revenue-overview', stationID];
  const overview = useQuery({
    queryKey: overviewKey,
    queryFn: ({ signal }) => api.getRevenueOverview(stationID, signal),
    enabled: !!stationID,
  });
  const aging = useQuery({
    queryKey: ['ar-aging'],
    queryFn: ({ signal }) => api.getARaging(signal),
  });

  const dayID = overview.data?.day?.id;
  const closeDay = useMutation({
    mutationFn: async () => {
      const rd = await api.computeRevenueDay(stationID, dayID!);
      return api.lockRevenueDay(rd.id);
    },
    onSuccess: () => {
      setActionError(null);
      qc.invalidateQueries({ queryKey: overviewKey });
    },
    onError: (e) => setActionError(e instanceof SdkError ? e.message : 'Could not close the day'),
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Revenue</h1>
          <p className="text-sm text-muted-foreground">
            Recognized revenue, margin, tender mix, and receivables.
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
      ) : overview.isError ? (
        (() => {
          const err = overview.error;
          const forbidden = err instanceof SdkError && err.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access to this station' : "Couldn't load revenue"}
              description={
                forbidden
                  ? "You don't have permission to view this station's revenue."
                  : String((err as Error).message)
              }
              onRetry={forbidden ? undefined : () => overview.refetch()}
            />
          );
        })()
      ) : overview.isPending ? (
        <LoadingState />
      ) : (
        <>
          {/* Today's summary */}
          <Card>
            <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
              <CardTitle className="text-base">
                {overview.data.day
                  ? `Operating day · ${overview.data.day.business_date}`
                  : 'No active operating day'}
              </CardTitle>
              {overview.data.day ? (
                <Button size="sm" disabled={closeDay.isPending} onClick={() => closeDay.mutate()}>
                  {closeDay.isPending ? 'Closing…' : 'Close & lock day'}
                </Button>
              ) : null}
            </CardHeader>
            {overview.data.summary ? (
              <CardContent className="grid grid-cols-2 gap-3 text-sm md:grid-cols-3 lg:grid-cols-5">
                <Metric label="Gross" value={money(overview.data.summary.gross_revenue)} />
                <Metric label="Net" value={money(overview.data.summary.net_revenue)} />
                <Metric label="Tax" value={money(overview.data.summary.tax_total)} />
                <Metric label="COGS" value={money(overview.data.summary.cogs_total)} />
                <Metric label="Margin" value={money(overview.data.summary.margin_total)} />
              </CardContent>
            ) : (
              <CardContent className="text-sm text-muted-foreground">
                No recognized sales for the active day yet.
              </CardContent>
            )}
          </Card>

          {/* Tender mix */}
          {overview.data.tenders ? (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Tender mix</CardTitle>
              </CardHeader>
              <CardContent className="grid grid-cols-2 gap-3 text-sm md:grid-cols-3 lg:grid-cols-6">
                <Metric label="Cash" value={money(overview.data.tenders.cash)} />
                <Metric label="Mobile money" value={money(overview.data.tenders.mobile_money)} />
                <Metric label="Card" value={money(overview.data.tenders.card)} />
                <Metric label="Credit" value={money(overview.data.tenders.credit)} />
                <Metric label="Voucher" value={money(overview.data.tenders.voucher)} />
                <Metric label="Total" value={money(overview.data.tenders.total)} />
              </CardContent>
            </Card>
          ) : null}

          {/* Recent trend */}
          {overview.data.recent_days.length > 0 ? (
            <Card>
              <CardHeader>
                <CardTitle className="text-base">Recent days</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1.5 text-sm">
                {overview.data.recent_days.map((d: RevenueDay) => (
                  <div key={d.id} className="flex items-center justify-between gap-2">
                    <span className="text-muted-foreground">{d.business_date}</span>
                    <span className="flex items-center gap-3 tabular-nums">
                      <span>gross {money(d.gross_revenue)}</span>
                      <span>margin {money(d.margin_total)}</span>
                      <Badge tone={d.status === 'locked' ? 'neutral' : 'warning'}>{d.status}</Badge>
                    </span>
                  </div>
                ))}
              </CardContent>
            </Card>
          ) : null}

          {/* AR aging */}
          <Card>
            <CardHeader>
              <CardTitle className="text-base">Receivables</CardTitle>
            </CardHeader>
            <CardContent className="text-sm">
              {aging.isPending ? (
                <LoadingState />
              ) : (aging.data?.items?.length ?? 0) === 0 ? (
                <EmptyState title="No outstanding balances" description="No customer owes money." />
              ) : (
                <div className="flex flex-col gap-1.5">
                  {aging.data!.items.map((c) => (
                    <div key={c.customer_id} className="flex items-center justify-between">
                      <span>
                        {c.name} <span className="text-muted-foreground">({c.code})</span>
                      </span>
                      <span className="font-medium tabular-nums">{money(c.balance)}</span>
                    </div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
}

function Metric({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex flex-col gap-0.5 rounded-md bg-muted/40 px-3 py-2">
      <span className="text-xs uppercase tracking-wider text-muted-foreground">{label}</span>
      <span className="font-semibold tabular-nums">{value}</span>
    </div>
  );
}
