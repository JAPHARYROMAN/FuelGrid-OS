'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { CheckCircle2, ClipboardCheck, Clock, ShieldAlert } from 'lucide-react';

import type { ReportEnvelope, ReportSummaryMetric } from '@fuelgrid/sdk';
import {
  AreaChart,
  Badge,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  EmptyState,
  TenderMixDonut,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
import { usePermission } from '@/hooks/use-permissions';

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

/** A day in the station-close chart_data payload (decimal strings). */
interface CloseChartDay {
  date: string;
  gross: string;
  margin: string;
  tendered: string;
  cash_variance: string;
}

const shortDate = (v: unknown) => {
  const s = String(v ?? '');
  return s.length >= 10 ? s.slice(5) : s;
};

/** Find a headline metric by label so the cash bar/checklist can read figures. */
function metric(summary: ReportSummaryMetric[], label: string): string | undefined {
  return summary.find((m) => m.label === label)?.value;
}

/** Coerce a decimal string to a finite number for bar geometry only. */
function num(v: string | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/** The approval status → a colored badge (the close's headline state). */
function ApprovalBadge({ status }: { status?: string }) {
  if (!status) return null;
  const map: Record<string, { tone: 'success' | 'warning' | 'danger' | 'neutral'; label: string }> =
    {
      approved: { tone: 'success', label: 'Approved' },
      pending_shifts: { tone: 'warning', label: 'Pending shifts' },
      draft: { tone: 'neutral', label: 'Draft' },
      no_data: { tone: 'danger', label: 'No data' },
    };
  const meta = map[status] ?? { tone: 'neutral' as const, label: status };
  return <Badge tone={meta.tone}>{meta.label}</Badge>;
}

/**
 * The expected-vs-submitted cash reconciliation bar: two horizontal bars on a
 * shared scale plus the signed variance. Figures are decimal strings (parsed to
 * float for bar WIDTH geometry only — the displayed numbers are formatMoney of
 * the original strings).
 */
function CashReconBar({
  expected,
  submitted,
  variance,
}: {
  expected?: string;
  submitted?: string;
  variance?: string;
}) {
  const e = num(expected);
  const s = num(submitted);
  const scale = Math.max(e, s, 1);
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
      <div className="flex items-baseline justify-between border-t border-border pt-3 text-sm">
        <span className="font-medium text-foreground">Cash variance</span>
        <span className={`font-mono font-semibold tabular-nums ${varTone}`}>
          {v > 0 ? '+' : ''}
          {formatMoney(variance ?? '0')}
        </span>
      </div>
    </div>
  );
}

/**
 * The shift-close checklist: a deterministic, honest read of the close state
 * derived from the already-computed headline metrics + data-quality flags. Each
 * item is done / pending / failed — no recompute, just a presentation of the
 * close gates a manager signs off against.
 */
function CloseChecklist({ env }: { env: ReportEnvelope }) {
  const openExceptions = num(metric(env.summary, 'Open exceptions'));
  const approval = metric(env.summary, 'Approval status');
  const dqMessages = env.data_quality.map((d) => d.message.toLowerCase());
  const cashUnsubmitted = dqMessages.some((m) => m.includes('cash has not been submitted'));
  const dayUnlocked = approval !== 'approved';

  const items: { label: string; state: 'done' | 'pending' | 'failed'; icon: React.ReactNode }[] = [
    {
      label:
        openExceptions === 0 ? 'All shifts closed' : `${openExceptions} shift(s) not yet closed`,
      state: openExceptions === 0 ? 'done' : 'failed',
      icon:
        openExceptions === 0 ? (
          <CheckCircle2 className="size-4" />
        ) : (
          <ShieldAlert className="size-4" />
        ),
    },
    {
      label: cashUnsubmitted ? 'Cash not submitted / reconciled' : 'Cash submitted & reconciled',
      state: cashUnsubmitted ? 'failed' : 'done',
      icon: cashUnsubmitted ? (
        <ShieldAlert className="size-4" />
      ) : (
        <CheckCircle2 className="size-4" />
      ),
    },
    {
      label: dayUnlocked ? 'Operating day not locked' : 'Operating day locked',
      state: dayUnlocked ? 'pending' : 'done',
      icon: dayUnlocked ? <Clock className="size-4" /> : <CheckCircle2 className="size-4" />,
    },
  ];
  const tone: Record<'done' | 'pending' | 'failed', string> = {
    done: 'text-success',
    pending: 'text-warning',
    failed: 'text-danger',
  };
  return (
    <ul className="flex flex-col gap-3">
      {items.map((it, i) => (
        <li key={i} className="flex items-center gap-2.5 text-sm">
          <span className={tone[it.state]}>{it.icon}</span>
          <span className={it.state === 'done' ? 'text-foreground' : 'font-medium text-foreground'}>
            {it.label}
          </span>
        </li>
      ))}
    </ul>
  );
}

export default function StationClosePage() {
  const { stations, items, stationId, setStationId } = useStationSelection();
  const allowed = usePermission('revenue.read', { stationID: stationId });

  const report = useQuery({
    queryKey: ['report', 'station-close', stationId],
    queryFn: ({ signal }) => api.getStationCloseReport(stationId, {}, signal),
    enabled: !!stationId,
  });

  const filters = { station_id: stationId };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Daily close"
        title="Daily Station Close"
        description="Recognized sales and litres, tender mix, cash position, open exceptions and approval status for the latest operating day, with a recent-day trend."
      />

      <ReportFilterBar
        items={items}
        stationId={stationId}
        onStation={setStationId}
        showPeriod={false}
      />

      <ReportStates
        stationsPending={stations.isPending}
        noStations={!stations.isPending && items.length === 0}
        query={report}
        loadingLabel="station close report"
      >
        {(env: ReportEnvelope) => {
          const days = (env.chart_data as CloseChartDay[] | null) ?? [];
          const mix = env.tender_mix;
          const expectedCash = metric(env.summary, 'Expected cash');
          const submittedCash = metric(env.summary, 'Submitted cash');
          const cashVariance = metric(env.summary, 'Cash variance');
          const approval = metric(env.summary, 'Approval status');

          return (
            <div className="flex flex-col gap-6">
              {/* Hero: data-quality first (most prominent), then KPI MetricCards. */}
              <DataQualityPanel items={env.data_quality} />

              <section className="flex flex-wrap items-center justify-between gap-3">
                <h2 className="text-sm font-medium uppercase tracking-wider text-muted-foreground">
                  Close summary
                </h2>
                <ApprovalBadge status={approval} />
              </section>
              <SummaryGrid summary={env.summary} />

              {/* Two-column report view (§18.2): main visuals + right context panel. */}
              <div className="grid grid-cols-1 gap-6 lg:grid-cols-[minmax(0,1fr)_320px]">
                <div className="flex min-w-0 flex-col gap-6">
                  {/* Tender mix donut + cash reconciliation bar, side by side. */}
                  <div className="grid grid-cols-1 gap-6 xl:grid-cols-2">
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

                    <Card>
                      <CardHeader>
                        <CardTitle>Cash reconciliation</CardTitle>
                        <p className="text-sm text-muted-foreground">
                          Expected cash tender vs submitted (counted), and the resulting variance.
                        </p>
                      </CardHeader>
                      <CardContent>
                        {expectedCash == null ? (
                          <EmptyState
                            title="No cash position"
                            description="No revenue day has been computed for this station yet."
                          />
                        ) : (
                          <CashReconBar
                            expected={expectedCash}
                            submitted={submittedCash}
                            variance={cashVariance}
                          />
                        )}
                      </CardContent>
                    </Card>
                  </div>

                  <Card>
                    <CardHeader>
                      <CardTitle>Recent-day revenue trend</CardTitle>
                      <p className="text-sm text-muted-foreground">
                        Gross revenue and tendered totals over recent operating days.
                      </p>
                    </CardHeader>
                    <CardContent>
                      {days.length < 2 ? (
                        <EmptyState
                          title="Not enough history"
                          description="At least two operating days are needed to plot a trend."
                        />
                      ) : (
                        <AreaChart
                          data={days}
                          xKey="date"
                          xFormatter={shortDate}
                          valueFormatter={(v) => formatMoney(v as string)}
                          series={[
                            { key: 'gross', label: 'Gross', color: chartColors.accent },
                            { key: 'tendered', label: 'Tendered', color: chartColors.success },
                          ]}
                          height={260}
                        />
                      )}
                    </CardContent>
                  </Card>

                  <EnvelopeTable table={env.table} caption="Operating days" />
                </div>

                {/* Right panel (§18.2): close checklist, data-quality, insights,
                    recommended actions, drill-down + export. */}
                <aside className="flex flex-col gap-6">
                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <ClipboardCheck className="size-4 text-accent" />
                      <CardTitle className="text-base">Shift close checklist</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <CloseChecklist env={env} />
                    </CardContent>
                  </Card>

                  <InsightPanel
                    insights={env.insights}
                    recommendedActions={env.recommended_actions}
                  />

                  <SnapshotsPanel
                    reportKey="station-close"
                    stationId={stationId}
                    filters={filters}
                    permitted={allowed}
                  />

                  <div className="flex flex-col gap-3">
                    <DrilldownLinks links={env.drilldown} />
                    <EnvelopeExports
                      options={env.export_options}
                      reportKey="station-close"
                      filters={filters}
                      filenameBase={`station-close-${stationId.slice(0, 8)}`}
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
