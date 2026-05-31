import { renderToStaticMarkup } from 'react-dom/server';
import { describe, expect, it } from 'vitest';

import { Badge } from '../components/badge';
import { cn } from '../lib/cn';

// Smoke test: proves the vitest + jsdom harness is wired for the ui package.
// Kept intentionally trivial — broader component coverage is a later wave.
describe('ui harness smoke', () => {
  it('runs in a jsdom environment (document is available)', () => {
    expect(typeof document).toBe('object');
    expect(document.createElement('div').tagName).toBe('DIV');
  });

  it('cn merges classes and lets the last conflicting utility win', () => {
    const isHidden = false;
    expect(cn('px-2', 'px-4')).toBe('px-4');
    expect(cn('text-sm', isHidden && 'hidden', 'font-medium')).toBe('text-sm font-medium');
  });

  it('renders a Badge component to markup', () => {
    const html = renderToStaticMarkup(<Badge tone="success">Active</Badge>);
    expect(html).toContain('Active');
    expect(html).toContain('<span');
  });
});
