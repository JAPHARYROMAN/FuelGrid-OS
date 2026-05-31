'use client';

import { useEffect, useState } from 'react';
import { useQuery } from '@tanstack/react-query';

import { SdkError, type InventoryOverviewTank, type RecentVariance } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  ErrorState,
  LoadingState,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres } from '@/lib/money';

// Litre fields arrive as exact decimal strings (book_balance, capacity_litres)
// or display numbers (latest_physical); formatLitres handles both.
function fmtLitres(n: number | string) {
  return formatLitres(n, { fallback: '0' });
}

function fillTone(pct: number): 'success' | 'warning' | 'danger' {
  if (pct <= 15) return 'danger';
  if (pct >= 95) return 'warning';
  return 'success';
}

export default function InventoryPage() {
  const [stationID, setStationID] = useState<string>('');

  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });

  useEffect(() => {
    const first = stations.data?.items?.[0];
    if (!stationID && first) setStationID(first.id);
  }, [stationID, stations.data]);

  const overview = useQuery({
    queryKey: ['inventory-overview', stationID],
    queryFn: ({ signal }) => api.getInventoryOverview(stationID, signal),
    enabled: !!stationID,
  });

  return (
    <div className="flex flex-col gap-5">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-col gap-1">
          <h1 className="text-2xl font-semibold tracking-tight">Inventory</h1>
          <p className="text-sm text-muted-foreground">
            Book stock vs physical, days of stock, and the recent variance trend per tank.
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
              title={forbidden ? 'No access to this station' : "Couldn't load inventory"}
              description={
                forbidden
                  ? "You don't have permission to view this station's inventory."
                  : String((err as Error).message)
              }
              onRetry={forbidden ? undefined : () => overview.refetch()}
            />
          );
        })()
      ) : overview.data.tanks.length === 0 ? (
        <EmptyState title="No tanks" description="This station has no tanks configured yet." />
      ) : (
        <div className="grid gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {overview.data.tanks.map((t) => (
            <TankCard key={t.tank.id} t={t} />
          ))}
        </div>
      )}
    </div>
  );
}

function TankCard({ t }: { t: InventoryOverviewTank }) {
  const last = t.last_reconciliation;
  const fill = Math.max(0, Math.min(100, t.fill_percent));

  return (
    <Card>
      <CardHeader className="flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base">
          {t.tank.name} <span className="font-normal text-muted-foreground">({t.tank.code})</span>
        </CardTitle>
        <Badge tone={fillTone(t.fill_percent)}>{Math.round(t.fill_percent)}%</Badge>
      </CardHeader>
      <CardContent className="flex flex-col gap-3 text-sm">
        {/* Fill bar: book stock against capacity. */}
        <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
          <div
            className={
              'h-full rounded-full ' +
              (fillTone(t.fill_percent) === 'danger'
                ? 'bg-danger'
                : fillTone(t.fill_percent) === 'warning'
                  ? 'bg-warning'
                  : 'bg-success')
            }
            style={{ width: `${fill}%` }}
          />
        </div>

        <div className="flex flex-col gap-1">
          <Row label="Book stock" value={`${fmtLitres(t.book_balance)} L`} />
          <Row
            label="Physical (dip)"
            value={t.latest_physical != null ? `${fmtLitres(t.latest_physical)} L` : 'no dip yet'}
          />
          <Row label="Capacity" value={`${fmtLitres(t.tank.capacity_litres)} L`} />
          <Row
            label="Days of stock"
            value={t.days_of_stock != null ? t.days_of_stock.toFixed(1) : '—'}
          />
        </div>

        {/* Last reconciliation tolerance status. */}
        <div className="flex items-center justify-between border-t border-border pt-3">
          <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
            Last reconciliation
          </span>
          {last ? (
            <div className="flex items-center gap-2">
              <span className="text-xs text-muted-foreground">{last.business_date}</span>
              <Badge tone={last.over_tolerance ? 'danger' : 'success'}>
                {last.over_tolerance ? 'over tolerance' : 'within tolerance'}
              </Badge>
            </div>
          ) : (
            <span className="text-xs text-muted-foreground">never</span>
          )}
        </div>

        {/* Variance trend. */}
        {t.recent_variances.length > 0 ? <VarianceTrend rows={t.recent_variances} /> : null}
      </CardContent>
    </Card>
  );
}

function VarianceTrend({ rows }: { rows: RecentVariance[] }) {
  // Oldest -> newest for a left-to-right reading.
  const ordered = [...rows].reverse();
  return (
    <div className="flex flex-col gap-1.5">
      <span className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">
        Variance trend
      </span>
      <div className="flex items-end gap-1">
        {ordered.map((v) => (
          <div
            key={v.operating_day_id}
            title={`${v.business_date}: ${v.variance_litres.toFixed(1)} L${
              v.over_tolerance ? ' (over tolerance)' : ''
            }`}
            className={
              'h-6 flex-1 rounded-sm ' + (v.over_tolerance ? 'bg-danger/60' : 'bg-success/50')
            }
            style={{
              opacity: 0.4 + Math.min(0.6, Math.abs(v.variance_percent) / 5),
            }}
          />
        ))}
      </div>
    </div>
  );
}

function Row({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex items-center justify-between">
      <span className="text-muted-foreground">{label}</span>
      <span className="font-medium tabular-nums">{value}</span>
    </div>
  );
}
