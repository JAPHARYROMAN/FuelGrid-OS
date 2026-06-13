'use client';

import * as React from 'react';
import { useQuery } from '@tanstack/react-query';

import type { Region, Station } from '@fuelgrid/sdk';
import { FilterField } from '@fuelgrid/ui';

import { api } from '@/lib/api';

/**
 * Station / region selector for the Reports Home top bar (blueprint §4.2).
 * Reuses the same accessible-stations source as the existing report filter
 * (api.listStations, already RLS- and permission-scoped server-side) and adds
 * REGION GROUPING: an optional region <select> narrows the station list to one
 * region, and the station <select> groups its options by region via <optgroup>.
 *
 * The selection is fully controlled by the hub so it can carry into a report as
 * default context. "All regions" + a real first station is the default.
 */

const selectClasses =
  'h-9 rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';

export interface StationRegionValue {
  stationId: string;
  regionId: string;
}

/** Loads accessible stations + regions and tracks the hub's station/region. */
export function useStationRegionSelection() {
  const stationsQ = useQuery({
    queryKey: ['stations'],
    queryFn: ({ signal }) => api.listStations({}, signal),
  });
  const regionsQ = useQuery({
    queryKey: ['regions'],
    queryFn: ({ signal }) => api.listRegions({}, signal),
  });

  const stations = React.useMemo<Station[]>(() => stationsQ.data?.items ?? [], [stationsQ.data]);
  const regions = React.useMemo<Region[]>(() => regionsQ.data?.items ?? [], [regionsQ.data]);

  const [regionId, setRegionId] = React.useState('');
  const [stationId, setStationId] = React.useState('');

  // Default to the first accessible station once stations load.
  React.useEffect(() => {
    const first = stations[0];
    if (!stationId && first) setStationId(first.id);
  }, [stationId, stations]);

  // Stations visible for the current region filter ('' = all regions).
  const visibleStations = React.useMemo(
    () => (regionId ? stations.filter((s) => s.region_id === regionId) : stations),
    [stations, regionId],
  );

  // Only surface regions that actually have accessible stations.
  const regionsWithStations = React.useMemo(() => {
    const used = new Set(stations.map((s) => s.region_id).filter(Boolean));
    return regions.filter((r) => used.has(r.id));
  }, [regions, stations]);

  // When the region filter excludes the current station, snap to the first
  // station in the new region so the downstream report stays in scope.
  const selectRegion = React.useCallback(
    (next: string) => {
      setRegionId(next);
      const inScope = next ? stations.filter((s) => s.region_id === next) : stations;
      if (!inScope.some((s) => s.id === stationId)) {
        setStationId(inScope[0]?.id ?? '');
      }
    },
    [stations, stationId],
  );

  return {
    stationsQ,
    regionsQ,
    stations,
    regions: regionsWithStations,
    visibleStations,
    stationId,
    setStationId,
    regionId,
    setRegionId: selectRegion,
  };
}

/** Region <select> field — only rendered when there is >1 region in scope. */
export function RegionSelectField({
  regions,
  value,
  onChange,
}: {
  regions: Region[];
  value: string;
  onChange: (id: string) => void;
}) {
  if (regions.length < 2) return null;
  return (
    <FilterField label="Region">
      <select
        className={selectClasses}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Region"
      >
        <option value="">All regions</option>
        {regions.map((r) => (
          <option key={r.id} value={r.id}>
            {r.code ? `${r.code} — ${r.name}` : r.name}
          </option>
        ))}
      </select>
    </FilterField>
  );
}

/**
 * Station <select> field. Groups options by region via <optgroup> when more
 * than one region is in scope (blueprint §4.2 region grouping); otherwise a
 * flat list. Stations without a region collapse under "Unassigned".
 */
export function StationSelectField({
  stations,
  regions,
  value,
  onChange,
}: {
  stations: Station[];
  regions: Region[];
  value: string;
  onChange: (id: string) => void;
}) {
  const grouped = regions.length > 1;
  const byRegion = React.useMemo(() => {
    const map = new Map<string, Station[]>();
    for (const s of stations) {
      const key = s.region_id ?? '';
      const arr = map.get(key) ?? [];
      arr.push(s);
      map.set(key, arr);
    }
    return map;
  }, [stations]);
  const regionName = (id: string) => regions.find((r) => r.id === id)?.name ?? 'Unassigned';

  return (
    <FilterField label="Station">
      <select
        className={selectClasses}
        value={value}
        onChange={(e) => onChange(e.target.value)}
        aria-label="Station"
        disabled={stations.length === 0}
      >
        {stations.length === 0 ? <option value="">No stations</option> : null}
        {grouped
          ? [...byRegion.entries()].map(([rid, group]) => (
              <optgroup key={rid || 'unassigned'} label={regionName(rid)}>
                {group.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.code} — {s.name}
                  </option>
                ))}
              </optgroup>
            ))
          : stations.map((s) => (
              <option key={s.id} value={s.id}>
                {s.code} — {s.name}
              </option>
            ))}
      </select>
    </FilterField>
  );
}
