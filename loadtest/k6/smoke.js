// Smoke scenario — the cheapest possible "is the auth path alive and fast?"
// check. One VU does login -> GET /me in a tight loop for a short window. This
// is the scenario the manual CI workflow runs as a gate: if login or /me breaks
// or blows the latency budget, the run fails.
//
// Run locally:
//   k6 run loadtest/k6/smoke.js
//   k6 run -e BASE_URL=https://api.staging.example -e TENANT_SLUG=demo \
//          -e USER_EMAIL=demo@fuelgrid.local -e USER_PASSWORD=... \
//          loadtest/k6/smoke.js

import { check, sleep } from 'k6';
import { Trend } from 'k6/metrics';
import { loginResponse, authGet } from './lib/auth.js';
import { USER_EMAIL, USER_PASSWORD, LATENCY_BUDGET_MS } from './lib/config.js';

// Per-endpoint latency trends so thresholds can target each call individually.
const loginDuration = new Trend('login_duration', true);
const meDuration = new Trend('me_duration', true);

export const options = {
  scenarios: {
    smoke: {
      executor: 'constant-vus',
      vus: 1,
      duration: '30s',
    },
  },
  thresholds: {
    // Hard gate: essentially no request may fail.
    http_req_failed: ['rate<0.01'],
    // Per-endpoint p95 budgets.
    login_duration: [`p(95)<${LATENCY_BUDGET_MS.login}`],
    me_duration: [`p(95)<${LATENCY_BUDGET_MS.me}`],
    // Every functional check must pass.
    checks: ['rate>0.99'],
  },
};

export default function () {
  // Log in once per iteration (the smoke scenario deliberately exercises the
  // login path on every loop — it is the thing most likely to regress). Record
  // the server-side login latency from the response timings.
  const loginRes = loginResponse(USER_EMAIL, USER_PASSWORD);
  loginDuration.add(loginRes.timings.duration);
  const token = loginRes.json('token');

  const me = authGet(token, '/api/v1/me', 'me');
  meDuration.add(me.timings.duration);

  check(me, {
    'me: has user_id': (r) => {
      try {
        return typeof r.json('user_id') === 'string';
      } catch (_e) {
        return false;
      }
    },
    'me: has tenant_id': (r) => {
      try {
        return typeof r.json('tenant_id') === 'string';
      } catch (_e) {
        return false;
      }
    },
  });

  sleep(1); // realistic think-time between iterations.
}
