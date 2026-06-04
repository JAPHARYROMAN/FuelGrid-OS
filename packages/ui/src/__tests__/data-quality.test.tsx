import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { DataQualityBanner, DataQualityCard } from '../index';

// SSR markup tests, matching the existing ui harness. The data-quality surfaces
// are advisory chrome: each renders its message list with a severity-driven
// role, and BOTH collapse to nothing when there are no messages.

describe('DataQualityBanner', () => {
  it('renders each advisory message', () => {
    const html = renderToStaticMarkup(
      <DataQualityBanner messages={['3 dips missing', '1 meter not read']} />,
    );
    expect(html).toContain('3 dips missing');
    expect(html).toContain('1 meter not read');
    // Default level is warning -> a status role (not alert).
    expect(html).toContain('role="status"');
  });

  it('uses role="alert" at the critical level', () => {
    const html = renderToStaticMarkup(
      <DataQualityBanner level="critical" messages={['ledger out of balance']} />,
    );
    expect(html).toContain('role="alert"');
  });

  it('renders nothing when the message list is empty', () => {
    const html = renderToStaticMarkup(<DataQualityBanner messages={[]} />);
    expect(html).toBe('');
  });

  it('honours a custom title', () => {
    const html = renderToStaticMarkup(
      <DataQualityBanner title="Provisional figures" messages={['pending close']} />,
    );
    expect(html).toContain('Provisional figures');
  });
});

describe('DataQualityCard', () => {
  it('renders the boxed card variant with its messages', () => {
    const html = renderToStaticMarkup(
      <DataQualityCard level="info" messages={['figures are provisional']} />,
    );
    expect(html).toContain('figures are provisional');
    // Default heading.
    expect(html).toContain('Data quality');
  });

  it('renders nothing when the message list is empty', () => {
    const html = renderToStaticMarkup(<DataQualityCard messages={[]} />);
    expect(html).toBe('');
  });
});
