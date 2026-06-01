// Auth + request helpers shared by the load-test scenarios.
//
// SETUP / RECONCILE NOTE
// ----------------------
// Scripts authenticate against the real API exactly the way the web app does:
//   POST /api/v1/auth/login  { tenant_slug, email, password }  ->  { token }
// The returned bearer token is then reused for every subsequent request via the
// Authorization header. There is no separate API key or service account — the
// harness logs in as a seeded user, so the dev/CI database must be seeded first
// (`go run ./services/api/cmd/seed`). The token's lifetime comfortably covers a
// load run; each VU logs in once in its own setup and reuses the bearer.

import http from 'k6/http';
import { check, fail } from 'k6';
import { BASE_URL, TENANT_SLUG } from './config.js';

// Log in and return the raw http response. Fails the iteration loudly if login
// does not yield a token — a misconfigured BASE_URL/credentials should surface
// immediately, not as a wall of downstream 401s. Callers that only want the
// token should use login(); those that also want timing can read res.timings.
export function loginResponse(email, password, tenantSlug = TENANT_SLUG) {
  const res = http.post(
    `${BASE_URL}/api/v1/auth/login`,
    JSON.stringify({ tenant_slug: tenantSlug, email: email, password: password }),
    { headers: { 'Content-Type': 'application/json' }, tags: { name: 'login' } },
  );

  const ok = check(res, {
    'login: status is 200': (r) => r.status === 200,
    'login: returns a token': (r) => {
      try {
        return typeof r.json('token') === 'string' && r.json('token').length > 0;
      } catch (_e) {
        return false;
      }
    },
  });

  if (!ok) {
    fail(
      `login failed for ${email}@${tenantSlug} (status ${res.status}): ${String(res.body).slice(0, 300)}`,
    );
  }

  return res;
}

// Log in and return just the bearer token.
export function login(email, password, tenantSlug = TENANT_SLUG) {
  return loginResponse(email, password, tenantSlug).json('token');
}

// Build the auth header bag for an authenticated request, merging an optional
// per-request name tag (so k6 groups metrics by logical endpoint rather than by
// the unique URL, which would explode the metric cardinality for {id} routes).
export function authParams(token, name) {
  return {
    headers: { Authorization: `Bearer ${token}` },
    tags: name ? { name: name } : {},
  };
}

// GET an authenticated endpoint and check the status, tagging the request with
// a stable logical name. Returns the response so callers can inspect the body.
export function authGet(token, path, name) {
  const res = http.get(`${BASE_URL}${path}`, authParams(token, name));
  check(res, {
    [`${name}: status is 200`]: (r) => r.status === 200,
  });
  return res;
}
