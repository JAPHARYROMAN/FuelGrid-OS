import { type ClassValue, clsx } from 'clsx';
import { twMerge } from 'tailwind-merge';

/**
 * cn merges Tailwind class strings, deduplicating conflicts (e.g. last
 * `bg-*` wins). Use it whenever a component composes class names from
 * variants and caller overrides.
 */
export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
