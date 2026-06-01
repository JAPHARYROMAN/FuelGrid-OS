# FuelGrid OS — load / performance test harness

A [k6](https://k6.io/) harness for the FuelGrid API. It contains two scenarios:

- **`smoke`** — a single VU doing `login -> GET /me` in a loop. The fast
  "is the auth path alive and within budget?" check. This is the scenario the
  manual CI workflow runs as a gate.
- **`read_heavy`** — ramps virtual users up/down while hitting the hot
  authenticated GETs (stations list, products list, tanks list, the
  command-center / station overview aggregate, and a paginated `audit-logs`
  list) with realistic think-time. Models the dominant production read traffic.

Nothing here runs on every push. The CI workflow is **`workflow_dispatch` only**
(manual). Local runs are entirely opt-in.

```
loadtest/
  k6/
    smoke.js          # login -> /me
    read_heavy.js     # hot authenticated GETs, ramping VUs
    lib/
      config.js       # env-driven config + per-endpoint latency budgets
      auth.js         # login + authenticated-GET helpers
  README.md
```

## Prerequisites

- [k6 installed](https://grafana.com/docs/k6/latest/set-up/install-k6/)
  (`brew install k6`, `choco install k6`, `winget install k6`, or a release
  binary).
- A running FuelGrid API with a **seeded** database. The harness logs in as a
  seeded user, so the demo seed must be present.

### Bring up the dev stack and seed it

```sh
# 1. Postgres + Redis (repo root).
docker compose up -d

# 2. Apply migrations + seed the demo tenant/users/stations.
go run ./services/api/cmd/migrate up
AUTH_PASSWORD_PEPPER=dev-pepper go run ./services/api/cmd/seed

# 3. Run the API.
AUTH_PASSWORD_PEPPER=dev-pepper go run ./services/api/cmd/api
```

The seed creates tenant `demo` with two users:

| Role            | Email                  | Password                       |
| --------------- | ---------------------- | ------------------------------ |
| station_manager | `demo@fuelgrid.local`  | `fuelgrid-demo-password-1234`  |
| system_admin    | `admin@fuelgrid.local` | `fuelgrid-admin-password-1234` |

## Running locally

```sh
# Smoke (login -> /me) against the local dev stack.
k6 run loadtest/k6/smoke.js

# Read-heavy against the local dev stack.
k6 run loadtest/k6/read_heavy.js
```

### Pointing at another environment

Everything that varies by environment is an env var (`-e KEY=value`):

```sh
k6 run \
  -e BASE_URL=https://api.staging.example.com \
  -e TENANT_SLUG=demo \
  -e USER_EMAIL=demo@fuelgrid.local \
  -e USER_PASSWORD='...' \
  -e ADMIN_EMAIL=admin@fuelgrid.local \
  -e ADMIN_PASSWORD='...' \
  loadtest/k6/read_heavy.js
```

| Env var          | Default                        | Used by    |
| ---------------- | ------------------------------ | ---------- |
| `BASE_URL`       | `http://localhost:8080`        | both       |
| `TENANT_SLUG`    | `demo`                         | both       |
| `USER_EMAIL`     | `demo@fuelgrid.local`          | smoke      |
| `USER_PASSWORD`  | `fuelgrid-demo-password-1234`  | smoke      |
| `ADMIN_EMAIL`    | `admin@fuelgrid.local`         | read_heavy |
| `ADMIN_PASSWORD` | `fuelgrid-admin-password-1234` | read_heavy |

> Never commit real credentials. For non-dev targets, pass secrets via the
> environment (`-e ADMIN_PASSWORD="$LOADTEST_ADMIN_PASSWORD"`), not on the
> command line where they land in shell history.

## Authentication / reconcile note

The scripts authenticate exactly the way the web app does — there is no special
service account or API key:

```
POST /api/v1/auth/login   { tenant_slug, email, password }  ->  { token }
```

The returned bearer `token` is reused for every subsequent request via the
`Authorization: Bearer <token>` header. Each VU logs in once and reuses its
bearer for the run. Because the harness logs in as a seeded user, **the target
database must be seeded first** (`go run ./services/api/cmd/seed`).

The `read_heavy` scenario logs in as the tenant-wide `system_admin` so the hot
GETs return real data instead of `403`/empty for a station-scoped actor, and
resolves a real station id in `setup()` to drive `/stations/{id}/overview`.

## Reading the results

k6 prints a summary at the end of a run. The lines that matter:

- **`checks`** — share of functional assertions that passed (status is 200,
  bodies have the expected fields). Should be `100.00%`.
- **`http_req_failed`** — share of requests that failed (non-2xx/3xx or
  transport error). The threshold is `rate<0.01` (< 1%).
- **`http_req_duration`** — overall request latency (`avg`, `p(90)`, `p(95)`,
  `max`).
- **`<endpoint>_duration`** — per-endpoint latency trends (e.g.
  `stations_list_duration`, `station_overview_duration`). These back the
  per-endpoint p95 thresholds.

A line with a green check / `✓` next to a threshold passed; a red `✗` failed and
k6 exits non-zero — which is exactly how the CI workflow gates.

Example (abridged):

```
     checks.........................: 100.00% ✓ 480       ✗ 0
     http_req_failed................: 0.00%   ✓ 0         ✗ 720
     http_req_duration..............: avg=41ms  p(95)=120ms
     ✓ stations_list_duration..........: p(95)=98ms
     ✓ station_overview_duration.......: p(95)=210ms
```

### Useful flags

```sh
# JSON summary for dashboards / artifacts.
k6 run --summary-export=summary.json loadtest/k6/read_heavy.js

# Override VUs/duration ad hoc (smoke uses constant-vus).
k6 run --vus 1 --duration 10s loadtest/k6/smoke.js
```

## Thresholds rationale

Thresholds are deliberate SLOs, not observed values — a breach is a signal to
investigate, not noise.

- **`http_req_failed: rate<0.01`** — under normal read load the API should be
  effectively error-free. A sustained > 1% error rate means saturation,
  load-shedding (`/readyz`-driven), or a broken endpoint.
- **Per-endpoint p95 budgets** (defined in `k6/lib/config.js`):
  - `login` — **800ms**: the loosest, because password verification
    (argon2/bcrypt) is intentionally expensive.
  - `me` — **250ms**: a cheap session lookup; should be fast.
  - `stations_list` / `products_list` — **300ms**: simple tenant-scoped lists.
  - `tanks_list` — **350ms**: list with station-scope filtering.
  - `station_overview` — **500ms**: an aggregate (tanks + pumps/nozzles +
    incidents); the budget is set to catch an N+1 regression creeping into the
    composition without flagging normal aggregate cost.
  - `audit_logs` — **400ms**: a paginated list over an append-only table.

Tune the budgets in `config.js` per environment. Raise them deliberately (with a
note on why); do not loosen one just to turn a red run green — that defeats the
purpose of the gate.
