import { describe, expect, it } from 'vitest';

import type { MePermissions } from '@fuelgrid/sdk';

import { loginDestination } from './login-destination';

const attendantAccess: MePermissions = {
  permissions: [
    { code: 'cash.submit', station_scoped: true },
    { code: 'incidents.report', station_scoped: true },
    { code: 'payment.record', station_scoped: true },
    { code: 'pricing.read', station_scoped: true },
    { code: 'reading.edit', station_scoped: true },
    { code: 'shift.open', station_scoped: true },
  ],
  station_ids: ['station-1'],
  tenant_wide: false,
};

describe('loginDestination', () => {
  it('sends an attendant-only account to the mobile workspace', () => {
    expect(loginDestination(null, attendantAccess)).toBe('/attendant');
  });

  it('honours an explicit safe destination', () => {
    expect(loginDestination('/attendant/collections', attendantAccess)).toBe(
      '/attendant/collections',
    );
  });

  it('refuses a desktop destination for an attendant-only account', () => {
    expect(loginDestination('/command-center', attendantAccess)).toBe('/attendant');
    expect(loginDestination('/settings/users', attendantAccess)).toBe('/attendant');
  });

  it('uses the authoritative role-based surface marker when present', () => {
    expect(
      loginDestination('/finance', {
        ...attendantAccess,
        permissions: [{ code: 'company.read', station_scoped: false }],
        roles: ['attendant'],
        is_attendant_only: true,
      }),
    ).toBe('/attendant');
  });

  it('keeps broader-access users on the command centre', () => {
    expect(
      loginDestination(null, {
        permissions: [
          ...attendantAccess.permissions,
          { code: 'company.read', station_scoped: false },
        ],
        roles: ['station_manager'],
        is_attendant_only: false,
        tenant_wide: true,
      }),
    ).toBe('/command-center');
  });
});
