import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';

import { ManualDownload } from './manual-download';

/**
 * The login-page "Download the Supervisor Manual (PDF)" affordance: it must be
 * an accessible link that points at the static PDF asset and triggers a
 * download rather than an in-tab render.
 */
describe('ManualDownload', () => {
  it('renders an accessible download link to the static PDF asset', () => {
    render(<ManualDownload />);

    const link = screen.getByRole('link', { name: /download the supervisor manual \(pdf\)/i });
    expect(link).toBeInTheDocument();
    expect(link).toHaveAttribute('href', '/supervisor-operations-manual.pdf');
    // The `download` attribute makes the browser save the file instead of
    // opening it; in the DOM it surfaces as an empty-string attribute.
    expect(link).toHaveAttribute('download', '');
    expect(link).toHaveAttribute('rel', 'noopener');
  });
});
