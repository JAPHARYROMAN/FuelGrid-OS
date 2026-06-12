import { beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import * as React from 'react';

vi.mock('@/lib/api', () => ({
  api: {
    attendantCurrentShift: vi.fn().mockResolvedValue({}),
    checkInToShift: vi.fn(),
    checkOutOfShift: vi.fn(),
    confirmNozzleAssignment: vi.fn(),
    captureMeterReading: vi.fn(),
    submitCash: vi.fn(),
  },
}));

import {
  AttendantPrefsProvider,
  CONTRAST_STORAGE_KEY,
  LOCALE_STORAGE_KEY,
  TEXT_SIZE_STORAGE_KEY,
} from '@/lib/i18n';
import { resetSyncEngineForTests } from '@/lib/offline';

import { AttendantShell } from './attendant-shell';

/**
 * Attendant shell tests (Phase 6b): the accessibility modes land as data
 * attributes on the shell root, the "Display & language" sheet applies every
 * choice instantly + persists it, and the language switch re-renders the
 * chrome without a reload.
 */

function renderShell() {
  return render(
    <AttendantPrefsProvider>
      <AttendantShell>
        <div>page-content</div>
      </AttendantShell>
    </AttendantPrefsProvider>,
  );
}

function shellRoot(container: HTMLElement): HTMLElement {
  const root = container.querySelector<HTMLElement>('[data-text-size]');
  if (!root) throw new Error('shell root with data-text-size not found');
  return root;
}

describe('AttendantShell display & language', () => {
  beforeEach(() => {
    localStorage.clear();
    resetSyncEngineForTests();
  });

  it('renders normal modes by default and hosts the settings affordance', () => {
    const { container } = renderShell();
    const root = shellRoot(container);
    expect(root).toHaveAttribute('data-text-size', 'normal');
    expect(root).toHaveAttribute('data-contrast', 'normal');
    expect(screen.getByText('Attendant')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Display & language' })).toBeInTheDocument();
  });

  it('applies large text + high contrast from the sheet and persists them', async () => {
    const { container } = renderShell();
    await userEvent.click(screen.getByRole('button', { name: 'Display & language' }));
    const dialog = await screen.findByRole('dialog', { name: 'Display & language' });
    expect(dialog).toBeInTheDocument();

    // Text size → Large (instant + persisted).
    await userEvent.click(screen.getByRole('button', { name: 'Large' }));
    expect(shellRoot(container)).toHaveAttribute('data-text-size', 'large');
    expect(localStorage.getItem(TEXT_SIZE_STORAGE_KEY)).toBe('large');

    // Contrast → High (instant + persisted).
    await userEvent.click(screen.getByRole('button', { name: 'High' }));
    expect(shellRoot(container)).toHaveAttribute('data-contrast', 'high');
    expect(localStorage.getItem(CONTRAST_STORAGE_KEY)).toBe('high');

    // The active options are marked, not colour-only.
    expect(screen.getByRole('button', { name: 'Large' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'High' })).toHaveAttribute('aria-pressed', 'true');
  });

  it('switches the whole chrome to Swahili instantly (no reload) and persists', async () => {
    renderShell();
    await userEvent.click(screen.getByRole('button', { name: 'Display & language' }));
    await userEvent.click(await screen.findByRole('button', { name: 'Kiswahili' }));

    // Header brand + sheet title flip immediately.
    expect(screen.getByText('Mhudumu')).toBeInTheDocument();
    expect(screen.getByRole('dialog', { name: 'Mwonekano na lugha' })).toBeInTheDocument();
    expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('sw');

    // Closing via the translated Done button.
    await userEvent.click(screen.getByRole('button', { name: 'Sawa' }));
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('restores persisted modes on a fresh mount (offline restart)', async () => {
    localStorage.setItem(LOCALE_STORAGE_KEY, 'sw');
    localStorage.setItem(TEXT_SIZE_STORAGE_KEY, 'large');
    localStorage.setItem(CONTRAST_STORAGE_KEY, 'high');
    const { container } = renderShell();

    expect(await screen.findByText('Mhudumu')).toBeInTheDocument();
    const root = shellRoot(container);
    expect(root).toHaveAttribute('data-text-size', 'large');
    expect(root).toHaveAttribute('data-contrast', 'high');
  });

  it('traps focus in the settings sheet and closes on Escape', async () => {
    renderShell();
    await userEvent.click(screen.getByRole('button', { name: 'Display & language' }));
    const dialog = await screen.findByRole('dialog', { name: 'Display & language' });

    // Focus moved into the sheet on open.
    expect(dialog.contains(document.activeElement)).toBe(true);

    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
    // Focus returns to the opener.
    expect(screen.getByRole('button', { name: 'Display & language' })).toHaveFocus();
  });
});
