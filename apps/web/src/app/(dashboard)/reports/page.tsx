'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import {
  ArrowRight,
  BarChart3,
  CalendarClock,
  Droplet,
  FileSpreadsheet,
  Gauge,
  Receipt,
  Scale,
  ShieldCheck,
  TrendingUp,
  Trophy,
  Users,
} from 'lucide-react';

import type { ReportCategory } from '@fuelgrid/sdk';
import { Badge, Button, ErrorState, PageHeader, ReportCategoryCard } from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

/**
 * Reports & Intelligence Center — the category hub. Driven by getReportsOverview
 * so every tile shows a LIVE headline metric and an alert/data-quality count
 * straight from the structured report API. Each category maps to its on-screen
 * signature report view (the API hrefs the overview returns are mapped here to
 * app routes; there are no dead links).
 */

interface CategoryMeta {
  href: string;
  icon: React.ReactNode;
}

// Map the overview category keys onto their on-screen route + icon.
const CATEGORY_META: Record<string, CategoryMeta> = {
  'inventory-reconciliation': {
    href: '/reports/inventory/reconciliation',
    icon: <Scale />,
  },
  'station-close': { href: '/reports/station-close', icon: <BarChart3 /> },
  'cash-reconciliation': { href: '/reports/cash-reconciliation', icon: <Receipt /> },
  'fuel-loss': { href: '/reports/fuel-loss', icon: <Droplet /> },
  receivables: { href: '/reports/customer-aging', icon: <Users /> },
  profitability: { href: '/reports/profitability', icon: <TrendingUp /> },
  'station-comparison': { href: '/reports/station-comparison', icon: <Trophy /> },
};

/** Format the category headline by its declared unit. */
function headline(c: ReportCategory): React.ReactNode {
  if (!c.headline) return '—';
  if (c.headline_unit === 'TZS') return formatMoney(c.headline);
  return c.headline;
}

/** A caption for the headline figure based on its unit. */
function headlineLabel(c: ReportCategory): string {
  if (c.headline_unit === 'TZS') return 'Total receivable';
  if (c.headline_unit === 'open alerts') return 'Open alerts';
  if (c.headline_unit) return c.headline_unit;
  return 'Latest';
}

export default function ReportsPage() {
  const overview = useQuery({
    queryKey: ['reports-overview'],
    queryFn: ({ signal }) => api.getReportsOverview(signal),
  });

  const categories = overview.data?.categories ?? [];

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports & intelligence"
        title="Reporting hub"
        description="A premium reporting center: open a signature report on-screen with the reconciliation waterfall, insights and data-quality checks, then export CSV, Excel, PDF and accountant-ready files. Money and litres are exact decimals throughout."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="secondary" size="sm" asChild>
              <Link href="/reports/executive">
                <Gauge className="size-4" />
                Executive report
              </Link>
            </Button>
            <Button variant="secondary" size="sm" asChild>
              <Link href="/reports/scheduled">
                <CalendarClock className="size-4" />
                Scheduled digests
              </Link>
            </Button>
          </div>
        }
      />

      {overview.isError ? (
        <ErrorState
          title="Couldn't load the reports overview"
          description={String((overview.error as Error).message)}
          onRetry={() => overview.refetch()}
        />
      ) : (
        <div className="grid grid-cols-1 gap-6 md:grid-cols-2 xl:grid-cols-3">
          {overview.isPending
            ? Array.from({ length: 6 }).map((_, i) => (
                <ReportCategoryCard key={i} title="Loading…" metricLabel="Headline" loading />
              ))
            : categories.map((c) => {
                const meta = CATEGORY_META[c.key];
                return (
                  <ReportCategoryCard
                    key={c.key}
                    icon={meta?.icon ?? <FileSpreadsheet />}
                    title={c.title}
                    description={c.description}
                    metricLabel={headlineLabel(c)}
                    metricValue={headline(c)}
                    alertCount={c.alert_count}
                    href={meta?.href}
                    linkComponent={Link}
                    actions={
                      meta?.href ? (
                        <Button size="sm" asChild>
                          <Link href={meta.href}>
                            View report
                            <ArrowRight className="size-4" />
                          </Link>
                        </Button>
                      ) : undefined
                    }
                  />
                );
              })}

          {/* Finance & accounting — accountant-ready exports live on their own page. */}
          {!overview.isPending ? (
            <ReportCategoryCard
              icon={<FileSpreadsheet />}
              title="Finance & Accounting"
              description="P&L, balance sheet and accountant-ready general ledger exports."
              metricLabel="Exports"
              metricValue="CSV · Excel · PDF · GL"
              href="/reports/exports"
              linkComponent={Link}
              actions={
                <Button size="sm" asChild>
                  <Link href="/reports/exports">
                    Open export center
                    <ArrowRight className="size-4" />
                  </Link>
                </Button>
              }
            />
          ) : null}
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
