'use client';

import { useSyncExternalStore } from 'react';

/**
 * Minimal dependency-free toast store. The app ships no toast library, so
 * this is a tiny pub/sub that the <Toaster /> subscribes to and any code
 * can push into via `toast.error(...)` / `toast.success(...)`.
 *
 * Toasts are the visible failure surface for mutations (PAGE-008): pages
 * that previously had a silent `onSuccess`-only mutation now get a toast on
 * failure, either explicitly or via the global MutationCache onError.
 */

export type ToastTone = 'error' | 'success' | 'info';

export interface Toast {
  id: number;
  tone: ToastTone;
  title: string;
  description?: string;
}

let toasts: Toast[] = [];
let nextId = 1;
const listeners = new Set<() => void>();

function emit() {
  // New array identity so useSyncExternalStore sees a change.
  toasts = [...toasts];
  for (const l of listeners) l();
}

function remove(id: number) {
  const next = toasts.filter((t) => t.id !== id);
  if (next.length === toasts.length) return;
  toasts = next;
  emit();
}

function push(tone: ToastTone, title: string, description?: string): number {
  const id = nextId++;
  toasts = [...toasts, { id, tone, title, description }];
  emit();
  // Auto-dismiss; errors linger a little longer than confirmations.
  if (typeof window !== 'undefined') {
    window.setTimeout(() => remove(id), tone === 'error' ? 6000 : 4000);
  }
  return id;
}

export const toast = {
  error: (title: string, description?: string) => push('error', title, description),
  success: (title: string, description?: string) => push('success', title, description),
  info: (title: string, description?: string) => push('info', title, description),
  dismiss: remove,
};

function subscribe(listener: () => void) {
  listeners.add(listener);
  return () => listeners.delete(listener);
}

function getSnapshot() {
  return toasts;
}

const EMPTY: Toast[] = [];
function getServerSnapshot() {
  return EMPTY;
}

/** Subscribe a component to the live toast list. */
export function useToasts(): Toast[] {
  return useSyncExternalStore(subscribe, getSnapshot, getServerSnapshot);
}
