# Runbook: Database unavailable / readiness failing / pool saturated

**Alerts:** `ApiReadinessFailing` (target down or `/readyz` 503 for 3m),
`DbPoolSaturation` (acquired/max > 90% for 5m).

**Severity:** critical (readiness) / warning (saturation).

## Background

The API's readiness probe `GET /readyz` pings Postgres (and Redis if
configured) with a 2s timeout. A single failing dependency returns **503** and
names the failing dep in the JSON body (`checks.postgres = "unreachable: ..."`).
Orchestrators stop routing traffic to instances whose readiness fails.

Postgres connectivity is via a pgx pool (`internal/database/postgres.go`).

## First 5 minutes — triage

1. Hit readiness directly to see which dep is down:
   ```bash
   curl -s http://<instance>:8080/readyz | jq .
   ```
   Look at `checks.postgres` / `checks.redis`.
2. Distinguish the two failure modes:
   - **Connectivity (readiness 503):** DB is unreachable / refusing connections.
   - **Saturation (pool > 90%):** DB is up but every connection is in use, so
     requests queue and latency climbs.

## Connectivity outage (readiness 503)

1. Can the DB host be reached at all?
   ```bash
   psql "$DATABASE_URL" -c 'select 1;'
   ```
2. Check the database provider's status/console:
   - Is the instance up, failing over, or out of disk?
   - Recent maintenance / credential rotation?
3. Check the connection string / secret — a rotated password or changed host
   manifests as immediate connection failures after a deploy.
4. Check max-connections at the server: if the DB is rejecting new connections
   with "too many clients", the cause may actually be pool/leak related (below).

**Mitigation:** restore the DB (provider failover / restart), fix the
secret, or roll back a deploy that changed connection config. Once Postgres is
reachable, `/readyz` returns 200 automatically and traffic resumes.

## Pool saturation (DbPoolSaturation)

1. Confirm via the **DB connection pool** dashboard panel: acquired ≈ max.
2. Find what is holding connections:
   ```sql
   SELECT pid, state, wait_event_type, now() - query_start AS age, left(query, 120)
   FROM pg_stat_activity
   WHERE datname = current_database() AND state <> 'idle'
   ORDER BY age DESC
   LIMIT 20;
   ```
3. Causes & actions:
   - **Long-running / missing-index queries:** optimise or add an index; kill a
     runaway with `SELECT pg_cancel_backend(<pid>)`.
   - **Connection leak (transactions/rows not closed):** look for code paths
     that acquire but never release; restart instances as a stopgap.
   - **Genuinely undersized pool:** raise `MaxOpenConns`
     (see `internal/database/postgres.go` config) within the DB's max-connections
     budget, then redeploy.

> Note: the pool gauges (`pgxpool_acquired_conns` / `pgxpool_max_conns`) require
> the pgx stats collector to be wired into `internal/observability/metrics.go`.
> Until then, infer saturation from latency + DB-side `pg_stat_activity`.

## Recovery & follow-up

- Confirm `/readyz` returns 200 and `up{job="fuelgrid-api"}` is 1.
- Confirm the DB pool panel is well below the saturation threshold.
- Post-incident: capacity review, query/index fix, or wiring the missing pool
  metrics so this is observable next time.
