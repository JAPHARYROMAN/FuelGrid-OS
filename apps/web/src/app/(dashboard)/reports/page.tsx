'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowRight,
  BarChart3,
  BookOpen,
  Download,
  FileSpreadsheet,
  FileText,
  Receipt,
  Scale,
  ShieldCheck,
  Users,
} from 'lucide-react';

import { type GeneralLedgerFormat, type ReportPeriod, type ReportSpec } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ErrorState,
  PageHeader,
  Skeleton,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';
import { usePermission } from '@/hooks/use-permissions';

import { PeriodSelect, StationSelect, useStationSelection } from './_components/filters';

const GL_FORMATS: { value: GeneralLedgerFormat; label: string }[] = [
  { value: 'csv', label: 'Generic CSV' },
  { value: 'iif', label: 'QuickBooks (IIF)' },
  { value: 'xero', label: 'Xero CSV' },
];

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

function triggerDownload(blob: Blob, filename: string) {
  const url = URL.createObjectURL(blob);
  const a = document.createElement('a');
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  URL.revokeObjectURL(url);
}

type BuildFn = () => { spec: ReportSpec; filename: string } | null;

/** A compact download button used inline on the hub category cards. */
function HubDownload({
  label,
  icon,
  permission,
  stationId,
  build,
}: {
  label: string;
  icon: React.ReactNode;
  permission: string;
  stationId?: string | null;
  build: BuildFn;
}) {
  const allowed = usePermission(permission, { stationID: stationId });
  const [busy, setBusy] = React.useState(false);
  const denied = allowed === false;

  async function download() {
    const built = build();
    if (!built) return;
    setBusy(true);
    try {
      const blob = await api.fetchReportBlob(built.spec);
      triggerDownload(blob, built.filename);
    } catch {
      // Errors surface on the dedicated report view; hub stays quiet.
    } finally {
      setBusy(false);
    }
  }

  return (
    <Button
      size="sm"
      variant="secondary"
      disabled={busy || denied || allowed === null}
      title={denied ? "You don't have permission" : undefined}
      onClick={download}
    >
      {icon}
      {busy ? '…' : label}
    </Button>
  );
}

interface CategoryCardProps {
  icon: React.ReactNode;
  title: string;
  description: string;
  metricLabel: string;
  metricValue: React.ReactNode;
  viewHref?: string;
  children?: React.ReactNode; // download buttons
}

function CategoryCard({
  icon,
  title,
  description,
  metricLabel,
  metricValue,
  viewHref,
  children,
}: CategoryCardProps) {
  return (
    <Card className="flex flex-col">
      <CardHeader className="flex-row items-start gap-3 space-y-0">
        <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
          {icon}
        </span>
        <div className="flex min-w-0 flex-col">
          <CardTitle>{title}</CardTitle>
          <p className="text-sm text-muted-foreground">{description}</p>
        </div>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col justify-between gap-4">
        <div className="flex flex-col gap-0.5">
          <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
            {metricLabel}
          </span>
          <span className="font-mono text-2xl font-semibold tabular-nums text-foreground">
            {metricValue}
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {viewHref ? (
            <Button size="sm" asChild>
              <Link href={viewHref}>
                View report
                <ArrowRight className="size-4" />
              </Link>
            </Button>
          ) : null}
          {children}
        </div>
      </CardContent>
    </Card>
  );
}

