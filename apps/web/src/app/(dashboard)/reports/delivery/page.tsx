'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { BarChart3, Gauge, Truck, Workflow } from 'lucide-react';

import type { ReportEnvelope, ReportPeriod } from '@fuelgrid/sdk';
import {
  BarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  SupplierScorecard,
  type ScorecardTone,
  type SupplierScorecardItem,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres } from '@/lib/money';
import { usePermission } from '@/hooks/use-permissions';

import { useStationSelection } from '../_components/filters';
import { PageHeader, ReportFilterBar, ReportStates } from '../_components/report-shell';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One product's ordered / loaded / received litres (decimal strings). */
interface DeliveryComparisonRow {
  key: string;
  label: string;
  color?: string;
  ordered: string;
  loaded: string;
  received: string;
}

/** One delivery receipt for the variance series. */
interface DeliveryLineRow {
  key: string;
  received_at: string;
  supplier: string;
  product: string;
  volume: string;
  dip_variance: string;
  match_status: string;
  late: boolean;
  landed_cost?: string;
}

/** One supplier scorecard (composite + per-dimension sub-scores). */
interface Scorecard {
  supplier_id: string;
  supplier_name: string;
  score: number;
  band: string;
  tone: ScorecardTone;
  grade: string;
  on_time_score: number;
  quantity_score: number;
  dispute_score: number;
  document_score: number;
  variance_score: number;
  price_score?: number | null;
  price_included: boolean;
  delivery_count: number;
  dispute_count: number;
}

/** One purchase-order-status bucket for the procurement pipeline. */
interface PipelineStage {
  status: string;
  count: number;
}

/** The Delivery report's report-specific chart_data payload. */
interface DeliveryChartData {
  comparison: DeliveryComparisonRow[];
  deliveries: DeliveryLineRow[];
  scorecards: Scorecard[];
  pipeline: PipelineStage[];
  cost_shown: boolean;
}

