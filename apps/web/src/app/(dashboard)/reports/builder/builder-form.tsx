'use client';

import * as React from 'react';
import { Plus, Trash2 } from 'lucide-react';

import type {
  BuilderAggFunc,
  BuilderDataset,
  BuilderOperator,
  BuilderSpec,
  BuilderSpecFilter,
} from '@fuelgrid/sdk';
import { Badge, Button, Card, CardContent, CardHeader, CardTitle, Label } from '@fuelgrid/ui';

import {
  AGG_LABELS,
  OPERATOR_LABELS,
  VIZ_OPTIONS,
  operatorIsList,
  operatorIsRange,
} from './builder-shared';

/**
 * The guided spec builder (blueprint §6.2). EVERY option offered here is drawn
 * from the registry the backend returned for the chosen dataset — the user can
 * NEVER type a column / dimension / measure / filter identifier. Dimensions,
 * measures (with a per-measure agg allowlist), filters (with a per-filter
 * operator allowlist), sort keys and the visualization are all picked from the
 * dataset's whitelist. Filter VALUES are free text but are always bound as
 * parameters server-side, never concatenated. The backend re-validates the whole
 * spec against the allowlist before any query is composed, so this UI is a
 * convenience over the contract, not the contract itself.
 */

const selectClasses =
  'h-9 w-full rounded-md border border-border bg-background px-2.5 text-sm text-foreground ' +
  'focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring disabled:opacity-50';
const inputClasses = selectClasses;

export interface SpecState {
  dataset: string;
  dimensions: string[];
  measures: { measure: string; agg: BuilderAggFunc }[];
  filters: FilterDraft[];
  sort: { by: string; desc: boolean }[];
  limit: string;
  viz: NonNullable<BuilderSpec['viz']>;
}

/** A filter draft keeps its value(s) as editable strings until the spec is built. */
export interface FilterDraft {
  filter: string;
  operator: BuilderOperator;
  value: string;
  values: string[];
}

export function emptySpecState(datasetKey = ''): SpecState {
  return {
    dataset: datasetKey,
    dimensions: [],
    measures: [],
    filters: [],
    sort: [],
    limit: '100',
    viz: 'table',
  };
}

/** Hydrate the editor state from a saved spec (template edit / "open in builder"). */
export function specToState(spec: BuilderSpec): SpecState {
  return {
    dataset: spec.dataset,
    dimensions: [...spec.dimensions],
    measures: spec.measures.map((m) => ({ measure: m.measure, agg: m.agg })),
    filters: (spec.filters ?? []).map((f) => ({
      filter: f.filter,
      operator: f.operator,
      value: f.value != null ? String(f.value) : '',
      values: (f.values ?? []).map((v) => String(v)),
    })),
    sort: (spec.sort ?? []).map((s) => ({ by: s.by, desc: !!s.desc })),
    limit: spec.limit != null ? String(spec.limit) : '',
    viz: spec.viz ?? 'table',
  };
}

/**
 * Build the wire BuilderSpec from the editor state. Drops empty/blank filters,
 * coerces the limit, and maps each filter's value(s) onto the operator's arity
 * (`in` → values, `between` → [lo, hi], otherwise a scalar value). Identifiers
 * are passed through verbatim — they came from the registry options, and the
 * backend re-checks them.
 */
export function stateToSpec(state: SpecState): BuilderSpec {
  const filters: BuilderSpecFilter[] = [];
  for (const f of state.filters) {
    if (!f.filter) continue;
    if (operatorIsList(f.operator)) {
      const vals = f.values.map((v) => v.trim()).filter(Boolean);
      if (vals.length === 0) continue;
      filters.push({ filter: f.filter, operator: f.operator, values: vals });
    } else if (operatorIsRange(f.operator)) {
      const lo = (f.values[0] ?? '').trim();
      const hi = (f.values[1] ?? '').trim();
      if (!lo || !hi) continue;
      filters.push({ filter: f.filter, operator: f.operator, values: [lo, hi] });
    } else {
      const v = f.value.trim();
      if (!v) continue;
      filters.push({ filter: f.filter, operator: f.operator, value: v });
    }
  }
  const limit = Number(state.limit);
  const spec: BuilderSpec = {
    dataset: state.dataset,
    dimensions: [...state.dimensions],
    measures: state.measures
      .filter((m) => m.measure)
      .map((m) => ({ measure: m.measure, agg: m.agg })),
    viz: state.viz,
  };
  if (filters.length > 0) spec.filters = filters;
  if (state.sort.length > 0)
    spec.sort = state.sort.filter((s) => s.by).map((s) => ({ by: s.by, desc: s.desc }));
  if (Number.isFinite(limit) && limit > 0) spec.limit = limit;
  return spec;
}

