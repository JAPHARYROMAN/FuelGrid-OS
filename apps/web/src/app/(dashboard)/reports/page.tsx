'use client';

import * as React from 'react';
import Link from 'next/link';
import { useQuery } from '@tanstack/react-query';
import { AlertTriangle, ArrowRight, BarChart3, Search, ShieldCheck } from 'lucide-react';

import type { ReportCatalogCategory } from '@fuelgrid/sdk';
import {
  Badge,
  Button,
  DataQualityBanner,
  ErrorState,
  FilterBar,
  Input,
  MetricCard,
  PageHeader,
  ReportCategoryCard,
} from '@fuelgrid/ui';

import { api } from '@/lib/api';

import { ReportRails } from './_components/rails';
import { categoryHref, categoryIcon, formatMetricValue } from './_components/hub';

/**
 * Reports & Intelligence Center — the premium home (blueprint §4). Driven by
 * getReportCatalog: the 16 blueprint categories as DATA, each permission-
 * filtered server-side, with a live key metric, an honest availability state
 * (live / partial / placeholder), an alert pill where present, and a hub-level
 * data-quality band. The top bar carries a global catalog search (the one
 * control that does real work in Phase 1). Bottom rails surface recent/
 * scheduled/locked/exports, empty-state aware. Money/litres are exact decimal
 * strings.
 *
 * Phase 1 is the FOUNDATION: cards link to their existing report page on its own
 * route (no fabricated cross-context querystring — each report page owns its own
 * station/period today). Hub-level station/region/date context that pre-scopes a
 * report is a deliberately deferred follow-up (it requires the report pages to
 * read those params, which they do not yet); the hub does not advertise behavior
 * it cannot deliver.
 */

/** A category card matches the search over its name + description. */
function matchesSearch(c: ReportCatalogCategory, q: string): boolean {
  if (!q) return true;
  const needle = q.toLowerCase();
  return (
    c.name.toLowerCase().includes(needle) ||
    c.description.toLowerCase().includes(needle) ||
    c.reports.some(
      (r) => r.name.toLowerCase().includes(needle) || r.description.toLowerCase().includes(needle),
    )
  );
}

