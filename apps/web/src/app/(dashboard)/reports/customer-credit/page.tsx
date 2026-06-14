'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';
import { CreditCard, Gauge, ListOrdered, TrendingUp } from 'lucide-react';

import type { CustomerCreditDrilldown, ReportEnvelope, ReportPeriod } from '@fuelgrid/sdk';
import { SdkError } from '@fuelgrid/sdk';
import {
  BarChart,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  CreditLimitMeter,
  type CreditLimitMeterItem,
  FilterBar,
  FilterField,
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
  EmptyState,
  ErrorState,
  RiskBadge,
  type RiskSeverity,
  Skeleton,
  StackedBarChart,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
  chartColors,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

import { PERIODS } from '../_components/filters';
import { PageHeader } from '../_components/report-shell';
import {
  DataQualityPanel,
  DrilldownLinks,
  EnvelopeExports,
  InsightPanel,
  SummaryGrid,
} from '../_components/report-envelope';

/** One credit customer's aging row from the report's chart_data (decimal strings). */
interface CreditCustomerRow {
  customer_id: string;
  code: string;
  name: string;
  current: string;
  days_1_30: string;
  days_31_60: string;
  days_61_90: string;
  days_90_plus: string;
  outstanding: string;
  overdue: string;
  risk_category: string;
  on_hold: boolean;
  status: string;
  // CREDIT EXPOSURE — present only when the actor holds the credit permission.
  credit_limit?: string;
  exposure?: string;
  available?: string;
  utilization?: string;
  warning_pct?: string;
  over_limit?: boolean;
}

/** One aging bucket of the tenant-wide chart (decimal-string amount). */
interface AgingBucketSlice {
  bucket: string;
  amount: string;
}

/** The Customer Credit report's chart_data payload. */
interface CustomerCreditChartData {
  buckets: AgingBucketSlice[];
  customers: CreditCustomerRow[];
  exposure_shown: boolean;
}

/** Coerce a decimal string to a finite number for chart/meter geometry only. */
function num(v: string | undefined): number {
  const n = Number(v);
  return Number.isFinite(n) ? n : 0;
}

/** Map a customer row's standing onto a RiskBadge severity + label. */
function riskFor(c: CreditCustomerRow): { severity: RiskSeverity; label: string } {
  if (c.on_hold) return { severity: 'critical', label: 'On hold' };
  if (c.over_limit) return { severity: 'high', label: 'Over limit' };
  if (num(c.days_90_plus) > 0) return { severity: 'high', label: '90+ overdue' };
  if (num(c.days_61_90) > 0) return { severity: 'medium', label: '61-90 overdue' };
  if (num(c.overdue) > 0) return { severity: 'low', label: 'Overdue' };
  return { severity: 'info', label: 'Current' };
}

/** The aging-bucket bar (§5.9): tenant-wide receivable by aging bucket. */
function AgingBucketChart({ buckets }: { buckets: AgingBucketSlice[] }) {
  const hasAny = buckets.some((b) => num(b.amount) !== 0);
  if (!hasAny) {
    return (
      <EmptyState
        title="No outstanding receivables"
        description="No credit customer carries an outstanding balance for this period."
      />
    );
  }
  // One column with five stacked aging segments — a part-to-whole-by-bucket view.
  const row: Record<string, string> = { label: 'Receivables' };
  for (const b of buckets) row[b.bucket] = b.amount;
  return (
    <StackedBarChart
      data={[row]}
      xKey="label"
      series={[
        { key: 'Current', label: 'Current', color: chartColors.success },
        { key: '1-30', label: '1-30 days', color: chartColors.accent },
        { key: '31-60', label: '31-60 days', color: chartColors.warning },
        { key: '61-90', label: '61-90 days', color: chartColors.muted },
        { key: '90+', label: '90+ days', color: chartColors.danger },
      ]}
      valueFormatter={(v) => formatMoney(v as string)}
      height={260}
    />
  );
}

/** Top-overdue ranking (§5.9): the largest overdue balances as a vertical bar. */
function TopOverdueRanking({ customers }: { customers: CreditCustomerRow[] }) {
  const ranked = [...customers]
    .filter((c) => num(c.overdue) > 0)
    .sort((a, b) => num(b.overdue) - num(a.overdue))
    .slice(0, 8)
    .map((c) => ({ name: c.name, overdue: c.overdue }));
  if (ranked.length === 0) {
    return <EmptyState title="No overdue balances" description="No customer is past due." />;
  }
  return (
    <BarChart
      data={ranked}
      xKey="name"
      series={[{ key: 'overdue', label: 'Overdue', color: chartColors.danger }]}
      valueFormatter={(v) => formatMoney(v as string)}
      layout="vertical"
      height={Math.max(180, ranked.length * 40)}
    />
  );
}

/** Balance-by-customer trend (§5.9): outstanding across the top customers. */
function BalanceByCustomer({ customers }: { customers: CreditCustomerRow[] }) {
  const top = customers.slice(0, 10).map((c) => ({ name: c.name, outstanding: c.outstanding }));
  if (top.length === 0) {
    return <EmptyState title="No balances" description="No customer carries a balance." />;
  }
  return (
    <BarChart
      data={top}
      xKey="name"
      series={[{ key: 'outstanding', label: 'Outstanding', color: chartColors.accent }]}
      valueFormatter={(v) => formatMoney(v as string)}
      height={260}
    />
  );
}

/** Build the credit-limit utilization meter items from the gated exposure rows. */
function meterItems(customers: CreditCustomerRow[]): CreditLimitMeterItem[] {
  return customers
    .filter((c) => c.utilization != null && c.credit_limit != null)
    .slice(0, 8)
    .map((c) => ({
      key: c.customer_id,
      label: c.name,
      utilization: num(c.utilization),
      warningPct: num(c.warning_pct) || 80,
      exposure: c.exposure != null ? formatMoney(c.exposure) : undefined,
      limit: c.credit_limit != null ? formatMoney(c.credit_limit) : undefined,
      tone: c.on_hold ? ('over' as const) : undefined,
      statusWord: c.on_hold ? 'On hold' : undefined,
    }));
}

/** The customer drilldown dialog: balance -> open invoices -> recent payments. */
function CustomerDrilldownDialog({
  customer,
  onClose,
}: {
  customer: CreditCustomerRow | null;
  onClose: () => void;
}) {
  const drill = useQuery<CustomerCreditDrilldown>({
    queryKey: ['customer-credit-drilldown', customer?.customer_id],
    queryFn: ({ signal }) => api.getCustomerCreditDrilldown(customer!.customer_id, signal),
    enabled: !!customer,
  });

  return (
    <Dialog open={!!customer} onOpenChange={(o) => !o && onClose()}>
      <DialogContent className="max-w-2xl">
        <DialogHeader>
          <DialogTitle>{customer?.name}</DialogTitle>
          <DialogDescription>
            Outstanding {customer ? formatMoney(customer.outstanding) : ''} · open invoices and
            recent payments.
          </DialogDescription>
        </DialogHeader>

        {drill.isPending ? (
          <Skeleton className="h-48 rounded-md" />
        ) : drill.isError ? (
          <ErrorState
            title="Couldn't load the drilldown"
            description={String((drill.error as Error).message)}
            onRetry={() => drill.refetch()}
          />
        ) : (
          <div className="flex max-h-[60vh] flex-col gap-5 overflow-y-auto">
            <section>
              <h3 className="mb-2 text-sm font-semibold text-foreground">Open invoices</h3>
              {(drill.data?.invoices.length ?? 0) === 0 ? (
                <p className="text-sm text-muted-foreground">No open invoices.</p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Invoice</TableHead>
                      <TableHead>Due</TableHead>
                      <TableHead>Bucket</TableHead>
                      <TableHead className="text-right">Outstanding</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {drill.data!.invoices.map((inv) => (
                      <TableRow key={inv.invoice_id}>
                        <TableCell>{inv.invoice_number ?? inv.invoice_id.slice(0, 8)}</TableCell>
                        <TableCell className="text-muted-foreground">
                          {inv.due_date ?? '—'}
                        </TableCell>
                        <TableCell>{inv.bucket}</TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(inv.outstanding)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </section>

            <section>
              <h3 className="mb-2 text-sm font-semibold text-foreground">Recent payments</h3>
              {(drill.data?.payments.length ?? 0) === 0 ? (
                <p className="text-sm text-muted-foreground">No payments recorded.</p>
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>Date</TableHead>
                      <TableHead>Method</TableHead>
                      <TableHead>Status</TableHead>
                      <TableHead className="text-right">Amount</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {drill.data!.payments.map((p) => (
                      <TableRow key={p.payment_id}>
                        <TableCell>{p.payment_date}</TableCell>
                        <TableCell className="text-muted-foreground">{p.method}</TableCell>
                        <TableCell>{p.status}</TableCell>
                        <TableCell className="text-right font-mono tabular-nums">
                          {formatMoney(p.amount)}
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </section>
          </div>
        )}
      </DialogContent>
    </Dialog>
  );
}

export default function CustomerCreditPage() {
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  const [drilldownCustomer, setDrilldownCustomer] = React.useState<CreditCustomerRow | null>(null);

  const report = useQuery<ReportEnvelope>({
    queryKey: ['report', 'customer-credit', period],
    queryFn: ({ signal }) => api.getCustomerCreditReport({ period }, signal),
  });

  const filters = { period };

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Customer Credit"
        title="Customer Credit"
        description="Receivables aged into Current / 1-30 / 31-60 / 61-90 / 90+ day buckets from invoice due dates, total and overdue exposure, credit-limit utilization per customer, top-overdue ranking, risk badges and a balance to invoices to payments drilldown. Money figures are exact decimals throughout; credit exposure, limit and utilization require the customer credit permission."
      />

      <FilterBar>
        <FilterField label="Period">
          <select
            className="h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50"
            value={period}
            onChange={(e) => setPeriod(e.target.value as ReportPeriod)}
            aria-label="Reporting period"
          >
            {PERIODS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </FilterField>
      </FilterBar>

      {report.isPending ? (
        <div className="flex flex-col gap-4">
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            {Array.from({ length: 3 }).map((_, i) => (
              <Skeleton key={i} className="h-[120px] rounded-xl" />
            ))}
          </section>
          <Skeleton className="h-64 rounded-xl" />
        </div>
      ) : report.isError ? (
        (() => {
          const forbidden = report.error instanceof SdkError && report.error.status === 403;
          return (
            <ErrorState
              title={forbidden ? 'No access' : "Couldn't load the customer credit report"}
              description={
                forbidden
                  ? "You don't have permission to view customer receivables."
                  : String((report.error as Error).message)
              }
              onRetry={forbidden ? undefined : () => report.refetch()}
            />
          );
        })()
      ) : (
        (() => {
          const env = report.data;
          const data = (env.chart_data as CustomerCreditChartData | null) ?? {
            buckets: [],
            customers: [],
            exposure_shown: false,
          };
          const meters = meterItems(data.customers);

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
                      <CreditCard className="size-4 text-accent" />
                      <CardTitle className="text-base">Receivables by aging bucket</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <AgingBucketChart buckets={data.buckets} />
                    </CardContent>
                  </Card>

                  {data.exposure_shown && meters.length > 0 ? (
                    <Card>
                      <CardHeader className="flex-row items-center gap-2 space-y-0">
                        <Gauge className="size-4 text-accent" />
                        <CardTitle className="text-base">Credit-limit utilization</CardTitle>
                      </CardHeader>
                      <CardContent>
                        <p className="mb-4 text-sm text-muted-foreground">
                          Exposure as a percent of each top customer&apos;s credit limit. Lowest
                          headroom first.
                        </p>
                        <CreditLimitMeter items={meters} />
                      </CardContent>
                    </Card>
                  ) : null}

                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <ListOrdered className="size-4 text-accent" />
                      <CardTitle className="text-base">Top overdue</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <TopOverdueRanking customers={data.customers} />
                    </CardContent>
                  </Card>

                  <Card>
                    <CardHeader className="flex-row items-center gap-2 space-y-0">
                      <TrendingUp className="size-4 text-accent" />
                      <CardTitle className="text-base">Balance by customer</CardTitle>
                    </CardHeader>
                    <CardContent>
                      <BalanceByCustomer customers={data.customers} />
                    </CardContent>
                  </Card>

                  {/* Drillable per-customer aging table with risk badges. */}
                  <Card>
                    <CardHeader>
                      <CardTitle className="text-base">Aging by customer</CardTitle>
                    </CardHeader>
                    <CardContent className="p-0">
                      {data.customers.length === 0 ? (
                        <p className="p-6 text-sm text-muted-foreground">
                          No credit customer carries an outstanding balance.
                        </p>
                      ) : (
                        <div className="overflow-x-auto">
                          <Table>
                            <TableHeader>
                              <TableRow>
                                <TableHead>Customer</TableHead>
                                <TableHead className="text-right">Current</TableHead>
                                <TableHead className="text-right">1-30</TableHead>
                                <TableHead className="text-right">31-60</TableHead>
                                <TableHead className="text-right">61-90</TableHead>
                                <TableHead className="text-right">90+</TableHead>
                                <TableHead className="text-right">Outstanding</TableHead>
                                {data.exposure_shown ? (
                                  <TableHead className="text-right">Utilization</TableHead>
                                ) : null}
                                <TableHead>Risk</TableHead>
                              </TableRow>
                            </TableHeader>
                            <TableBody>
                              {data.customers.map((c) => {
                                const risk = riskFor(c);
                                return (
                                  <TableRow
                                    key={c.customer_id}
                                    className="cursor-pointer hover:bg-accent-muted/30"
                                    onClick={() => setDrilldownCustomer(c)}
                                  >
                                    <TableCell className="font-medium">{c.name}</TableCell>
                                    <TableCell className="text-right font-mono tabular-nums">
                                      {formatMoney(c.current)}
                                    </TableCell>
                                    <TableCell className="text-right font-mono tabular-nums">
                                      {formatMoney(c.days_1_30)}
                                    </TableCell>
                                    <TableCell className="text-right font-mono tabular-nums">
                                      {formatMoney(c.days_31_60)}
                                    </TableCell>
                                    <TableCell className="text-right font-mono tabular-nums">
                                      {formatMoney(c.days_61_90)}
                                    </TableCell>
                                    <TableCell className="text-right font-mono tabular-nums">
                                      {formatMoney(c.days_90_plus)}
                                    </TableCell>
                                    <TableCell className="text-right font-mono font-medium tabular-nums">
                                      {formatMoney(c.outstanding)}
                                    </TableCell>
                                    {data.exposure_shown ? (
                                      <TableCell className="text-right font-mono tabular-nums">
                                        {c.utilization != null ? `${c.utilization}%` : '—'}
                                      </TableCell>
                                    ) : null}
                                    <TableCell>
                                      <RiskBadge severity={risk.severity}>{risk.label}</RiskBadge>
                                    </TableCell>
                                  </TableRow>
                                );
                              })}
                            </TableBody>
                          </Table>
                        </div>
                      )}
                    </CardContent>
                  </Card>
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
                      reportKey="customer-aging"
                      filters={filters}
                      filenameBase={`customer-credit-${period}`}
                      permitted
                    />
                  </div>
                </aside>
              </div>

              <CustomerDrilldownDialog
                customer={drilldownCustomer}
                onClose={() => setDrilldownCustomer(null)}
              />
            </div>
          );
        })()
      )}
    </div>
  );
}
