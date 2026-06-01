import * as React from 'react';
import { AlertTriangle, Info, ShieldAlert } from 'lucide-react';

import { cn } from '../lib/cn';
import { Badge, type BadgeProps } from './badge';

/**
 * InsightCard — a single deterministic rule insight / recommendation. Promotes
 * the page-local insight row from the Reports Center into a reusable surface:
 * a severity badge, the observation message, and an optional recommended
 * action. Figures are derived server-side; this is pure presentation.
 */
export type InsightSeverity = 'info' | 'warning' | 'critical';

const SEVERITY_META: Record<
  InsightSeverity,
  { wrap: string; tone: string; icon: React.ReactNode; badge: BadgeProps['tone']; label: string }
> = {
  info: {
    wrap: 'border-accent/30 bg-accent-muted/40',
    tone: 'text-accent',
    icon: <Info className="size-4" />,
    badge: 'accent',
    label: 'Insight',
  },
  warning: {
    wrap: 'border-warning/30 bg-warning/10',
    tone: 'text-warning',
    icon: <AlertTriangle className="size-4" />,
    badge: 'warning',
    label: 'Warning',
  },
  critical: {
    wrap: 'border-danger/30 bg-danger/10',
    tone: 'text-danger',
    icon: <ShieldAlert className="size-4" />,
    badge: 'danger',
    label: 'Critical',
  },
};

export interface InsightCardProps {
  severity: InsightSeverity;
  message: React.ReactNode;
  /** Optional remediation guidance, shown as "Recommended: …". */
  recommendedAction?: React.ReactNode;
  /** Override the badge label; defaults to the severity label. */
  label?: React.ReactNode;
  /** Hide the severity badge, keeping just the icon + message. */
  hideBadge?: boolean;
  className?: string;
}

export function InsightCard({
  severity,
  message,
  recommendedAction,
  label,
  hideBadge = false,
  className,
}: InsightCardProps) {
  const meta = SEVERITY_META[severity];
  return (
    <div
      className={cn('flex items-start gap-3 rounded-lg border px-3.5 py-3', meta.wrap, className)}
    >
      <span className={cn('mt-0.5 shrink-0', meta.tone)}>{meta.icon}</span>
      <div className="flex min-w-0 flex-col gap-1">
        <div className="flex flex-wrap items-center gap-2">
          {hideBadge ? null : <Badge tone={meta.badge}>{label ?? meta.label}</Badge>}
          <span className="text-sm text-foreground">{message}</span>
        </div>
        {recommendedAction ? (
          <span className="text-xs text-muted-foreground">Recommended: {recommendedAction}</span>
        ) : null}
      </div>
    </div>
  );
}
