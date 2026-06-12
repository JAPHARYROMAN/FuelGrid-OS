import { beforeEach, describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import * as React from 'react';

import { LOCALE_STORAGE_KEY } from '@/lib/i18n';

import { AttendantInstall } from './attendant-install';

/**
 * Login-page attendant affordance (Phase 6b): the language selector sits
 * right next to the QR affordance (PRD §15.2 "easy to find"), switches the
 * affordance instantly, and shares the attendant app's persisted locale.
 */
describe('AttendantInstall language selector', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('renders the English affordance with the selector alongside', () => {
    render(<AttendantInstall />);
    expect(screen.getByText('Pump attendant? Install the app')).toBeInTheDocument();
    const group = screen.getByRole('group', { name: 'Language' });
    expect(group).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'English' })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: 'Kiswahili' })).toHaveAttribute(
      'aria-pressed',
      'false',
    );
  });

  it('switches to Swahili instantly and persists the shared locale', async () => {
    render(<AttendantInstall />);
    await userEvent.click(screen.getByRole('button', { name: 'Kiswahili' }));

    expect(screen.getByText('Mhudumu wa pampu? Sakinisha programu')).toBeInTheDocument();
    expect(screen.getByRole('group', { name: 'Lugha' })).toBeInTheDocument();
    expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('sw');
  });

  it('restores a persisted Swahili choice on mount', async () => {
    localStorage.setItem(LOCALE_STORAGE_KEY, 'sw');
    render(<AttendantInstall />);
    expect(await screen.findByText('Mhudumu wa pampu? Sakinisha programu')).toBeInTheDocument();
  });

  it('still expands the QR panel with translated instructions', async () => {
    localStorage.setItem(LOCALE_STORAGE_KEY, 'sw');
    render(<AttendantInstall />);
    await userEvent.click(await screen.findByText('Mhudumu wa pampu? Sakinisha programu'));
    expect(screen.getByText('Ongeza kwenye Skrini ya Mwanzo')).toBeInTheDocument();
  });
});
