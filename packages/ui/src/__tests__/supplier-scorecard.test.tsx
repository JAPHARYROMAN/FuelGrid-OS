import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { SupplierScorecard, type SupplierScorecardItem } from '../index';

// The SupplierScorecard renders a real, server-renderable treatment, so we assert
// its full DOM: the supplier name, the numeric composite score, the band word and
// grade letter (colour is never the sole signal), each labelled sub-score, and an
// accessible meter per dimension. The component computes nothing — it is pure
// presentation of already-scored data, so no NaN can leak.
describe('SupplierScorecard', () => {
  const suppliers: SupplierScorecardItem[] = [
    {
      key: 'risky',
      name: 'Risky Fuels Ltd',
      score: 42,
      band: 'At risk',
      tone: 'critical',
      grade: 'D',
      detail: '10 deliveries · 10 disputes',
      dimensions: [
        { key: 'on_time', label: 'On-time', score: 50 },
        { key: 'quantity', label: 'Quantity', score: 0 },
        { key: 'disputes', label: 'Disputes', score: 0 },
      ],
    },
    {
      key: 'acme',
      name: 'Acme Petroleum',
      score: 96,
      band: 'Excellent',
      tone: 'low',
      grade: 'A',
      dimensions: [{ key: 'on_time', label: 'On-time', score: 100 }],
    },
  ];

  it('renders each supplier name, composite score, band word and grade', () => {
    const html = renderToStaticMarkup(<SupplierScorecard suppliers={suppliers} />);
    expect(html).toContain('Risky Fuels Ltd');
    expect(html).toContain('Acme Petroleum');
    // The band + grade are rendered as text, not encoded only in the card colour.
    expect(html).toContain('At risk');
    expect(html).toContain('Excellent');
    expect(html).toContain('D · At risk');
    expect(html).toContain('A · Excellent');
    // The numeric composite scores ride along.
    expect(html).toContain('42');
    expect(html).toContain('96');
    expect(html).toContain('10 deliveries · 10 disputes');
    expect(html).not.toContain('NaN');
  });

  it('renders each sub-score as an accessible meter (text + bar, not colour alone)', () => {
    const html = renderToStaticMarkup(<SupplierScorecard suppliers={suppliers} />);
    expect(html).toContain('On-time');
    expect(html).toContain('Quantity');
    expect(html).toContain('Disputes');
    // Each dimension is a role=meter with the value + bounds for assistive tech.
    expect(html).toContain('role="meter"');
    expect(html).toContain('aria-valuenow="50"');
    expect(html).toContain('aria-valuemax="100"');
    // The whole card carries an accessible composite summary.
    expect(html).toContain('Risky Fuels Ltd: score 42 of 100, At risk');
  });

  it('renders nothing when there are no suppliers (honest empty state)', () => {
    expect(renderToStaticMarkup(<SupplierScorecard suppliers={[]} />)).toBe('');
  });
});