export default function ReportsPage() {
  const { stations, items, stationId, setStationId, current } = useStationSelection();
  const [period, setPeriod] = React.useState<ReportPeriod>('this-month');
  const [glFormat, setGlFormat] = React.useState<GeneralLedgerFormat>('csv');
  const stationCode = current?.code ?? 'station';

  const revenue = useQuery({
    queryKey: ['revenue-overview', stationId],
    queryFn: ({ signal }) => api.getRevenueOverview(stationId, signal),
    enabled: !!stationId,
  });
  const reconciliation = useQuery({
    queryKey: ['reconciliation-overview', stationId],
    queryFn: ({ signal }) => api.getReconciliationOverview(stationId, {}, signal),
    enabled: !!stationId,
  });
  const finance = useQuery({
    queryKey: ['finance-overview'],
    queryFn: ({ signal }) => api.getFinanceOverview(signal),
  });
  const aging = useQuery({
    queryKey: ['ar-aging'],
    queryFn: ({ signal }) => api.getARaging(signal),
  });

  const metric = (q: { isPending: boolean; isError: boolean }, value: React.ReactNode) =>
    q.isPending ? <Skeleton className="h-7 w-28 rounded-md" /> : q.isError ? '—' : value;

  const overCount = (reconciliation.data?.tanks ?? []).filter(
    (t) => t.reconciliation?.over_tolerance,
  ).length;
  const agingTotal = (aging.data?.items ?? []).reduce((a, c) => a + (Number(c.balance) || 0), 0);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports & exports"
        title="Reporting hub"
        description="Open a signature report on-screen with insights and data-quality checks, or download CSV, Excel, PDF and accountant-ready exports. Money and litres are exact decimals throughout."
        actions={
          items.length > 0 ? (
            <StationSelect items={items} value={stationId} onChange={setStationId} />
          ) : undefined
        }
      />

      {stations.isError ? (
        <ErrorState
          title="Couldn't load stations"
          description={String((stations.error as Error).message)}
          onRetry={() => stations.refetch()}
        />
      ) : (
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2 xl:grid-cols-3">
          <CategoryCard
            icon={<BarChart3 className="size-4" />}
            title="Daily Station Close"
            description="Recognized revenue, margin and tender split per day."
            metricLabel="Latest gross"
            metricValue={metric(revenue, formatMoney(revenue.data?.summary?.gross_revenue))}
            viewHref="/reports/daily-close"
          >
            <HubDownload
              label="PDF"
              icon={<FileText className="size-4" />}
              permission="revenue.read"
              stationId={stationId}
              build={() =>
                stationId
                  ? {
                      spec: { kind: 'daily-close-pdf', stationID: stationId },
                      filename: `daily-close-${stationCode}.pdf`,
                    }
                  : null
              }
            />
          </CategoryCard>

          <CategoryCard
            icon={<BarChart3 className="size-4" />}
            title="Sales Summary"
            description="Gross sales, litres and margin trend."
            metricLabel="Litres sold"
            metricValue={metric(revenue, revenue.data?.summary?.litres_sold ?? '—')}
            viewHref="/reports/sales-summary"
          >
            <HubDownload
              label="CSV"
              icon={<Download className="size-4" />}
              permission="revenue.read"
              stationId={stationId}
              build={() =>
                stationId
                  ? {
                      spec: { kind: 'revenue', stationID: stationId },
                      filename: `sales-${stationCode}.csv`,
                    }
                  : null
              }
            />
          </CategoryCard>

          <CategoryCard
            icon={<Scale className="size-4" />}
            title="Fuel Stock Reconciliation"
            description="Per-tank book→physical variance for the active day."
            metricLabel="Tanks over tolerance"
            metricValue={metric(reconciliation, overCount)}
            viewHref="/reports/stock-reconciliation"
          >
            <HubDownload
              label="CSV"
              icon={<Download className="size-4" />}
              permission="reconciliation.read"
              stationId={stationId}
              build={() =>
                stationId
                  ? {
                      spec: { kind: 'reconciliation', stationID: stationId },
                      filename: `reconciliation-${stationCode}.csv`,
                    }
                  : null
              }
            />
          </CategoryCard>

          <CategoryCard
            icon={<Receipt className="size-4" />}
            title="Cash Reconciliation"
            description="Expected vs counted cash by operating day."
            metricLabel="Cash today"
            metricValue={metric(revenue, formatMoney(revenue.data?.tenders?.cash))}
            viewHref="/reports/cash-reconciliation"
          />

          <CategoryCard
            icon={<Users className="size-4" />}
            title="Customer Aging"
            description="Outstanding credit balances by customer."
            metricLabel="Total receivable"
            metricValue={metric(aging, formatMoney(agingTotal.toFixed(2)))}
            viewHref="/reports/customer-aging"
          >
            <HubDownload
              label="CSV"
              icon={<Download className="size-4" />}
              permission="customer.read"
              build={() => ({ spec: { kind: 'ar-aging' }, filename: 'customer-aging.csv' })}
            />
          </CategoryCard>

          {/* Finance — downloads only (statements + general ledger). */}
          <Card className="flex flex-col">
            <CardHeader className="flex-row items-start gap-3 space-y-0">
              <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                <FileSpreadsheet className="size-4" />
              </span>
              <div className="flex min-w-0 flex-col">
                <CardTitle>Finance &amp; Accounting</CardTitle>
                <p className="text-sm text-muted-foreground">
                  P&amp;L, balance sheet and accountant-ready general ledger.
                </p>
              </div>
            </CardHeader>
            <CardContent className="flex flex-1 flex-col justify-between gap-4">
              <div className="flex flex-col gap-0.5">
                <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                  Net profit
                </span>
                <span className="font-mono text-2xl font-semibold tabular-nums text-foreground">
                  {metric(finance, formatMoney(finance.data?.income_statement?.net_profit))}
                </span>
              </div>
              <div className="flex flex-col gap-2">
                <PeriodSelect value={period} onChange={setPeriod} />
                <div className="flex flex-wrap items-center gap-2">
                  <HubDownload
                    label="P&L CSV"
                    icon={<Download className="size-4" />}
                    permission="finance.read"
                    build={() => ({
                      spec: { kind: 'financials', period },
                      filename: `financials-${period}.csv`,
                    })}
                  />
                  <HubDownload
                    label="Excel"
                    icon={<FileSpreadsheet className="size-4" />}
                    permission="finance.read"
                    build={() => ({
                      spec: { kind: 'financials-xlsx', period },
                      filename: `financials-${period}.xlsx`,
                    })}
                  />
                  <HubDownload
                    label="PDF"
                    icon={<FileText className="size-4" />}
                    permission="finance.read"
                    build={() => ({
                      spec: { kind: 'financials-pdf', period },
                      filename: `financials-${period}.pdf`,
                    })}
                  />
                </div>
                <div className="flex flex-wrap items-center gap-2">
                  <select
                    className={selectClasses}
                    value={glFormat}
                    onChange={(e) => setGlFormat(e.target.value as GeneralLedgerFormat)}
                    aria-label="General ledger format"
                  >
                    {GL_FORMATS.map((f) => (
                      <option key={f.value} value={f.value}>
                        {f.label}
                      </option>
                    ))}
                  </select>
                  <HubDownload
                    label="GL export"
                    icon={<BookOpen className="size-4" />}
                    permission="finance.read"
                    build={() => {
                      const ext = glFormat === 'iif' ? 'iif' : 'csv';
                      return {
                        spec: { kind: 'gl-export', period, format: glFormat },
                        filename: `general-ledger-${period}-${glFormat}.${ext}`,
                      };
                    }}
                  />
                </div>
              </div>
            </CardContent>
          </Card>
        </div>
      )}

      <p className="text-xs text-muted-foreground">
        <Badge tone="neutral">
          <ShieldCheck className="mr-1 inline size-3" />
          Audited
        </Badge>{' '}
        Every export is recorded in the audit log.
      </p>
    </div>
  );
}
