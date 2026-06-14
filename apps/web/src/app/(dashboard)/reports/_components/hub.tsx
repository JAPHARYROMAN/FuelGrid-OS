'use client';

import * as React from 'react';
import {
  Banknote,
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
import { formatMoney, formatLitres } from '@fuelgrid/ui';

/**
 * Hub helpers for the Reports & Intelligence Center home: mapping the catalog's
 * server-chosen icon string onto a lucide glyph, resolving whether a category
 * card is actually navigable, and formatting its live key metric honestly.
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

/**
 * The set of catalog target routes that have a real page in this app today.
 * The catalog can mark a category `partial` while its dedicated page is not yet
 * built (pump, fleet, audit); in that case the card must NOT produce a dead
 * link — it links only when both the category is reachable AND the page exists.
 * Kept in sync with apps/web/src/app/(dashboard)/reports/*.
 */
const ROUTES_WITH_PAGE = new Set<string>([
  '/reports/executive',
  '/reports/sales',
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
