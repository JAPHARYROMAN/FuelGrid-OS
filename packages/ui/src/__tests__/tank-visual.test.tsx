import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { TankVisual } from '../index';

// SSR markup tests, matching the existing ui harness (renderToStaticMarkup, no
// @testing-library). The contract under test: TankVisual is fed exact decimal
// STRINGS for its dimensions (the numeric->text SDK contract) and must render
// the geometry/figures without ever leaking NaN into the markup.

describe('TankVisual', () => {
  const base = {
    name: 'AGO T2',
    code: 'T2',
    color: '#2563eb',
    capacityLitres: '30000.000',
    safeMinLitres: '5000.000',
    safeMaxLitres: '28500.000',
  };

  it('renders name, code and a labelled fill from decimal-string dimensions', () => {
    const html = renderToStaticMarkup(<TankVisual {...base} currentLitres="17500.000" />);
    expect(html).toContain('AGO T2');
    expect(html).toContain('T2');
    // Geometry/display parsed from the strings — never the literal string back.
    expect(html).toContain('aria-label="AGO T2 tank fill"');
    // Current + capacity figures are formatted whole litres.
    expect(html).toContain('17,500 L');
    expect(html).toContain('30,000 L');
    // Ullage = capacity - current = 12,500.
    expect(html).toContain('12,500 L');
    // No NaN ever reaches the DOM.
    expect(html).not.toContain('NaN');
  });

  it('renders the awaiting-reading placeholder with an em dash when currentLitres is null', () => {
    const html = renderToStaticMarkup(<TankVisual {...base} currentLitres={null} />);
    expect(html).toContain('awaiting reading');
    // Current + ullage collapse to an em dash, not "NaN L".
    expect(html).toContain('—');
    expect(html).not.toContain('NaN');
  });

  it('treats an empty-string reading as no reading (no NaN)', () => {
    const html = renderToStaticMarkup(<TankVisual {...base} currentLitres="" />);
    expect(html).toContain('awaiting reading');
    expect(html).not.toContain('NaN');
  });

  it('accepts a numeric currentLitres (the API still emits it as a number)', () => {
    const html = renderToStaticMarkup(<TankVisual {...base} currentLitres={9000} />);
    expect(html).toContain('9,000 L');
    expect(html).not.toContain('NaN');
  });

  it('clamps an over-capacity reading without overflowing or emitting NaN', () => {
    // current > capacity: the fill fraction clamps to 1; figures still render.
    const html = renderToStaticMarkup(<TankVisual {...base} currentLitres="45000.000" />);
    expect(html).toContain('45,000 L');
    // Ullage floors at 0 ("0 L"), never negative.
    expect(html).toContain('0 L');
    expect(html).not.toContain('NaN');
  });

  it('shows a non-active status pill', () => {
    const html = renderToStaticMarkup(
      <TankVisual {...base} currentLitres="100" status="maintenance" />,
    );
    expect(html).toContain('maintenance');
  });

  it('does not show a status pill for an active tank', () => {
    const html = renderToStaticMarkup(<TankVisual {...base} currentLitres="100" status="active" />);
    // The "active" word only appears via the pill; the pill is suppressed.
    expect(html).not.toContain('>active<');
  });

  it('survives a zero-capacity tank without dividing by zero into NaN', () => {
    const html = renderToStaticMarkup(
      <TankVisual {...base} capacityLitres="0" currentLitres="0" />,
    );
    expect(html).not.toContain('NaN');
  });
});
