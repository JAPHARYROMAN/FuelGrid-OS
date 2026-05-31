'use client';

import * as React from 'react';
import { motion } from 'framer-motion';

import { cn } from '../lib/cn';
import { parseDecimal } from '../lib/money';

export interface TankVisualProps {
  name: string;
  code: string;
  /** Product colour (any CSS colour — usually the product's stored hex). */
  color: string;
  /**
   * Tank dimensions as exact decimal STRINGS from the SDK (numeric -> text).
   * Parsed once here for the SVG geometry/display; never fed back into
   * business logic.
   */
  capacityLitres: string;
  safeMinLitres: string;
  safeMaxLitres: string;
  /**
   * Current volume in litres. Phase 2 has no readings yet, so leave it
   * null/undefined — the tank renders an "awaiting reading" placeholder.
   * Phase 3 fills this from dip readings and the fill animates in. The API
   * still emits current_litres as a number, so accept either.
   */
  currentLitres?: number | string | null;
  status?: string;
  className?: string;
}

// SVG canvas units. The body is the inner region the fluid occupies.
const W = 120;
const H = 200;
const PAD = 12;
const BODY_TOP = PAD;
const BODY_H = H - PAD * 2;
const BODY_X = PAD;
const BODY_W = W - PAD * 2;

const clamp01 = (n: number) => Math.max(0, Math.min(1, n));
const fmt = (n: number) => Math.round(n).toLocaleString();

// y of the waterline for a given litre level (0 at bottom of the body).
function levelY(litres: number, capacity: number) {
  return BODY_TOP + BODY_H * (1 - clamp01(capacity > 0 ? litres / capacity : 0));
}

export function TankVisual({
  name,
  code,
  color,
  capacityLitres,
  safeMinLitres,
  safeMaxLitres,
  currentLitres,
  status,
  className,
}: TankVisualProps) {
  const gradientID = React.useId();
  // Parse the decimal-string dimensions once for geometry/display.
  const capacity = parseDecimal(capacityLitres);
  const safeMin = parseDecimal(safeMinLitres);
  const safeMax = parseDecimal(safeMaxLitres);
  const current = parseDecimal(currentLitres);
  const hasReading = currentLitres != null && currentLitres !== '' && Number.isFinite(current);
  const fillFrac = hasReading ? clamp01(current / capacity) : 0;
  const fillH = BODY_H * fillFrac;
  const fillY = BODY_TOP + BODY_H - fillH;

  const minY = levelY(safeMin, capacity);
  const maxY = levelY(safeMax, capacity);
  const ullage = hasReading ? Math.max(0, capacity - current) : null;

  return (
    <div
      className={cn('flex flex-col gap-3 rounded-lg border border-border bg-card p-4', className)}
    >
      <div className="flex items-start justify-between gap-2">
        <div className="flex items-center gap-2">
          <span
            className="inline-block size-3 rounded-full border border-border"
            style={{ backgroundColor: color }}
            aria-hidden
          />
          <div className="leading-tight">
            <p className="text-sm font-medium">{name}</p>
            <p className="font-mono text-[11px] text-muted-foreground">{code}</p>
          </div>
        </div>
        <span className="rounded-full bg-muted px-2 py-0.5 text-[11px] uppercase tracking-wider text-muted-foreground">
          ullage {ullage != null ? `${fmt(ullage)} L` : '—'}
        </span>
      </div>

      <div className="flex items-center gap-4">
        <svg
          viewBox={`0 0 ${W} ${H}`}
          className="h-40 w-auto shrink-0"
          role="img"
          aria-label={`${name} tank fill`}
        >
          <defs>
            <linearGradient id={gradientID} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.65} />
              <stop offset="100%" stopColor={color} stopOpacity={0.95} />
            </linearGradient>
          </defs>

          {/* Tank body outline */}
          <rect
            x={BODY_X}
            y={BODY_TOP}
            width={BODY_W}
            height={BODY_H}
            rx={10}
            className="fill-muted/40 stroke-border"
            strokeWidth={1.5}
          />

          {/* Animated fluid fill, clipped to the rounded body */}
          <clipPath id={`${gradientID}-clip`}>
            <rect x={BODY_X} y={BODY_TOP} width={BODY_W} height={BODY_H} rx={10} />
          </clipPath>
          {hasReading ? (
            <g clipPath={`url(#${gradientID}-clip)`}>
              <motion.rect
                x={BODY_X}
                width={BODY_W}
                fill={`url(#${gradientID})`}
                initial={{ y: BODY_TOP + BODY_H, height: 0 }}
                animate={{ y: fillY, height: fillH }}
                transition={{ duration: 0.9, ease: [0.22, 1, 0.36, 1] }}
              />
            </g>
          ) : (
            <text
              x={W / 2}
              y={BODY_TOP + BODY_H / 2}
              textAnchor="middle"
              className="fill-muted-foreground"
              style={{ fontSize: 9 }}
            >
              awaiting reading
            </text>
          )}

          {/* Safe-min / safe-max markers */}
          <g className="stroke-warning" strokeWidth={1} strokeDasharray="3 2">
            <line x1={BODY_X} y1={maxY} x2={BODY_X + BODY_W} y2={maxY} opacity={0.8} />
            <line x1={BODY_X} y1={minY} x2={BODY_X + BODY_W} y2={minY} opacity={0.8} />
          </g>
          <text
            x={BODY_X + BODY_W + 1}
            y={maxY + 3}
            className="fill-muted-foreground"
            style={{ fontSize: 7 }}
          >
            max
          </text>
          <text
            x={BODY_X + BODY_W + 1}
            y={minY + 3}
            className="fill-muted-foreground"
            style={{ fontSize: 7 }}
          >
            min
          </text>
        </svg>

        <dl className="flex flex-col gap-1.5 text-sm">
          <div>
            <dt className="text-[11px] uppercase tracking-wider text-muted-foreground">Current</dt>
            <dd className="font-semibold tabular-nums">{hasReading ? `${fmt(current)} L` : '—'}</dd>
          </div>
          <div>
            <dt className="text-[11px] uppercase tracking-wider text-muted-foreground">Capacity</dt>
            <dd className="tabular-nums text-muted-foreground">{fmt(capacity)} L</dd>
          </div>
          {status && status !== 'active' ? (
            <span className="w-fit rounded-full bg-warning/15 px-2 py-0.5 text-[11px] uppercase tracking-wider text-warning">
              {status}
            </span>
          ) : null}
        </dl>
      </div>
    </div>
  );
}
