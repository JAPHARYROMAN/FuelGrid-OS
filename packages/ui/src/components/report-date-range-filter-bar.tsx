import * as React from 'react';
import { CalendarRange } from 'lucide-react';

import { cn } from '../lib/cn';
import { FilterBar, FilterField } from './filter-bar';

/**
 * ReportDateRangeFilterBar — the shared global date-range control for the
 * Reports & Intelligence Center (blueprint §4.2 top bar). The existing report
 * filter offered station + four fixed period presets only; the plan flagged a
 * FREE range picker as net-new. This component keeps the presets AND adds a
 * custom `from`/`to` range, in one framework-agnostic, fully-controlled bar.
 *
 * It is intentionally headless of any router/store: the caller owns the value
 * and the change handlers, so the same bar drives the hub (carried as default
 * context into a report) and any report view. Selecting a preset clears the
 * custom range; typing a custom date switches the selection to `custom`.
 *
 * Children render to the LEFT of the date controls so a caller can drop a
 * station/region selector into the same bar; `actions` pin to the right.
 */

/** The four canonical relative presets, plus a free custom range. */
export type ReportRangePreset = 'this-month' | 'last-month' | 'ytd' | 'last-30' | 'custom';

/** The resolved range selection the bar emits. */
export interface ReportDateRange {
  /** The active preset, or `custom` when a free from/to is in effect. */
  preset: ReportRangePreset;
  /** Inclusive ISO `YYYY-MM-DD` start, or '' when a relative preset is active. */
  from: string;
  /** Inclusive ISO `YYYY-MM-DD` end, or '' when a relative preset is active. */
  to: string;
}

export const REPORT_RANGE_PRESETS: {
  value: Exclude<ReportRangePreset, 'custom'>;
  label: string;
}[] = [
  { value: 'this-month', label: 'This month' },
  { value: 'last-month', label: 'Last month' },
  { value: 'ytd', label: 'Year to date' },
  { value: 'last-30', label: 'Last 30 days' },
];

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

export interface ReportDateRangeFilterBarProps {
  /** The current selection (fully controlled). */
  value: ReportDateRange;
  /** Emits the next selection whenever the preset or a custom date changes. */
  onChange: (next: ReportDateRange) => void;
  /** Controls placed to the left of the date range (e.g. station/region). */
  children?: React.ReactNode;
  /** Right-aligned actions (reset, generate, …). */
  actions?: React.ReactNode;
  className?: string;
}

export function ReportDateRangeFilterBar({
  value,
  onChange,
  children,
  actions,
  className,
}: ReportDateRangeFilterBarProps) {
  // The <select> tracks the preset; choosing "custom" reveals the from/to
  // inputs (and keeps any range already typed). A relative preset clears the
  // custom range so the resolved window is unambiguous downstream.
  const handlePreset = (next: string) => {
    if (next === 'custom') {
      onChange({ preset: 'custom', from: value.from, to: value.to });
      return;
    }
    onChange({ preset: next as ReportRangePreset, from: '', to: '' });
  };

  const handleFrom = (from: string) => onChange({ preset: 'custom', from, to: value.to });
  const handleTo = (to: string) => onChange({ preset: 'custom', from: value.from, to });

  const isCustom = value.preset === 'custom';

  return (
    <FilterBar actions={actions} className={className}>
      {children}
      <FilterField label="Date range">
        <select
          className={cn(selectClasses, 'min-w-[9rem]')}
          value={value.preset}
          onChange={(e) => handlePreset(e.target.value)}
          aria-label="Date range"
        >
          {REPORT_RANGE_PRESETS.map((p) => (
            <option key={p.value} value={p.value}>
              {p.label}
            </option>
          ))}
          <option value="custom">Custom range…</option>
        </select>
      </FilterField>
      {isCustom ? (
        <>
          <FilterField label="From">
            <input
              type="date"
              className={selectClasses}
              value={value.from}
              max={value.to || undefined}
              onChange={(e) => handleFrom(e.target.value)}
              aria-label="From date"
            />
          </FilterField>
          <FilterField label="To">
            <input
              type="date"
              className={selectClasses}
              value={value.to}
              min={value.from || undefined}
              onChange={(e) => handleTo(e.target.value)}
              aria-label="To date"
            />
          </FilterField>
        </>
      ) : (
        <span
          className="hidden items-center gap-1.5 self-end pb-1.5 text-xs text-muted-foreground sm:inline-flex"
          aria-hidden="true"
        >
          <CalendarRange className="size-3.5" />
          Relative window
        </span>
      )}
    </FilterBar>
  );
}
