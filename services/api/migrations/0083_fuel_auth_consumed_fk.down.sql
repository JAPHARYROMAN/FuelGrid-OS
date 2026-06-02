-- Reverse 0083_fuel_auth_consumed_fk: drop the consumed_by referential integrity.
ALTER TABLE fuel_authorizations
    DROP CONSTRAINT fuel_auth_consumed_by_fk;
