import * as React from 'react';
import { AlertTriangle, Info, ShieldAlert } from 'lucide-react';

import { cn } from '../lib/cn';
import { Card, CardContent, CardHeader, CardTitle } from './card';

/**
 * Data-quality surfaces warn that report figures may be incomplete or
 * provisional. `level` drives the semantic colour; the message list is a plain
 * array of advisory strings derived server-side. These never block a report —
 * they are advisory chrome around it.
 */
export type DataQualityLevel = 'info' | 'warning' | 'critical';

const LEVEL_META: Record<
  DataQualityLevel,
  { wrap: string; tone: string; icon: React.ReactNode; label: string }
> = {
  info: {
    wrap: 'border-info/40 bg-info/10',
    tone: 'text-info',
    icon: <Info className="size-4" />,
    label: 'Data quality',
  },
  warning: {
    wrap: 'border-warning/40 bg-warning/10',
    tone: 'text-warning',
    icon: <AlertTriangle className="size-4" />,
    label: 'Data quality',
  },
  critical: {
    wrap: 'border-danger/40 bg-danger/10',
    tone: 'text-danger',
    icon: <ShieldAlert className="size-4" />,
    label: 'Data quality',
  },
};

export interface DataQualityBannerProps {
  /** Severity colour. Defaults to "warning". */
  level?: DataQualityLevel;
  /** Heading override; defaults to "Data quality". */
  title?: React.ReactNode;
  /** Advisory messages. When empty the banner renders nothing. */
  messages: string[];
  className?: string;
}

/** A compact inline banner that warns figures may be incomplete or provisional. */
export function DataQualityBanner({
  level = 'warning',
  title,
  messages,
  className,
}: DataQualityBannerProps) {
  if (messages.length === 0) return null;
  const meta = LEVEL_META[level];
  return (
    <div
      className={cn('flex items-start gap-3 rounded-xl border px-4 py-3', meta.wrap, className)}
      role={level === 'critical' ? 'alert' : 'status'}
    >
      <span className={cn('mt-0.5 shrink-0', meta.tone)}>{meta.icon}</span>
      <div className="flex flex-col gap-1">
        <span className="text-sm font-medium text-foreground">{title ?? meta.label}</span>
        <ul className="flex flex-col gap-0.5">
          {messages.map((m, i) => (
            <li key={i} className="text-xs text-muted-foreground">
              {m}
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

export interface DataQualityCardProps extends DataQualityBannerProps {}

/** A boxed Card variant of the banner for placement in a sidebar/column. */
export function DataQualityCard({
  level = 'warning',
  title,
  messages,
  className,
}: DataQualityCardProps) {
  if (messages.length === 0) return null;
  const meta = LEVEL_META[level];
  return (
    <Card className={cn('overflow-hidden border', meta.wrap, className)}>
      <CardHeader className="flex-row items-center gap-2 space-y-0 pb-2">
        <span className={cn('shrink-0', meta.tone)}>{meta.icon}</span>
        <CardTitle className="text-base">{title ?? meta.label}</CardTitle>
      </CardHeader>
      <CardContent>
        <ul className="flex flex-col gap-1">
          {messages.map((m, i) => (
            <li key={i} className="text-sm text-muted-foreground">
              {m}
            </li>
          ))}
        </ul>
      </CardContent>
    </Card>
  );
}
