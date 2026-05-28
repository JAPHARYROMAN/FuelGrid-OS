'use client';

import * as React from 'react';

import { cn } from '../lib/cn';

export interface PumpCardNozzle {
  id: string;
  number: number;
  productName: string;
  productColor: string;
  tankCode: string;
  price: number;
}

export interface PumpCardProps {
  number: number;
  status: string;
  nozzles: PumpCardNozzle[];
  /** Optional click handler — when set the whole card is interactive. */
  onActivate?: () => void;
  className?: string;
}

// Status pulse colour: green active, amber needs-attention, grey otherwise.
function pulseClasses(status: string): { dot: string; ping: boolean } {
  switch (status) {
    case 'active':
      return { dot: 'bg-success', ping: true };
    case 'maintenance':
      return { dot: 'bg-warning', ping: false };
    case 'inactive':
    case 'decommissioned':
    default:
      return { dot: 'bg-muted-foreground', ping: false };
  }
}

export function PumpCard({ number, status, nozzles, onActivate, className }: PumpCardProps) {
  const pulse = pulseClasses(status);
  const interactive = Boolean(onActivate);

  return (
    <div
      className={cn(
        'flex flex-col rounded-lg border border-border bg-card transition-colors',
        interactive && 'cursor-pointer hover:border-accent',
        className,
      )}
      {...(interactive
        ? {
            role: 'button',
            tabIndex: 0,
            onClick: onActivate,
            onKeyDown: (e: React.KeyboardEvent) => {
              if (e.key === 'Enter' || e.key === ' ') {
                e.preventDefault();
                onActivate!();
              }
            },
          }
        : {})}
    >
      <div className="flex items-center justify-between gap-2 border-b border-border px-4 py-3">
        <div className="flex items-center gap-2.5">
          <span className="relative flex size-2.5">
            {pulse.ping ? (
              <span
                className={cn(
                  'absolute inline-flex size-full animate-ping rounded-full opacity-60',
                  pulse.dot,
                )}
              />
            ) : null}
            <span className={cn('relative inline-flex size-2.5 rounded-full', pulse.dot)} />
          </span>
          <span className="font-semibold">Pump {number}</span>
        </div>
        <span className="text-[11px] uppercase tracking-wider text-muted-foreground">{status}</span>
      </div>

      <div className="flex flex-col">
        {nozzles.length === 0 ? (
          <p className="px-4 py-3 text-sm text-muted-foreground">No nozzles configured.</p>
        ) : (
          nozzles
            .slice()
            .sort((a, b) => a.number - b.number)
            .map((n) => (
              <div
                key={n.id}
                className="flex items-center justify-between gap-3 border-b border-border/60 px-4 py-2 text-sm last:border-b-0"
              >
                <div className="flex items-center gap-2.5">
                  <span className="w-8 font-mono text-xs text-muted-foreground">N{n.number}</span>
                  <span
                    className="inline-block size-3 rounded-full border border-border"
                    style={{ backgroundColor: n.productColor }}
                    aria-hidden
                  />
                  <span>{n.productName}</span>
                  <span className="text-muted-foreground">← {n.tankCode}</span>
                </div>
                <span className="tabular-nums font-medium">{n.price.toFixed(2)}</span>
              </div>
            ))
        )}
      </div>
    </div>
  );
}
