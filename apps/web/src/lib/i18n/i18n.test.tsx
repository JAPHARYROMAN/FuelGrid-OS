import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import * as React from 'react';

import {
  AttendantPrefsProvider,
  CONTRAST_STORAGE_KEY,
  LOCALE_STORAGE_KEY,
  TEXT_SIZE_STORAGE_KEY,
  useAttendantPrefs,
  useT,
} from './index';
import { en } from './messages/en';
import { sw } from './messages/sw';

/**
 * i18n foundation tests (Mobile Attendant Phase 6b): dictionary parity,
 * locale/display persistence, instant switching, and provider-less defaults.
 *
 * Missing-key safety is primarily TYPE-level — sw.ts is declared
 * `const sw: Messages` (Messages = typeof en), so a key present in en but
 * absent in sw fails `pnpm typecheck`. The runtime parity test below is the
 * belt-and-braces companion: it walks both trees and compares key paths and
 * leaf kinds (string vs function).
 */

function leafPaths(value: unknown, prefix: string, out: Map<string, string>): void {
  if (typeof value === 'string') {
    out.set(prefix, 'string');
    return;
  }
  if (typeof value === 'function') {
    out.set(prefix, 'function');
    return;
  }
  if (value !== null && typeof value === 'object') {
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      leafPaths(child, prefix === '' ? key : `${prefix}.${key}`, out);
    }
    return;
  }
  throw new Error(`unexpected message leaf at ${prefix}: ${typeof value}`);
}

describe('dictionary parity', () => {
  it('en and sw expose identical key paths with identical leaf kinds', () => {
    const enPaths = new Map<string, string>();
    const swPaths = new Map<string, string>();
    leafPaths(en, '', enPaths);
    leafPaths(sw, '', swPaths);

    const missingInSw = [...enPaths.keys()].filter((k) => !swPaths.has(k));
    const extraInSw = [...swPaths.keys()].filter((k) => !enPaths.has(k));
    expect(missingInSw).toEqual([]);
    expect(extraInSw).toEqual([]);
    for (const [path, kind] of enPaths) {
      expect(swPaths.get(path), `leaf kind mismatch at ${path}`).toBe(kind);
    }
  });

  it('keeps glossary-critical Swahili terms in place', () => {
    expect(sw.home.ctaCheckIn).toBe('Ingia kazini');
    expect(sw.complete.checkOut).toBe('Toka kazini');
    expect(sw.collections.title).toBe('Makusanyo');
    expect(sw.collections.shortage('5,000.00')).toContain('Upungufu');
    expect(sw.collections.excess('5,000.00')).toContain('Ziada');
    expect(sw.home.slotShiftHeader('morning')).toBe('Zamu ya asubuhi');
    expect(sw.opening.reportIssue).toContain('Ripoti tatizo');
  });
});

function Probe() {
  const t = useT();
  const prefs = useAttendantPrefs();
  return (
    <div>
      <span data-testid="cta">{t.home.ctaCheckIn}</span>
      <span data-testid="text-size">{prefs.textSize}</span>
      <span data-testid="contrast">{prefs.contrast}</span>
      <button onClick={() => prefs.setLocale('sw')}>switch-sw</button>
      <button onClick={() => prefs.setLocale('en')}>switch-en</button>
      <button onClick={() => prefs.setTextSize('large')}>large</button>
      <button onClick={() => prefs.setContrast('high')}>high</button>
    </div>
  );
}

describe('AttendantPrefsProvider', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('defaults to English / normal display with nothing stored', () => {
    render(
      <AttendantPrefsProvider>
        <Probe />
      </AttendantPrefsProvider>,
    );
    expect(screen.getByTestId('cta')).toHaveTextContent('Check in');
    expect(screen.getByTestId('text-size')).toHaveTextContent('normal');
    expect(screen.getByTestId('contrast')).toHaveTextContent('normal');
  });

  it('applies persisted choices after mount (offline-safe localStorage)', async () => {
    localStorage.setItem(LOCALE_STORAGE_KEY, 'sw');
    localStorage.setItem(TEXT_SIZE_STORAGE_KEY, 'large');
    localStorage.setItem(CONTRAST_STORAGE_KEY, 'high');
    render(
      <AttendantPrefsProvider>
        <Probe />
      </AttendantPrefsProvider>,
    );
    expect(await screen.findByText('Ingia kazini')).toBeInTheDocument();
    expect(screen.getByTestId('text-size')).toHaveTextContent('large');
    expect(screen.getByTestId('contrast')).toHaveTextContent('high');
  });

  it('switches language instantly and persists every preference', async () => {
    render(
      <AttendantPrefsProvider>
        <Probe />
      </AttendantPrefsProvider>,
    );
    await userEvent.click(screen.getByText('switch-sw'));
    expect(screen.getByTestId('cta')).toHaveTextContent('Ingia kazini');
    expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('sw');

    await userEvent.click(screen.getByText('switch-en'));
    expect(screen.getByTestId('cta')).toHaveTextContent('Check in');
    expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('en');

    await userEvent.click(screen.getByText('large'));
    expect(localStorage.getItem(TEXT_SIZE_STORAGE_KEY)).toBe('large');
    await userEvent.click(screen.getByText('high'));
    expect(localStorage.getItem(CONTRAST_STORAGE_KEY)).toBe('high');
  });

  it('ignores unknown stored values (falls back to defaults)', () => {
    localStorage.setItem(LOCALE_STORAGE_KEY, 'fr');
    localStorage.setItem(TEXT_SIZE_STORAGE_KEY, 'huge');
    render(
      <AttendantPrefsProvider>
        <Probe />
      </AttendantPrefsProvider>,
    );
    expect(screen.getByTestId('cta')).toHaveTextContent('Check in');
    expect(screen.getByTestId('text-size')).toHaveTextContent('normal');
  });

  it('works without a provider: English defaults, no-op setters (test envs)', async () => {
    render(<Probe />);
    expect(screen.getByTestId('cta')).toHaveTextContent('Check in');
    await userEvent.click(screen.getByText('switch-sw'));
    // No provider — the setter is a no-op and nothing crashes.
    expect(screen.getByTestId('cta')).toHaveTextContent('Check in');
  });
});
