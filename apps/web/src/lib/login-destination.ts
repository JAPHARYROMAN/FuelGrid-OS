import type { MePermissions } from '@fuelgrid/sdk';

import { safeRedirect } from './safe-redirect';

const ATTENDANT_PERMISSION_CODES = new Set([
  'cash.submit',
  'incidents.report',
  'payment.record',
  'pricing.read',
  'reading.edit',
  'shift.open',
]);

/**
 * Pump attendants use the focused mobile workspace. An explicit, safe `next`
 * destination always wins; otherwise users whose access is limited to the
 * attendant permission bundle land on the attendant app instead of the
 * desktop command centre.
 */
export function loginDestination(next: string | null, access: MePermissions): string {
  if (next) return safeRedirect(next);

  const codes = access.permissions.map((permission) => permission.code);
  const attendantOnly =
    !access.is_system_admin &&
    codes.includes('shift.open') &&
    codes.includes('reading.edit') &&
    codes.every((code) => ATTENDANT_PERMISSION_CODES.has(code));

  return attendantOnly ? '/attendant' : '/command-center';
}

/** A hard navigation makes the server-side cookie gate validate the new session. */
export function navigateAfterLogin(destination: string): void {
  window.location.replace(destination);
}
