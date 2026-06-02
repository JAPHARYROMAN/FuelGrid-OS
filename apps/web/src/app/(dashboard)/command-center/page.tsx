'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import {
  AlertTriangle,
  ArrowDownLeft,
  ArrowRight,
  ArrowUpRight,
  Building2,
  ChevronRight,
  CircleDollarSign,
  ClipboardCheck,
  Droplet,
  Fuel,
  Gauge,
  PackageCheck,
  Receipt,
  Rocket,
  ShieldAlert,
  TrendingUp,
} from 'lucide-react';

import { SdkError, type RiskAlert, type StationRank } from '@fuelgrid/sdk';
import {
  Badge,
  BarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  chartColors,
  DataQualityCard,
  EmptyState,
  ErrorState,
  InsightCard,
  MetricCard,
  PageHeader,
  RiskBadge,
  ShiftTimeline,
  Skeleton,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  type InsightSeverity,
  type RiskSeverity,
  type ShiftMilestone,
} from '@fuelgrid/ui';

import { PermissionGate } from '@/components/permission-gate';
import { api } from '@/lib/api';
import { formatLitres, formatMoney, sumMoney } from '@/lib/money';
import { setSentryUser } from '@/lib/sentry';

/* -------------------------------------------------------------------------- */
/*  Small helpers                                                             */
/* -------------------------------------------------------------------------- */

/** A `403` from a station/network read means "not your scope", not a failure. */
function isForbidden(err: unknown): boolean {
  return err instanceof SdkError && err.status === 403;
}

/** Map a backend severity string onto the RiskBadge five-level scale. */
function riskSeverity(s: string | undefined): RiskSeverity {
  switch ((s ?? '').toLowerCase()) {
    case 'critical':
      return 'critical';
    case 'high':
      return 'high';
    case 'medium':
      return 'medium';
    case 'low':
      return 'low';
    default:
      return 'info';
  }
}

/** Render a clock label (HH:MM) from an RFC3339 timestamp, or em-dash. */
function clock(ts?: string): string {
  if (!ts) return '—';
  const d = new Date(ts);
  if (Number.isNaN(d.getTime())) return '—';
  return d.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit' });
}

/** Sum a list of decimal-string balances without float drift in the label. */
function sumBalances(items: { balance: string }[]): number {
  return items.reduce((acc, c) => acc + (Number(c.balance) || 0), 0);
}

interface DerivedInsight {
  severity: InsightSeverity;
  message: string;
  recommendedAction?: string;
}

/* -------------------------------------------------------------------------- */
/*  Page                                                                      */
/* -------------------------------------------------------------------------- */