/** A spec must reference exactly one dataset and at least one measure to run. */
export function specIsRunnable(state: SpecState): boolean {
  return !!state.dataset && state.measures.some((m) => m.measure);
}

export function BuilderForm({
  dataset,
  state,
  onChange,
  fieldError,
}: {
  dataset: BuilderDataset;
  state: SpecState;
  onChange: (next: SpecState) => void;
  /** A machine error code (e.g. unknown_dimension) to surface inline, if any. */
  fieldError?: string;
}) {
  const set = (patch: Partial<SpecState>) => onChange({ ...state, ...patch });

  // The sort menu offers any selected dimension + any selected measure id.
  const sortOptions = React.useMemo(() => {
    const opts: { id: string; label: string }[] = [];
    for (const d of state.dimensions) {
      const dim = dataset.dimensions.find((x) => x.id === d);
      if (dim) opts.push({ id: dim.id, label: dim.label });
    }
    for (const m of state.measures) {
      const meas = dataset.measures.find((x) => x.id === m.measure);
      if (meas) opts.push({ id: meas.id, label: `${AGG_LABELS[m.agg]} of ${meas.label}` });
    }
    return opts;
  }, [state.dimensions, state.measures, dataset]);

  return (
    <div className="flex flex-col gap-5">
      {/* 2) DIMENSIONS (group by) — multi-pick from the dataset whitelist only. */}
      <section className="flex flex-col gap-2">
        <div className="flex items-center justify-between">
          <Label>Group by (dimensions)</Label>
          <span className="text-xs text-muted-foreground">{state.dimensions.length} selected</span>
        </div>
        <p className="text-xs text-muted-foreground">
          Choose how to group the rows. Only this dataset&apos;s registered dimensions are offered.
        </p>
        <div className="flex flex-wrap gap-2" role="group" aria-label="Dimensions">
          {dataset.dimensions.length === 0 ? (
            <p className="text-sm text-muted-foreground">
              This dataset has no group-by dimensions.
            </p>
          ) : (
            dataset.dimensions.map((d) => {
              const on = state.dimensions.includes(d.id);
              return (
                <button
                  key={d.id}
                  type="button"
                  aria-pressed={on}
                  onClick={() =>
                    set({
                      dimensions: on
                        ? state.dimensions.filter((x) => x !== d.id)
                        : [...state.dimensions, d.id],
                    })
                  }
                  className={
                    'rounded-full border px-3 py-1 text-sm transition ' +
                    (on
                      ? 'border-accent bg-accent/10 text-accent'
                      : 'border-border bg-card text-foreground hover:bg-accent-muted/40')
                  }
                >
                  {d.label}
                </button>
              );
            })
          )}
        </div>
      </section>

      {/* 2) MEASURES (with agg) — each picked from the whitelist + per-measure agg. */}
      <MeasuresSection dataset={dataset} state={state} set={set} />

      {/* 3) FILTERS — column from the whitelist + an allowed operator + a value. */}
      <FiltersSection dataset={dataset} state={state} set={set} />

      {/* 4) SORT + LIMIT. */}
      <section className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="flex flex-col gap-2">
          <Label htmlFor="b-sort">Sort by</Label>
          <div className="flex gap-2">
            <select
              id="b-sort"
              className={selectClasses}
              value={state.sort[0]?.by ?? ''}
              onChange={(e) => {
                const by = e.target.value;
                set({ sort: by ? [{ by, desc: state.sort[0]?.desc ?? true }] : [] });
              }}
              disabled={sortOptions.length === 0}
            >
              <option value="">No sort</option>
              {sortOptions.map((o) => (
                <option key={o.id} value={o.id}>
                  {o.label}
                </option>
              ))}
            </select>
            <select
              className={`${selectClasses} w-32`}
              value={state.sort[0]?.desc ? 'desc' : 'asc'}
              onChange={(e) =>
                set({
                  sort: state.sort[0]
                    ? [{ by: state.sort[0].by, desc: e.target.value === 'desc' }]
                    : [],
                })
              }
              disabled={!state.sort[0]}
              aria-label="Sort direction"
            >
              <option value="desc">High → low</option>
              <option value="asc">Low → high</option>
            </select>
          </div>
        </div>
        <div className="flex flex-col gap-2">
          <Label htmlFor="b-limit">Row limit</Label>
          <input
            id="b-limit"
            type="number"
            min={1}
            max={5000}
            className={inputClasses}
            value={state.limit}
            onChange={(e) => set({ limit: e.target.value })}
          />
        </div>
      </section>

      {/* 5) VISUALIZATION. */}
      <section className="flex flex-col gap-2">
        <Label htmlFor="b-viz">Visualization</Label>
        <select
          id="b-viz"
          className={selectClasses}
          value={state.viz}
          onChange={(e) => set({ viz: e.target.value as SpecState['viz'] })}
        >
          {VIZ_OPTIONS.map((v) => (
            <option key={v.value} value={v.value}>
              {v.label}
            </option>
          ))}
        </select>
        <p className="text-xs text-muted-foreground">
          The table is always available; a chart needs at least one dimension and one measure.
        </p>
      </section>

      {fieldError ? (
        <p className="rounded-md border border-danger/30 bg-danger/5 px-3 py-2 text-sm text-danger">
          The report couldn&apos;t be composed ({fieldError.replace(/_/g, ' ')}). Adjust your
          dimensions, measures or filters and preview again.
        </p>
      ) : null}
    </div>
  );
}

