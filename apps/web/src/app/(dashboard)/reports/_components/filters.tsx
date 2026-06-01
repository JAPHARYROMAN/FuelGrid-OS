'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';

import type { ReportPeriod, Station } from '@fuelgrid/sdk';
import { Badge } from '@fuelgrid/ui';

import { api } from '@/lib/api';

export const PERIODS: { value: ReportPeriod; label: string }[] = [
  { value: 'this-month', label: 'This month' },
  { value: 'last-month', label: 'Last month' },
  { value: 'ytd', label: 'Year to date' },
  { value: 'last-30', label: 'Last 30 days' },
];

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

/** Loads stations and tracks the selected one, defaulting to the first. */
export function useStationSelection() {
  const stations = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const items = React.useMemo<Station[]>(() => stations.data?.items ?? [], [stations.data]);
  const [stationId, setStationId] = React.useState('');
  React.useEffect(() => {
    const first = items[0];
    if (!stationId && first) setStationId(first.id);
  }, [stationId, items]);
  const current = items.find((s) => s.id === stationId);
  return { stations, items, stationId, setStationId, current };
}

export function StationSelect({
  items,
  value,
  onChange,
}: {
  items: Station[];
  value: string;
  onChange: (id: string) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Station</span>
      <select
        className={selectClasses}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Station"
        disabled={items.length === 0}
      >
        {items.length === 0 ? <option value="">No stations</option> : null}
        {items.map((s) => (
          <option key={s.id} value={s.id}>
            {s.code} — {s.name}
          </option>
        ))}
      </select>
    </label>
  );
}

export function PeriodSelect({
  value,
  onChange,
}: {
  value: ReportPeriod;
  onChange: (p: ReportPeriod) => void;
}) {
  return (
    <label className="flex items-center gap-2 text-sm">
      <span className="text-muted-foreground">Period</span>
      <select
        className={selectClasses}
        value={value}
        onChange={(e) => onChange(e.target.value as ReportPeriod)}
        aria-label="Reporting period"
      >
        {PERIODS.map((p) => (
          <option key={p.value} value={p.value}>
            {p.label}
          </option>
        ))}
      </select>
    </label>
  );
}

/** A "Final" badge shown when the underlying period/day is locked/sealed. */
export function FinalBadge({ locked }: { locked: boolean }) {
  return locked ? <Badge tone="success">Final</Badge> : <Badge tone="warning">Provisional</Badge>;
}
