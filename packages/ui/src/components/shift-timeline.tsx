import * as React from 'react';
import { Check, CircleDashed, CircleDot, X } from 'lucide-react';

import { cn } from '../lib/cn';

/**
 * ShiftTimeline — an ordered list of shift milestones (opened → readings →
 * closed → approved) with timestamps and per-milestone status. Renders a
 * vertical stepped rail; the status drives the node colour/glyph. Timestamps
 * use the mono/tabular face so a stack of times aligns.
 */
export type MilestoneStatus = 'done' | 'current' | 'pending' | 'failed';

export interface ShiftMilestone {
  label: React.ReactNode;
  /** Pre-formatted timestamp string from the caller (e.g. "08:02"). */
  timestamp?: React.ReactNode;
  status?: MilestoneStatus;
  /** Optional supporting line under the label. */
  detail?: React.ReactNode;
}

export interface ShiftTimelineProps {
  milestones: ShiftMilestone[];
  className?: string;
}

const NODE: Record<MilestoneStatus, { ring: string; icon: React.ReactNode; text: string }> = {
  done: {
    ring: 'border-success/40 bg-success/15 text-success',
    icon: <Check className="size-3" />,
    text: 'text-foreground',
  },
  current: {
    ring: 'border-accent/50 bg-accent/15 text-accent',
    icon: <CircleDot className="size-3" />,
    text: 'text-foreground',
  },
  pending: {
    ring: 'border-border bg-muted/40 text-muted-foreground',
    icon: <CircleDashed className="size-3" />,
    text: 'text-muted-foreground',
  },
  failed: {
    ring: 'border-danger/40 bg-danger/15 text-danger',
    icon: <X className="size-3" />,
    text: 'text-foreground',
  },
};

export function ShiftTimeline({ milestones, className }: ShiftTimelineProps) {
  if (milestones.length === 0) {
    return <p className={cn('text-sm text-muted-foreground', className)}>No shift activity yet.</p>;
  }
  return (
    <ol className={cn('flex flex-col', className)}>
      {milestones.map((m, i) => {
        const status = m.status ?? 'pending';
        const node = NODE[status];
        const last = i === milestones.length - 1;
        return (
          <li key={i} className="flex gap-3">
            <div className="flex flex-col items-center">
              <span
                className={cn(
                  'flex size-6 shrink-0 items-center justify-center rounded-full border',
                  node.ring,
                )}
                aria-hidden
              >
                {node.icon}
              </span>
              {last ? null : (
                <span
                  className={cn('w-px flex-1', status === 'done' ? 'bg-success/40' : 'bg-border')}
                />
              )}
            </div>
            <div className={cn('flex min-w-0 flex-col gap-0.5 pb-5', last && 'pb-0')}>
              <div className="flex items-baseline justify-between gap-3">
                <span className={cn('text-sm font-medium', node.text)}>{m.label}</span>
                {m.timestamp != null ? (
                  <span className="shrink-0 font-mono text-xs tabular-nums text-muted-foreground">
                    {m.timestamp}
                  </span>
                ) : null}
              </div>
              {m.detail ? <span className="text-xs text-muted-foreground">{m.detail}</span> : null}
            </div>
          </li>
        );
      })}
    </ol>
  );
}