function MeasuresSection({
  dataset,
  state,
  set,
}: {
  dataset: BuilderDataset;
  state: SpecState;
  set: (patch: Partial<SpecState>) => void;
}) {
  // Measures not yet chosen, so the "add" picker never offers a duplicate.
  const remaining = dataset.measures.filter((m) => !state.measures.some((s) => s.measure === m.id));

  function addMeasure(id: string) {
    const meas = dataset.measures.find((m) => m.id === id);
    if (!meas) return;
    const agg = meas.allowed_aggs[0] ?? 'sum';
    set({ measures: [...state.measures, { measure: id, agg }] });
  }

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <Label>Measures</Label>
        <span className="text-xs text-muted-foreground">{state.measures.length} selected</span>
      </div>
      <p className="text-xs text-muted-foreground">
        What to aggregate. Each measure&apos;s aggregate is constrained to the functions the dataset
        allows for it.
      </p>

      {state.measures.length === 0 ? (
        <p className="rounded-md border border-dashed border-border/70 px-3 py-3 text-sm text-muted-foreground">
          Add at least one measure to run the report.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {state.measures.map((sm, i) => {
            const meas = dataset.measures.find((m) => m.id === sm.measure);
            if (!meas) return null;
            return (
              <li
                key={sm.measure}
                className="flex flex-wrap items-center gap-2 rounded-md border border-border/70 px-3 py-2"
              >
                <span className="flex items-center gap-2 text-sm font-medium">
                  {meas.label}
                  {meas.unit ? <Badge tone="neutral">{meas.unit}</Badge> : null}
                </span>
                <select
                  className={`${selectClasses} ml-auto w-40`}
                  value={sm.agg}
                  aria-label={`Aggregate for ${meas.label}`}
                  onChange={(e) => {
                    const next = [...state.measures];
                    next[i] = { ...sm, agg: e.target.value as BuilderAggFunc };
                    set({ measures: next });
                  }}
                >
                  {meas.allowed_aggs.map((a) => (
                    <option key={a} value={a}>
                      {AGG_LABELS[a]}
                    </option>
                  ))}
                </select>
                <Button
                  size="sm"
                  variant="ghost"
                  aria-label={`Remove ${meas.label}`}
                  onClick={() => set({ measures: state.measures.filter((_, j) => j !== i) })}
                >
                  <Trash2 className="size-4" />
                </Button>
              </li>
            );
          })}
        </ul>
      )}

      {remaining.length > 0 ? (
        <select
          className={selectClasses}
          value=""
          aria-label="Add a measure"
          onChange={(e) => {
            if (e.target.value) addMeasure(e.target.value);
          }}
        >
          <option value="">+ Add a measure…</option>
          {remaining.map((m) => (
            <option key={m.id} value={m.id}>
              {m.label}
              {m.unit ? ` (${m.unit})` : ''}
            </option>
          ))}
        </select>
      ) : null}
    </section>
  );
}

