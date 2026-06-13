import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { StatusBoard, statusToneColor, type StatusBoardItem } from '../index';

// The StatusBoard renders a real, server-renderable board (no client charts), so
// we can assert its full DOM: each medium label, the STATUS rendered as text
// (colour is never the sole signal), the already-formatted figure, the detail
// caption, and the honest empty state. Amounts are caller-formatted display
// strings — the component never parses them, so no NaN can leak.
describe('StatusBoard', () => {
  const items: StatusBoardItem[] = [
    {
      key: 'cash',
      label: 'Cash',
      status: 'Settled',
      tone: 'settled',
      amount: 'TZS 600,000',
      detail: 'Reconciliation posted',
      ariaLabel: 'Cash settlement: settled',
    },
    {
      key: 'mobile_money',
      label: 'Mobile money',
      status: 'Pending',
      tone: 'pending',
      amount: 'TZS 250,000',
      detail: 'Awaiting settlement confirmation',
    },
    {
      key: 'bank_deposit',
      label: 'Bank deposit',
      status: 'Not posted',
      tone: 'at_risk',
      amount: 'TZS 0',
    },
  ];

  it('renders every medium label, status word and figure as text', () => {
    const html = renderToStaticMarkup(<StatusBoard items={items} />);
    expect(html).toContain('Cash');
    expect(html).toContain('Mobile money');
    expect(html).toContain('Bank deposit');
    // The status is rendered as a text word, not encoded only in the chip colour.
    expect(html).toContain('Settled');
    expect(html).toContain('Pending');
    expect(html).toContain('Not posted');
    // The caller-formatted figures + details ride along.
    expect(html).toContain('TZS 600,000');
    expect(html).toContain('Reconciliation posted');
    expect(html).not.toContain('NaN');
  });

  it('exposes an accessible label per chip (falls back to label + status)', () => {
    const html = renderToStaticMarkup(<StatusBoard items={items} />);
    // Explicit ariaLabel is used when supplied…
    expect(html).toContain('Cash settlement: settled');
    // …and the fallback is `${label}: ${status}` when it is not.
    expect(html).toContain('Mobile money: Pending');
  });

  it('renders nothing when there are no items (honest empty state)', () => {
    expect(renderToStaticMarkup(<StatusBoard items={[]} />)).toBe('');
  });

  it('exposes token-driven tone colours as hsl(var(--…)) references', () => {
    for (const tone of ['settled', 'pending', 'at_risk', 'neutral'] as const) {
      const c = statusToneColor(tone);
      expect(c).toMatch(/^hsl\(var\(--/);
      expect(c).not.toMatch(/#[0-9a-f]/i);
    }
  });
});
