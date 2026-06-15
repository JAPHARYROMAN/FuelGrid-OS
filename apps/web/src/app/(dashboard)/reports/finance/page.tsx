'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { ArrowUpRight, BarChart3, FileText, Landmark, ListTree } from 'lucide-react';

import type { ReportEnvelope, ReportPeriod } from '@fuelgrid/sdk';
import {
  BarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  FinancialWaterfall,
  type FinancialWaterfallStep,
  StatusBoard,
  type StatusBoardItem,
  type StatusTone,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
import { canUsePermission, usePermission, usePermissions } from '@/hooks/use-permissions';

import { useStationSelection } from '../_components/filters';
import { PageHeader, ReportFilterBar, ReportStates } from '../_components/report-shell';
import { SnapshotsPanel } from '../_components/snapshots-panel';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One step of the P&L waterfall (decimal-string value). */
interface FinanceWaterfallStep {
  key: string;
  label: string;
  value: string;
  kind: 'base' | 'delta' | 'total';
  negative?: boolean;
}

/** One product's P&L contribution; cogs/margin omitted without margin.view. */
interface FinanceProductRow {
  product: string;
  litres: string;
  revenue: string;
  cogs?: string;
  margin?: string;
}

/** One settlement / accounting-period status chip. */
interface FinanceSettlementChip {
  key: string;
  label: string;
  status: string;
  tone: string;
  detail?: string;
}

/** One embedded finance JSON sub-report link. */
interface FinanceStatementLink {
  key: string;
  label: string;
  endpoint: string;
  permission: string;
}

/** The Finance report's report-specific chart_data payload. */
interface FinanceChartData {
  waterfall: FinanceWaterfallStep[];
  by_product: FinanceProductRow[];
  settlements: FinanceSettlementChip[];
  statements: FinanceStatementLink[];
  cost_shown: boolean;
}

/** Coerce a server tone string onto the StatusBoard tone vocabulary. */
function settlementTone(t: string): StatusTone {
  if (t === 'settled' || t === 'pending' || t === 'at_risk') return t;
  return 'neutral';
}

/** The P&L waterfall card (§5.8): revenue → COGS → margin → expenses → net. */
function PnlWaterfall({ steps }: { steps: FinanceWaterfallStep[] }) {
  if (steps.length === 0) {
    return (
      <EmptyState
        title="No P&L for this period"
        description="No recognized sales for this station in the selected period."
      />
    );
  }
  // The server's decimal-string steps map straight onto the FinancialWaterfall
  // primitive; money is formatted for display only (never recomputed).
  const wf: FinancialWaterfallStep[] = steps.map((s) => ({
    key: s.key,
    label: s.label,
    value: s.value,
    kind: s.kind,
    negative: s.negative,
  }));
  return (
    <FinancialWaterfall
      steps={wf}
      ariaLabel="Profit and loss waterfall"
      valueFormatter={(v) => formatMoney(v as string)}
      unit="TZS"
    />
  );
}

/** Revenue + gross-margin by product (margin column present only with margin.view). */
function ProductBars({
  products,
  costShown,
}: {
  products: FinanceProductRow[];
  costShown: boolean;
}) {
  if (products.length === 0) {
    return (
      <EmptyState
        title="No product sales"
        description="No recognized sales for this station in the selected period."
      />
    );
  }
  const series = costShown
    ? [
        { key: 'revenue', label: 'Revenue', color: chartColors.accent },
        { key: 'margin', label: 'Gross margin', color: chartColors.success },
      ]
    : [{ key: 'revenue', label: 'Revenue', color: chartColors.accent }];
  return (
    <BarChart
      data={products}
      xKey="product"
      valueFormatter={(v) => formatMoney(v as string)}
      series={series}
      // With two series (Revenue + Gross margin) the bars are otherwise
      // distinguishable by colour alone (WCAG 1.4.1); the text+swatch legend
      // disambiguates them on print / touch / for CVD users.
      legend={costShown}
      height={260}
    />
  );
}

export default function FinancePage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  // The export + statement routes are gated requirePermissionHeld("finance.read")
  // — held tenant-wide, station-agnostic — so gate their UI affordances the same
  // way ("held" mode) rather than against the currently-viewed station, which
  // would otherwise hide an action the server would allow.
  const allowed = usePermission('finance.read', { mode: 'held' });
  const perms = usePermissions();

  const report = useQuery<ReportEnvelope>({
    queryKey: ['report', 'finance', stationId, period],
    queryFn: ({ signal }) => api.getFinanceReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Finance"
        title="Finance"
        description="A premium profit-and-loss view for a station over the selected period: net revenue, COGS, gross margin, operating expenses and net operating result as a money waterfall, with period comparison, cash position, settlement status and the underlying financial statements. Money and litres are exact decimals throughout; COGS, gross margin and net margin require the margin.view permission."
      />

      <ReportFilterBar
        items={items}
        stationId={stationId}
        onStation={setStationId}
        period={period}
        onPeriod={(p) => setPeriod(p as ReportPeriod)}
      />

      <ReportStates
        stationsPending={stations.isPending}
        noStations={!stations.isPending && items.length === 0}
        query={report}
        loadingLabel="finance report"
      >
        {(env: ReportEnvelope) => {
          const data = (env.chart_data as FinanceChartData | null) ?? {
            waterfall: [],
            by_product: [],
            settlements: [],
            statements: [],
            cost_shown: false,
          };
          const settlementItems: StatusBoardItem[] = data.settlements.map((c) => ({
            key: c.key,
            label: c.label,
            status: c.status,
            tone: settlementTone(c.tone),
            detail: c.detail,
          }));

          return (
            <div className="flex flex-col gap-6">
              {/* Hero: data-quality first (most prominent), then KPI MetricCards. */}
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              {/* Two-column report view (§18.2): main visuals + right context. */}
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="flex min-w-0 flex-col gap-6">
                  {/* The signature P&L waterfall — the net-new finance viz. */}
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <BarChart3 className="size-4 text-accent" />
                      <CardTitle className="text-base">Profit &amp; loss waterfall</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <p className="mb-4 text-sm text-muted-foreground">
                        {data.cost_shown
                          ? 'Net revenue less COGS yields gross margin; less operating expenses yields the net operating result.'
                          : 'Net revenue less operating expenses. COGS and gross margin require the margin.view permission.'}
                      </p>
                      <PnlWaterfall steps={data.waterfall} />
                    </CardContent>
                  </Card>

                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <BarChart3 className="size-4 text-accent" />
                      <CardTitle className="text-base">
                        Revenue{data.cost_shown ? ' and gross margin' : ''} by product
                      </CardTitle>
                    </CardHeader>
                    <CardContent>
                      <ProductBars products={data.by_product} costShown={data.cost_shown} />
                    </CardContent>
                  </Card>

                  <EnvelopeTable table={env.table} caption="Per-product profitability" />
                </div>

                {/* Right panel (§18.2): settlement, statements, insights, drilldown. */}
                <aside className="flex flex-col gap-6">
                  {settlementItems.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <Landmark className="size-4 text-accent" />
                        <CardTitle className="text-base">Accounting periods</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <p className="mb-3 text-sm text-muted-foreground">
                          Close / lock status of the periods overlapping this window.
                        </p>
                        <StatusBoard items={settlementItems} columnsClassName="grid-cols-1" />
                      </CardContent>
                    </Card>
                  ) : null}

                  {data.statements.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <FileText className="size-4 text-accent" />
                        <CardTitle className="text-base">Financial statements</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <p className="mb-3 text-sm text-muted-foreground">
                          The underlying ledger statements (JSON), gated by your finance permission.
                        </p>
                        <ul className="flex flex-col gap-2">
                          {data.statements.map((st) => {
                            const Icon = st.key === 'general-ledger' ? ListTree : FileText;
                            // Gate each statement link on the permission the route
                            // carries (held tenant-wide, matching the API gate). A
                            // statement the actor can't open renders disabled rather
                            // than as a dead link.
                            const canOpen = perms.data
                              ? canUsePermission(perms.data, st.permission, { mode: 'held' })
                              : false;
                            return (
                              <li key={st.key}>
                                {canOpen ? (
                                  <a
                                    href={st.endpoint}
                                    target="_blank"
                                    rel="noreferrer"
                                    className="flex items-center gap-2 rounded-md border border-border bg-card px-2.5 py-2 text-sm text-foreground hover:bg-accent-muted/40"
                                  >
                                    <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                                    <span className="flex-1">{st.label}</span>
                                    <ArrowUpRight className="size-3.5 shrink-0 text-muted-foreground" />
                                  </a>
                                ) : (
                                  <span
                                    className="flex items-center gap-2 rounded-md border border-border/60 bg-muted/30 px-2.5 py-2 text-sm text-muted-foreground"
                                    title={`Requires the ${st.permission} permission`}
                                  >
                                    <Icon className="size-3.5 shrink-0 text-muted-foreground" />
                                    <span className="flex-1">{st.label}</span>
                                  </span>
                                )}
                              </li>
                            );
                          })}
                        </ul>
                      </CardContent>
                    </Card>
                  ) : null}

                  <InsightPanel
                    insights={env.insights}
                    recommendedActions={env.recommended_actions}
                  />

                  {/* Snapshot/lock the tenant-wide financial statement for the
                      period (the snapshot key 'financials' is the tenant-wide
                      statement, gated by finance.read — matching this page's gate). */}
                  <SnapshotsPanel reportKey="financials" filters={{ period }} permitted={allowed} />

                  <div className="flex flex-col gap-3">
                    <DrilldownLinks links={env.drilldown} />
                    <EnvelopeExports
                      options={env.export_options}
                      reportKey="financials"
                      filters={filters}
                      filenameBase={`finance-${stationId.slice(0, 8)}-${period}`}
                      permitted={allowed}
                    />
                  </div>
                </aside>
              </div>
            </div>
          );
        }}
      </ReportStates>
    </div>
  );
}