export default function CommandCenterPage() {
  const meQuery = useQuery({ queryKey: ['me'], queryFn: ({ signal }) => api.me(signal) });
  const me = meQuery.data;
  React.useEffect(() => {
    if (me) setSentryUser({ id: me.user_id, tenantId: me.tenant_id });
  }, [me]);

  // ---- Network-level reads (tenant scope) --------------------------------
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const enterprise = useQuery({
    queryKey: ['enterprise-overview'],
    queryFn: ({ signal }) => api.getEnterpriseOverview({}, signal),
  });
  const ranking = useQuery({
    queryKey: ['enterprise-ranking'],
    queryFn: ({ signal }) => api.getStationRanking({}, signal),
  });
  const riskOverview = useQuery({
    queryKey: ['risk-overview'],
    queryFn: ({ signal }) => api.getRiskOverview(signal),
  });
  const riskAlerts = useQuery({
    queryKey: ['risk-alerts', 'open'],
    queryFn: ({ signal }) => api.listRiskAlerts({ status: 'open' }, signal),
  });
  const aging = useQuery({
    queryKey: ['ar-aging'],
    queryFn: ({ signal }) => api.getARaging(signal),
  });
  const reports = useQuery({
    queryKey: ['reports-overview'],
    queryFn: ({ signal }) => api.getReportsOverview(signal),
  });

  const stationItems = React.useMemo(() => stations.data?.items ?? [], [stations.data]);
  const activeStations = React.useMemo(
    () => stationItems.filter((s) => s.status === 'active'),
    [stationItems],
  );

  // The hero focuses one station for the operational (station-scoped) panels.
  // Default to the first active site; the user can re-point it.
  const [focusId, setFocusId] = React.useState<string>('');
  React.useEffect(() => {
    const fallback = activeStations[0] ?? stationItems[0];
    if (!focusId && fallback) {
      setFocusId(fallback.id);
    }
  }, [focusId, stationItems, activeStations]);
  const focusStation = stationItems.find((s) => s.id === focusId);

  // ---- Station-scoped reads (the focused site) ---------------------------
  const revenue = useQuery({
    queryKey: ['revenue-overview', focusId],
    queryFn: ({ signal }) => api.getRevenueOverview(focusId, signal),
    enabled: !!focusId,
  });
  const inventory = useQuery({
    queryKey: ['inventory-overview', focusId],
    queryFn: ({ signal }) => api.getInventoryOverview(focusId, signal),
    enabled: !!focusId,
  });
  const reconciliation = useQuery({
    queryKey: ['reconciliation-overview', focusId],
    queryFn: ({ signal }) => api.getReconciliationOverview(focusId, {}, signal),
    enabled: !!focusId,
  });
  const operatingDayId = revenue.data?.day?.id ?? reconciliation.data?.day?.id;
  const shifts = useQuery({
    queryKey: ['shifts', focusId, operatingDayId],
    queryFn: ({ signal }) =>
      api.listShifts(focusId, operatingDayId ? { operatingDayID: operatingDayId } : {}, signal),
    enabled: !!focusId,
  });
  const deliveries = useQuery({
    queryKey: ['station-deliveries', focusId],
    queryFn: ({ signal }) => api.listStationDeliveries(focusId, signal),
    enabled: !!focusId,
  });

  // ---- Setup CTA: surface only once the core reads have resolved ---------
  const setupResolved = !stations.isPending && !enterprise.isPending;
  const setupIncomplete = setupResolved && stationItems.length === 0;

  /* ----------------------------------------------------------------------- */
  /*  Derived figures                                                        */
  /* ----------------------------------------------------------------------- */

  const summary = revenue.data?.summary;
  const tenders = revenue.data?.tenders;
  const tanks = inventory.data?.tanks ?? [];
  // latest_physical is a decimal STRING; sum decimal-safe (never Number()+reduce).
  const totalStockLitres = sumMoney(tanks.map((t) => t.latest_physical));
  const stockoutTanks = tanks
    .filter((t) => t.days_of_stock != null && t.days_of_stock <= 2)
    .sort((a, b) => (a.days_of_stock ?? 0) - (b.days_of_stock ?? 0));
  const lowestDays = tanks
    .map((t) => t.days_of_stock)
    .filter((d): d is number => d != null)
    .sort((a, b) => a - b)[0];

  const reconTanks = reconciliation.data?.tanks ?? [];
  const overToleranceCount = reconTanks.filter((t) => t.reconciliation?.over_tolerance).length;
  const allShiftsApproved = reconciliation.data?.all_shifts_approved ?? false;

  const shiftItems = shifts.data?.items ?? [];
  const openShifts = shiftItems.filter((s) => s.status === 'open');
  const closedAwaitingApproval = shiftItems.filter((s) => s.status === 'closed');

  // Cash position for the focused day: the RevenueDay carrying cash_variance is
  // in recent_days (day on the overview is the OperatingDay). Prefer the day
  // that matches the open operating day, else the newest recorded day.
  const recentDays = revenue.data?.recent_days ?? [];
  const activeRevenueDay =
    recentDays.find((d) => d.operating_day_id === revenue.data?.day?.id) ?? recentDays[0];
  const cashVariance = activeRevenueDay?.cash_variance;
  const cashVarianceNum = cashVariance != null ? Number(cashVariance) : null;

  const openAlerts = riskAlerts.data?.items ?? [];
  const criticalAlerts = openAlerts
    .slice()
    .sort((a, b) => (b.score ?? 0) - (a.score ?? 0))
    .slice(0, 5);
  const openBySeverity = riskOverview.data?.open_by_severity ?? {};

  const agingItems = aging.data?.items ?? [];
  const agingTotal = sumBalances(agingItems);
  const topDebtors = agingItems
    .slice()
    .sort((a, b) => (Number(b.balance) || 0) - (Number(a.balance) || 0))
    .slice(0, 5);

  const recentDeliveries = (deliveries.data?.items ?? [])
    .slice()
    .sort((a, b) => new Date(b.received_at).getTime() - new Date(a.received_at).getTime())
    .slice(0, 5);

  // Pending daily closes across the network: report-overhead headline, if present.
  const reportCategories = reports.data?.categories ?? [];

  /* ----------------------------------------------------------------------- */
  /*  Deterministic insights (NO AI text — pure rules over computed data)    */
  /* ----------------------------------------------------------------------- */

  const insights = React.useMemo<DerivedInsight[]>(() => {
    const out: DerivedInsight[] = [];

    // Critical risk alerts open in the network.
    const critOpen = Number(openBySeverity.critical ?? 0);
    if (critOpen > 0) {
      out.push({
        severity: 'critical',
        message: `${critOpen} critical risk ${critOpen === 1 ? 'alert is' : 'alerts are'} open and unresolved.`,
        recommendedAction: 'Triage in Risk & Intelligence before close.',
      });
    }

    // Stock runway on the focused station.
    if (stockoutTanks.length > 0) {
      out.push({
        severity: lowestDays != null && lowestDays <= 1 ? 'critical' : 'warning',
        message: `${stockoutTanks.length} tank${stockoutTanks.length === 1 ? '' : 's'} at ${focusStation?.name ?? 'the focused station'} ${stockoutTanks.length === 1 ? 'has' : 'have'} ≤ 2 days of stock.`,
        recommendedAction: 'Raise a purchase order to avoid a stockout.',
      });
    }

    // Reconciliation variance over tolerance.
    if (overToleranceCount > 0) {
      out.push({
        severity: 'warning',
        message: `${overToleranceCount} tank${overToleranceCount === 1 ? '' : 's'} closed over the variance tolerance today.`,
        recommendedAction: 'Review the fuel reconciliation and log adjustments.',
      });
    }

    // Cash variance on the focused day.
    if (cashVarianceNum != null && Math.abs(cashVarianceNum) >= 0.01) {
      out.push({
        severity: Math.abs(cashVarianceNum) >= 1000 ? 'warning' : 'info',
        message: `Cash ${cashVarianceNum < 0 ? 'shortage' : 'excess'} of ${formatMoney(Math.abs(cashVarianceNum).toFixed(2))} on the active day.`,
        recommendedAction: 'Reconcile the till against banked deposits.',
      });
    }

    // Approvals waiting in the enterprise queue.
    const approvals = enterprise.data?.approvals_waiting ?? 0;
    if (approvals > 0) {
      out.push({
        severity: 'info',
        message: `${approvals} approval${approvals === 1 ? '' : 's'} waiting in the network queue.`,
        recommendedAction: 'Clear the approvals queue to lock the day.',
      });
    }

    // Shifts closed but not yet approved.
    if (closedAwaitingApproval.length > 0) {
      out.push({
        severity: 'info',
        message: `${closedAwaitingApproval.length} shift${closedAwaitingApproval.length === 1 ? '' : 's'} closed and awaiting approval at ${focusStation?.name ?? 'the focused station'}.`,
        recommendedAction: 'Approve shifts to enable the daily close.',
      });
    }

    // Credit exposure trending high.
    if (agingTotal > 0) {
      out.push({
        severity: 'info',
        message: `Outstanding customer credit stands at ${formatMoney(agingTotal.toFixed(2))} across ${agingItems.length} account${agingItems.length === 1 ? '' : 's'}.`,
      });
    }

    return out;
  }, [
    openBySeverity,
    stockoutTanks.length,
    lowestDays,
    overToleranceCount,
    cashVarianceNum,
    enterprise.data,
    closedAwaitingApproval.length,
    agingTotal,
    agingItems.length,
    focusStation,
  ]);

  /* ----------------------------------------------------------------------- */
  /*  Data-quality warnings (advisory — figures may be incomplete)           */
  /* ----------------------------------------------------------------------- */

  const dataQuality = React.useMemo<string[]>(() => {
    const out: string[] = [];
    if (focusStation && !revenue.isPending && !revenue.isError && !revenue.data?.day) {
      out.push(
        `No operating day is open at ${focusStation.name} — today's figures are provisional.`,
      );
    }
    if (focusStation && !reconciliation.isError && !allShiftsApproved && reconTanks.length > 0) {
      out.push('Not all shifts are approved yet — the reconciliation may change before close.');
    }
    if (enterprise.data?.projection_rebuilt_at) {
      out.push(
        `Network figures reflect projections rebuilt ${clock(enterprise.data.projection_rebuilt_at)}; very recent activity may not be included.`,
      );
    }
    if (tanks.length > 0 && tanks.every((t) => t.latest_physical == null)) {
      out.push('No recent dip readings — current stock is estimated from book balances.');
    }
    return out;
  }, [
    focusStation,
    revenue.isPending,
    revenue.isError,
    revenue.data,
    reconciliation.isError,
    allShiftsApproved,
    reconTanks.length,
    enterprise.data,
    tanks,
  ]);

  /* ----------------------------------------------------------------------- */
  /*  Shift timeline for the most recent shift at the focused station        */
  /* ----------------------------------------------------------------------- */

  const timelineMilestones = React.useMemo<ShiftMilestone[]>(() => {
    const latest = shiftItems
      .slice()
      .sort((a, b) => new Date(b.opened_at).getTime() - new Date(a.opened_at).getTime())[0];
    if (!latest) return [];
    return [
      { label: 'Shift opened', timestamp: clock(latest.opened_at), status: 'done' },
      {
        label: 'Shift closed',
        timestamp: clock(latest.closed_at),
        status: latest.closed_at ? 'done' : latest.status === 'open' ? 'current' : 'pending',
      },
      {
        label: 'Approved',
        timestamp: clock(latest.approved_at),
        status: latest.approved_at ? 'done' : 'pending',
      },
    ];
  }, [shiftItems]);

  /* ----------------------------------------------------------------------- */
  /*  Render                                                                 */
  /* ----------------------------------------------------------------------- */

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Executive command center"
        title="How is my fuel business performing right now?"
        description="A live operational read on the network — revenue, stock, cash, risk and the signals that need a decision today."
        actions={
          stationItems.length > 0 ? (
            <label className="flex items-center gap-2 text-sm">
              <span className="text-muted-foreground">Focus station</span>
              <select
                aria-label="Focus station"
                className="h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
                value={focusId}
                onChange={(e) => setFocusId(e.target.value)}
              >
                {stationItems.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name} ({s.code})
                  </option>
                ))}
              </select>
            </label>
          ) : undefined
        }
      />

      {setupIncomplete ? (
        <Link
          href="/setup"
          className="group flex items-center gap-4 rounded-xl border border-accent/40 bg-accent-muted/40 px-5 py-4 transition-colors hover:bg-accent-muted/60"
        >
          <span className="flex size-10 shrink-0 items-center justify-center rounded-full bg-accent/15 text-accent">
            <Rocket className="size-5" />
          </span>
          <div className="flex min-w-0 flex-1 flex-col">
            <span className="font-medium text-foreground">Finish setting up your tenant</span>
            <span className="text-sm text-muted-foreground">
              No stations exist yet. Walk through the guided checklist to get operational.
            </span>
          </div>
          <span className="inline-flex shrink-0 items-center gap-1 text-sm font-medium text-accent">
            Open setup
            <ArrowRight className="size-4 transition-transform group-hover:translate-x-0.5" />
          </span>
        </Link>
      ) : null}

      {/* ---- KPI HERO ROW ------------------------------------------------ */}
      {enterprise.isError && !isForbidden(enterprise.error) ? (
        <ErrorState
          title="Couldn't load the network overview"
          description={String((enterprise.error as Error).message)}
          onRetry={() => enterprise.refetch()}
        />
      ) : (
        <section
          aria-label="Key metrics"
          className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4"
        >
          <MetricCard
            label="Revenue today"
            sublabel={focusStation?.code}
            value={formatMoney(summary?.gross_revenue)}
            hint={revenue.data?.day ? 'Recognized gross' : 'No open day'}
            icon={<TrendingUp />}
            loading={!!focusId && revenue.isPending}
          />
          <MetricCard
            label="Litres sold today"
            sublabel={focusStation?.code}
            value={formatLitres(summary?.litres_sold ?? 0)}
            unit="L"
            hint={`${summary?.sale_count ?? 0} sales`}
            icon={<Fuel />}
            loading={!!focusId && revenue.isPending}
          />
          <MetricCard
            label="Current fuel stock"
            sublabel={focusStation?.code}
            value={formatLitres(totalStockLitres)}
            unit="L"
            trend={stockoutTanks.length > 0 ? 'down' : 'flat'}
            trendValue={lowestDays != null ? `${lowestDays.toFixed(1)}d lowest runway` : undefined}
            hint={`${tanks.length} tanks`}
            icon={<Droplet />}
            loading={!!focusId && inventory.isPending}
          />
          <MetricCard
            label="Network margin"
            value={formatMoney(enterprise.data?.margin_total)}
            hint="All stations"
            trend="up"
            icon={<CircleDollarSign />}
            loading={enterprise.isPending}
          />
        </section>
      )}

      {/* ---- SECONDARY KPI STRIP ----------------------------------------- */}
      <section
        aria-label="Operational signals"
        className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4"
      >
        <MetricCard
          label="Cash reconciliation"
          sublabel={focusStation?.code}
          value={formatMoney(tenders?.cash)}
          hint={
            cashVarianceNum == null
              ? 'No variance recorded'
              : cashVarianceNum === 0
                ? 'Balanced'
                : `${cashVarianceNum < 0 ? 'Short' : 'Over'} ${formatMoney(Math.abs(cashVarianceNum).toFixed(2))}`
          }
          trend={cashVarianceNum != null && cashVarianceNum < 0 ? 'down' : 'flat'}
          icon={<Receipt />}
          loading={!!focusId && revenue.isPending}
        />
        <MetricCard
          label="Open shifts"
          sublabel={focusStation?.code}
          value={openShifts.length}
          hint={`${closedAwaitingApproval.length} awaiting approval`}
          icon={<ClipboardCheck />}
          loading={!!focusId && shifts.isPending}
        />
        <MetricCard
          label="Fuel variance"
          sublabel={focusStation?.code}
          value={overToleranceCount}
          hint={`of ${reconTanks.length} tanks over tolerance`}
          trend={overToleranceCount > 0 ? 'down' : 'flat'}
          icon={<Gauge />}
          loading={!!focusId && reconciliation.isPending}
        />
        <MetricCard
          label="Open risk alerts"
          value={riskOverview.data?.open_total ?? openAlerts.length}
          hint={`${Number(openBySeverity.critical ?? 0)} critical`}
          trend={Number(openBySeverity.critical ?? 0) > 0 ? 'down' : 'flat'}
          icon={<ShieldAlert />}
          loading={riskOverview.isPending && riskAlerts.isPending}
        />
      </section>

      {/* ---- DATA QUALITY ------------------------------------------------- */}
      {dataQuality.length > 0 ? (
        <DataQualityCard level="warning" title="Data quality" messages={dataQuality} />
      ) : null}

      {/* ---- ALERTS / INSIGHTS + STATION RANKING ------------------------- */}
      <section className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Critical alerts */}
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Critical alerts</CardTitle>
              <p className="text-sm text-muted-foreground">Highest-scoring open risk signals.</p>
            </div>
            <Link
              href="/risk"
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              All risk
              <ChevronRight className="size-4" />
            </Link>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {riskAlerts.isPending ? (
              <div className="flex flex-col gap-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : riskAlerts.isError && isForbidden(riskAlerts.error) ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                You don&apos;t have access to risk alerts.
              </p>
            ) : criticalAlerts.length === 0 ? (
              <EmptyState
                title="No open alerts"
                description="Nothing needs attention right now."
                icon={<ShieldAlert className="size-7" />}
              />
            ) : (
              criticalAlerts.map((a: RiskAlert) => (
                <Link
                  key={a.id}
                  href="/risk"
                  className="group flex items-start gap-3 rounded-lg border border-border/70 px-3 py-2.5 transition-colors hover:bg-muted"
                >
                  <RiskBadge severity={riskSeverity(a.severity)}>{a.severity}</RiskBadge>
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium text-foreground">
                      {a.detail ?? a.alert_type}
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">
                      {a.rule_code ?? a.alert_type}
                      {a.amount ? ` · ${formatMoney(a.amount)}` : ''}
                    </span>
                  </div>
                  <ChevronRight className="mt-0.5 size-4 shrink-0 text-muted-foreground transition-transform group-hover:translate-x-0.5" />
                </Link>
              ))
            )}
          </CardContent>
        </Card>

        {/* Insights */}
        <Card>
          <CardHeader>
            <CardTitle>What needs a decision</CardTitle>
            <p className="text-sm text-muted-foreground">
              Rule-based observations across the network.
            </p>
          </CardHeader>
          <CardContent className="flex flex-col gap-2">
            {insights.length === 0 ? (
              <EmptyState
                title="All clear"
                description="No outstanding signals to action."
                icon={<ClipboardCheck className="size-7" />}
              />
            ) : (
              insights.map((ins, i) => (
                <InsightCard
                  key={i}
                  severity={ins.severity}
                  message={ins.message}
                  recommendedAction={ins.recommendedAction}
                />
              ))
            )}
          </CardContent>
        </Card>

        {/* Station ranking */}
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Station ranking</CardTitle>
              <p className="text-sm text-muted-foreground">Top sites by gross revenue.</p>
            </div>
            <Link
              href="/enterprise"
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              Enterprise
              <ChevronRight className="size-4" />
            </Link>
          </CardHeader>
          <CardContent>
            {ranking.isPending ? (
              <div className="flex flex-col gap-2">
                {Array.from({ length: 4 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 rounded-lg" />
                ))}
              </div>
            ) : ranking.isError && isForbidden(ranking.error) ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                You don&apos;t have enterprise access.
              </p>
            ) : (ranking.data?.items?.length ?? 0) === 0 ? (
              <EmptyState
                title="No ranked stations"
                description="Rankings appear once projections are built."
                icon={<TrendingUp className="size-7" />}
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-10">#</TableHead>
                    <TableHead>Station</TableHead>
                    <TableHead className="text-right">Gross</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {ranking.data!.items.slice(0, 6).map((s: StationRank, i: number) => (
                    <TableRow key={s.station_id}>
                      <TableCell>
                        <Badge tone="neutral">#{i + 1}</Badge>
                      </TableCell>
                      <TableCell className="font-medium text-foreground">{s.name}</TableCell>
                      <TableCell className="text-right font-mono font-medium tabular-nums">
                        {formatMoney(s.gross_revenue)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>
      </section>

      {/* ---- NETWORK FINANCE STRIP --------------------------------------- */}
      {!enterprise.isError ? (
        <section
          aria-label="Network finance"
          className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-4"
        >
          <MetricCard
            label="Network gross revenue"
            value={formatMoney(enterprise.data?.gross_revenue)}
            hint="All stations"
            icon={<TrendingUp />}
            loading={enterprise.isPending}
          />
          <MetricCard
            label="AR outstanding"
            value={formatMoney(enterprise.data?.ar_outstanding ?? agingTotal.toFixed(2))}
            hint="Customer credit"
            icon={<ArrowDownLeft />}
            loading={enterprise.isPending && aging.isPending}
          />
          <MetricCard
            label="AP outstanding"
            value={formatMoney(enterprise.data?.ap_outstanding)}
            hint="Supplier payables"
            icon={<ArrowUpRight />}
            loading={enterprise.isPending}
          />
          <MetricCard
            label="Open incidents"
            value={enterprise.data?.open_incidents ?? 0}
            hint={`${enterprise.data?.approvals_waiting ?? 0} approvals waiting`}
            icon={<AlertTriangle />}
            loading={enterprise.isPending}
          />
        </section>
      ) : null}

      {/* ---- SECONDARY PANELS -------------------------------------------- */}
      <section className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Stockout risk */}
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Stockout risk</CardTitle>
              <p className="text-sm text-muted-foreground">
                Tanks at {focusStation?.name ?? 'the focused station'} by days of stock.
              </p>
            </div>
            <PermissionGate permission="inventory.read" stationId={focusId} mode="hide">
              <Link
                href={focusId ? `/stations/${focusId}` : '/stations'}
                className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
              >
                Station
                <ChevronRight className="size-4" />
              </Link>
            </PermissionGate>
          </CardHeader>
          <CardContent className="flex flex-col gap-1">
            {!focusId || inventory.isPending ? (
              <div className="flex flex-col gap-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : inventory.isError && isForbidden(inventory.error) ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                You don&apos;t have inventory access for this station.
              </p>
            ) : tanks.length === 0 ? (
              <EmptyState
                title="No tanks"
                description="No tanks are configured at this station."
                icon={<Droplet className="size-7" />}
              />
            ) : (
              tanks
                .slice()
                .sort((a, b) => (a.days_of_stock ?? Infinity) - (b.days_of_stock ?? Infinity))
                .slice(0, 5)
                .map((t) => {
                  const days = t.days_of_stock;
                  const tone: RiskSeverity =
                    days == null
                      ? 'info'
                      : days <= 1
                        ? 'critical'
                        : days <= 2
                          ? 'high'
                          : days <= 4
                            ? 'medium'
                            : 'low';
                  return (
                    <div
                      key={t.tank.id}
                      className="flex items-center gap-3 rounded-lg border border-border/60 px-3 py-2.5"
                    >
                      <div className="flex min-w-0 flex-1 flex-col">
                        <span className="truncate text-sm font-medium text-foreground">
                          {t.tank.name}
                        </span>
                        <span className="font-mono text-xs text-muted-foreground">
                          {formatLitres(t.latest_physical ?? 0)} L · {t.fill_percent.toFixed(0)}%
                          full
                        </span>
                      </div>
                      <RiskBadge severity={tone}>
                        {days == null ? '—' : `${days.toFixed(1)}d`}
                      </RiskBadge>
                    </div>
                  );
                })
            )}
          </CardContent>
        </Card>

        {/* Shift timeline */}
        <Card>
          <CardHeader>
            <CardTitle>Latest shift</CardTitle>
            <p className="text-sm text-muted-foreground">
              Lifecycle of the most recent shift at {focusStation?.name ?? 'the focused station'}.
            </p>
          </CardHeader>
          <CardContent>
            {!focusId || shifts.isPending ? (
              <Skeleton className="h-32 rounded-lg" />
            ) : shifts.isError && isForbidden(shifts.error) ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                You don&apos;t have access to shifts for this station.
              </p>
            ) : (
              <ShiftTimeline milestones={timelineMilestones} />
            )}
          </CardContent>
        </Card>

        {/* Recent deliveries */}
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Recent deliveries</CardTitle>
              <p className="text-sm text-muted-foreground">
                Latest fuel receipts at {focusStation?.name ?? 'the focused station'}.
              </p>
            </div>
            <Link
              href="/procurement"
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              Procurement
              <ChevronRight className="size-4" />
            </Link>
          </CardHeader>
          <CardContent className="flex flex-col gap-1">
            {!focusId || deliveries.isPending ? (
              <div className="flex flex-col gap-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : deliveries.isError && isForbidden(deliveries.error) ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                You don&apos;t have access to deliveries for this station.
              </p>
            ) : recentDeliveries.length === 0 ? (
              <EmptyState
                title="No deliveries"
                description="No recent fuel receipts at this station."
                icon={<PackageCheck className="size-7" />}
              />
            ) : (
              recentDeliveries.map((d) => (
                <div
                  key={d.id}
                  className="flex items-center gap-3 rounded-lg border border-border/60 px-3 py-2.5"
                >
                  <span className="flex size-8 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                    <PackageCheck className="size-4" />
                  </span>
                  <div className="flex min-w-0 flex-1 flex-col">
                    <span className="truncate text-sm font-medium text-foreground">
                      {formatLitres(d.volume_litres)} L
                    </span>
                    <span className="font-mono text-xs text-muted-foreground">
                      {clock(d.received_at)}
                      {d.supplier_ref ? ` · ${d.supplier_ref}` : ''}
                    </span>
                  </div>
                  <Badge tone={d.match_status === 'matched' ? 'success' : 'warning'}>
                    {d.match_status}
                  </Badge>
                </div>
              ))
            )}
          </CardContent>
        </Card>
      </section>

      {/* ---- CREDIT EXPOSURE + REGIONAL OVERVIEW ------------------------- */}
      <section className="grid grid-cols-1 gap-6 lg:grid-cols-3">
        {/* Credit exposure */}
        <Card className="lg:col-span-2">
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Credit exposure</CardTitle>
              <p className="text-sm text-muted-foreground">
                Top customer balances · {formatMoney(agingTotal.toFixed(2))} outstanding.
              </p>
            </div>
            <Link
              href="/reports/customer-aging"
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              Aging report
              <ChevronRight className="size-4" />
            </Link>
          </CardHeader>
          <CardContent>
            {aging.isPending ? (
              <div className="flex flex-col gap-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-10 rounded-lg" />
                ))}
              </div>
            ) : aging.isError && isForbidden(aging.error) ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                You don&apos;t have access to customer balances.
              </p>
            ) : topDebtors.length === 0 ? (
              <EmptyState
                title="No outstanding credit"
                description="No customers carry a balance right now."
                icon={<CircleDollarSign className="size-7" />}
              />
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Customer</TableHead>
                    <TableHead>Code</TableHead>
                    <TableHead className="text-right">Balance</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {topDebtors.map((c) => (
                    <TableRow key={c.customer_id}>
                      <TableCell className="font-medium text-foreground">{c.name}</TableCell>
                      <TableCell className="font-mono text-xs text-muted-foreground">
                        {c.code}
                      </TableCell>
                      <TableCell className="text-right font-mono font-medium tabular-nums">
                        {formatMoney(c.balance)}
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent>
        </Card>

        {/* Network / regional overview */}
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Network</CardTitle>
              <p className="text-sm text-muted-foreground">Your sites at a glance.</p>
            </div>
            <Link
              href="/stations"
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              All stations
              <ChevronRight className="size-4" />
            </Link>
          </CardHeader>
          <CardContent className="flex flex-col gap-1">
            {stations.isPending ? (
              <div className="flex flex-col gap-2">
                {Array.from({ length: 3 }).map((_, i) => (
                  <Skeleton key={i} className="h-12 rounded-lg" />
                ))}
              </div>
            ) : stations.isError ? (
              <p className="py-4 text-center text-sm text-muted-foreground">
                Couldn&apos;t load stations.
              </p>
            ) : stationItems.length === 0 ? (
              <EmptyState
                title="No stations"
                description="Create a station to get started."
                icon={<Building2 className="size-7" />}
              />
            ) : (
              <>
                <div className="mb-1 flex items-center justify-between text-xs text-muted-foreground">
                  <span>{stationItems.length} stations</span>
                  <span>{activeStations.length} active</span>
                </div>
                {stationItems.slice(0, 6).map((s) => (
                  <button
                    type="button"
                    key={s.id}
                    onClick={() => setFocusId(s.id)}
                    className={`group -mx-2 flex items-center gap-3 rounded-lg px-2 py-2 text-left transition-colors hover:bg-muted ${
                      s.id === focusId ? 'bg-muted' : ''
                    }`}
                  >
                    <span className="flex size-8 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                      <Building2 className="size-4" />
                    </span>
                    <div className="flex min-w-0 flex-1 flex-col">
                      <span className="truncate text-sm font-medium text-foreground">{s.name}</span>
                      <span className="font-mono text-xs text-muted-foreground">
                        {s.code}
                        {s.city ? ` · ${s.city}` : ''}
                      </span>
                    </div>
                    <Badge tone={s.status === 'active' ? 'success' : 'warning'}>{s.status}</Badge>
                  </button>
                ))}
              </>
            )}
          </CardContent>
        </Card>
      </section>

      {/* ---- REPORTS HEADLINES (deep links) ------------------------------ */}
      {reportCategories.length > 0 ? (
        <Card>
          <CardHeader className="flex-row items-center justify-between space-y-0">
            <div className="flex flex-col gap-1">
              <CardTitle>Reports at a glance</CardTitle>
              <p className="text-sm text-muted-foreground">
                Live headline figures from the signature reports.
              </p>
            </div>
            <Link
              href="/reports"
              className="inline-flex items-center gap-1 text-sm font-medium text-accent hover:underline"
            >
              Reporting hub
              <ChevronRight className="size-4" />
            </Link>
          </CardHeader>
          <CardContent>
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {reportCategories.map((c) => {
                // headline_unit is a free string from the backend ("TZS",
                // "count", "open alerts", …). Format money-shaped headlines
                // through the decimal-string formatter; render the rest as-is.
                const isMoney = /^[A-Z]{3}$/.test(c.headline_unit);
                const headline = c.headline
                  ? isMoney
                    ? `${formatMoney(c.headline)} ${c.headline_unit}`
                    : c.headline_unit
                      ? `${c.headline} ${c.headline_unit}`
                      : c.headline
                  : '—';
                return (
                  <Link
                    key={c.key}
                    href="/reports"
                    className="group flex flex-col gap-1 rounded-xl border border-border/70 p-4 transition-colors hover:bg-muted"
                  >
                    <div className="flex items-center justify-between gap-2">
                      <span className="text-sm font-medium text-foreground">{c.title}</span>
                      {c.alert_count > 0 ? <Badge tone="warning">{c.alert_count}</Badge> : null}
                    </div>
                    <span className="font-mono text-xl font-semibold tabular-nums text-foreground">
                      {headline}
                    </span>
                    <span className="text-xs text-muted-foreground">{c.description}</span>
                  </Link>
                );
              })}
            </div>
          </CardContent>
        </Card>
      ) : null}

      {/* ---- REVENUE TREND CHART ----------------------------------------- */}
      {focusId && (revenue.data?.recent_days?.length ?? 0) > 1 ? (
        <Card>
          <CardHeader>
            <CardTitle>Revenue trend</CardTitle>
            <p className="text-sm text-muted-foreground">
              Recent recognized days at {focusStation?.name ?? 'the focused station'}.
            </p>
          </CardHeader>
          <CardContent>
            <BarChart
              data={revenue.data!.recent_days.slice().reverse()}
              xKey="business_date"
              series={[
                { key: 'gross_revenue', label: 'Gross', color: chartColors.accent },
                { key: 'margin_total', label: 'Margin', color: chartColors.success },
              ]}
              valueFormatter={(v) => formatMoney(v as string)}
              height={220}
            />
          </CardContent>
        </Card>
      ) : null}
    </div>
  );
}
