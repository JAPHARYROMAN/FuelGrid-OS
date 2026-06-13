import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { ReportDateRangeFilterBar, REPORT_RANGE_PRESETS, type ReportDateRange } from '../index';

// SSR markup tests, matching the existing ui harness (no @testing-library). The
// ReportDateRangeFilterBar is the net-new shared global date-range control for
// the Reports Home: it keeps the four fixed presets AND adds a free custom
// from/to range. The contract under test: it offers every preset plus a Custom
// option, hides the date inputs for a relative preset, and reveals them
// (carrying the typed range) once Custom is selected. The onChange emission of a
// custom range is exercised interactively in the web hub page test.

const noop = () => {};

describe('ReportDateRangeFilterBar', () => {
  it('renders every relative preset plus a custom option', () => {
    const value: ReportDateRange = { preset: 'last-30', from: '', to: '' };
    const html = renderToStaticMarkup(<ReportDateRangeFilterBar value={value} onChange={noop} />);
    for (const p of REPORT_RANGE_PRESETS) {
      expect(html).toContain(p.label);
    }
    expect(html).toContain('Custom range');
  });

  it('hides the from/to date inputs for a relative preset', () => {
    const value: ReportDateRange = { preset: 'this-month', from: '', to: '' };
    const html = renderToStaticMarkup(<ReportDateRangeFilterBar value={value} onChange={noop} />);
    expect(html).not.toContain('aria-label="From date"');
    expect(html).not.toContain('aria-label="To date"');
    // The relative-window cue is shown instead.
    expect(html).toContain('Relative window');
  });

  it('reveals the from/to date inputs with the typed range for a custom range', () => {
    const value: ReportDateRange = { preset: 'custom', from: '2026-01-01', to: '2026-01-31' };
    const html = renderToStaticMarkup(<ReportDateRangeFilterBar value={value} onChange={noop} />);
    expect(html).toContain('aria-label="From date"');
    expect(html).toContain('aria-label="To date"');
    expect(html).toContain('value="2026-01-01"');
    expect(html).toContain('value="2026-01-31"');
  });

  it('renders caller children (station/region) and actions slots', () => {
    const value: ReportDateRange = { preset: 'last-30', from: '', to: '' };
    const html = renderToStaticMarkup(
      <ReportDateRangeFilterBar value={value} onChange={noop} actions={<span>my-actions</span>}>
        <span>my-station-field</span>
      </ReportDateRangeFilterBar>,
    );
    expect(html).toContain('my-station-field');
    expect(html).toContain('my-actions');
  });
});
