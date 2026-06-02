-- 0083_fuel_auth_consumed_fk (W1-FLEET-FK): give fuel_authorizations.consumed_by
-- referential integrity. consumed_by names the Phase-6 sale that consumed the
-- authorization; until now nothing stopped a fulfillment from pointing at a
-- non-existent (or cross-tenant) sale id. A composite FK on (tenant_id,
-- consumed_by) -> sales(tenant_id, id) closes that gap. ON DELETE RESTRICT keeps
-- a consumed sale from being deleted out from under its authorization. NULLs are
-- allowed so unfulfilled authorizations (consumed_by IS NULL) are unaffected; a
-- composite FK is not enforced when any referencing column is NULL (MATCH
-- SIMPLE, the Postgres default).
ALTER TABLE fuel_authorizations
    ADD CONSTRAINT fuel_auth_consumed_by_fk
        FOREIGN KEY (tenant_id, consumed_by)
        REFERENCES sales (tenant_id, id)
        ON DELETE RESTRICT;
