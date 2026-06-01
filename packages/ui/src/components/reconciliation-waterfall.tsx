import { cn } from '../lib/cn';
import { formatLitres, parseDecimal } from '../lib/money';
import { Badge } from './badge';

/**
 * ReconciliationWaterfall — the signature reconciliation visual.
 *
 *   Opening Stock  (+ Deliveries)  (− Sales)  (± Adjustments)  = Expected Closing
 *                                                vs  Actual Closing  = Variance
 *
 * Inputs are exact decimal STRINGS (the numeric->text contract); they are
 * parsed once here for the bar geometry only and never fed back into business
 * logic. A `valueFormatter` controls display (defaults to litre formatting).
 * When |variance| exceeds `tolerance` the variance is rendered in an
 * over-tolerance (danger) state.
 */
type Decimal = string | number | null | undefined;

export interface ReconciliationWaterfallProps {
  openingStock: Decimal;
  deliveries?: Decimal;
  sales?: Decimal;
  adjustments?: Decimal;
  expectedClosing: Decimal;
  actualClosing: Decimal;
  /**
   * Signed variance (actual − expected). When omitted it is derived from
   * actualClosing − expectedClosing. Provide the server's value when available.
   */
  variance?: Decimal;
  /** Absolute tolerance band; |variance| above this flips to the over state. */
  tolerance?: Decimal;
  /** Display formatter for every figure. Defaults to whole-litre formatting. */
  valueFormatter?: (v: Decimal) => string;
  /** Unit suffix shown after the variance figure, e.g. "L". */
  unit?: string;
  className?: string;
}

interface Step {
  label: string;
  /** Signed magnitude used for the bar (additive/subtractive). */
  delta: number;
  display: string;
  sign: '+' | '−' | '±' | '';
  kind: 'base' | 'add' | 'sub' | 'adjust' | 'result';
}

const clamp01 = (n: number) => Math.max(0, Math.min(1, n));

export function ReconciliationWaterfall({
  openingStock,
  deliveries,
  sales,
  adjustments,
  expectedClosing,
  actualClosing,
  variance,
  tolerance,
  valueFormatter,
  unit = 'L',
  className,
}: ReconciliationWaterfallProps) {
  const fmt = valueFormatter ?? ((v: Decimal) => formatLitres(v));

  const opening = parseDecimal(openingStock) || 0;
  const deliv = parseDecimal(deliveries) || 0;
  const sale = parseDecimal(sales) || 0;
  const adj = parseDecimal(adjustments) || 0;
  const expected = parseDecimal(expectedClosing) || 0;
  const actual = parseDecimal(actualClosing) || 0;

  const varianceNum =
    variance != null && variance !== '' ? parseDecimal(variance) : actual - expected;
  const tol = tolerance != null && tolerance !== '' ? Math.abs(parseDecimal(tolerance)) : 0;
  const overTolerance = Number.isFinite(varianceNum) && Math.abs(varianceNum) > tol;

  // Steps that build to the expected closing. Deliveries/sales/adjustments are
  // only rendered when supplied (a non-empty input), so a station with no
  // movements still reads cleanly.
  const steps: Step[] = [
    {
      label: 'Opening stock',
      delta: opening,
      display: fmt(openingStock),
      sign: '',
      kind: 'base',
    },
  ];
  if (deliveries != null && deliveries !== '')
    steps.push({
      label: 'Deliveries',
      delta: deliv,
      display: fmt(deliveries),
      sign: '+',
      kind: 'add',
    });
  if (sales != null && sales !== '')
    steps.push({ label: 'Sales', delta: -sale, display: fmt(sales), sign: '−', kind: 'sub' });
  if (adjustments != null && adjustments !== '')
    steps.push({
      label: 'Adjustments',
      delta: adj,
      display: fmt(adjustments),
      sign: '±',
      kind: 'adjust',
    });
  steps.push({
    label: 'Expected closing',
    delta: expected,
    display: fmt(expectedClosing),
    sign: '',
    kind: 'result',
  });

  // Bar scale: the largest absolute figure across the flow drives the width.
  const scale = Math.max(
    Math.abs(opening),
    Math.abs(deliv),
    Math.abs(sale),
    Math.abs(adj),
    Math.abs(expected),
    Math.abs(actual),
    1,
  );

  const barTone: Record<Step['kind'], string> = {
    base: 'bg-muted-foreground/40',
    add: 'bg-success/60',
    sub: 'bg-warning/60',
    adjust: 'bg-info/60',
    result: 'bg-accent/60',
  };

  return (
    <div
      className={cn(
        'flex flex-col gap-4 rounded-xl border border-border/80 bg-card p-5',
        className,
      )}
      role="group"
      aria-label="Stock reconciliation"
    >
      <ol className="flex flex-col gap-2.5">
        {steps.map((s) => {
          const frac = clamp01(Math.abs(s.delta) / scale);
          return (
            <li key={s.label} className="flex items-center gap-3">
              <span className="w-32 shrink-0 text-sm text-muted-foreground">
                {s.sign ? (
                  <span className="mr-1 font-mono text-muted-foreground">{s.sign}</span>
                ) : null}
                {s.label}
              </span>
              <div className="relative h-5 flex-1 overflow-hidden rounded bg-muted/40">
                <div
                  className={cn(
                    'absolute inset-y-0 left-0 rounded',
                    barTone[s.kind],
                    s.kind === 'result' && 'ring-1 ring-inset ring-accent/40',
                  )}
                  style={{ width: `${Math.max(frac * 100, 2)}%` }}
                  aria-hidden
                />
              </div>
              <span
                className={cn(
                  'w-28 shrink-0 text-right font-mono text-sm tabular-nums',
                  s.kind === 'result' ? 'font-semibold text-foreground' : 'text-foreground',
                )}
              >
                {s.display}
              </span>
            </li>
          );
        })}
      </ol>

      <div className="h-px bg-border" />

      <div className="flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between">
        <dl className="flex gap-6">
          <div className="flex flex-col">
            <dt className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Expected closing
            </dt>
            <dd className="font-mono text-base font-medium tabular-nums text-foreground">
              {fmt(expectedClosing)}
            </dd>
          </div>
          <div className="flex flex-col">
            <dt className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Actual closing
            </dt>
            <dd className="font-mono text-base font-medium tabular-nums text-foreground">
              {fmt(actualClosing)}
            </dd>
          </div>
        </dl>

        <div
          className={cn(
            'flex items-center gap-3 rounded-lg border px-3.5 py-2',
            overTolerance ? 'border-danger/40 bg-danger/10' : 'border-success/30 bg-success/10',
          )}
        >
          <div className="flex flex-col">
            <span className="text-[11px] uppercase tracking-wider text-muted-foreground">
              Variance
            </span>
            <span
              className={cn(
                'font-mono text-lg font-semibold tabular-nums',
                overTolerance ? 'text-danger' : 'text-success',
              )}
            >
              {varianceNum > 0 ? '+' : ''}
              {variance != null && variance !== '' ? fmt(variance) : fmt(String(actual - expected))}
              {unit ? (
                <span className="ml-1 text-xs font-normal text-muted-foreground">{unit}</span>
              ) : null}
            </span>
          </div>
          <Badge tone={overTolerance ? 'danger' : 'success'}>
            {overTolerance ? 'Over tolerance' : 'Within tolerance'}
          </Badge>
        </div>
      </div>
    </div>
  );
}
