'use client';

import * as React from 'react';
import type { UseQueryResult } from '@tanstack/react-query';

import { SdkError } from '@fuelgrid/sdk';
import { EmptyState, ErrorState, FilterBar, FilterField, PageHeader, Skeleton } from '@fuelgrid/ui';

import type { Station } from '@fuelgrid/sdk';

import { PERIODS, StationSelectBare } from './filters';

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

/** A station + period FilterBar for the envelope-driven report views. */
export function ReportFilterBar({
  items,
  stationId,
  onStation,
  period,
  onPeriod,
  showPeriod = true,
  actions,
}: {
  items: Station[];
  stationId: string;
  onStation: (id: string) => void;
  period?: string;
  onPeriod?: (p: string) => void;
  showPeriod?: boolean;
  actions?: React.ReactNode;
}) {
  return (
    <FilterBar actions={actions}>
      <FilterField label="Station">
        <StationSelectBare items={items} value={stationId} onChange={onStation} />
      </FilterField>
      {showPeriod && onPeriod ? (
        <FilterField label="Period">
          <select
            className={selectClasses}
            value={period}
            onChange={(e) => onPeriod(e.target.value)}
            aria-label="Reporting period"
          >
            {PERIODS.map((p) => (
              <option key={p.value} value={p.value}>
                {p.label}
              </option>
            ))}
          </select>
        </FilterField>
      ) : null}
    </FilterBar>
  );
}

/**
 * Renders the standard loading / empty / error / permission states around an
 * envelope query, delegating the success state to `children`. Keeps every
 * signature report view consistent.
 */
export function ReportStates<T>({
  stationsPending,
  noStations,
  query,
  loadingLabel = 'report',
  children,
}: {
  stationsPending: boolean;
  noStations: boolean;
  query: Pick<UseQueryResult<T>, 'isPending' | 'isError' | 'error' | 'refetch' | 'data'>;
  loadingLabel?: string;
  children: (data: T) => React.ReactNode;
}) {
  if (stationsPending || (query.isPending && !noStations)) {
    return (
      <div className="flex flex-col gap-4">
        <section className="grid grid-cols-1 gap-4 sm:grid-cols-2 xl:grid-cols-3">
          {Array.from({ length: 3 }).map((_, i) => (
            <Skeleton key={i} className="h-[120px] rounded-xl" />
          ))}
        </section>
        <Skeleton className="h-64 rounded-xl" />
      </div>
    );
  }
  if (noStations) {
    return (
      <EmptyState title="No stations yet" description="Create a station to view this report." />
    );
  }
  if (query.isError) {
    const forbidden = query.error instanceof SdkError && query.error.status === 403;
    return (
      <ErrorState
        title={forbidden ? 'No access to this station' : `Couldn't load the ${loadingLabel}`}
        description={
          forbidden
            ? "You don't have permission to view this station's report."
            : String((query.error as Error).message)
        }
        onRetry={forbidden ? undefined : () => query.refetch()}
      />
    );
  }
  if (!query.data) return null;
  return <>{children(query.data)}</>;
}

export { PageHeader };
