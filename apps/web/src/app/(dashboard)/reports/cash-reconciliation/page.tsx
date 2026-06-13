'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { Banknote, Landmark } from 'lucide-react';

import type { ReportEnvelope, ReportSummaryMetric } from '@fuelgrid/sdk';
import {
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  StatusBoard,
  TenderMixDonut,
  type StatusBoardItem,
  type StatusTone,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
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

/** One reconciliation in the cash flow payload (decimal strings). */
interface CashFlowRow {
  created_at: string;
  status: string;
  expected: string;
  submitted: string;
  variance: string;
  shortage: string;
  excess: string;
}

/** One settlement-status chip in the chart_data payload. */
interface SettlementChip {
  key: string;
  label: string;
  status: string;
  tone: StatusTone;
  amount: string;
  detail: string;
}

/** The cash report's chart_data: the per-reconciliation flow + settlement board. */
interface CashChartData {
  flow: CashFlowRow[];
  settlement: SettlementChip[];
}

/** Find a headline metric by label so the flow bar can read the window figures. */
function metric(summary: ReportSummaryMetric[], label: string): string | undefined {
  return summary.find((m) => m.label === label)?.value;
}

/** Coerce a decimal string to a finite number for bar geometry only. */
function num(v: string | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/** The variance status → a colored badge (the headline shortage/excess state). */
function VarianceBadge({ status }: { status?: string }) {
  if (!status) return null;
  const map: Record<string, { tone: 'success' | 'warning' | 'danger' | 'neutral'; label: string }> =
    {
      Balanced: { tone: 'success', label: 'Balanced' },
      Excess: { tone: 'warning', label: 'Excess' },
      Shortage: { tone: 'danger', label: 'Shortage' },
    };
  const meta = map[status] ?? { tone: 'neutral' as const, label: status };
  return <Badge tone={meta.tone}>{meta.label}</Badge>;
}

/**
 * The cash reconciliation flow (§20.5): expected → submitted → deposited as
 * horizontal bars on a shared scale, plus the resulting signed variance. Figures
 * are decimal strings (parsed to float for bar WIDTH geometry only — the
 * displayed numbers are formatMoney of the original strings).
 */
function CashFlowBar({
  expected,
  submitted,
  deposited,
  variance,
}: {
  expected?: string;
  submitted?: string;
  deposited?: string;
  variance?: string;
}) {
  const e = num(expected);
  const s = num(submitted);
  const d = num(deposited);
  const scale = Math.max(e, s, d, 1);
  const v = num(variance);
  const varTone = v === 0 ? 'text-muted-foreground' : v < 0 ? 'text-danger' : 'text-success';
  const Row = ({
    label,
    value,
    width,
    tone,
  }: {
    label: string;
    value?: string;
    width: number;
    tone: string;
  }) => (
    <div className="flex flex-col gap-1">
      <div className="flex items-baseline justify-between text-sm">
        <span className="text-muted-foreground">{label}</span>
        <span className="font-mono tabular-nums text-foreground">{formatMoney(value ?? '0')}</span>
      </div>
      <div className="h-2.5 w-full overflow-hidden rounded-full bg-muted/50">
        <div
          className={tone}
          style={{
            width: `${Math.min(100, (width / scale) * 100)}%`,
            height: '100%',
            backgroundColor: 'currentColor',
          }}
        />
      </div>
    </div>
  );
  return (
    <div className="flex flex-col gap-4">
      <Row label="Expected cash" value={expected} width={e} tone="text-accent" />
      <Row
        label="Submitted cash"
        value={submitted}
        width={s}
        tone="text-[hsl(var(--color-success))]"
      />
      <Row
        label="Deposited cash"
        value={deposited}
        width={d}
        tone="text-[hsl(var(--color-accent-muted))]"
      />
      <div className="flex items-baseline justify-between border-t border-border pt-3 text-sm">
        <span className="font-medium text-foreground">Net variance</span>
        <span className={`font-mono font-semibold tabular-nums ${varTone}`}>
          {v > 0 ? '+' : ''}
          {formatMoney(variance ?? '0')}
        </span>
      </div>
    </div>
  );
}

/** Map the envelope's settlement chips → StatusBoard items (money formatted). */
function settlementItems(chips: SettlementChip[]): StatusBoardItem[] {
  return chips.map((c) => ({
    key: c.key,
    label: c.label,
    status: c.status,
    tone: c.tone,
    amount: formatMoney(c.amount),
    detail: c.detail,
    ariaLabel: `${c.label} settlement: ${c.status}`,
  }));
}

export default function CashReconciliationPage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const [period, setPeriod] = React.useState('current');
  const allowed = usePermission('finance.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'cash-reconciliation', stationId, period],
    queryFn: ({ signal }) => api.getCashReconciliationReport(stationId, { period }, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId, period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Cash"
        title="Cash Reconciliation"
        description="Expected → submitted → deposited cash with the resulting shortage or excess, a settlement-status board across cash, mobile money, card and bank deposits, insights and data-quality checks. Money figures are exact decimals."
      />

      <ReportFilterBar
        items={items}
        stationId={stationId}
        onStation={setStationId}
        period={period}
        onPeriod={setPeriod}
      />

      <ReportStates
        stationsPending={stations.isPending}
        noStations={!stations.isPending && items.length === 0}
        query={report}
        loadingLabel="cash reconciliation report"
      >
        {(env: ReportEnvelope) => {
          const chart = (env.chart_data as CashChartData | null) ?? { flow: [], settlement: [] };
          const flow = chart.flow ?? [];
          const settlement = chart.settlement ?? [];
          const mix = env.tender_mix;

          const expected = metric(env.summary, 'Expected cash');
          const submitted = metric(env.summary, 'Submitted cash');
          const deposited = metric(env.summary, 'Deposited cash');
          const variance = metric(env.summary, 'Net variance');
          const varStatus = metric(env.summary, 'Variance status');

          return (
            <div className="flex flex-col gap-6">
              {/* Hero: data-quality first (most prominent), then the KPI grid. */}
              <DataQualityPanel items={env.data_quality} />

              <section className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
                  Cash position
                </h2>
                <VarianceBadge status={varStatus} />
              </section>
              <SummaryGrid summary={env.summary} />

              {/* Two-column report view (§18.2): main visuals + right context panel. */}
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="flex min-w-0 flex-col gap-6">
                  {/* The centerpiece: cash flow bar + tender split, side by side. */}
                  <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
                    <Card>
                      <CardHeader>
                        <CardTitle>Cash reconciliation flow</CardTitle>
                        <p className="text-sm text-muted-foreground">
                          Expected → submitted → deposited cash, and the resulting variance.
                        </p>
                      </CardHeader>
                      <CardContent>
                        {flow.length === 0 ? (
                          <EmptyState
                            title="No cash position"
                            description="No cash reconciliations recorded for this station yet."
                          />
                        ) : (
                          <CashFlowBar
                            expected={expected}
                            submitted={submitted}
                            deposited={deposited}
                            variance={variance}
                          />
                        )}
                      </CardContent>
                    </Card>

                    <Card>
                      <CardHeader>
                        <CardTitle>Tender mix</CardTitle>
                        <p className="text-sm text-muted-foreground">
                          Recorded tenders by type for the latest operating day.
                        </p>
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
                            description="No payments have been recorded for this operating day."
                          />
                        )}
                      </CardContent>
                    </Card>
                  </div>

                  {/* Settlement-status board (§20.5, net-new): a chip per medium. */}
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Landmark className="size-4 text-accent" />
                      <CardTitle>Settlement status</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {settlement.length === 0 ? (
                        <EmptyState
                          title="No settlement data"
                          description="No cash, tender or deposit activity recorded for this station yet."
                        />
                      ) : (
                        <StatusBoard
                          items={settlementItems(settlement)}
                          columnsClassName="grid-cols-1 sm:grid-cols-2 xl:grid-cols-4"
                        />
                      )}
                    </CardContent>
                  </Card>

                  <EnvelopeTable table={env.table} caption="Reconciliations" />
                </div>

                {/* Right panel (§18.2): variance status, data-quality, insights,
                    recommended actions, drill-down + export. */}
                <aside className="flex flex-col gap-6">
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <Banknote className="size-4 text-accent" />
                      <CardTitle className="text-base">Over / short</CardTitle>
                    </CardHeader>
                    <CardContent>
                      {flow.length === 0 ? (
                        <p className="text-sm text-muted-foreground">
                          No cash reconciliation recorded for this station yet.
                        </p>
                      ) : varStatus === 'Balanced' ? (
                        <p className="text-sm text-success">
                          Cash is balanced across {flow.length} reconciliation(s) — no shortage or
                          excess.
                        </p>
                      ) : varStatus === 'Shortage' ? (
                        <p className="text-sm text-danger">
                          A net shortage was recorded — review the flagged reconciliations and the
                          collection receipts.
                        </p>
                      ) : (
                        <p className="text-sm text-warning">
                          A net excess was recorded — confirm the count and look for an
                          under-recorded tender.
                        </p>
                      )}
                    </CardContent>
                  </Card>

                  <InsightPanel
                    insights={env.insights}
                    recommendedActions={env.recommended_actions}
                  />

                  <div className="flex flex-col gap-3">
                    <DrilldownLinks links={env.drilldown} />
                    <EnvelopeExports
                      options={env.export_options}
                      reportKey="cash-reconciliation"
                      filters={filters}
                      filenameBase={`cash-reconciliation-${stationId.slice(0, 8)}`}
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
