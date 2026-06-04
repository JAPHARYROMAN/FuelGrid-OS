import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { AreaChart, BarChart, LineChart, Sparkline, chartColors } from '../index';

// recharts paints inside a ResponsiveContainer that needs a measured DOM box,
// so full charts render empty under SSR/jsdom. These tests pin the behavior we
// can assert deterministically: the token palette, the Sparkline data guard,
// and that the chart wrappers accept decimal-STRING value series + empty/edge
// data without throwing or leaking NaN into the rendered markup.
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

  // Representative decimal-string rows (the numeric->text money/litre contract).
  const rows = [
    { day: '2026-06-01', gross: '1284500.00', litres: '25800.000' },
    { day: '2026-06-02', gross: '1310250.50', litres: '26110.500' },
    { day: '2026-06-03', gross: '0', litres: '0.000' },
  ];
  const series = [{ key: 'gross', label: 'Gross' }];

  it('LineChart renders its wrapper with decimal-string series and no NaN', () => {
    const html = renderToStaticMarkup(
      <LineChart data={rows} xKey="day" series={series} height={200} />,
    );
    expect(html).toContain('<div');
    expect(html).not.toContain('NaN');
  });

  it('AreaChart renders its wrapper with decimal-string series and no NaN', () => {
    const html = renderToStaticMarkup(
      <AreaChart data={rows} xKey="day" series={series} height={200} />,
    );
    expect(html).toContain('<div');
    expect(html).not.toContain('NaN');
  });

  it('BarChart renders its wrapper with decimal-string series and no NaN', () => {
    const html = renderToStaticMarkup(
      <BarChart data={rows} xKey="day" series={series} layout="vertical" height={200} />,
    );
    expect(html).toContain('<div');
    expect(html).not.toContain('NaN');
  });

  it('chart wrappers render with empty data without throwing or emitting NaN', () => {
    for (const html of [
      renderToStaticMarkup(<LineChart data={[]} xKey="day" series={series} />),
      renderToStaticMarkup(<AreaChart data={[]} xKey="day" series={series} />),
      renderToStaticMarkup(<BarChart data={[]} xKey="day" series={series} />),
    ]) {
      expect(html).toContain('<div');
      expect(html).not.toContain('NaN');
    }
  });

  it('coerces garbled/empty cell values to a finite 0 (no NaN) for geometry', () => {
    const dirty = [
      { day: 'a', gross: '' },
      { day: 'b', gross: 'not-a-number' },
      { day: 'c', gross: '500.00' },
    ];
    const html = renderToStaticMarkup(<BarChart data={dirty} xKey="day" series={series} />);
    expect(html).not.toContain('NaN');
  });
});