function FiltersSection({
  dataset,
  state,
  set,
}: {
  dataset: BuilderDataset;
  state: SpecState;
  set: (patch: Partial<SpecState>) => void;
}) {
  function addFilter() {
    const first = dataset.filters[0];
    if (!first) return;
    set({
      filters: [
        ...state.filters,
        { filter: first.id, operator: first.operators[0] ?? 'eq', value: '', values: [] },
      ],
    });
  }

  function patchFilter(i: number, patch: Partial<FilterDraft>) {
    const next = [...state.filters];
    next[i] = { ...next[i], ...patch } as FilterDraft;
    set({ filters: next });
  }

  return (
    <section className="flex flex-col gap-2">
      <div className="flex items-center justify-between">
        <Label>Filters</Label>
        {dataset.filters.length > 0 ? (
          <Button size="sm" variant="outline" onClick={addFilter}>
            <Plus className="size-3.5" />
            Add filter
          </Button>
        ) : null}
      </div>
      <p className="text-xs text-muted-foreground">
        Narrow the rows. The column and operator come from the dataset whitelist; the value is
        always passed as a bound parameter, never inlined into SQL.
      </p>

      {dataset.filters.length === 0 ? (
        <p className="text-sm text-muted-foreground">This dataset exposes no filters.</p>
      ) : state.filters.length === 0 ? (
        <p className="rounded-md border border-dashed border-border/70 px-3 py-3 text-sm text-muted-foreground">
          No filters — the report covers every row you can see.
        </p>
      ) : (
        <ul className="flex flex-col gap-2">
          {state.filters.map((f, i) => {
            const def = dataset.filters.find((x) => x.id === f.filter);
            const operators = def?.operators ?? [];
            return (
              <li
                key={i}
                className="flex flex-wrap items-center gap-2 rounded-md border border-border/70 px-3 py-2"
              >
                <select
                  className={`${selectClasses} w-44`}
                  value={f.filter}
                  aria-label="Filter column"
                  onChange={(e) => {
                    const nd = dataset.filters.find((x) => x.id === e.target.value);
                    patchFilter(i, {
                      filter: e.target.value,
                      operator: nd?.operators[0] ?? 'eq',
                      value: '',
                      values: [],
                    });
                  }}
                >
                  {dataset.filters.map((x) => (
                    <option key={x.id} value={x.id}>
                      {x.label}
                    </option>
                  ))}
                </select>
                <select
                  className={`${selectClasses} w-36`}
                  value={f.operator}
                  aria-label="Filter operator"
                  onChange={(e) =>
                    patchFilter(i, {
                      operator: e.target.value as BuilderOperator,
                      value: '',
                      values: [],
                    })
                  }
                >
                  {operators.map((op) => (
                    <option key={op} value={op}>
                      {OPERATOR_LABELS[op]}
                    </option>
                  ))}
                </select>

                <FilterValueInputs
                  draft={f}
                  type={def?.type}
                  onScalar={(value) => patchFilter(i, { value })}
                  onList={(values) => patchFilter(i, { values })}
                />

                <Button
                  size="sm"
                  variant="ghost"
                  aria-label="Remove filter"
                  className="ml-auto"
                  onClick={() => set({ filters: state.filters.filter((_, j) => j !== i) })}
                >
                  <Trash2 className="size-4" />
                </Button>
              </li>
            );
          })}
        </ul>
      )}
    </section>
  );
}

function FilterValueInputs({
  draft,
  type,
  onScalar,
  onList,
}: {
  draft: FilterDraft;
  type?: string;
  onScalar: (v: string) => void;
  onList: (v: string[]) => void;
}) {
  const inputType =
    type === 'date' ? 'date' : type === 'numeric' || type === 'int' ? 'number' : 'text';

  if (operatorIsRange(draft.operator)) {
    return (
      <div className="flex items-center gap-1.5">
        <input
          type={inputType}
          className={`${inputClasses} w-32`}
          placeholder="from"
          aria-label="Range from"
          value={draft.values[0] ?? ''}
          onChange={(e) => onList([e.target.value, draft.values[1] ?? ''])}
        />
        <span className="text-xs text-muted-foreground">and</span>
        <input
          type={inputType}
          className={`${inputClasses} w-32`}
          placeholder="to"
          aria-label="Range to"
          value={draft.values[1] ?? ''}
          onChange={(e) => onList([draft.values[0] ?? '', e.target.value])}
        />
      </div>
    );
  }
  if (operatorIsList(draft.operator)) {
    return (
      <input
        type="text"
        className={`${inputClasses} w-56`}
        placeholder="value, value, …"
        aria-label="Filter values (comma separated)"
        value={draft.values.join(', ')}
        onChange={(e) => onList(e.target.value.split(',').map((v) => v.trim()))}
      />
    );
  }
  return (
    <input
      type={inputType}
      className={`${inputClasses} w-44`}
      placeholder="value"
      aria-label="Filter value"
      value={draft.value}
      onChange={(e) => onScalar(e.target.value)}
    />
  );
}

/** A read-only chip summary of the spec for the preview/save header. */
export function SpecSummary({ dataset, state }: { dataset: BuilderDataset; state: SpecState }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-sm">This report</CardTitle>
      </CardHeader>
      <CardContent className="flex flex-wrap gap-1.5">
        <Badge tone="accent">{dataset.name}</Badge>
        {state.measures.map((m) => {
          const meas = dataset.measures.find((x) => x.id === m.measure);
          return meas ? (
            <Badge key={m.measure} tone="neutral">
              {AGG_LABELS[m.agg]} · {meas.label}
            </Badge>
          ) : null;
        })}
        {state.dimensions.map((d) => {
          const dim = dataset.dimensions.find((x) => x.id === d);
          return dim ? (
            <Badge key={d} tone="info">
              by {dim.label}
            </Badge>
          ) : null;
        })}
      </CardContent>
    </Card>
  );
}
