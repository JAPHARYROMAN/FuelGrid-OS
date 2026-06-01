// Shared configuration for the FuelGrid k6 load-test harness.
//
// Everything that varies by environment is read from k6 env vars (passed with
// `k6 run -e KEY=value ...` or via the real process environment). Sensible
// defaults point at the local dev stack (docker-compose + seeded demo tenant),
// so `k6 run loadtest/k6/smoke.js` works out of the box once the API is up and
// `go run ./services/api/cmd/seed` has run.
//
// Env vars:
//   BASE_URL        API base, no trailing slash. Default http://localhost:8080
//   TENANT_SLUG     Tenant slug for login.           Default "demo"
//   USER_EMAIL      Smoke-login user (login -> /me).  Default demo@fuelgrid.local
//   USER_PASSWORD   Smoke-login user password.        Default fuelgrid-demo-password-1234
//   ADMIN_EMAIL     Read-heavy user (tenant-wide).    Default admin@fuelgrid.local
//   ADMIN_PASSWORD  Read-heavy user password.         Default fuelgrid-admin-password-1234
//
// The read-heavy scenario uses the admin (system_admin, tenant-wide) so the hot
// authenticated GETs return real data instead of 403/empty for a station-scoped
// actor. The smoke scenario uses the plain demo user since it only exercises
// login -> /me.

function env(key, fallback) {
  const v = __ENV[key];
  return v === undefined || v === '' ? fallback : v;
}

export const BASE_URL = env('BASE_URL', 'http://localhost:8080').replace(/\/+$/, '');

export const TENANT_SLUG = env('TENANT_SLUG', 'demo');

// Smoke user (login -> /me).
export const USER_EMAIL = env('USER_EMAIL', 'demo@fuelgrid.local');
export const USER_PASSWORD = env('USER_PASSWORD', 'fuelgrid-demo-password-1234');

// Read-heavy user — tenant-wide so list/overview endpoints return data.
export const ADMIN_EMAIL = env('ADMIN_EMAIL', 'admin@fuelgrid.local');
export const ADMIN_PASSWORD = env('ADMIN_PASSWORD', 'fuelgrid-admin-password-1234');

// p95 latency budgets (ms) per logical endpoint. These are deliberate SLOs, not
// observed values — a breach is a signal to investigate, not noise. They are
// generous enough for a single-node dev stack but tight enough to catch a
// regression (e.g. an N+1 sneaking into an overview aggregate). Tune per
// environment via the thresholds in each scenario.
export const LATENCY_BUDGET_MS = {
  login: 800, // bcrypt/argon verify dominates; intentionally the loosest.
  me: 250,
  stations_list: 300,
  products_list: 300,
  tanks_list: 350,
  station_overview: 500, // aggregate: tanks + pumps/nozzles + incidents.
  audit_logs: 400, // paginated list.
};
