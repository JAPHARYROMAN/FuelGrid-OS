import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { RiskAlertCard } from '../index';

// SSR markup smoke tests, matching the existing harness (no @testing-library).

describe('RiskAlertCard', () => {
  it('renders severity badge, title, description, metric and recommended action', () => {
    const html = renderToStaticMarkup(
      <RiskAlertCard
        severity="critical"
        title="Stock variance over tolerance"
        description="Tank 3 closing dip is 240L below expected."
        metricLabel="Variance"
        metricValue="-240 L"
        recommendedAction="Re-dip tank 3 and recount deliveries."
        station="ACC-01"
        occurredAt="08:42"
      />,
    );
    // RiskBadge defaults its label to the severity word.
    expect(html).toContain('critical');
    expect(html).toContain('Stock variance over tolerance');
    expect(html).toContain('240L below expected');
    expect(html).toContain('Variance');
    expect(html).toContain('-240 L');
    expect(html).toContain('Recommended action');
    expect(html).toContain('Re-dip tank 3');
    expect(html).toContain('ACC-01');
    expect(html).toContain('08:42');
  });

  it('renders a focusable anchor overlay when href is set', () => {
    const html = renderToStaticMarkup(
      <RiskAlertCard severity="high" title="Pump downtime" href="/risk/alerts/42" />,
    );
    expect(html).toContain('href="/risk/alerts/42"');
  });

  it('renders a button overlay when onClick is set and no href', () => {
    const html = renderToStaticMarkup(
      <RiskAlertCard severity="low" title="Late shift close" onClick={() => {}} />,
    );
    expect(html).toContain('<button');
  });

  it('omits the metric block and meta row when not provided', () => {
    const html = renderToStaticMarkup(<RiskAlertCard severity="info" title="No metric" />);
    expect(html).not.toContain('font-mono');
    expect(html).not.toContain('Station');
  });
});
