import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { FinancialWaterfall, type FinancialWaterfallStep } from '../index';

// The FinancialWaterfall is a server-renderable, accessible money cascade (no
// client charts), so we can assert its full DOM: each step label, the figure
// rendered as TEXT (colour is never the sole signal), the +/−/= sign glyph, and
// the role="list"/role="group" + per-step aria-label a screen reader announces.
// Money figures are caller-formatted display strings — the component parses the
// raw decimal only for geometry, never for display, so no NaN can leak.
describe('FinancialWaterfall', () => {
  // A P&L cascade: net revenue → (− COGS) = gross margin → (− expenses) = net.
  const fmt = (v: string | number | null | undefined) =>
    new Intl.NumberFormat('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 }).format(
      Number(v ?? 0),
    );

  // Deductions are supplied as positive magnitudes flagged `negative` (the way the
  // finance handler feeds COGS/expenses), so the formatted figure carries no
  // double sign — the component owns the −/= glyph.
  const steps: FinancialWaterfallStep[] = [
    { key: 'rev', label: 'Net revenue', value: '1000.00', kind: 'base' },
    { key: 'cogs', label: 'COGS', value: '600.00', kind: 'delta', negative: true },
    { key: 'gm', label: 'Gross margin', value: '400.00', kind: 'total' },
    { key: 'exp', label: 'Expenses', value: '150.00', kind: 'delta', negative: true },
    { key: 'net', label: 'Net operating result', value: '250.00', kind: 'total' },
  ];

  it('renders every step label and figure as text, with sign glyphs', () => {
    const html = renderToStaticMarkup(
      <FinancialWaterfall steps={steps} valueFormatter={(v) => fmt(v)} unit="TZS" />,
    );
    expect(html).toContain('Net revenue');
    expect(html).toContain('COGS');
    expect(html).toContain('Gross margin');
    expect(html).toContain('Expenses');
    expect(html).toContain('Net operating result');
    // Figures rendered as text, not encoded only in the bar colour/width.
    expect(html).toContain('1,000.00');
    expect(html).toContain('600.00');
    expect(html).toContain('400.00');
    expect(html).toContain('250.00');
    // Deduction steps print a minus glyph; the total prints an equals glyph.
    expect(html).toContain('−');
    expect(html).toContain('=');
    // The caller's unit rides along; no NaN leaks from geometry parsing.
    expect(html).toContain('TZS');
    expect(html).not.toContain('NaN');
  });

  it('exposes an accessible group + per-step aria-labels', () => {
    const html = renderToStaticMarkup(
      <FinancialWaterfall
        steps={steps}
        valueFormatter={(v) => fmt(v)}
        ariaLabel="Profit and loss waterfall"
        unit="TZS"
      />,
    );
    expect(html).toContain('role="group"');
    expect(html).toContain('aria-label="Profit and loss waterfall"');
    expect(html).toContain('role="list"');
    // Each step's aria-label narrates the figure + sign for screen readers.
    expect(html).toContain('Net revenue: 1,000.00 TZS');
    expect(html).toContain('COGS: − 600.00 TZS');
  });

  it('lands a negative result in a loss tone (text + colour, never colour alone)', () => {
    const loss: FinancialWaterfallStep[] = [
      { key: 'rev', label: 'Net revenue', value: '500.00', kind: 'base' },
      { key: 'cogs', label: 'COGS', value: '-700.00', kind: 'delta' },
      { key: 'net', label: 'Net operating result', value: '-200.00', kind: 'total' },
    ];
    const html = renderToStaticMarkup(
      <FinancialWaterfall steps={loss} valueFormatter={(v) => fmt(v)} />,
    );
    // The negative result is rendered as text (the figure carries the sign), so a
    // loss reads even in monochrome.
    expect(html).toContain('-200.00');
    expect(html).toContain('text-danger');
  });

  it('treats an unsigned magnitude flagged negative as a deduction', () => {
    const stepsUnsigned: FinancialWaterfallStep[] = [
      { key: 'rev', label: 'Revenue', value: '1000.00', kind: 'base' },
      { key: 'cost', label: 'Cost', value: '300.00', kind: 'delta', negative: true },
      { key: 'gm', label: 'Margin', value: '700.00', kind: 'total' },
    ];
    const html = renderToStaticMarkup(
      <FinancialWaterfall steps={stepsUnsigned} valueFormatter={(v) => fmt(v)} />,
    );
    // The explicit `negative` flag makes the cost a deduction → minus glyph + danger.
    expect(html).toContain('−');
    expect(html).toContain('text-danger');
  });

  it('renders nothing for an empty step list (honest empty state)', () => {
    const html = renderToStaticMarkup(<FinancialWaterfall steps={[]} />);
    expect(html).toBe('');
  });
});
