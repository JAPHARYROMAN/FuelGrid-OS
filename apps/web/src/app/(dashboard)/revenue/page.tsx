'use client';

import { useEffect, useState } from 'react';
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query';
import { Coins, Percent, Receipt, Wallet } from 'lucide-react';

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
  PageHeader,
  Skeleton,
  Stat,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

function money(n?: string) {
  return formatMoney(n);
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
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Commerce"
        title="Revenue"
        description="Recognized revenue, margin, tender mix, and receivables."
        actions={
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
          ) : undefined
        }
      />

      {actionError ? (
        <p className="rounded-md bg-danger/10 px-3 py-2 text-sm text-danger" role="alert">
          {actionError}
        </p>
      ) : null}

      {stations.isPending ? (
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
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
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
      ) : (
        <>
          {/* Today's summary */}
          {overview.data.summary ? (
            <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4">
              <Stat
                label="Gross"
                value={money(overview.data.summary.gross_revenue)}
                hint={
                  overview.data.day
                    ? `Operating day · ${overview.data.day.business_date}`
                    : undefined
                }
                icon={<Coins />}
              />
              <Stat
                label="Net"
                value={money(overview.data.summary.net_revenue)}
                icon={<Wallet />}
              />
              <Stat label="Tax" value={money(overview.data.summary.tax_total)} icon={<Receipt />} />
              <Stat
                label="Margin"
                value={money(overview.data.summary.margin_total)}
                hint={`COGS ${money(overview.data.summary.cogs_total)}`}
                icon={<Percent />}
              />
            </section>
          ) : null}

          {/* Operating day actions */}
          <Card>
            <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
              <CardTitle>
                {overview.data.day
                  ? `Operating day · ${overview.data.day.business_date}`
                  : 'No active operating day'}
              </CardTitle>
              {overview.data.day ? (
                <PermissionGate permission="period.lock">
                  <Button size="sm" disabled={closeDay.isPending} onClick={() => closeDay.mutate()}>
                    {closeDay.isPending ? 'Closing…' : 'Close & lock day'}
                  </Button>
                </PermissionGate>
              ) : null}
            </CardHeader>
            {!overview.data.summary ? (
              <CardContent className="text-sm text-muted-foreground">
                No recognized sales for the active day yet.
              </CardContent>
            ) : null}
          </Card>

          {/* Tender mix */}
          {overview.data.tenders ? (
            <Card>
              <CardHeader>
                <CardTitle>Tender mix</CardTitle>
              </CardHeader>
              <CardContent className="grid grid-cols-2 gap-3 md:grid-cols-3 lg:grid-cols-6">
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
                <CardTitle>Recent days</CardTitle>
              </CardHeader>
              <CardContent className="flex flex-col gap-1">
                {overview.data.recent_days.map((d: RevenueDay) => (
                  <div
                    key={d.id}
                    className="-mx-2 flex items-center justify-between gap-3 rounded-lg px-2 py-2.5"
                  >
                    <span className="text-sm text-muted-foreground">{d.business_date}</span>
                    <span className="flex items-center gap-3">
                      <span className="font-mono text-sm tabular-nums text-foreground">
                        gross {money(d.gross_revenue)}
                      </span>
                      <span className="font-mono text-sm tabular-nums text-muted-foreground">
                        margin {money(d.margin_total)}
                      </span>
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
              <CardTitle>Receivables</CardTitle>
            </CardHeader>
            <CardContent>
              {aging.isPending ? (
                <div className="flex flex-col gap-2">
                  {Array.from({ length: 3 }).map((_, i) => (
                    <Skeleton key={i} className="h-14 rounded-lg" />
                  ))}
                </div>
              ) : (aging.data?.items?.length ?? 0) === 0 ? (
                <EmptyState title="No outstanding balances" description="No customer owes money." />
              ) : (
                <div className="flex flex-col gap-1">
                  {aging.data!.items.map((c) => (
                    <div
                      key={c.customer_id}
                      className="-mx-2 flex items-center justify-between gap-3 rounded-lg px-2 py-2.5"
                    >
                      <span className="text-sm text-foreground">
                        {c.name} <span className="text-muted-foreground">({c.code})</span>
                      </span>
                      <span className="font-mono text-sm font-medium tabular-nums text-foreground">
                        {money(c.balance)}
                      </span>
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
    <div className="flex flex-col gap-0.5 rounded-lg border border-border/80 bg-muted/40 px-3 py-2.5">
      <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </span>
      <span className="font-mono text-sm font-semibold tabular-nums text-foreground">{value}</span>
    </div>
  );
}
