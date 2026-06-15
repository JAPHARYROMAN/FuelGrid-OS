'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { CreditCard, Fuel, TrendingUp } from 'lucide-react';

import type { ReportEnvelope, ReportPeriod } from '@fuelgrid/sdk';
import {
  AreaChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  Heatmap,
  type HeatmapCell,
  type HeatmapRow,
  StackedBarChart,
  TenderMixDonut,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatLitres, formatMoney } from '@/lib/money';
import { usePermission } from '@/hooks/use-permissions';

import { useStationSelection } from '../_components/filters';
import { PageHeader, ReportFilterBar, ReportStates } from '../_components/report-shell';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  EnvelopeTable,
  InsightPanel,
  InsightRulesPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One day of the revenue-trend line (decimal strings). */
interface SalesTrendDay {
  date: string;
  gross: string;
  litres: string;
}

/** One dimension row (product / shift / attendant / nozzle / station). */
interface SalesDimRow {
  key: string;
  label: string;
  color?: string;
  litres: string;
  gross: string;
  net: string;
  margin?: string;
  txn_count: number;
}

/** One hour-of-day bucket for the peak-hours heatmap. */
interface SalesHourCell {
  hour: number;
  gross: string;
  litres: string;
  txn: number;
}

/** The Sales report's report-specific chart_data payload. */
interface SalesChartData {
  trend: SalesTrendDay[];
  by_product: SalesDimRow[];
  by_shift: SalesDimRow[];
  by_attendant: SalesDimRow[];
  by_nozzle: SalesDimRow[];
  by_hour: SalesHourCell[];
  stations: SalesDimRow[];
  margin_shown: boolean;
}

