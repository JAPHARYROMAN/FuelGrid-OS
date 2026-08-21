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

export function isAttendantOnlyAccess(access: MePermissions): boolean {
  if (typeof access.is_attendant_only === 'boolean') return access.is_attendant_only;

  if (access.roles?.length) {
    return access.roles.length === 1 && access.roles[0] === 'attendant';
  }

  const codes = access.permissions.map((permission) => permission.code);
  return (
    !access.is_system_admin &&
    codes.includes('shift.open') &&
    codes.includes('reading.edit') &&
    codes.every((code) => ATTENDANT_PERMISSION_CODES.has(code))
  );
}

function isAttendantPath(path: string): boolean {
  return (
    path === '/attendant' ||
    path.startsWith('/attendant/') ||
    path.startsWith('/attendant?') ||
    path.startsWith('/attendant#')
  );
}

/**
 * Pump attendants use the focused mobile workspace. A safe `next` destination
 * is honoured only when it stays inside that workspace; dedicated attendant
 * accounts can never use login routing to enter the desktop application.
 */
export function loginDestination(next: string | null, access: MePermissions): string {
  const requested = next ? safeRedirect(next) : null;

  if (isAttendantOnlyAccess(access)) {
    return requested && isAttendantPath(requested) ? requested : '/attendant';
  }

  return requested ?? '/command-center';
}

/** A hard navigation makes the server-side cookie gate validate the new session. */
export function navigateAfterLogin(destination: string): void {
  window.location.replace(destination);
}
