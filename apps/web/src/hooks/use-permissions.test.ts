import { describe, expect, it } from 'vitest';

import type { MePermissions } from '@fuelgrid/sdk';

import { canUsePermission } from './use-permissions';

const basePermissions: MePermissions = {
  permissions: [
    { code: 'products.manage', station_scoped: false },
    { code: 'station.read', station_scoped: true },
  ],
  station_ids: ['station-1'],
  tenant_wide: false,
};

describe('canUsePermission', () => {
  it('allows tenant-level permissions without a station target', () => {
    expect(canUsePermission(basePermissions, 'products.manage')).toBe(true);
  });

  it('denies station-scoped target checks without a station target', () => {
    expect(canUsePermission(basePermissions, 'station.read')).toBe(false);
  });

  it('allows held checks when the actor has the station-scoped permission', () => {
    expect(canUsePermission(basePermissions, 'station.read', { mode: 'held' })).toBe(true);
  });

  it('allows station-scoped target checks for assigned stations', () => {
    expect(canUsePermission(basePermissions, 'station.read', { stationID: 'station-1' })).toBe(
      true,
    );
  });

  it('denies missing permissions even in held mode', () => {
    expect(canUsePermission(basePermissions, 'reports.export', { mode: 'held' })).toBe(false);
  });

  it('allows system admin regardless of explicit permission or station scope', () => {
    expect(
      canUsePermission(
        { ...basePermissions, permissions: [], station_ids: [], is_system_admin: true },
        'stock.approve_adjustment',
      ),
    ).toBe(true);
  });
});