/** Coerce a decimal string to a finite number for chart geometry only. */
function num(v: string | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/** Human-readable purchase-order status. */
function statusLabel(status: string): string {
  return status
    .split('_')
    .map((w) => w.charAt(0).toUpperCase() + w.slice(1))
    .join(' ');
}

/**
 * The ordered vs loaded vs received comparison (§5.7): a grouped bar per product,
 * three bars side-by-side. Reuses the shared BarChart (groups series side by
 * side); values are decimal litre strings formatted for the tooltip/axis.
 */
function OrderedLoadedReceived({ rows }: { rows: DeliveryComparisonRow[] }) {
  if (rows.length === 0) {
    return (
      <EmptyState
        title="No deliveries or orders"
        description="No purchase orders or deliveries for this station in the selected period."
      />
    );
  }
  return (
    <BarChart
      data={rows}
      xKey="label"
      series={[
        { key: 'ordered', label: 'Ordered', color: chartColors.muted },
        { key: 'loaded', label: 'Loaded', color: chartColors.accentMuted },
        { key: 'received', label: 'Received', color: chartColors.accent },
      ]}
      valueFormatter={(v) => formatLitres(v as string)}
      height={280}
    />
  );
}

/**
 * The delivery-variance chart (§5.7): the per-delivery dip variance (declared −
 * measured litres) over the period, newest last so the trend reads left→right.
 * Reuses the shared BarChart; a positive bar is an over-declaration, negative an
 * under-declaration. Values are decimal litre strings.
 */
function DeliveryVarianceChart({ deliveries }: { deliveries: DeliveryLineRow[] }) {
  // Oldest→newest for a readable timeline; cap to the most recent 30 for density.
  const series = [...deliveries].reverse().slice(-30);
  if (series.length === 0) {
    return (
      <EmptyState
        title="No deliveries"
        description="No deliveries were received for this station in the selected period."
      />
    );
  }
  const data = series.map((d) => ({
    key: d.key,
    label: d.received_at.slice(5, 10),
    variance: d.dip_variance,
  }));
  return (
    <BarChart
      data={data}
      xKey="label"
      series={[{ key: 'variance', label: 'Dip variance', color: chartColors.warning }]}
      valueFormatter={(v) => formatLitres(v as string)}
      height={240}
    />
  );
}

/** Map the wire scorecard onto the shared SupplierScorecard item shape. */
function toScorecardItems(cards: Scorecard[]): SupplierScorecardItem[] {
  return cards.map((c) => {
    const dimensions = [
      { key: 'on_time', label: 'On-time', score: c.on_time_score },
      { key: 'quantity', label: 'Quantity', score: c.quantity_score },
      { key: 'disputes', label: 'Disputes', score: c.dispute_score },
      { key: 'document', label: 'Documents', score: c.document_score },
      { key: 'variance', label: 'Variance', score: c.variance_score },
    ];
    // Price competitiveness rides only when the actor may read supplier cost.
    if (c.price_included && c.price_score != null) {
      dimensions.push({ key: 'price', label: 'Price', score: c.price_score });
    }
    const disputeWord = c.dispute_count === 1 ? 'dispute' : 'disputes';
    const deliveryWord = c.delivery_count === 1 ? 'delivery' : 'deliveries';
    return {
      key: c.supplier_id,
      name: c.supplier_name,
      score: c.score,
      band: c.band,
      tone: c.tone,
      grade: c.grade,
      detail: `${c.delivery_count} ${deliveryWord} · ${c.dispute_count} ${disputeWord}`,
      dimensions,
    };
  });
}

/** The procurement pipeline: PO counts by status as a horizontal funnel. */
function ProcurementPipeline({ stages }: { stages: PipelineStage[] }) {
  if (stages.length === 0) {
    return (
      <EmptyState
        title="No purchase orders"
        description="No purchase orders were raised for this station in the selected period."
      />
    );
  }
  const data = stages.map((s) => ({ status: statusLabel(s.status), count: s.count }));
  return (
    <BarChart
      data={data}
      xKey="status"
      series={[{ key: 'count', label: 'Purchase orders', color: chartColors.accent }]}
      valueFormatter={(v) => String(Math.round(num(String(v))))}
      layout="vertical"
      height={Math.max(160, data.length * 44)}
    />
  );
}

export default function DeliveryReportPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  const allowed = usePermission('station.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'delivery', stationId, period],
    queryFn: ({ signal }) => api.getDeliveryReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Delivery & Procurement"
        title="Delivery & Procurement"
        description="Ordered vs loaded vs received litres, delivery variance, delivery delays, the procurement pipeline and a deterministic supplier scorecard (on-time, quantity accuracy, disputes, document completeness, delivery-variance history) for a station. Money and litres are exact decimals throughout; supplier cost requires the margin permission."
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
        loadingLabel="delivery report"
      >
        {(env: ReportEnvelope) => {
          const data = (env.chart_data as DeliveryChartData | null) ?? {
            comparison: [],
            deliveries: [],
            scorecards: [],
            pipeline: [],
            cost_shown: false,
          };
          const scorecards = toScorecardItems(data.scorecards);

          return (
            <div className="flex flex-col gap-6">
              {/* Hero: data-quality first (most prominent), then KPI MetricCards. */}
              <DataQualityPanel items={env.data_quality} />
              <SummaryGrid summary={env.summary} />

              {/* Two-column report view (§18.2): main visuals + right context. */}
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="flex min-w-0 flex-col gap-6">
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <BarChart3 className="size-4 text-accent" />
                      <CardTitle className="text-base">Ordered vs loaded vs received</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <OrderedLoadedReceived rows={data.comparison} />
                    </CardContent>
                  </Card>

                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Gauge className="size-4 text-accent" />
                      <CardTitle className="text-base">Delivery variance</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <p className="mb-3 text-sm text-muted-foreground">
                        Per-delivery dip variance (declared minus measured litres). A positive bar
                        over-declares, a negative bar under-declares.
                      </p>
                      <DeliveryVarianceChart deliveries={data.deliveries} />
                    </CardContent>
                  </Card>

                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Truck className="size-4 text-accent" />
                      <CardTitle className="text-base">Supplier scorecard</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {scorecards.length === 0 ? (
                        <EmptyState
                          title="No suppliers to score"
                          description="No supplier-attributed deliveries, orders or invoices for this station in the period."
                        />
                      ) : (
                        <>
                          <p className="mb-4 text-sm text-muted-foreground">
                            Each supplier scored 0–100 from on-time delivery, quantity accuracy,
                            disputes, document completeness and delivery-variance history
                            {data.cost_shown ? ', plus price competitiveness' : ''}. Lowest scores
                            first.
                          </p>
                          <SupplierScorecard suppliers={scorecards} />
                        </>
                      )}
                    </CardContent>
                  </Card>

                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Workflow className="size-4 text-accent" />
                      <CardTitle className="text-base">Procurement pipeline</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <ProcurementPipeline stages={data.pipeline} />
                    </CardContent>
                  </Card>

                  <EnvelopeTable table={env.table} caption="Delivery receipts" />
                </div>

                {/* Right panel (§18.2): insights + actions, drill-down + export. */}
                <aside className="flex flex-col gap-6">
                  <InsightPanel
                    insights={env.insights}
                    recommendedActions={env.recommended_actions}
                  />

                  <div className="flex flex-col gap-3">
                    <DrilldownLinks links={env.drilldown} />
                    <EnvelopeExports
                      options={env.export_options}
                      reportKey="delivery"
                      filters={filters}
                      filenameBase={`delivery-${stationId.slice(0, 8)}-${period}`}
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
