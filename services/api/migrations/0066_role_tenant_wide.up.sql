-- 0066_role_tenant_wide: make tenant-wide reach an EXPLICIT role property (AUTH-20).
--
-- Previously a user with zero user_station_access rows was treated as
-- tenant-wide for every station-scoped permission their roles granted. That
-- fails open: a misconfigured user, or one whose last station grant is
-- revoked, is silently promoted to every station in the tenant. Authority is
-- now explicit — only holders of a role flagged tenant_wide see across the
-- whole tenant; everyone else is confined to their user_station_access rows,
-- and zero rows means zero station-scoped access (default-deny).

ALTER TABLE roles ADD COLUMN tenant_wide boolean NOT NULL DEFAULT false;

-- Back-office and leadership system roles operate across the whole tenant by
-- design (network-wide visibility, finance, procurement, audit, admin).
-- Operational roles (attendant, supervisor, station_manager, regional_manager)
-- stay station-scoped through user_station_access.
UPDATE roles
   SET tenant_wide = true
 WHERE is_system
   AND code IN ('system_admin', 'executive', 'auditor', 'finance_officer', 'procurement_officer');
