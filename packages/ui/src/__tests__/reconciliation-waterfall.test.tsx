import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { ReconciliationWaterfall } from '../index';

// SSR markup tests, matching the existing ui harness. The waterfall is fed exact
// decimal STRINGS (the numeric->text contract) and parses them once for bar
// geometry only. The contract under test: it renders every supplied step, flips
// the variance to over/within-tolerance correctly, and never leaks NaN — even
// for empty/edge inputs.

describe('ReconciliationWaterfall', () => {
  const base = {
    openingStock: '25800.000',
    deliveries: '9800.000',
    sales: '8200.000',
    adjustments: '0.000',
    expectedClosing: '27400.000',
    actualClosing: '27250.000',
  };

  it('renders all steps and figures from decimal strings without NaN', () => {
    const html = renderToStaticMarkup(<ReconciliationWaterfall {...base} tolerance="200" />);
    expect(html).toContain('Opening stock');
    expect(html).toContain('Deliveries');
    expect(html).toContain('Sales');
    expect(html).toContain('Adjustments');
    expect(html).toContain('Expected closing');
    expect(html).toContain('Actual closing');
    // Whole-litre formatting of the opening figure.
    expect(html).toContain('25,800');
    expect(html).not.toContain('NaN');
  });

  it('shows the within-tolerance state when |variance| <= tolerance', () => {
    // variance = 27250 - 27400 = -150, tolerance 200 -> within.
    const html = renderToStaticMarkup(<ReconciliationWaterfall {...base} tolerance="200" />);
    expect(html).toContain('Within tolerance');
    expect(html).not.toContain('Over tolerance');
  });

  it('flips to over-tolerance when |variance| exceeds tolerance', () => {
    // variance = -150, tolerance 100 -> over.
    const html = renderToStaticMarkup(<ReconciliationWaterfall {...base} tolerance="100" />);
    expect(html).toContain('Over tolerance');
  });

  it('derives the variance from actual - expected when not provided', () => {
    const html = renderToStaticMarkup(
      <ReconciliationWaterfall
        openingStock="1000"
        expectedClosing="1000"
        actualClosing="900"
        tolerance="50"
      />,
    );
    // -100 magnitude over a 50 tolerance.
    expect(html).toContain('Over tolerance');
    expect(html).not.toContain('NaN');
  });

  it('prefixes a positive derived variance with a plus sign', () => {
    const html = renderToStaticMarkup(
      <ReconciliationWaterfall openingStock="1000" expectedClosing="1000" actualClosing="1100" />,
    );
    expect(html).toContain('+');
  });

  it('omits optional movement steps when those inputs are absent', () => {
    const html = renderToStaticMarkup(
      <ReconciliationWaterfall openingStock="1000" expectedClosing="1000" actualClosing="1000" />,
    );
    // No deliveries/sales/adjustments supplied -> those rows are not rendered.
    expect(html).not.toContain('Deliveries');
    expect(html).not.toContain('Sales');
    expect(html).not.toContain('Adjustments');
    expect(html).toContain('Opening stock');
  });

  it('treats empty-string and null figures as zero, never NaN', () => {
    const html = renderToStaticMarkup(
      <ReconciliationWaterfall
        openingStock=""
        deliveries={null}
        sales={undefined}
        expectedClosing=""
        actualClosing={null}
      />,
    );
    expect(html).not.toContain('NaN');
    // formatLitres of an unparseable value falls back to the em dash.
    expect(html).toContain('—');
  });

  it('honours a custom valueFormatter for every figure', () => {
    const html = renderToStaticMarkup(
      <ReconciliationWaterfall
        openingStock="1000"
        expectedClosing="1000"
        actualClosing="1000"
        valueFormatter={(v) => `KES ${String(v)}`}
      />,
    );
    expect(html).toContain('KES 1000');
  });
});
