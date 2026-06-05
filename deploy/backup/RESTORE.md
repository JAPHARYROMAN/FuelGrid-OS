# FuelGrid OS — Postgres restore drill

The nightly job (`deploy/backup/pg_backup.sh`, driven by
`fuelgrid-backup.timer`) writes `pg_dump -Fc` (custom-format) gzipped dumps to
`/var/backups/fuelgrid/fuelgrid-<timestamp>.dump.gz`, optionally mirrored to
DigitalOcean Spaces.

This is a **logical backup**: a consistent snapshot of the database **as of the
moment the dump started**. It does **not** provide point-in-time recovery — any
writes after the last successful nightly dump are not recoverable from it. See
[If you need PITR](#if-you-need-pitr) below.

Run a restore drill against a **scratch** database periodically (quarterly is a
reasonable cadence) so you know the dumps are actually restorable before you
need them in anger.

---

## A. Restore drill into a throwaway scratch Postgres (recommended verify)

This proves the dump restores cleanly **without touching production**.

```sh
# 1. Pick a dump to verify (newest local one shown here).
DUMP=$(ls -1t /var/backups/fuelgrid/fuelgrid-*.dump.gz | head -1)
echo "Verifying ${DUMP}"

# 2. Spin up a scratch Postgres 16 (same major as prod), isolated from the stack.
docker run -d --name fuelgrid-restore-test \
  -e POSTGRES_USER=fuelgrid \
  -e POSTGRES_PASSWORD=scratch \
  -e POSTGRES_DB=fuelgrid \
  postgres:16-alpine

# Wait for it to accept connections.
until docker exec fuelgrid-restore-test pg_isready -U fuelgrid -d fuelgrid; do sleep 1; done

# 3. Restore the dump. --clean --if-exists makes it idempotent; -Fc dumps are
#    consumed by pg_restore (NOT psql). gunzip the stream first.
gunzip -c "${DUMP}" \
  | docker exec -i fuelgrid-restore-test \
      pg_restore -U fuelgrid -d fuelgrid --clean --if-exists --no-owner

# 4. Verify: schema_migrations version + a few row counts.
docker exec fuelgrid-restore-test psql -U fuelgrid -d fuelgrid -c \
  "SELECT version, dirty FROM schema_migrations;"
docker exec fuelgrid-restore-test psql -U fuelgrid -d fuelgrid -c \
  "SELECT (SELECT count(*) FROM tenants)  AS tenants,
          (SELECT count(*) FROM users)    AS users,
          (SELECT count(*) FROM stations) AS stations;"

# 5. Tear down the scratch DB.
docker rm -f fuelgrid-restore-test
```

If `pg_restore` exits 0 (a few "already exists" notices under `--clean` are
fine) and the counts look sane, the dump is good.

> The `fuelgrid_app` role is created by migration `0005_rls.up.sql`, which is
> part of the schema captured in the dump, so the restored DB recreates it. The
> scratch container is owner-only and never serves real traffic, so RLS/non-owner
> wiring is irrelevant for the drill.

---

## B. Real production restore (disaster recovery)

**Destructive — this overwrites the live database. Take a fresh dump first if
the DB is still reachable.**

```sh
cd /opt/fuelgrid    # where docker-compose.prod.yml + .env live

# 1. Stop the app tiers so nothing writes mid-restore (leave postgres running).
docker compose -f docker-compose.prod.yml stop api web caddy

# 2. Pick the dump to restore (local, or pull from Spaces first).
DUMP=$(ls -1t /var/backups/fuelgrid/fuelgrid-*.dump.gz | head -1)

# 3. Restore into the live postgres container as the owner.
gunzip -c "${DUMP}" \
  | docker compose -f docker-compose.prod.yml exec -T postgres \
      pg_restore -U fuelgrid -d fuelgrid --clean --if-exists --no-owner

# 4. Re-set the non-owner app role password (the dump recreates the role with
#    its weak default), keeping DATABASE_APP_URL in .env in sync.
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U fuelgrid -c "ALTER ROLE fuelgrid_app PASSWORD '<DATABASE_APP_URL password>';"

# 5. Bring the app back up and confirm health.
docker compose -f docker-compose.prod.yml up -d
curl -fsS https://<API_DOMAIN>/readyz    # expect 200 {"status":"ready"}
```

If you instead restore into a **fresh** database (new volume), run migrations
first (`docker compose -f docker-compose.prod.yml run --rm migrate`) only if the
dump is schema-less; a `-Fc` dump from `pg_dump` already contains the full
schema, so a plain `pg_restore` into an empty DB is sufficient — do **not** also
run `migrate up` against a restored dump or you risk a dirty migration state.

---

## If you need PITR

Logical dumps lose everything written since the last nightly run (up to ~24h of
data at the default cadence). If your recovery point objective is tighter than
that, you need **point-in-time recovery**, which logical dumps cannot provide.
Two paths:

1. **WAL archiving on the self-hosted Postgres.** Enable continuous archiving so
   the base backup + WAL stream can replay to any moment:
   - Set `archive_mode = on` and an `archive_command` that ships completed WAL
     segments to DigitalOcean Spaces (e.g. via `aws s3 cp --endpoint-url ...`),
     plus periodic `pg_basebackup` base backups.
   - Restore = restore the base backup, then replay archived WAL up to the
     target time (`recovery_target_time`). This is more moving parts to own.

2. **Migrate Postgres to DigitalOcean Managed Databases.** DO Managed Postgres
   provides automated daily backups **and** PITR (within the retention window)
   with no WAL plumbing to maintain — repoint `DATABASE_URL` /
   `DATABASE_APP_URL` at the managed cluster (use `sslmode=require`) and drop the
   `postgres` service from the compose stack. This is the recommended path once
   the RPO requirement justifies leaving self-hosted PG.
