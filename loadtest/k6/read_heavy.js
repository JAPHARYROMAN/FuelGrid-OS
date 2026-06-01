// Read-heavy scenario — models the dominant production traffic shape: dashboard
// and list reads by authenticated operators. It ramps VUs up, holds, and ramps
// down, hitting the hot authenticated GETs with realistic think-time between
// calls:
//
//   GET /api/v1/stations                      (list)
//   GET /api/v1/products                      (tenant-wide list)
//   GET /api/v1/tanks                          (list, station-scoped)
//   GET /api/v1/stations/{id}/overview         (command-center / station dash:
//                                               tanks + pumps/nozzles + incidents)
//   GET /api/v1/audit-logs?limit=50            (a paginated list)
//
// It authenticates as the tenant-wide admin (system_admin) so every endpoint
// returns real data rather than 403/empty for a station-scoped actor.
//
// Run locally:
//   k6 run loadtest/k6/read_heavy.js
//   k6 run -e BASE_URL=https://api.staging.example -e ADMIN_EMAIL=... \
//          -e ADMIN_PASSWORD=... loadtest/k6/read_heavy.js

import { check, sleep, group } from 'k6';
import { Trend } from 'k6/metrics';
import { login, authGet } from './lib/auth.js';
import { ADMIN_EMAIL, ADMIN_PASSWORD, LATENCY_BUDGET_MS } from './lib/config.js';

// Per-endpoint latency trends — these back the per-endpoint p95 thresholds.
const stationsListDuration = new Trend('stations_list_duration', true);
const productsListDuration = new Trend('products_list_duration', true);
const tanksListDuration = new Trend('tanks_list_duration', true);
const stationOverviewDuration = new Trend('station_overview_duration', true);
const auditLogsDuration = new Trend('audit_logs_duration', true);

export const options = {
  scenarios: {
    read_heavy: {
      executor: 'ramping-vus',
      startVUs: 0,
      stages: [
        { duration: '30s', target: 10 }, // ramp up
        { duration: '1m', target: 10 }, // hold
        { duration: '30s', target: 25 }, // ramp to peak
        { duration: '1m', target: 25 }, // hold peak
        { duration: '30s', target: 0 }, // ramp down
      ],
      gracefulRampDown: '15s',
    },
  },
  thresholds: {
    // Reliability gate: <1% of all requests may fail.
    http_req_failed: ['rate<0.01'],
    // Per-endpoint p95 latency budgets (see config.js for rationale).
    stations_list_duration: [`p(95)<${LATENCY_BUDGET_MS.stations_list}`],
    products_list_duration: [`p(95)<${LATENCY_BUDGET_MS.products_list}`],
    tanks_list_duration: [`p(95)<${LATENCY_BUDGET_MS.tanks_list}`],
    station_overview_duration: [`p(95)<${LATENCY_BUDGET_MS.station_overview}`],
    audit_logs_duration: [`p(95)<${LATENCY_BUDGET_MS.audit_logs}`],
    checks: ['rate>0.99'],
  },
};

// setup() runs once before the VUs spin up. It logs in as admin, captures the
// bearer, and resolves a station id to drive the per-station overview endpoint.
// The token + station id are handed to every VU via the returned object.
export function setup() {
  const token = login(ADMIN_EMAIL, ADMIN_PASSWORD);

  const res = authGet(token, '/api/v1/stations', 'stations');
  let stationID = '';
  try {
    const items = res.json('items');
    if (Array.isArray(items) && items.length > 0) {
      stationID = items[0].id;
    }
  } catch (_e) {
    // Leave stationID empty; the overview group below is skipped if so.
  }

  return { token: token, stationID: stationID };
}

export default function (data) {
  const token = data.token;

  group('lists', function () {
    const stations = authGet(token, '/api/v1/stations', 'stations');
    stationsListDuration.add(stations.timings.duration);
    check(stations, {
      'stations: has items array': (r) => {
        try {
          return Array.isArray(r.json('items'));
        } catch (_e) {
          return false;
        }
      },
    });
    sleep(Math.random() * 1 + 0.5); // 0.5–1.5s think-time.

    const products = authGet(token, '/api/v1/products', 'products');
    productsListDuration.add(products.timings.duration);
    sleep(Math.random() * 1 + 0.5);

    const tanks = authGet(token, '/api/v1/tanks', 'tanks');
    tanksListDuration.add(tanks.timings.duration);
    sleep(Math.random() * 1 + 0.5);
  });

  group('paginated', function () {
    // audit-logs is a paginated list (limit/offset). system_admin has
    // audit.read, so this returns the tenant's audit trail.
    const audit = authGet(token, '/api/v1/audit-logs?limit=50', 'audit_logs');
    auditLogsDuration.add(audit.timings.duration);
    sleep(Math.random() * 1 + 0.5);
  });

  if (data.stationID) {
    group('station_overview', function () {
      // The command-center / station dashboard aggregate: one call returning
      // the station with tanks, pumps (nested nozzles), and active incidents.
      const overview = authGet(
        token,
        `/api/v1/stations/${data.stationID}/overview`,
        'station_overview',
      );
      stationOverviewDuration.add(overview.timings.duration);
      sleep(Math.random() * 1.5 + 0.5); // 0.5–2s — operators dwell on dashboards.
    });
  }
}
