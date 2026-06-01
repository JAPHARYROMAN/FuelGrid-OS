-- Reverse 0077_workforce.

ALTER TABLE shifts DROP CONSTRAINT IF EXISTS shifts_team_fk;
ALTER TABLE shifts DROP CONSTRAINT IF EXISTS chk_shifts_slot;
DROP INDEX IF EXISTS idx_shifts_team_id;
ALTER TABLE shifts DROP COLUMN IF EXISTS team_id;
ALTER TABLE shifts DROP COLUMN IF EXISTS slot;

ALTER TABLE stations DROP COLUMN IF EXISTS rotation_anchor_date;

DROP TABLE IF EXISTS shift_team_members;
DROP TABLE IF EXISTS shift_teams;
DROP TABLE IF EXISTS employees;