export default function ReportsPage() {
  const catalog = useQuery({
    queryKey: ['reports-catalog'],
    queryFn: ({ signal }) => api.getReportCatalog(signal),
  });

  const [search, setSearch] = React.useState('');

  const categories = catalog.data?.categories ?? [];
  const dataQuality = catalog.data?.data_quality ?? [];

  const filtered = React.useMemo(
    () => categories.filter((c) => matchesSearch(c, search)),
    [categories, search],
  );

  // Hero: total open alerts across the categories the actor sees, plus the
  // count of live (vs partial/placeholder) categories — an honest readiness cue.
  const totalAlerts = categories.reduce((sum, c) => sum + (c.alert_count || 0), 0);
  const liveCount = categories.filter((c) => c.availability === 'live').length;

  const dqWarnings = dataQuality.filter((d) => d.level === 'warning').map((d) => d.message);
  const dqInfos = dataQuality.filter((d) => d.level === 'info').map((d) => d.message);

  return (
    <div className="flex flex-col gap-7">
      <PageHeader
        eyebrow="Reports & intelligence"
        title="Reports center"
        description="A premium analytics library and command center. Open a signature report with its reconciliation visuals, deterministic insights and data-quality checks, then export CSV, Excel, PDF and accountant-ready files. Money and litres are exact decimals throughout."
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <Button variant="secondary" size="sm" asChild>
              <Link href="/reports/executive">
                <BarChart3 className="size-4" />
                Executive report
              </Link>
            </Button>
          </div>
        }
      />

      {/* Top bar (§4.2): catalog search — the control that does real work today. */}
      <FilterBar>
        <div className="relative w-full sm:w-72">
          <Search className="pointer-events-none absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
          <Input
            type="search"
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search reports…"
            aria-label="Search reports"
            className="pl-8"
          />
        </div>
      </FilterBar>

      {catalog.isError ? (
        <ErrorState
          title="Couldn't load the reports catalog"
          description={String((catalog.error as Error).message)}
          onRetry={() => catalog.refetch()}
        />
      ) : (
        <>
          {/* Hero / key report alerts band (§4.2). */}
          {!catalog.isPending ? (
            <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
              {/* State tiles: no `trend` — a directional arrow would misread as
                  "alerts decreasing". The count is a state, not a trend; the
                  hint carries the severity tone instead. */}
              <MetricCard
                label="Open report alerts"
                sublabel="Across your categories"
                value={String(totalAlerts)}
                icon={<AlertTriangle />}
                hint={totalAlerts > 0 ? 'Needs review' : 'All clear'}
              />
              <MetricCard
                label="Live report categories"
                sublabel="Ready to open now"
                value={`${liveCount} / ${categories.length}`}
                icon={<BarChart3 />}
                hint="The rest are limited or coming soon"
              />
              <MetricCard
                label="Data-quality notes"
                sublabel="Hub-wide"
                value={String(dataQuality.length)}
                icon={<ShieldCheck />}
                hint={dqWarnings.length > 0 ? `${dqWarnings.length} warning(s)` : 'Advisory only'}
              />
            </section>
          ) : null}

          {/* Hub data-quality warnings band (§4.2 bottom / §3.5). */}
          {dqWarnings.length > 0 ? (
            <DataQualityBanner
              level="warning"
              title="Data quality — needs attention"
              messages={dqWarnings}
            />
          ) : null}

          {/* Category grid (§4.3 / §4.4). */}
          <section aria-label="Report categories">
            {!catalog.isPending && filtered.length === 0 ? (
              <p className="rounded-xl border border-dashed border-border/70 px-4 py-8 text-center text-sm text-muted-foreground">
                No categories match “{search}”.
              </p>
            ) : (
              <div className="grid grid-cols-1 gap-6 md:grid-cols-2 xl:grid-cols-3">
                {catalog.isPending
                  ? Array.from({ length: 9 }).map((_, i) => (
                      <ReportCategoryCard key={i} title="Loading…" metricLabel="Metric" loading />
                    ))
                  : filtered.map((c) => <CategoryCard key={c.key} category={c} />)}
              </div>
            )}
          </section>

          {/* Advisory (non-warning) data-quality notes, de-emphasised. */}
          {!catalog.isPending && dqInfos.length > 0 ? (
            <DataQualityBanner level="info" title="Coverage notes" messages={dqInfos} />
          ) : null}

          {/* Bottom rails (§4.2): recent / scheduled / locked / exports. */}
          <ReportRails />
        </>
      )}

      <p className="text-xs text-muted-foreground">
        <Badge tone="neutral">
          <ShieldCheck className="mr-1 inline size-3" />
          Audited
        </Badge>{' '}
        Every export is recorded in the audit log. Sensitive figures (margins, supplier cost, credit
        exposure) are permission-gated.
      </p>
    </div>
  );
}

/** One category tile, wiring the catalog row onto the design-system card. */
function CategoryCard({ category: c }: { category: ReportCatalogCategory }) {
  // Link to the report's own route, unadorned. Each report page owns its station
  // and period today, so the hub does NOT fabricate a ?station_id/?from/?to the
  // page would ignore — that cross-context pre-scoping is a deferred follow-up.
  const linkHref = categoryHref(c);
  const value = formatMetricValue(c.metric.value, c.metric.unit);
  // The catalog contractually carries an honest `reason` for every null metric.
  // Render it directly; the fallback is intentionally distinct so a missing
  // reason (a contract miss) is visible in QA rather than masked as intentional.
  const reason = value == null ? (c.metric.reason ?? 'No reason provided.') : undefined;

  return (
    <ReportCategoryCard
      icon={categoryIcon(c.key)}
      title={c.name}
      description={c.description}
      availability={c.availability}
      metricLabel={c.metric.label}
      metricValue={value}
      metricReason={reason}
      alertCount={c.alert_count}
      href={linkHref}
      linkComponent={Link}
      actions={
        linkHref ? (
          <Button size="sm" asChild>
            <Link href={linkHref}>
              {c.availability === 'partial' ? 'Open report' : 'View report'}
              <ArrowRight className="size-4" />
            </Link>
          </Button>
        ) : c.availability === 'placeholder' ? (
          <Badge tone="neutral">Coming soon</Badge>
        ) : (
          <Badge tone="info">No on-screen view yet</Badge>
        )
      }
    />
  );
}
