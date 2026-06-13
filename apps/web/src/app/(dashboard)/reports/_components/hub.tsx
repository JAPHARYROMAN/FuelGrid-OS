'use client';

import * as React from 'react';
import {
  Banknote,
  Calendar,
  CalendarClock,
  Car,
  Clock,
  CreditCard,
  Database,
  Download,
  FileSearch,
  FileSpreadsheet,
  Gauge,
  LayoutDashboard,
  Layers,
  ShieldAlert,
  ShoppingCart,
  Sliders,
  TrendingUp,
  Truck,
} from 'lucide-react';

import type { ReportCatalogCategory } from '@fuelgrid/sdk';
import { formatMoney, formatLitres, type ReportDateRange } from '@fuelgrid/ui';

/**
 * Hub helpers for the Reports & Intelligence Center home: mapping the catalog's
 * server-chosen icon string onto a lucide glyph, resolving whether a category
 * card is actually navigable, formatting its live key metric honestly, and
 * carrying the hub's global context (station/region/date range) into a report
 * as querystring defaults.
 */

/** Map the backend icon key (catalog `icon`) onto a lucide component. */
const ICON_MAP: Record<string, React.ComponentType<{ className?: string }>> = {
  'layout-dashboard': LayoutDashboard,
  'trending-up': TrendingUp,
  layers: Layers,
  database: Database,
  gauge: Gauge,
  clock: Clock,
  truck: Truck,
  'shopping-cart': ShoppingCart,
  banknote: Banknote,
  'credit-card': CreditCard,
  car: Car,
  'shield-alert': ShieldAlert,
  'file-search': FileSearch,
  sliders: Sliders,
  'calendar-clock': CalendarClock,
  download: Download,
};

export function categoryIcon(key: string): React.ReactNode {
  const Icon = ICON_MAP[key] ?? FileSpreadsheet;
  return <Icon />;
}

export const RANGE_ICON = Calendar;

/**
 * The set of catalog target routes that have a real page in this app today.
 * The catalog can mark a category `partial` while its dedicated page is not yet
 * built (pump, fleet, audit); in that case the card must NOT produce a dead
 * link — it links only when both the category is reachable AND the page exists.
 * Kept in sync with apps/web/src/app/(dashboard)/reports/*.
 */
const ROUTES_WITH_PAGE = new Set<string>([
  '/reports/executive',
  '/reports/sales-summary',
  '/reports/inventory/reconciliation',
  '/reports/station-close',
  '/reports/cash-reconciliation',
  '/reports/credit-cashflow',
  '/reports/profitability',
  '/reports/customer-aging',
  '/reports/fuel-loss',
  '/reports/scheduled',
  '/reports/exports',
  '/reports/attendance',
  '/reports/corrections-variances',
  '/reports/station-comparison',
]);

/**
 * Resolve the href for a category card. A `placeholder` category is never
 * linked (it is coming-soon, not faked). A `live`/`partial` category links only
 * when its target route is backed by a real page — otherwise it stays a plain,
 * non-navigable card so the hub never dangles a dead link.
 */
export function categoryHref(c: ReportCatalogCategory): string | undefined {
  if (c.availability === 'placeholder') return undefined;
  return ROUTES_WITH_PAGE.has(c.target_route) ? c.target_route : undefined;
}

/**
 * Format a catalog metric value for display. Money/litre values arrive as exact
 * decimal STRINGS — formatted with the decimal-safe helpers, never Number().
 * Counts and other strings render verbatim. Returns null when there is no
 * genuine value (the caller then shows `metric.reason`).
 */
export function formatMetricValue(value: string | null, unit?: string): React.ReactNode {
  if (value == null) return null;
  if (unit === 'TZS') return formatMoney(value);
  if (unit === 'L') return formatLitres(value);
  return value;
}

/**
 * The hub's global selection, carried as querystring defaults into a report so
 * the report opens pre-scoped to what the user was looking at on the hub. Each
 * report page reads only the params it understands (station/from/to), matching
 * the existing station-scoped report contract; unknown params are ignored.
 */
export interface HubContext {
  stationId: string;
  regionId: string;
  range: ReportDateRange;
}

/**
 * Resolve a relative preset into an inclusive ISO from/to window so the hub can
 * always pass a concrete range downstream. Mirrors the backend's relative
 * windows; deterministic (no AI) and parse-safe.
 */
export function resolveRange(range: ReportDateRange): { from: string; to: string } {
  if (range.preset === 'custom') return { from: range.from, to: range.to };
  const now = new Date();
  const iso = (d: Date) => d.toISOString().slice(0, 10);
  const startOfMonth = (d: Date) => new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth(), 1));
  const endOfMonth = (d: Date) => new Date(Date.UTC(d.getUTCFullYear(), d.getUTCMonth() + 1, 0));
  switch (range.preset) {
    case 'this-month':
      return { from: iso(startOfMonth(now)), to: iso(now) };
    case 'last-month': {
      const lm = new Date(Date.UTC(now.getUTCFullYear(), now.getUTCMonth() - 1, 1));
      return { from: iso(startOfMonth(lm)), to: iso(endOfMonth(lm)) };
    }
    case 'ytd':
      return { from: `${now.getUTCFullYear()}-01-01`, to: iso(now) };
    case 'last-30': {
      const start = new Date(now);
      start.setUTCDate(start.getUTCDate() - 30);
      return { from: iso(start), to: iso(now) };
    }
    default:
      return { from: '', to: '' };
  }
}

/**
 * Append the hub's current context to a report href as querystring defaults.
 * Only emits params that carry a value, so a report with no station/date scope
 * opens clean. Reports that take `?station_id`/`?from`/`?to` will pick these up.
 */
export function withHubContext(href: string, ctx: HubContext): string {
  const params = new URLSearchParams();
  if (ctx.stationId) params.set('station_id', ctx.stationId);
  const { from, to } = resolveRange(ctx.range);
  if (from) params.set('from', from);
  if (to) params.set('to', to);
  const qs = params.toString();
  return qs ? `${href}?${qs}` : href;
}
