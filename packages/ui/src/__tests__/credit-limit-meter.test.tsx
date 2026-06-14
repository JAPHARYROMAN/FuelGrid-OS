import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { CreditLimitMeter, creditMeterTone, type CreditLimitMeterItem } from '../index';

// The CreditLimitMeter is a server-renderable, accessible utilization gauge (no
// client charts), so we can assert its full DOM: each customer label, the
// utilization percent rendered as TEXT (colour is never the sole signal), the
// status word, the exposure / limit caption, and the role="meter" + aria-value*
// attributes a screen reader announces. Money figures are caller-formatted
// display strings — the component never parses them, so no NaN can leak.
describe('CreditLimitMeter', () => {
  const items: CreditLimitMeterItem[] = [
    {
      key: 'a',
      label: 'Acme Logistics',
      utilization: 42,
      exposure: 'TZS 420,000',
      limit: 'TZS 1,000,000',
    },
    {
      key: 'b',
      label: 'Beta Transport',
      utilization: 88,
      warningPct: 80,
      exposure: 'TZS 880,000',
      limit: 'TZS 1,000,000',
    },
    {
      key: 'c',
      label: 'Gamma Freight',
      utilization: 125,
      exposure: 'TZS 1,250,000',
      limit: 'TZS 1,000,000',
    },
  ];

  it('renders every label, the percent as text, the status word and the caption', () => {
    const html = renderToStaticMarkup(<CreditLimitMeter items={items} />);
    expect(html).toContain('Acme Logistics');
    expect(html).toContain('Beta Transport');
    expect(html).toContain('Gamma Freight');
    // Percent is rendered as a text figure, not encoded only in the bar colour.
    expect(html).toContain('42%');
    expect(html).toContain('88%');
    // >100% reads honestly rather than being clamped in the text.
    expect(html).toContain('125%');
    // Derived status words by tone (text + colour, never colour alone).
    expect(html).toContain('Within limit');
    expect(html).toContain('Near limit');
    expect(html).toContain('Over limit');
    // The caller-formatted exposure / limit caption rides along; no NaN leaks.
    expect(html).toContain('TZS 420,000');
    expect(html).toContain('TZS 1,000,000');
    expect(html).not.toContain('NaN');
  });

  it('exposes an accessible meter with aria-value* per customer', () => {
    const html = renderToStaticMarkup(<CreditLimitMeter items={items} />);
    expect(html).toContain('role="meter"');
    expect(html).toContain('aria-valuemin="0"');
    expect(html).toContain('aria-valuemax="100"');
    // aria-valuenow carries the rounded utilization figure, CLAMPED into the
    // declared [0,100] range (WAI-ARIA requires valuenow ≤ valuemax).
    expect(html).toContain('aria-valuenow="42"');
    // An over-limit customer (125%) clamps valuenow to 100 (never out of range)…
    expect(html).toContain('aria-valuenow="100"');
    expect(html).not.toContain('aria-valuenow="125"');
    // …while the TRUE percent is still printed as text and narrated in valuetext.
    expect(html).toContain('125%');
    // aria-valuetext narrates the figure + status for screen readers.
    expect(html).toContain('of credit limit used');
  });

  it('respects an overridden tone and status word (e.g. on-hold customers)', () => {
    const held: CreditLimitMeterItem[] = [
      { key: 'h', label: 'Held Co', utilization: 10, tone: 'over', statusWord: 'On hold' },
    ];
    const html = renderToStaticMarkup(<CreditLimitMeter items={held} />);
    expect(html).toContain('On hold');
    // The percent still reflects the true (low) utilization, not the forced tone.
    expect(html).toContain('10%');
  });

  it('renders nothing for an empty item list (honest empty state)', () => {
    const html = renderToStaticMarkup(<CreditLimitMeter items={[]} />);
    expect(html).toBe('');
  });

  describe('creditMeterTone', () => {
    it('derives ok / warning / over against the warning threshold and 100% limit', () => {
      expect(creditMeterTone(0)).toBe('ok');
      expect(creditMeterTone(79)).toBe('ok');
      expect(creditMeterTone(80)).toBe('warning');
      expect(creditMeterTone(99)).toBe('warning');
      expect(creditMeterTone(100)).toBe('over');
      expect(creditMeterTone(140)).toBe('over');
      // A custom warning threshold shifts the warning band.
      expect(creditMeterTone(70, 90)).toBe('ok');
      expect(creditMeterTone(92, 90)).toBe('warning');
    });
  });
});
