'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { ArrowRight, Droplet, Receipt, Scale, TrendingUp, Users } from 'lucide-react';

import type { ReportCategory } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  Card,
  CardContent,
  CardHeader,
  CardTitle,
  ErrorState,
  MetricCard,
  PageHeader,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';
import { formatMoney } from '@/lib/money';

/**
 * Executive Fuel Business Report — a SKELETON. The headline KPIs are wired to
 * the live reports overview; the deeper sections (margin trend, station league
 * table, fuel-loss exposure, receivables concentration) render graceful
 * placeholders that link to the signature report where that data already lives,
 * so the page is useful today and ready to be filled in as those rollups land.
 */

function findCat(cats: ReportCategory[], key: string): ReportCategory | undefined {
  return cats.find((c) => c.key === key);
}

function headlineValue(c?: ReportCategory): React.ReactNode {
  if (!c || !c.headline) return '—';
  if (c.headline_unit === 'TZS') return formatMoney(c.headline);
  return c.headline;
}

interface SectionPlaceholder {
  title: string;
  description: string;
  href: string;
  cta: string;
  icon: React.ReactNode;
}

const SECTIONS: SectionPlaceholder[] = [
  {
    title: 'Sales & margin trend',
    description:
      'Period-over-period gross revenue and margin across the business. Open the daily station close for the per-day detail that feeds this view.',
    href: '/reports/station-close',
    cta: 'Daily station close',
    icon: <TrendingUp className="size-4" />,
  },
  {
    title: 'Inventory integrity',
    description:
      'Network-wide reconciliation status and tolerance breaches. The per-tank waterfall is available now on the reconciliation report.',
    href: '/reports/inventory/reconciliation',
    cta: 'Reconciliation report',
    icon: <Scale className="size-4" />,
  },
  {
    title: 'Fuel-loss exposure',
    description:
      'Loss litres, value and repeated-incident patterns. The fuel-loss report carries the live figures and risk severity per station.',
    href: '/reports/fuel-loss',
    cta: 'Fuel-loss report',
    icon: <Droplet className="size-4" />,
  },
  {
    title: 'Cash & receivables',
    description:
      'Cash variance and outstanding credit exposure. Open cash reconciliation and customer aging for the underlying detail.',
    href: '/reports/cash-reconciliation',
    cta: 'Cash reconciliation',
    icon: <Receipt className="size-4" />,
  },
];

export default function ExecutiveReportPage() {
  const overview = useQuery({
    queryKey: ['reports-overview'],
    queryFn: ({ signal }) => api.getReportsOverview(signal),
  });

  const cats = overview.data?.categories ?? [];
  const recon = findCat(cats, 'inventory-reconciliation');
  const loss = findCat(cats, 'fuel-loss');
  const receivables = findCat(cats, 'receivables');
  const generatedAt = overview.data?.generated_at
    ? new Date(overview.data.generated_at).toLocaleString()
    : null;

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports · Executive"
        title="Executive Fuel Business Report"
        description="A board-level summary of the fuel business: integrity, loss exposure and receivables at a glance, with drill-downs into the signature reports. Sections fill in as network rollups land."
        actions={generatedAt ? <Badge tone="neutral">Generated {generatedAt}</Badge> : undefined}
      />

      {overview.isError ? (
        <ErrorState
          title="Couldn't load the executive summary"
          description={String((overview.error as Error).message)}
          onRetry={() => overview.refetch()}
        />
      ) : (
        <>
          <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
            <MetricCard
              label="Open risk alerts"
              value={overview.isPending ? '—' : headlineValue(recon)}
              loading={overview.isPending}
              icon={<Scale />}
              sublabel="Inventory"
            />
            <MetricCard
              label="Fuel-loss alerts"
              value={overview.isPending ? '—' : headlineValue(loss)}
              loading={overview.isPending}
              icon={<Droplet />}
              sublabel="Loss"
            />
            <MetricCard
              label="Total receivable"
              value={overview.isPending ? '—' : headlineValue(receivables)}
              unit={receivables?.headline_unit === 'TZS' ? 'TZS' : undefined}
              loading={overview.isPending}
              icon={<Users />}
              sublabel="Credit"
            />
          </section>

          <section className="grid grid-cols-1 gap-6 lg:grid-cols-2">
            {SECTIONS.map((s) => (
              <Card key={s.title} className="flex flex-col">
                <CardHeader className="flex-row items-start gap-3 space-y-0">
                  <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent">
                    {s.icon}
                  </span>
                  <div className="flex min-w-0 flex-col">
                    <CardTitle>{s.title}</CardTitle>
                    <p className="text-sm text-muted-foreground">{s.description}</p>
                  </div>
                </CardHeader>
                <CardContent className="mt-auto flex items-center justify-between gap-3">
                  <Badge tone="neutral">Summary coming soon</Badge>
                  <Button size="sm" variant="secondary" asChild>
                    <Link href={s.href}>
                      {s.cta}
                      <ArrowRight className="size-4" />
                    </Link>
                  </Button>
                </CardContent>
              </Card>
            ))}
          </section>
        </>
      )}
    </div>
  );
}
