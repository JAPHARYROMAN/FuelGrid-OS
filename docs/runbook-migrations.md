# Runbook: Database Migrations

This runbook covers operating the FuelGrid OS schema migration CLI
(`services/api/cmd/migrate`). It explains how to apply migrations, roll back,
recover from a dirty/failed migration, and the confirmation gate that protects
destructive operations.

## Prerequisites

- `DATABASE_URL` must point at the target Postgres database, e.g.
  `postgres://user:pass@host:5432/fuelgrid?sslmode=disable`.
- Migration files are resolved in this order:
  1. `MIGRATIONS_PATH` (if set),
  2. `services/api/migrations` (repo-relative default),
  3. `./migrations`.

Run the CLI with `go run` from the repo root, or build a binary:

```sh
go run ./services/api/cmd/migrate <command> [args]
# or
go build -o migrate ./services/api/cmd/migrate
```

## Commands at a glance

| Command            | Effect                                          | Destructive? |
|--------------------|-------------------------------------------------|--------------|
| `up`               | Apply all pending migrations                    | No           |
| `down`             | Roll back exactly one migration                 | No           |
| `down-all`         | Roll back **every** migration (schema to zero)  | **Yes**      |
| `goto <version>`   | Migrate up or down to a specific version        | Situational  |
| `force <version>`  | Mark a version as applied without running it    | **Yes**      |
| `version`          | Print current version and the dirty flag        | No           |

## The confirmation gate

Destructive commands (`down-all` and `force`) **refuse to run** unless the
operator explicitly opts in. This prevents accidental data loss (rolling the
schema to zero) and silent overrides of the dirty-version recovery marker.

Opt in either way:

- Set the environment variable `MIGRATE_CONFIRM=1`, or
- Pass the `--yes` flag.

Without confirmation the CLI prints a clear refusal and exits non-zero:

```
refusing to run destructive command "down-all" without confirmation;
set MIGRATE_CONFIRM=1 or pass --yes to proceed
```

Non-destructive commands (`up`, `down`, `goto`, `version`) never require
confirmation.

## Apply migrations (normal deploy)

```sh
go run ./services/api/cmd/migrate up
go run ./services/api/cmd/migrate version   # confirm version, dirty=false
```

`up` is idempotent: if there is nothing to apply it exits 0 (no-op).

## Roll back

Roll back a single migration (safe, unguarded):

```sh
go run ./services/api/cmd/migrate down
```

Roll the entire schema back to zero (DESTRUCTIVE — drops all migrated objects):

```sh
MIGRATE_CONFIRM=1 go run ./services/api/cmd/migrate down-all
# or
go run ./services/api/cmd/migrate --yes down-all
```

Migrate to a specific target version (direction inferred from current state):

```sh
go run ./services/api/cmd/migrate goto 7
```

## Recover from a dirty / failed migration

If a migration fails partway, `golang-migrate` marks the version **dirty** and
refuses further `up`/`down` until the state is reconciled.

1. Inspect the current state:

   ```sh
   go run ./services/api/cmd/migrate version
   # e.g. "current version" version=12 dirty=true
   ```

2. Manually inspect the database and finish or undo the partial change so the
   schema actually matches a known version. (This is a human judgement step —
   read the failed migration's SQL and decide which version the schema truly
   reflects.)

3. Clear the dirty flag by forcing the version the schema now matches. `force`
   is destructive (it overwrites the migration bookkeeping without running
   SQL), so it requires confirmation:

   ```sh
   MIGRATE_CONFIRM=1 go run ./services/api/cmd/migrate force 11
   # or
   go run ./services/api/cmd/migrate --yes force 11
   ```

4. Re-run `up` to apply any remaining migrations cleanly:

   ```sh
   go run ./services/api/cmd/migrate up
   go run ./services/api/cmd/migrate version   # dirty=false
   ```

> Caution: `force` does **not** run migration SQL — it only updates the version
> marker. Forcing the wrong version will desync the bookkeeping from the actual
> schema. Always verify the real schema state first.

## CI note

The CI migration job exercises the full lifecycle (apply → version →
roll-back-to-zero → re-apply). Because the roll-back step uses `down-all`, that
specific step sets `MIGRATE_CONFIRM: "1"` in its environment so the guard
allows it. See `.github/workflows/ci.yml`.
