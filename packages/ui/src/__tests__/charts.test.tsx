import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { Sparkline, chartColors } from '../index';

// recharts paints inside a ResponsiveContainer that needs a measured DOM box,
// so full charts render empty under SSR/jsdom. These tests pin the behavior we
// can assert deterministically: the token palette and the Sparkline data guard.
describe('charts', () => {
  it('exposes token-driven colors as hsl(var(--…)) references', () => {
    expect(chartColors.accent).toBe('hsl(var(--color-accent))');
    expect(chartColors.grid).toBe('hsl(var(--color-border))');
    expect(chartColors.success).toBe('hsl(var(--color-success))');
    // No hard-coded hex anywhere in the palette.
    for (const value of Object.values(chartColors)) {
      expect(value).not.toMatch(/#[0-9a-f]/i);
    }
  });

  it('Sparkline renders nothing with fewer than two points', () => {
    const html = renderToStaticMarkup(<Sparkline data={[{ v: '1' }]} valueKey="v" />);
    expect(html).toBe('');
  });

  it('Sparkline renders a container once there are enough points', () => {
    const html = renderToStaticMarkup(
      <Sparkline data={[{ v: '1' }, { v: '2' }, { v: '3' }]} valueKey="v" />,
    );
    // The wrapper div is emitted even though recharts itself measures to 0 in jsdom.
    expect(html).toContain('<div');
  });
});
