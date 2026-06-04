import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { ReportCategoryCard } from '../index';

// SSR markup tests, matching the existing ui harness. ReportCategoryCard is the
// Reports Center hub tile: an icon + title + description, a headline metric
// (a pre-formatted decimal string from the caller), an optional alert pill, and
// an actions slot. The contract under test: it renders the metric verbatim,
// pluralises the alert pill, links the title when href is set, and shows a
// skeleton (not the value) while loading.

describe('ReportCategoryCard', () => {
  it('renders the title, description, metric label and pre-formatted value', () => {
    const html = renderToStaticMarkup(
      <ReportCategoryCard
        title="Profitability"
        description="Margin by product and station"
        metricLabel="Latest gross"
        metricValue="1,284,500.00"
      />,
    );
    expect(html).toContain('Profitability');
    expect(html).toContain('Margin by product and station');
    expect(html).toContain('Latest gross');
    // The caller already formatted the decimal string; the card renders it as-is.
    expect(html).toContain('1,284,500.00');
    expect(html).not.toContain('NaN');
  });

  it('shows a singular alert pill for a single alert', () => {
    const html = renderToStaticMarkup(
      <ReportCategoryCard title="Risk" metricValue="3" alertCount={1} />,
    );
    expect(html).toContain('1 alert');
    expect(html).not.toContain('1 alerts');
  });

  it('pluralises the alert pill for multiple alerts', () => {
    const html = renderToStaticMarkup(
      <ReportCategoryCard title="Risk" metricValue="3" alertCount={4} />,
    );
    expect(html).toContain('4 alerts');
  });

  it('omits the alert pill when alertCount is zero or undefined', () => {
    const zero = renderToStaticMarkup(
      <ReportCategoryCard title="Risk" metricValue="0" alertCount={0} />,
    );
    expect(zero).not.toContain('alert');
    const none = renderToStaticMarkup(<ReportCategoryCard title="Risk" metricValue="0" />);
    expect(none).not.toContain('alert');
  });

  it('links the title through an anchor when href is set', () => {
    const html = renderToStaticMarkup(
      <ReportCategoryCard title="Profitability" href="/reports/profitability" />,
    );
    expect(html).toContain('href="/reports/profitability"');
  });

  it('renders a skeleton instead of the metric value while loading', () => {
    const html = renderToStaticMarkup(
      <ReportCategoryCard title="Profitability" metricLabel="Latest gross" loading />,
    );
    // The metric label still shows, but the value is replaced by a skeleton box.
    expect(html).toContain('Latest gross');
    expect(html).toContain('animate-pulse');
  });

  it('renders without a metric block when no metric is supplied', () => {
    const html = renderToStaticMarkup(<ReportCategoryCard title="Plain" />);
    expect(html).toContain('Plain');
    expect(html).not.toContain('NaN');
  });
});
