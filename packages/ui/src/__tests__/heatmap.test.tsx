import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { Heatmap, heatmapToneColor, type HeatmapRow } from '../index';

// The Heatmap renders a real, server-renderable grid (no ResponsiveContainer),
// so we can assert its full DOM: the row/column headers, the caller-formatted
// figures as TEXT (colour is never the sole signal), the over-threshold flag
// chip, and the honest empty state. Values are decimal-string-derived display
// strings — the component never parses them, so no NaN can leak.
describe('Heatmap', () => {
  const rows: HeatmapRow[] = [
    {
      key: 'tank-1',
      label: 'PMS-01',
      sublabel: 'Petrol',
      cells: [
        {
          key: 'var',
          display: '-1.20%',
          intensity: 1,
          tone: 'danger',
          flagged: true,
          sublabel: 'tol 0.50%',
          ariaLabel: 'PMS-01 variance -1.20% over tolerance',
        },
      ],
    },
    {
      key: 'tank-2',
      label: 'AGO-01',
      sublabel: 'Diesel',
      cells: [
        {
          key: 'var',
          display: '-0.10%',
          intensity: 0.1,
          tone: 'success',
          flagged: false,
          sublabel: 'tol 0.50%',
        },
      ],
    },
  ];

  it('renders the column + row headers and every cell figure as text', () => {
    const html = renderToStaticMarkup(<Heatmap rows={rows} columns={['Variance %']} />);
    expect(html).toContain('Variance %');
    expect(html).toContain('PMS-01');
    expect(html).toContain('AGO-01');
    expect(html).toContain('Petrol');
    // The figures are rendered as text, not encoded only in the cell colour.
    expect(html).toContain('-1.20%');
    expect(html).toContain('-0.10%');
    // The tolerance sub-label rides along.
    expect(html).toContain('tol 0.50%');
    expect(html).not.toContain('NaN');
  });

  it('marks flagged cells with a text chip (colour is not the only signal)', () => {
    const html = renderToStaticMarkup(
      <Heatmap rows={rows} columns={['Variance %']} flagLabel="Over tolerance" />,
    );
    // The over-tolerance cell carries the textual chip…
    expect(html).toContain('Over tolerance');
    // …and an accessible label describing it.
    expect(html).toContain('PMS-01 variance -1.20% over tolerance');
  });

  it('renders nothing when there are no rows or no columns', () => {
    expect(renderToStaticMarkup(<Heatmap rows={[]} columns={['Variance %']} />)).toBe('');
    expect(renderToStaticMarkup(<Heatmap rows={rows} columns={[]} />)).toBe('');
  });

  it('exposes token-driven tone colours as hsl(var(--…)) references', () => {
    for (const tone of ['danger', 'warning', 'success', 'accent', 'neutral'] as const) {
      const c = heatmapToneColor(tone);
      expect(c).toMatch(/^hsl\(var\(--/);
      expect(c).not.toMatch(/#[0-9a-f]/i);
    }
  });
});
