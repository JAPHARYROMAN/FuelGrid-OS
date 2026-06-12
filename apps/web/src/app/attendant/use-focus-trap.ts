'use client';

import { useEffect, useRef } from 'react';

const FOCUSABLE =
  'a[href], button:not([disabled]), input:not([disabled]), select:not([disabled]), textarea:not([disabled]), [tabindex]:not([tabindex="-1"])';

/**
 * Minimal focus trap for the attendant bottom sheets (sync details, display
 * settings) — they are hand-rolled dialogs, not the Radix-based desktop
 * Dialog, so they need their own trap (PRD §15 accessibility):
 *
 *   - focus moves into the sheet on open (first focusable element);
 *   - Tab / Shift+Tab cycle within the sheet;
 *   - Escape closes it;
 *   - focus returns to the previously focused element on close.
 *
 * Attach the returned ref to the sheet's panel element.
 */
export function useSheetFocusTrap(onClose: () => void) {
  const ref = useRef<HTMLDivElement>(null);
  // Latest-callback ref so the trap effect runs once per mount even when the
  // caller passes a fresh closure every render.
  const onCloseRef = useRef(onClose);
  useEffect(() => {
    onCloseRef.current = onClose;
  }, [onClose]);

  useEffect(() => {
    const root = ref.current;
    if (!root) return;
    const previous = document.activeElement instanceof HTMLElement ? document.activeElement : null;
    const focusables = () => Array.from(root.querySelectorAll<HTMLElement>(FOCUSABLE));
    (focusables()[0] ?? root).focus();

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.preventDefault();
        onCloseRef.current();
        return;
      }
      if (e.key !== 'Tab') return;
      const items = focusables();
      if (items.length === 0) return;
      const first = items[0] as HTMLElement;
      const last = items[items.length - 1] as HTMLElement;
      const active = document.activeElement;
      if (e.shiftKey && (active === first || !root.contains(active))) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && (active === last || !root.contains(active))) {
        e.preventDefault();
        first.focus();
      }
    };
    document.addEventListener('keydown', onKeyDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown);
      previous?.focus();
    };
  }, []);

  return ref;
}
