import * as React from 'react';

import { cn } from '../lib/cn';
import { Card, CardContent, CardHeader, CardTitle } from './card';
import { Badge } from './badge';
import { Skeleton } from './skeleton';

/**
 * ReportCategoryCard — the hub tile that promotes the page-local CategoryCard
 * from the Reports Center: an icon + title + description, a headline metric,
 * an optional alert count, an availability state, and an actions slot.
 *
 * `href` is intentionally render-prop free: callers wrap the whole card with
 * their own router <Link> by passing a `linkComponent`, keeping @fuelgrid/ui
 * free of a next/link dependency. When omitted the card is a plain surface.
 *
 * Availability is honest (blueprint §3.5): a `live` card carries a real figure
 * and is clickable; a `partial` card is clickable but flags a limited view; a
 * `placeholder` card is visibly unavailable / coming-soon and never faked. When
 * a category has no genuine metric, pass `metricReason` (gated/partial/absent)
 * instead of a value and the card shows the honest reason in muted text.
 */
export type ReportCardAvailability = 'live' | 'partial' | 'placeholder';

const AVAILABILITY_BADGE: Record<
  ReportCardAvailability,
  { tone: 'success' | 'info' | 'neutral'; label: string } | null
> = {
  live: null,
  partial: { tone: 'info', label: 'Limited' },
  placeholder: { tone: 'neutral', label: 'Coming soon' },
};

export interface ReportCategoryCardProps {
  icon?: React.ReactNode;
  title: React.ReactNode;
  description?: React.ReactNode;
  /** The headline figure caption, e.g. "Latest gross". */
  metricLabel?: React.ReactNode;
  /** The headline figure itself (a formatted decimal string or count). */
  metricValue?: React.ReactNode;
  /**
   * The honest reason there is no figure (gated, partial, or no data). Shown in
   * place of the value when `metricValue` is null/undefined. Never a number.
   */
  metricReason?: React.ReactNode;
  /** Renders a skeleton in place of the metric value. */
  loading?: boolean;
  /** When > 0, an amber pill is shown next to the title. */
  alertCount?: number;
  /**
   * The card's availability. `partial`/`placeholder` show a status pill and,
   * for placeholders, mute the whole card so it reads as unavailable.
   */
  availability?: ReportCardAvailability;
  /** Right/bottom actions: download buttons, a "View report" link, etc. */
  actions?: React.ReactNode;
  /**
   * Optional href for an app-level link. When set the title row becomes a
   * focusable anchor. Pair with `linkComponent` to use a framework router.
   */
  href?: string;
  /** A custom anchor component (e.g. next/link). Defaults to a plain <a>. */
  linkComponent?: React.ElementType;
  className?: string;
}

export function ReportCategoryCard({
  icon,
  title,
  description,
  metricLabel,
  metricValue,
  metricReason,
  loading = false,
  alertCount,
  availability = 'live',
  actions,
  href,
  linkComponent,
  className,
}: ReportCategoryCardProps) {
  const Link = linkComponent ?? 'a';
  const hasAlert = typeof alertCount === 'number' && alertCount > 0;
  const statusBadge = AVAILABILITY_BADGE[availability];
  const isPlaceholder = availability === 'placeholder';
  const hasValue = metricValue != null;

  const titleNode = href ? (
    <Link
      href={href}
      className="rounded-sm outline-none after:absolute after:inset-0 hover:underline focus-visible:ring-2 focus-visible:ring-ring"
    >
      {title}
    </Link>
  ) : (
    title
  );

  return (
    <Card
      className={cn('group relative flex flex-col', isPlaceholder && 'opacity-75', className)}
      aria-disabled={isPlaceholder || undefined}
    >
      <CardHeader className="flex-row items-start gap-3 space-y-0">
        {icon ? (
          <span className="mt-0.5 flex size-9 shrink-0 items-center justify-center rounded-lg bg-accent-muted/60 text-accent [&_svg]:size-4">
            {icon}
          </span>
        ) : null}
        <div className="flex min-w-0 flex-col gap-0.5">
          <div className="flex flex-wrap items-center gap-2">
            <CardTitle>{titleNode}</CardTitle>
            {hasAlert ? (
              <Badge tone="warning" className="relative z-10">
                {alertCount} alert{alertCount === 1 ? '' : 's'}
              </Badge>
            ) : null}
            {statusBadge ? (
              <Badge tone={statusBadge.tone} className="relative z-10">
                {statusBadge.label}
              </Badge>
            ) : null}
          </div>
          {description ? <p className="text-sm text-muted-foreground">{description}</p> : null}
        </div>
      </CardHeader>
      <CardContent className="flex flex-1 flex-col justify-between gap-4">
        {metricLabel || hasValue || metricReason || loading ? (
          <div className="flex flex-col gap-0.5">
            {metricLabel ? (
              <span className="text-xs font-medium uppercase tracking-wider text-muted-foreground">
                {metricLabel}
              </span>
            ) : null}
            {loading ? (
              <Skeleton className="h-7 w-28 rounded-md" />
            ) : hasValue ? (
              <span className="font-mono text-2xl font-semibold tabular-nums text-foreground">
                {metricValue}
              </span>
            ) : metricReason ? (
              <span className="text-sm text-muted-foreground">{metricReason}</span>
            ) : null}
          </div>
        ) : null}
        {actions ? (
          <div className="relative z-10 flex flex-wrap items-center gap-2">{actions}</div>
        ) : null}
      </CardContent>
    </Card>
  );
}
