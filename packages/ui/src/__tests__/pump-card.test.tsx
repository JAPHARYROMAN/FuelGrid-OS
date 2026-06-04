import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { PumpCard, type PumpCardNozzle } from '../index';

// SSR markup tests, matching the existing ui harness. PumpCard receives nozzle
// unit prices as exact decimal STRINGS (numeric(14,2)->text) and must format
// them for display without coercing NaN.

const nozzle = (over: Partial<PumpCardNozzle> = {}): PumpCardNozzle => ({
  id: 'n1',
  number: 1,
  productName: 'Premium',
  productColor: '#f97316',
  tankCode: 'T1',
  price: '2950.00',
  ...over,
});

describe('PumpCard', () => {
  it('renders the pump number, status and each nozzle with a formatted price', () => {
    const html = renderToStaticMarkup(<PumpCard number={3} status="active" nozzles={[nozzle()]} />);
    expect(html).toContain('Pump 3');
    expect(html).toContain('active');
    expect(html).toContain('Premium');
    expect(html).toContain('T1');
    expect(html).toContain('N1');
    // "2950.00" -> "2,950.00" (display format), never NaN.
    expect(html).toContain('2,950.00');
    expect(html).not.toContain('NaN');
  });

  it('sorts nozzles by number regardless of input order', () => {
    const html = renderToStaticMarkup(
      <PumpCard
        number={1}
        status="active"
        nozzles={[
          nozzle({ id: 'b', number: 2, productName: 'Diesel', price: '2820.00' }),
          nozzle({ id: 'a', number: 1, productName: 'Premium', price: '2950.00' }),
        ]}
      />,
    );
    expect(html.indexOf('N1')).toBeLessThan(html.indexOf('N2'));
  });

  it('renders the empty state when there are no nozzles', () => {
    const html = renderToStaticMarkup(<PumpCard number={2} status="inactive" nozzles={[]} />);
    expect(html).toContain('No nozzles configured.');
    expect(html).not.toContain('NaN');
  });

  it('falls back to the em-dash money placeholder for an empty/garbled price', () => {
    const html = renderToStaticMarkup(
      <PumpCard number={1} status="active" nozzles={[nozzle({ price: '' })]} />,
    );
    // formatMoney('') -> '—', not 'NaN'.
    expect(html).toContain('—');
    expect(html).not.toContain('NaN');
  });

  it('renders an interactive button-role card when onActivate is supplied', () => {
    const html = renderToStaticMarkup(
      <PumpCard number={1} status="active" nozzles={[nozzle()]} onActivate={() => {}} />,
    );
    expect(html).toContain('role="button"');
    expect(html).toContain('tabindex="0"');
  });

  it('is a plain (non-interactive) surface without onActivate', () => {
    const html = renderToStaticMarkup(
      <PumpCard number={1} status="maintenance" nozzles={[nozzle()]} />,
    );
    expect(html).not.toContain('role="button"');
  });
});
