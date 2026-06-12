'use client';

/**
 * Lightweight i18n + display preferences for the Mobile Attendant App
 * (Phase 6b, PRD §15.1/§15.2). Deliberately NOT a router-level i18n refactor:
 * locale is a device-local user preference (no URL segments), the
 * dictionaries are two small typed modules (en/sw), and switching is an
 * in-memory context update — instant, no reload, fully offline-safe.
 *
 * Scope: the attendant tree (apps/web/src/app/attendant) plus the login
 * page's attendant affordance. The desktop dashboard is untouched.
 *
 * Type safety: `useT()` returns the whole `Messages` object, so message
 * access is plain typed property access (`t.home.ctaCheckIn`) — an unknown
 * key fails `tsc`, and a locale missing a key fails in sw.ts itself.
 *
 * Persistence: localStorage, read AFTER mount (never during SSR/hydration so
 * server and first client render agree), written through on every change —
 * the choice survives offline restarts of the PWA.
 */

import { createContext, useCallback, useContext, useEffect, useMemo, useState } from 'react';

import { en, type Messages } from './messages/en';
import { sw } from './messages/sw';

export type { Messages };

export type Locale = 'en' | 'sw';
export type TextSize = 'normal' | 'large';
export type Contrast = 'normal' | 'high';

export const LOCALE_STORAGE_KEY = 'fg.attendant.locale';
export const TEXT_SIZE_STORAGE_KEY = 'fg.attendant.text-size';
export const CONTRAST_STORAGE_KEY = 'fg.attendant.contrast';

export const LOCALES: readonly Locale[] = ['en', 'sw'];
const TEXT_SIZES: readonly TextSize[] = ['normal', 'large'];
const CONTRASTS: readonly Contrast[] = ['normal', 'high'];

export const dictionaries: Record<Locale, Messages> = { en, sw };

export interface AttendantPrefs {
  locale: Locale;
  textSize: TextSize;
  contrast: Contrast;
  setLocale: (locale: Locale) => void;
  setTextSize: (size: TextSize) => void;
  setContrast: (contrast: Contrast) => void;
}

/**
 * The default context value keeps everything working without a provider
 * (e.g. component tests rendering a single screen): English, normal display,
 * no-op setters.
 */
const defaultPrefs: AttendantPrefs = {
  locale: 'en',
  textSize: 'normal',
  contrast: 'normal',
  setLocale: () => {},
  setTextSize: () => {},
  setContrast: () => {},
};

const PrefsContext = createContext<AttendantPrefs>(defaultPrefs);

/** Read a persisted choice; anything unknown/unreadable falls back to null. */
function readStored<T extends string>(key: string, allowed: readonly T[]): T | null {
  try {
    const value = window.localStorage.getItem(key);
    return value != null && (allowed as readonly string[]).includes(value) ? (value as T) : null;
  } catch {
    return null; // storage blocked (private mode) — defaults apply
  }
}

function writeStored(key: string, value: string): void {
  try {
    window.localStorage.setItem(key, value);
  } catch {
    // Storage blocked — the in-memory choice still applies for this session.
  }
}

export function AttendantPrefsProvider({ children }: { children: React.ReactNode }) {
  const [locale, setLocaleState] = useState<Locale>('en');
  const [textSize, setTextSizeState] = useState<TextSize>('normal');
  const [contrast, setContrastState] = useState<Contrast>('normal');

  // Hydration-safe: the persisted choices are applied after mount.
  useEffect(() => {
    const storedLocale = readStored(LOCALE_STORAGE_KEY, LOCALES);
    if (storedLocale) setLocaleState(storedLocale);
    const storedSize = readStored(TEXT_SIZE_STORAGE_KEY, TEXT_SIZES);
    if (storedSize) setTextSizeState(storedSize);
    const storedContrast = readStored(CONTRAST_STORAGE_KEY, CONTRASTS);
    if (storedContrast) setContrastState(storedContrast);
  }, []);

  const setLocale = useCallback((next: Locale) => {
    setLocaleState(next);
    writeStored(LOCALE_STORAGE_KEY, next);
  }, []);
  const setTextSize = useCallback((next: TextSize) => {
    setTextSizeState(next);
    writeStored(TEXT_SIZE_STORAGE_KEY, next);
  }, []);
  const setContrast = useCallback((next: Contrast) => {
    setContrastState(next);
    writeStored(CONTRAST_STORAGE_KEY, next);
  }, []);

  const value = useMemo<AttendantPrefs>(
    () => ({ locale, textSize, contrast, setLocale, setTextSize, setContrast }),
    [locale, textSize, contrast, setLocale, setTextSize, setContrast],
  );

  return <PrefsContext.Provider value={value}>{children}</PrefsContext.Provider>;
}

/** The full preference set + setters (language selector, settings sheet). */
export function useAttendantPrefs(): AttendantPrefs {
  return useContext(PrefsContext);
}

/** The active dictionary. Message access is fully typed property access. */
export function useT(): Messages {
  return dictionaries[useContext(PrefsContext).locale];
}
