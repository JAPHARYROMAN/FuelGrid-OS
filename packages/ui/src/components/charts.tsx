'use client';

import * as React from 'react';
import {
  Area,
  AreaChart as RAreaChart,
  Bar,
  BarChart as RBarChart,
  CartesianGrid,
  Cell,
  Line,
  LineChart as RLineChart,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from 'recharts';

import { cn } from '../lib/cn';
import { axisTick, chartColors } from '../lib/chart-theme';

/**
 * Refined Console chart wrappers over recharts.
 *
 * House style: one confident indigo accent for data, muted hairline grid that
 * never competes, font-mono tabular-nums in axes + tooltips, and colors driven
 * entirely by the live design tokens (so charts track light/dark). Callers pass
 * already-fetched rows and the keys to read — these never fabricate series.
 *
 * Money/litre values arrive as decimal STRINGS; pass a `valueFormatter`
 * (e.g. formatMoney/formatLitres) so the float coercion recharts needs for
 * geometry never leaks into the displayed number.
 */

// A row of chart data. Kept open so typed SDK interfaces (which lack an index
// signature) satisfy it; series keys are read via `keyof T` at the call sites.
type Datum = object;

export interface ChartSeries {
  /** Object key to read from each row. */
  key: string;
  /** Human label shown in the tooltip. */
  label: string;
  /** Token color; defaults to the indigo accent. */
  color?: string;
}

interface BaseChartProps<T extends Datum> {
  data: T[];
  /** Key for the category/time axis. */
  xKey: keyof T & string;
  /** Format an x value for axis ticks + tooltip heading. */
  xFormatter?: (value: unknown) => string;
  /** Format a y value for the tooltip body + Y axis ticks. */
  valueFormatter?: (value: unknown) => string;
  /** Pixel height; defaults to 220. */
  height?: number;
  className?: string;
}

/** Coerce a decimal string (or number) into a finite number for geometry only. */
function toNumber(v: unknown): number {
  if (typeof v === 'number') return Number.isFinite(v) ? v : 0;
  if (typeof v === 'string') {
    const n = Number(v);
    return Number.isFinite(n) ? n : 0;
  }
  return 0;
}

const identity = (v: unknown) => String(v ?? '');

interface TooltipPayloadEntry {
  name?: React.ReactNode;
  value?: unknown;
  color?: string;
  dataKey?: string | number;
}

/** Shared tooltip — surface card, hairline border, mono tabular values. */
function ChartTooltip({
  active,
  payload,
  label,
  xFormatter = identity,
  valueFormatter = identity,
}: {
  active?: boolean;
  payload?: TooltipPayloadEntry[];
  label?: unknown;
  xFormatter?: (value: unknown) => string;
  valueFormatter?: (value: unknown) => string;
}) {
  if (!active || !payload || payload.length === 0) return null;
  return (
    <div className="rounded-lg border border-border bg-popover px-3 py-2 shadow-elev-md">
      <p className="mb-1 text-xs font-medium text-muted-foreground">{xFormatter(label)}</p>
      <ul className="flex flex-col gap-0.5">
        {payload.map((p, i) => (
          <li key={i} className="flex items-center justify-between gap-4 text-xs">
            <span className="flex items-center gap-1.5 text-muted-foreground">
              <span
                aria-hidden
                className="size-2 rounded-full"
                style={{ backgroundColor: p.color ?? chartColors.accent }}
              />
              {p.name}
            </span>
            <span className="font-mono font-medium tabular-nums text-foreground">
              {valueFormatter(p.value)}
            </span>
          </li>
        ))}
      </ul>
    </div>
  );
}

const GRID = { stroke: chartColors.grid, strokeDasharray: '0', vertical: false } as const;
const MARGIN = { top: 8, right: 8, bottom: 0, left: 0 } as const;

/**
 * Adapt recharts' tooltip render-prop (whose generic payload type does not line
 * up with our narrowed entry shape) onto ChartTooltip. recharts passes a single
 * props object; we read only active/payload/label.
 */
function renderTooltip(
  xFormatter: (value: unknown) => string,
  valueFormatter?: (value: unknown) => string,
) {
  return (p: { active?: boolean; payload?: TooltipPayloadEntry[]; label?: unknown }) => (
    <ChartTooltip
      active={p.active}
      payload={p.payload}
      label={p.label}
      xFormatter={xFormatter}
      valueFormatter={valueFormatter}
    />
  );
}

/** Multi-series line chart — trends over time. */
export function LineChart<T extends Datum>({
  data,
  xKey,
  series,
  xFormatter = identity,
  valueFormatter,
  height = 220,
  className,
}: BaseChartProps<T> & { series: ChartSeries[] }) {
  return (
    <div className={cn('w-full', className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <RLineChart data={data} margin={MARGIN}>
          <CartesianGrid {...GRID} />
          <XAxis
            dataKey={xKey}
            tick={axisTick}
            tickFormatter={(v) => xFormatter(v)}
            tickLine={false}
            axisLine={{ stroke: chartColors.grid }}
            minTickGap={16}
          />
          <YAxis
            tick={axisTick}
            tickFormatter={valueFormatter ? (v) => valueFormatter(v) : undefined}
            tickLine={false}
            axisLine={false}
            width={56}
          />
          <Tooltip
            cursor={{ stroke: chartColors.grid }}
            content={renderTooltip(xFormatter, valueFormatter)}
          />
          {series.map((s) => (
            <Line
              key={s.key}
              type="monotone"
              dataKey={(row) => toNumber((row as Record<string, unknown>)[s.key])}
              name={s.label}
              stroke={s.color ?? chartColors.accent}
              strokeWidth={2}
              dot={false}
              activeDot={{ r: 4 }}
              isAnimationActive={false}
            />
          ))}
        </RLineChart>
      </ResponsiveContainer>
    </div>
  );
}

/** Multi-series area chart — cumulative or band trends (e.g. variance). */
export function AreaChart<T extends Datum>({
  data,
  xKey,
  series,
  xFormatter = identity,
  valueFormatter,
  height = 220,
  className,
}: BaseChartProps<T> & { series: ChartSeries[] }) {
  const gradId = React.useId();
  return (
    <div className={cn('w-full', className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <RAreaChart data={data} margin={MARGIN}>
          <defs>
            {series.map((s, i) => (
              <linearGradient key={s.key} id={`${gradId}-${i}`} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={s.color ?? chartColors.accent} stopOpacity={0.28} />
                <stop offset="100%" stopColor={s.color ?? chartColors.accent} stopOpacity={0.02} />
              </linearGradient>
            ))}
          </defs>
          <CartesianGrid {...GRID} />
          <XAxis
            dataKey={xKey}
            tick={axisTick}
            tickFormatter={(v) => xFormatter(v)}
            tickLine={false}
            axisLine={{ stroke: chartColors.grid }}
            minTickGap={16}
          />
          <YAxis
            tick={axisTick}
            tickFormatter={valueFormatter ? (v) => valueFormatter(v) : undefined}
            tickLine={false}
            axisLine={false}
            width={56}
          />
          <Tooltip
            cursor={{ stroke: chartColors.grid }}
            content={renderTooltip(xFormatter, valueFormatter)}
          />
          {series.map((s, i) => (
            <Area
              key={s.key}
              type="monotone"
              dataKey={(row) => toNumber((row as Record<string, unknown>)[s.key])}
              name={s.label}
              stroke={s.color ?? chartColors.accent}
              strokeWidth={2}
              fill={`url(#${gradId}-${i})`}
              isAnimationActive={false}
            />
          ))}
        </RAreaChart>
      </ResponsiveContainer>
    </div>
  );
}

/** Single- or multi-series bar chart — rankings, mix, P&L. */
export function BarChart<T extends Datum>({
  data,
  xKey,
  series,
  xFormatter = identity,
  valueFormatter,
  height = 220,
  layout = 'horizontal',
  className,
}: BaseChartProps<T> & {
  series: ChartSeries[];
  /** 'vertical' draws horizontal bars (good for long category labels). */
  layout?: 'horizontal' | 'vertical';
}) {
  const vertical = layout === 'vertical';
  return (
    <div className={cn('w-full', className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <RBarChart data={data} layout={layout} margin={vertical ? { ...MARGIN, left: 8 } : MARGIN}>
          <CartesianGrid stroke={chartColors.grid} vertical={vertical} horizontal={!vertical} />
          {vertical ? (
            <>
              <XAxis
                type="number"
                tick={axisTick}
                tickFormatter={valueFormatter ? (v) => valueFormatter(v) : undefined}
                tickLine={false}
                axisLine={false}
              />
              <YAxis
                type="category"
                dataKey={xKey}
                tick={axisTick}
                tickFormatter={(v) => xFormatter(v)}
                tickLine={false}
                axisLine={{ stroke: chartColors.grid }}
                width={120}
              />
            </>
          ) : (
            <>
              <XAxis
                dataKey={xKey}
                tick={axisTick}
                tickFormatter={(v) => xFormatter(v)}
                tickLine={false}
                axisLine={{ stroke: chartColors.grid }}
                minTickGap={8}
              />
              <YAxis
                tick={axisTick}
                tickFormatter={valueFormatter ? (v) => valueFormatter(v) : undefined}
                tickLine={false}
                axisLine={false}
                width={56}
              />
            </>
          )}
          <Tooltip
            cursor={{ fill: chartColors.grid, fillOpacity: 0.25 }}
            content={renderTooltip(xFormatter, valueFormatter)}
          />
          {series.map((s) => (
            <Bar
              key={s.key}
              dataKey={(row) => toNumber((row as Record<string, unknown>)[s.key])}
              name={s.label}
              fill={s.color ?? chartColors.accent}
              radius={vertical ? [0, 4, 4, 0] : [4, 4, 0, 0]}
              isAnimationActive={false}
              maxBarSize={48}
            />
          ))}
        </RBarChart>
      </ResponsiveContainer>
    </div>
  );
}

/** Per-row colored bar chart — one series, color resolved per datum. */
export function CategoricalBarChart<T extends Datum>({
  data,
  xKey,
  valueKey,
  label,
  colorFor,
  xFormatter = identity,
  valueFormatter,
  height = 220,
  layout = 'horizontal',
  className,
}: BaseChartProps<T> & {
  valueKey: keyof T & string;
  label: string;
  /** Resolve a token color per row (e.g. status-driven). */
  colorFor?: (row: T) => string;
  layout?: 'horizontal' | 'vertical';
}) {
  const vertical = layout === 'vertical';
  return (
    <div className={cn('w-full', className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <RBarChart data={data} layout={layout} margin={vertical ? { ...MARGIN, left: 8 } : MARGIN}>
          <CartesianGrid stroke={chartColors.grid} vertical={vertical} horizontal={!vertical} />
          {vertical ? (
            <>
              <XAxis
                type="number"
                tick={axisTick}
                tickFormatter={valueFormatter ? (v) => valueFormatter(v) : undefined}
                tickLine={false}
                axisLine={false}
              />
              <YAxis
                type="category"
                dataKey={xKey}
                tick={axisTick}
                tickFormatter={(v) => xFormatter(v)}
                tickLine={false}
                axisLine={{ stroke: chartColors.grid }}
                width={120}
              />
            </>
          ) : (
            <>
              <XAxis
                dataKey={xKey}
                tick={axisTick}
                tickFormatter={(v) => xFormatter(v)}
                tickLine={false}
                axisLine={{ stroke: chartColors.grid }}
                minTickGap={8}
              />
              <YAxis
                tick={axisTick}
                tickFormatter={valueFormatter ? (v) => valueFormatter(v) : undefined}
                tickLine={false}
                axisLine={false}
                width={56}
              />
            </>
          )}
          <Tooltip
            cursor={{ fill: chartColors.grid, fillOpacity: 0.25 }}
            content={renderTooltip(xFormatter, valueFormatter)}
          />
          <Bar
            dataKey={(row) => toNumber((row as Record<string, unknown>)[valueKey])}
            name={label}
            radius={vertical ? [0, 4, 4, 0] : [4, 4, 0, 0]}
            isAnimationActive={false}
            maxBarSize={48}
          >
            {data.map((row, i) => (
              <Cell key={i} fill={colorFor ? colorFor(row) : chartColors.accent} />
            ))}
          </Bar>
        </RBarChart>
      </ResponsiveContainer>
    </div>
  );
}

/** Compact inline trend line for a Stat tile — no axes, no grid. */
export function Sparkline<T extends Datum>({
  data,
  valueKey,
  color = chartColors.accent,
  height = 36,
  className,
  fill = true,
}: {
  data: T[];
  valueKey: keyof T & string;
  color?: string;
  height?: number;
  className?: string;
  /** Render the faint area fill under the line. */
  fill?: boolean;
}) {
  const gradId = React.useId();
  if (data.length < 2) return null;
  return (
    <div className={cn('w-full', className)} style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <RAreaChart data={data} margin={{ top: 2, right: 0, bottom: 2, left: 0 }}>
          {fill ? (
            <defs>
              <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor={color} stopOpacity={0.25} />
                <stop offset="100%" stopColor={color} stopOpacity={0} />
              </linearGradient>
            </defs>
          ) : null}
          <Area
            type="monotone"
            dataKey={(row) => toNumber((row as Record<string, unknown>)[valueKey])}
            stroke={color}
            strokeWidth={1.75}
            fill={fill ? `url(#${gradId})` : 'none'}
            dot={false}
            isAnimationActive={false}
          />
        </RAreaChart>
      </ResponsiveContainer>
    </div>
  );
}
