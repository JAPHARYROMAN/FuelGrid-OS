import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import {
  AreaChart,
  BarChart,
  DonutChart,
  LineChart,
  Sparkline,
  StackedBarChart,
  TenderMixDonut,
  chartColors,
} from '../index';

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
      renderToStaticMarkup(<StackedBarChart data={[]} xKey="day" series={series} />),
    ]) {
      expect(html).toContain('<div');
      expect(html).not.toContain('NaN');
    }
  });

  it('StackedBarChart renders a multi-series product-mix column with no NaN', () => {
    // A single category (period total) whose segments are per-product revenue —
    // the §5.2 product-mix shape. Each series carries a decimal-string value.
    const mix = [{ bucket: 'Period total', petrol: '4200000.00', diesel: '3100500.25' }];
    const mixSeries = [
      { key: 'petrol', label: 'Petrol', color: chartColors.accent },
      { key: 'diesel', label: 'Diesel', color: chartColors.success },
    ];
    const html = renderToStaticMarkup(
      <StackedBarChart
        data={mix}
        xKey="bucket"
        series={mixSeries}
        valueFormatter={(v) => `TZS ${String(v)}`}
        height={200}
      />,
    );
    expect(html).toContain('<div');
    expect(html).not.toContain('NaN');
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

  // The donut paints inside a ResponsiveContainer (0-box under jsdom) like the
  // other charts, but it ALSO renders a real legend + center label in DOM we can
  // assert: the slice labels, the caller-formatted center value, and the honest
  // empty state. Values are decimal STRINGS — no NaN must leak.
  const donutSlices = [
    { key: 'cash', label: 'Cash', value: '600.00' },
    { key: 'card', label: 'Card', value: '400.00' },
  ];

  it('DonutChart renders its legend labels + center value with no NaN', () => {
    const html = renderToStaticMarkup(
      <DonutChart
        slices={donutSlices}
        valueFormatter={(v) => `TZS ${String(v)}`}
        centerLabel="Total"
        centerValue="TZS 1000.00"
      />,
    );
    expect(html).toContain('Cash');
    expect(html).toContain('Card');
    // The legend renders each slice's share of the total (600/1000 = 60%).
    expect(html).toContain('60%');
    expect(html).toContain('40%');
    // The caller-formatted center value is rendered in the donut hole.
    expect(html).toContain('TZS 1000.00');
    expect(html).not.toContain('NaN');
  });

  it('DonutChart renders nothing when every slice is zero/empty', () => {
    const zero = renderToStaticMarkup(
      <DonutChart
        slices={[
          { key: 'a', label: 'A', value: '0' },
          { key: 'b', label: 'B', value: '' },
        ]}
      />,
    );
    expect(zero).toBe('');
    const empty = renderToStaticMarkup(<DonutChart slices={[]} />);
    expect(empty).toBe('');
  });

  it('TenderMixDonut maps a tender mix to slices, dropping zero tenders', () => {
    const html = renderToStaticMarkup(
      <TenderMixDonut
        mix={{
          cash: '600.00',
          mobile_money: '300.00',
          card: '0',
          credit: '100.00',
          voucher: '0',
          total: '1000.00',
        }}
        valueFormatter={(v) => `TZS ${String(v)}`}
      />,
    );
    // Non-zero tenders appear...
    expect(html).toContain('Cash');
    expect(html).toContain('Mobile money');
    expect(html).toContain('Credit');
    // ...zero tenders (card, voucher) are dropped from the legend.
    expect(html).not.toContain('Card');
    expect(html).not.toContain('Voucher');
    // The total is surfaced in the center label, formatted by the caller.
    expect(html).toContain('Total tendered');
    expect(html).toContain('TZS 1000.00');
    expect(html).not.toContain('NaN');
  });

  it('TenderMixDonut renders nothing when all tenders are zero', () => {
    const html = renderToStaticMarkup(
      <TenderMixDonut
        mix={{
          cash: '0',
          mobile_money: '0',
          card: '0',
          credit: '0',
          voucher: '0',
          total: '0',
        }}
      />,
    );
    expect(html).toBe('');
  });
});
