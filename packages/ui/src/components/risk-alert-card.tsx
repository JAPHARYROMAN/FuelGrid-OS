import * as React from 'react';

import { cn } from '../lib/cn';
import { RiskBadge, type RiskSeverity } from './risk-badge';

/**
 * RiskAlertCard — a full alert surface (not just the badge). Promotes the
 * page-local "open risk alert" row into a reusable card: a severity-tinted
 * left accent bar, a RiskBadge, the title + description, an optional headline
 * metric (mono/tabular so a column of metrics aligns), and an optional
 * "Recommended action" line. Station + occurredAt render as a quiet meta row.
 *
 * Like ReportCategoryCard, this stays free of any router dependency: pass an
 * `href` (optionally with a `linkComponent`, e.g. next/link) for a navigable
 * card, or `onClick` for an in-app handler. Either makes the whole card
 * interactive via a focusable overlay link/button.
 */

/** Per-severity accent bar + soft wash, keyed to the semantic status tokens. */
const SEVERITY_ACCENT: Record<RiskSeverity, string> = {
  critical: 'bg-danger',
  high: 'bg-warning',
  medium: 'bg-warning/70',
  low: 'bg-info',
  info: 'bg-muted-foreground/40',
};

export interface RiskAlertCardProps {
  severity: RiskSeverity;
  title: React.ReactNode;
  description?: React.ReactNode;
  /** Headline figure caption, e.g. "Variance". */
  metricLabel?: React.ReactNode;
  /** Headline figure itself (a formatted decimal string or count). */
  metricValue?: React.ReactNode;
  /** Remediation guidance, shown as a "Recommended action" line. */
  recommendedAction?: React.ReactNode;
  /** Station code/name shown in the meta row. */
  station?: React.ReactNode;
  /** Pre-formatted timestamp string shown in the meta row. */
  occurredAt?: React.ReactNode;
  /** In-app click handler — makes the whole card a focusable button overlay. */
  onClick?: () => void;
  /**
   * Optional href for an app-level link. When set the card becomes navigable
   * via a focusable overlay anchor. Pair with `linkComponent` for a router.
   */
  href?: string;
  /** A custom anchor component (e.g. next/link). Defaults to a plain <a>. */
  linkComponent?: React.ElementType;
  /** Override the RiskBadge label; defaults to the severity word. */
  badgeLabel?: React.ReactNode;
  className?: string;
}

export function RiskAlertCard({
  severity,
  title,
  description,
  metricLabel,
  metricValue,
  recommendedAction,
  station,
  occurredAt,
  onClick,
  href,
  linkComponent,
  badgeLabel,
  className,
}: RiskAlertCardProps) {
  const Link = linkComponent ?? 'a';
  const interactive = Boolean(href || onClick);
  const hasMeta = station != null || occurredAt != null;

  const overlay = href ? (
    <Link
      href={href}
      aria-label={typeof title === 'string' ? title : undefined}
      className="absolute inset-0 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
    />
  ) : onClick ? (
    <button
      type="button"
      onClick={onClick}
      aria-label={typeof title === 'string' ? title : undefined}
      className="absolute inset-0 rounded-xl outline-none focus-visible:ring-2 focus-visible:ring-ring"
    />
  ) : null;

  return (
    <div
      className={cn(
        'group relative flex gap-0 overflow-hidden rounded-xl border border-border/80 bg-card text-card-foreground shadow-elev-sm',
        interactive && 'transition-colors hover:border-accent/40 hover:shadow-elev-md',
        className,
      )}
    >
      {/* Severity-tinted left accent bar. */}
      <span aria-hidden className={cn('w-1 shrink-0', SEVERITY_ACCENT[severity])} />

      <div className="flex min-w-0 flex-1 flex-col gap-2.5 p-4">
        <div className="flex items-start justify-between gap-3">
          <div className="flex min-w-0 flex-col gap-1.5">
            <RiskBadge severity={severity}>{badgeLabel}</RiskBadge>
            <h3 className="text-sm font-semibold leading-snug tracking-tight text-foreground">
              {title}
            </h3>
            {description ? (
              <p className="text-sm leading-relaxed text-muted-foreground">{description}</p>
            ) : null}
          </div>
          {metricLabel != null || metricValue != null ? (
            <div className="flex shrink-0 flex-col items-end gap-0.5 text-right">
              {metricLabel ? (
                <span className="text-[11px] font-medium uppercase tracking-wider text-muted-foreground">
                  {metricLabel}
                </span>
              ) : null}
              {metricValue != null ? (
                <span className="font-mono text-lg font-semibold tabular-nums text-foreground">
                  {metricValue}
                </span>
              ) : null}
            </div>
          ) : null}
        </div>

        {recommendedAction ? (
          <p className="text-xs text-muted-foreground">
            <span className="font-medium text-foreground">Recommended action:</span>{' '}
            {recommendedAction}
          </p>
        ) : null}

        {hasMeta ? (
          <div className="flex flex-wrap items-center gap-x-3 gap-y-1 text-xs text-muted-foreground">
            {station != null ? (
              <span className="inline-flex items-center gap-1.5">
                <span className="font-medium text-foreground/80">Station</span>
                <span className="font-mono tabular-nums">{station}</span>
              </span>
            ) : null}
            {occurredAt != null ? (
              <span className="font-mono tabular-nums">{occurredAt}</span>
            ) : null}
          </div>
        ) : null}
      </div>

      {/* Focusable overlay sits above content but below interactive metric/action. */}
      {overlay}
    </div>
  );
}