/** Coerce a decimal string to a finite number for chart geometry only. */
function num(v: string | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

const shortDate = (v: unknown) => {
  const s = String(v ?? '');
  return s.length >= 10 ? s.slice(5) : s;
};

/**
 * The product-mix STACKED BAR: each business-day column stacks its products by
 * revenue, so the reader sees both total revenue per day and the product split
 * within it. Built from the trend dates × the per-product totals — the by-product
 * payload is period-wide totals (not per-day), so we render a single stacked
 * column ("Period") per product when there is no per-day product split, which is
 * the honest shape the backend provides today. Each product keeps its own token
 * color (from the catalog), and StackedBarChart renders a text+swatch legend
 * (default on) so segment identity is never colour-alone.
 */
function ProductMixStacked({ products }: { products: SalesDimRow[] }) {
  // Guard on the summed segment total (like the donut/heatmap), not just row
  // count: products with all-zero revenue would otherwise render an empty,
  // zero-height stacked column instead of an honest "no data" message.
  const total = products.reduce((sum, p) => sum + num(p.net), 0);
  if (products.length === 0 || total <= 0) {
    return (
      <EmptyState
        title="No product sales"
        description="No recognized sales for this station in the selected period."
      />
    );
  }
  // One stacked column ("Period total") whose segments are the products. recharts
  // stacks series across a shared category, so we shape a single-row dataset with
  // one numeric field per product and a series per product.
  const row: Record<string, string> = { bucket: 'Period total' };
  const series = products.map((p) => {
    row[p.key] = p.net;
    return {
      key: p.key,
      label: p.label,
      color: p.color || undefined,
    };
  });
  return (
    <StackedBarChart
      data={[row]}
      xKey="bucket"
      series={series}
      valueFormatter={(v) => formatMoney(v as string)}
      height={260}
    />
  );
}

/**
 * The peak-hours heatmap: the 24-hour grid laid out as four rows of six hours,
 * each cell washed by its share of the busiest hour's revenue (intensity) and
 * labelled with the formatted revenue (text, never colour alone). Reuses the
 * merged Heatmap primitive (§5.2). Empty hours read as a faint, zero-value cell.
 */
function PeakHoursHeatmap({ hours }: { hours: SalesHourCell[] }) {
  const max = hours.reduce((m, h) => Math.max(m, num(h.gross)), 0);
  if (max <= 0) {
    return (
      <EmptyState
        title="No hourly sales"
        description="No recognized sales with a recognition time in the selected period."
      />
    );
  }
  const columns = ['00–03', '04–07', '08–11', '12–15', '16–19', '20–23'];
  const rows: HeatmapRow[] = [];
  // 4 rows × 6 columns = 24 hours, row r covering hours [r*6 .. r*6+5].
  const rowLabels = ['Night', 'Early', 'Morning', 'Day'];
  for (let r = 0; r < 4; r++) {
    const cells: HeatmapCell[] = [];
    for (let c = 0; c < 6; c++) {
      const hour = r * 6 + c;
      const cell = hours.find((h) => h.hour === hour);
      const gross = cell?.gross ?? '0';
      const hh = String(hour).padStart(2, '0');
      cells.push({
        key: `h-${hour}`,
        display: num(gross) > 0 ? formatMoney(gross) : '—',
        intensity: max > 0 ? num(gross) / max : 0,
        sublabel: `${hh}:00`,
        ariaLabel: `${hh}:00 revenue ${formatMoney(gross)}`,
      });
    }
    rows.push({ key: `r-${r}`, label: rowLabels[r] ?? `Row ${r}`, cells });
  }
  return <Heatmap rows={rows} columns={columns} tone="accent" />;
}

/**
 * A ranked dimension table (shift / attendant / nozzle / station): the top rows
 * by revenue with litres, revenue and (when permitted) margin. Pure presentation
 * of the already-computed decimal strings — never recomputed.
 */
function DimensionTable({
  title,
  description,
  rows,
  marginShown,
  emptyLabel,
}: {
  title: string;
  description: string;
  rows: SalesDimRow[];
  marginShown: boolean;
  emptyLabel: string;
}) {
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        <p className="text-sm text-muted-foreground">{description}</p>
      </CardHeader>
      <CardContent className="p-0">
        {rows.length === 0 ? (
          <p className="p-6 text-sm text-muted-foreground">{emptyLabel}</p>
        ) : (
          <div className="overflow-x-auto">
            <table className="w-full text-sm">
              <thead>
                <tr className="border-b border-border text-left text-muted-foreground">
                  <th className="px-4 py-2 font-medium">Name</th>
                  <th className="px-4 py-2 text-right font-medium">Litres</th>
                  <th className="px-4 py-2 text-right font-medium">Revenue</th>
                  {marginShown ? (
                    <th className="px-4 py-2 text-right font-medium">Margin</th>
                  ) : null}
                  <th className="px-4 py-2 text-right font-medium">Txns</th>
                </tr>
              </thead>
              <tbody>
                {rows.map((row) => (
                  <tr key={row.key} className="border-b border-border/50 last:border-0">
                    <td className="px-4 py-2 text-foreground">{row.label}</td>
                    <td className="px-4 py-2 text-right font-mono tabular-nums">
                      {formatLitres(row.litres)}
                    </td>
                    <td className="px-4 py-2 text-right font-mono tabular-nums">
                      {formatMoney(row.gross || row.net)}
                    </td>
                    {marginShown ? (
                      <td className="px-4 py-2 text-right font-mono tabular-nums">
                        {row.margin != null ? formatMoney(row.margin) : '—'}
                      </td>
                    ) : null}
                    <td className="px-4 py-2 text-right font-mono tabular-nums">{row.txn_count}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
      </CardContent>
    </Card>
  );
}

export default function SalesReportPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  const allowed = usePermission('revenue.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'sales', stationId, period],
    queryFn: ({ signal }) => api.getSalesReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Sales"
        title="Sales"
        description="Litres sold, revenue, average selling price, transaction count and period-over-period growth for a station, with the product mix, payment breakdown, shift / attendant / nozzle drill-down and a peak-hours heatmap. Money and litres are exact decimals throughout."
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
        loadingLabel="sales report"
      >
        {(env: ReportEnvelope) => {
          const data = (env.chart_data as SalesChartData | null) ?? {
            trend: [],
            by_product: [],
            by_shift: [],
            by_attendant: [],
            by_nozzle: [],
            by_hour: [],
            stations: [],
            margin_shown: false,
          };
          const mix = env.tender_mix;
          const marginShown = data.margin_shown;

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
                      <TrendingUp className="size-4 text-accent" />
                      <CardTitle className="text-base">Revenue trend</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {data.trend.length < 2 ? (
                        <EmptyState
                          title="Not enough history"
                          description="At least two days with sales are needed to plot a trend."
                        />
                      ) : (
                        <AreaChart
                          data={data.trend}
                          xKey="date"
                          xFormatter={shortDate}
                          valueFormatter={(v) => formatMoney(v as string)}
                          series={[{ key: 'gross', label: 'Revenue', color: chartColors.accent }]}
                          height={260}
                        />
                      )}
                    </CardContent>
                  </Card>

                  <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <Fuel className="size-4 text-accent" />
                        <CardTitle className="text-base">Product mix</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <ProductMixStacked products={data.by_product} />
                      </CardContent>
                    </Card>

                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <CreditCard className="size-4 text-accent" />
                        <CardTitle className="text-base">Payment mix</CardTitle>
                      </CardHeader>
                      <CardContent>
                        {mix && num(mix.total) > 0 ? (
                          <TenderMixDonut
                            mix={mix}
                            valueFormatter={(v) => formatMoney(v as string)}
                          />
                        ) : (
                          <EmptyState
                            title="No tenders yet"
                            description="No payments have been recorded for this period."
                          />
                        )}
                      </CardContent>
                    </Card>
                  </div>

                  <Card>
                    <CardHeader>
                      <CardTitle>Sales by hour</CardTitle>
                      <p className="text-sm text-muted-foreground">
                        Revenue by recognition hour — the peak-hours heatmap. Each cell shows its
                        figure; colour intensity tracks the busiest hour.
                      </p>
                    </CardHeader>
                    <CardContent>
                      <PeakHoursHeatmap hours={data.by_hour} />
                    </CardContent>
                  </Card>

                  <EnvelopeTable table={env.table} caption="Sales by product" />

                  <DimensionTable
                    title="Sales by shift"
                    description="Recognized sales attributed to each shift in the period."
                    rows={data.by_shift}
                    marginShown={marginShown}
                    emptyLabel="No shift sales recognized for this period yet."
                  />

                  <DimensionTable
                    title="Top attendants"
                    description="Sales attributed to each attendant via their nozzle assignments."
                    rows={data.by_attendant}
                    marginShown={marginShown}
                    emptyLabel="No attendant-attributed sales for this period yet."
                  />

                  <DimensionTable
                    title="Sales by nozzle"
                    description="Recognized sales per nozzle across the period."
                    rows={data.by_nozzle}
                    marginShown={marginShown}
                    emptyLabel="No nozzle sales recognized for this period yet."
                  />

                  {data.stations.length > 0 ? (
                    <DimensionTable
                      title="Station ranking"
                      description="Net recognized revenue (net of voids) across every station you can read, for the period."
                      rows={data.stations}
                      marginShown={marginShown}
                      emptyLabel="No station sales for this period yet."
                    />
                  ) : null}
                </div>

                {/* Right panel (§18.2): data-quality, insights + actions, drill-down + export. */}
                <aside className="flex flex-col gap-6">
                  <InsightPanel
                    insights={env.insights}
                    recommendedActions={env.recommended_actions}
                  />

                  <InsightRulesPanel rules={env.insight_rules} />

                  <div className="flex flex-col gap-3">
                    <DrilldownLinks links={env.drilldown} />
                    <EnvelopeExports
                      options={env.export_options}
                      reportKey="sales"
                      filters={filters}
                      filenameBase={`sales-${stationId.slice(0, 8)}-${period}`}
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
