-- Reverse of 0001_init.

DROP TABLE IF EXISTS stations;
DROP TABLE IF EXISTS regions;
DROP TABLE IF EXISTS companies;
DROP TABLE IF EXISTS tenants;
DROP FUNCTION IF EXISTS set_updated_at();
-- pgcrypto is intentionally left in place; it is harmless and likely needed
-- by any future schema that uses gen_random_uuid().
