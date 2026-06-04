# Deployment

How FuelGrid OS reaches production. Stage 10 picks the target and wires the CI image build; the *actual* first deploy is deliberately deferred until Phase 2 features are real.

## Target: Fly.io

Decided Stage 10. Revisitable when traffic patterns are known.

### Why Fly

| Concern | Fly.io fit |
|---|---|
| Modular monolith + workers | Single `fly.toml` per app; `[processes]` lets the API and any future worker tier share one image. |
| Postgres | Managed Postgres clusters with point-in-time restore, easy major-version upgrades, regional read replicas when needed. |
| Redis | Native Upstash add-on, or run Redis as a Fly app for $0–$2/mo at dev scale. |
| Dockerfile-first | Our [services/api/Dockerfile](../services/api/Dockerfile) (distroless, multi-stage) deploys as-is — no platform-specific build step. |
| Multi-region | Fuel businesses outside the deployment region need low-latency reads. Fly's Anycast + region scheduling is the cheapest path. |
| Cost | Free tier serves a single-station demo. Paid tiers scale linearly without surprise per-resource markups. |

### What we did **not** pick

- **Railway** — best onboarding ergonomics but per-resource pricing accelerates fast, and the region story is thin.
- **Render** — solid managed services, but cold-start behavior on free tier and steeper per-service pricing on production tiers.
- **Self-hosted (k8s)** — most control, most ops surface. Reasonable when we have ≥3 customers and a need that managed platforms don't meet; today it's premature.

## Deploy topology

```
                    ┌────────────────────────────────────┐
                    │  Cloudflare (DNS + WAF + cache)    │
                    └───────────────┬────────────────────┘
                                    │
                ┌───────────────────┴────────────────────┐
                │                                        │
        ┌───────▼──────────┐                  ┌──────────▼─────────┐
        │  fuelgrid-web    │                  │  fuelgrid-api      │
        │  Next.js (Fly)   │ ── /api/v1/ ────►│  Go / chi (Fly)    │
        │  3000            │                  │  8080              │
        └──────────────────┘                  └──────────┬─────────┘
                                                         │
                                            ┌────────────┴────────────┐
                                            │                         │
                                    ┌───────▼────────┐      ┌─────────▼────────┐
                                    │  Postgres 16   │      │  Redis 7         │
                                    │  Fly Managed   │      │  Fly app or      │
                                    │  Pg cluster    │      │  Upstash add-on  │
                                    └────────────────┘      └──────────────────┘
```

## Repository conventions for the deploy

These now exist in-repo:

```
services/api/fly.toml         # process model, scaling, /readyz healthcheck (image-based deploy)
apps/web/fly.toml             # Next.js standalone, port 3000, /login healthcheck
.github/workflows/deploy.yml  # gated on main; flyctl rollout gated on FLY_API_TOKEN
.env.production.example        # full prod config + secret inventory
```

The Dockerfile is already production-ready: distroless, non-root, multi-stage, BuildKit cache mounts.

## CI image strategy

Today's CI ([.github/workflows/ci.yml](../.github/workflows/ci.yml)):

1. Builds the api image on every push / PR via `docker/build-push-action`.
2. **Does not push** — the image is built and load-tested in CI only.
3. Smoke-tests the image: boots it, hits `/healthz` and `/readyz`.

Stage-10 addition: **on push to `main`**, the image is also tagged with the commit SHA. Pushing to a registry is added with the first deploy.

## Continuous Deployment (CICD-4 / OPS-5)

The CD pipeline lives in [.github/workflows/deploy.yml](../.github/workflows/deploy.yml). It is a **separate** workflow from CI and runs on push to `main` and on `v*` tags, single-flight per ref (`concurrency: deploy-<ref>`, `cancel-in-progress: false` so a migration is never interrupted mid-flight).

It is intentionally a **safe no-op until the deployment secrets are configured** — the workflow is lint-valid and runnable today; the migrate and smoke jobs skip cleanly when their secrets are absent.

### Jobs

1. **build-push** — builds `services/api/Dockerfile` and **pushes to GHCR** at `ghcr.io/<owner>/<repo>-api` (lowercased), via `docker/build-push-action`. Tags:
   - `sha-<full-sha>` — immutable handle for every build.
   - `latest` — only on `main` branch pushes.
   - `<tag>` — the git tag name on `v*` tag pushes (e.g. `v1.2.3`).

   GHCR auth is automatic: `docker/login-action` uses the built-in `GITHUB_TOKEN` with `packages: write` permission — **no manually-created registry secret is needed**.

2. **migrate** — runs `go run ./services/api/cmd/migrate up` (the project's golang-migrate v4 wrapper) against `${{ secrets.DATABASE_URL }}`. Guarded: if `DATABASE_URL` is unset the job no-ops (emits a `::notice::` and skips). `up` is idempotent (`ErrNoChange` → success).

3. **smoke** — curls the deployed `/readyz` (`${{ secrets.DEPLOY_HEALTH_URL }}`) and **fails the deploy unless it returns `200` with `{"status":"ready"}`** (i.e. postgres + redis checks pass). Polls up to 30× with 5s backoff. Guarded the same way: if `DEPLOY_HEALTH_URL` is unset the gate no-ops.

All three jobs use `environment: production`, so GitHub Environment protection rules (required reviewers, wait timers, branch restrictions) apply when configured in repo settings.

### Required secrets / configuration

| Name | Where | Purpose | Behavior if unset |
|---|---|---|---|
| `GITHUB_TOKEN` | automatic (no setup) | Push the image to GHCR | Always present |
| `DATABASE_URL` | repo/environment secret | Target DB for `migrate up` (e.g. `postgres://...?sslmode=require`) | migrate job no-ops |
| `DEPLOY_HEALTH_URL` | repo/environment secret | Full URL of the deployed `/readyz` (e.g. `https://api.example.com/readyz`) | smoke gate no-ops |
| `FLY_API_TOKEN` | repo/environment secret | Deploy-scoped Fly token (`fly tokens create deploy`) for the flyctl rollout | deploy job no-ops |

Add the secrets under **Settings → Secrets and variables → Actions** (or scope them to the `production` Environment). Until both are set the pipeline still builds and publishes the image but does not touch a database or assert on a live endpoint.

### Deploy flow

```
push to main / v* tag
  → build-push  (image → GHCR: :sha-…, :latest | :<tag>)
  → migrate     (golang-migrate up @ DATABASE_URL; gated on DATABASE_URL)
  → deploy      (flyctl deploy --image ghcr.io/<owner>/<repo>-api:sha-<sha>; gated on FLY_API_TOKEN)
  → smoke       (curl /readyz @ DEPLOY_HEALTH_URL must be 200; gated on DEPLOY_HEALTH_URL)
```

The `deploy` job rolls the freshly-pushed GHCR image onto the Fly API app with
`flyctl deploy --config services/api/fly.toml --image …:sha-<sha>` (pinned to
the immutable per-commit tag, never a moving `:latest`). It sits between
`migrate` and `smoke`, so the ordering encodes the online-migration discipline:
**schema first → rollout → health-assert last**.

It is gated on `FLY_API_TOKEN` exactly like the `migrate`/`smoke` guards: a
first step checks the secret and exports a flag; every real step is
`if: steps.gate.outputs.configured == 'true'`. When `FLY_API_TOKEN` is unset
the job emits a `::notice::` and skips — the pipeline stays a safe, lint-valid
no-op until the Fly runtime is provisioned. The web rollout in the same job is
additionally guarded on a published web image existing in GHCR (it `docker
manifest inspect`s the tag and skips with a notice if absent), so it never
fails the pipeline before the web image pipeline exists.

`FLY_API_TOKEN` is created with `fly tokens create deploy` (a deploy-scoped
token) and added under **Settings → Secrets and variables → Actions** (or the
`production` Environment).

## Environment variables in production

The complete production config + secret inventory — every variable, where it
comes from, which are **SECRET** (set via `fly secrets`) vs plain config
(`fly.toml [env]`), and which **fail-stop** the boot outside dev — lives in
[.env.production.example](../.env.production.example). Secrets are injected, not
filed:

```sh
fly secrets set --app fuelgrid-api \
    DATABASE_URL="postgres://<owner>:<pw>@<host>:5432/fuelgrid?sslmode=require" \
    DATABASE_APP_URL="postgres://fuelgrid_app:<pw>@<host>:5432/fuelgrid?sslmode=require" \
    REDIS_URL="rediss://default:<pw>@<host>:6379/0" \
    AUTH_PASSWORD_PEPPER="$(openssl rand -base64 32)" \
    SENTRY_DSN="<dsn>"
```

`DATABASE_APP_URL` (non-owner `fuelgrid_app` role) is **required outside dev** —
the API refuses to boot without it when a DB is configured, so it never runs
request queries RLS-bypassed on the owner pool. `API_CORS_ALLOWED_ORIGINS` must
be **https** origins (config fail-stops on `http://` or `*`). See the Go-live
runbook below for the full ordered procedure.

Rotation: secrets are stored in Fly's Vault, never committed. The `AUTH_PASSWORD_PEPPER` rotation invalidates all existing password hashes — coordinate carefully (force a password reset wave).

The full secret inventory, redaction model (the `config.Secret` type redacts secrets in logs/errors), rotation procedures, and leak response live in [docs/security/secrets.md](security/secrets.md).

## Observability in production

| Signal | Endpoint | Scraper |
|---|---|---|
| Metrics | `GET /metrics` (Prometheus exposition) | Grafana Cloud Free, scraped via the Grafana Agent we'll bake into the Fly machine |
| Traces | OTLP over gRPC | Grafana Tempo or Honeycomb; `OTEL_EXPORTER=otlp` + `OTEL_EXPORTER_OTLP_ENDPOINT=...` |
| Errors | Sentry | `SENTRY_DSN=...`, sample rate per env |
| Structured logs | stdout (JSON) → Fly logs → BetterStack/Loki | Standardized field names per [.github/workflows/ci.yml](../.github/workflows/ci.yml) middleware |

The `/metrics` endpoint is intentionally open in dev. In production it MUST be reached only via the metrics scraper — gate it via Fly's internal network or an ingress rule.

### Distributed tracing (OTLP)

The API exports spans through OpenTelemetry. `OTEL_EXPORTER` selects the exporter:

| `OTEL_EXPORTER` | Behaviour |
|---|---|
| `none` (default) | Tracing disabled — spans are created but discarded (no-op provider). Boot never fails. |
| `stdout` | Pretty-prints spans to stderr. Dev / CI only. |
| `otlp` | Ships spans over **OTLP/gRPC** to the collector at `OTEL_EXPORTER_OTLP_ENDPOINT`. |

`OTEL_EXPORTER_OTLP_ENDPOINT` is the collector address used when `OTEL_EXPORTER=otlp`:

- A bare `host:port` (e.g. `tempo:4317`) or an `https://` URL connects over **TLS** — the secure default for a remote collector.
- An `http://` prefix forces an **insecure/plaintext** connection, intended for a local collector or a same-host sidecar.

**Fail-stop semantics:** when `OTEL_EXPORTER=otlp` and the exporter cannot be built (endpoint unset, malformed, or unresolvable), the API **refuses to boot** — it exits with a non-zero status rather than start with traces silently dropped. Telemetry the operator explicitly asked for must never disappear unnoticed. The `none` and `stdout` paths stay best-effort: a failure there is logged and the API continues.

The tracer provider is flushed on shutdown (`SIGTERM`/`SIGINT`) with a 5s timeout so in-flight span batches are delivered before exit.

## Database migrations on deploy

**Migrations are owned by the CD `migrate` job, NOT by a Fly `release_command`** —
we pick one place to migrate so the schema is never applied twice. The `migrate`
job runs `go run ./services/api/cmd/migrate up` against `secrets.DATABASE_URL`
(the table **owner** role) *before* the `deploy` job rolls the new image — the
standard online-migration discipline (schema first, then replace replicas).

`services/api/fly.toml` therefore has **no** `release_command`: its `[deploy]`
block only sets `strategy = "rolling"`. The distroless production image ships
only the `/api` binary (no migrate binary), so there is nothing to run as a Fly
release command anyway. If you ever prefer Fly-driven migrations instead, you
must remove the CD `migrate` job to avoid double-migrating — and ship a migrate
binary in the image — but the CD path is the supported one.

Migrations run as the **owner** (`DATABASE_URL`); the running API uses the
**non-owner** `fuelgrid_app` pool (`DATABASE_APP_URL`) so Postgres RLS enforces
tenant isolation on every request.

## Branch protection (one-time setup)

Apply once via the GitHub UI or `gh`:

```sh
gh api -X PUT repos/JAPHARYROMAN/FuelGrid-OS/branches/main/protection \
    -F required_status_checks.strict=true \
    -F required_status_checks.checks[][context]=Node — lint, typecheck, test, build \
    -F required_status_checks.checks[][context]=Go — vet, lint, test, build \
    -F required_status_checks.checks[][context]=Migrations — apply, seed, /readyz check \
    -F required_status_checks.checks[][context]=Docker — build api image \
    -F enforce_admins=true \
    -F required_pull_request_reviews.required_approving_review_count=1 \
    -F required_pull_request_reviews.dismiss_stale_reviews=true \
    -F restrictions=null
```

Once applied, `main` cannot be pushed to directly; PRs must pass all four checks.

## Go-live runbook

The exact ordered steps an operator runs for the **first real deploy**. Replace
every `<...>` placeholder with your values; never paste real secrets into a
file that gets committed. App configs: [`services/api/fly.toml`](../services/api/fly.toml),
[`apps/web/fly.toml`](../apps/web/fly.toml). Full env/secret inventory:
[`.env.production.example`](../.env.production.example).

### Pre-launch checklist (all must be true before flipping traffic on)

- [ ] Both Fly apps created; `services/api/fly.toml` and `apps/web/fly.toml` placeholders (app name, `primary_region`, `API_ORIGIN`) replaced.
- [ ] Managed Postgres + Redis provisioned; backups / point-in-time restore enabled on Postgres.
- [ ] `fuelgrid_app` non-owner role has a **strong** password set; `DATABASE_APP_URL` built from it and **distinct** from `DATABASE_URL`.
- [ ] All API secrets set via `fly secrets` (see step 4); `AUTH_PASSWORD_PEPPER` generated **once** and recorded safely (rotation invalidates every password hash).
- [ ] `API_CORS_ALLOWED_ORIGINS` is **https only** (no `*`, no `http://`) — the API fail-stops otherwise.
- [ ] GitHub Actions secrets set: `DATABASE_URL`, `DEPLOY_HEALTH_URL`, `FLY_API_TOKEN`.
- [ ] `/readyz` returns `200 {"status":"ready"}` (postgres + redis ok) and a login smoke passes.
- [ ] RLS confirmed: the API runs on the non-owner `DATABASE_APP_URL` pool (it refuses to boot in prod without it).

### (a) Create the two Fly apps

```sh
flyctl auth login
flyctl apps create fuelgrid-api          # match services/api/fly.toml `app`
flyctl apps create fuelgrid-web          # match apps/web/fly.toml `app`
```

### (b) Provision Postgres + Redis

```sh
# Managed Postgres (or bring an external Postgres 16 with sslmode=require):
flyctl postgres create --name fuelgrid-pg --region <region>
flyctl postgres attach fuelgrid-pg --app fuelgrid-api   # prints the owner DSN

# Redis — Upstash add-on (TLS rediss://) or a Fly Redis app:
flyctl redis create     # note the rediss:// URL it prints
```

Enable scheduled backups / PITR on the Postgres cluster in the Fly dashboard.

### (c) Create the non-owner `fuelgrid_app` role password + build the app DSN

Migration `0005_rls.up.sql` (run in step f) **creates** the `fuelgrid_app`
LOGIN role and grants it RLS-confined DML. The operator must set a **strong
password** for it and build `DATABASE_APP_URL` from that. Connect as the owner
and set it:

```sh
flyctl postgres connect --app fuelgrid-pg
```
```sql
ALTER ROLE fuelgrid_app WITH PASSWORD '<STRONG_APP_ROLE_PASSWORD>';
```

Then assemble (note: **distinct** from the owner DSN, `sslmode=require`):

```
DATABASE_URL     = postgres://<owner>:<owner_pw>@<pg-host>:5432/fuelgrid?sslmode=require
DATABASE_APP_URL = postgres://fuelgrid_app:<STRONG_APP_ROLE_PASSWORD>@<pg-host>:5432/fuelgrid?sslmode=require
```

> Chicken-and-egg: the role does not exist until migrations run (step f). Either
> run migrations once with only `DATABASE_URL` set, then set the role password
> and `DATABASE_APP_URL` and redeploy; or pre-create the role manually before
> the first migrate. Both reach the same end state.

### (d) Set the API secrets (never in fly.toml)

```sh
flyctl secrets set --app fuelgrid-api \
  DATABASE_URL="postgres://<owner>:<owner_pw>@<pg-host>:5432/fuelgrid?sslmode=require" \
  DATABASE_APP_URL="postgres://fuelgrid_app:<app_pw>@<pg-host>:5432/fuelgrid?sslmode=require" \
  REDIS_URL="rediss://default:<redis_pw>@<redis-host>:6379/0" \
  AUTH_PASSWORD_PEPPER="$(openssl rand -base64 32)" \
  PLATFORM_ADMIN_TOKEN="$(openssl rand -hex 32)"
# Optional integrations (only if used): SENTRY_DSN, SMTP_PASSWORD,
# MPESA_CONSUMER_KEY / MPESA_CONSUMER_SECRET / MPESA_PASSKEY, OTEL_EXPORTER_OTLP_ENDPOINT.

# Web app secrets (the API_ORIGIN itself is non-secret config in apps/web/fly.toml):
flyctl secrets set --app fuelgrid-web NEXT_PUBLIC_SENTRY_DSN="<dsn-or-omit>"
```

Set non-secret config (`API_CORS_ALLOWED_ORIGINS=https://...`, `MPESA_*`
non-secret fields, `APP_BASE_URL`) in `services/api/fly.toml` `[env]`, and the
web `API_ORIGIN` in `apps/web/fly.toml` `[env]`.

### (e) Configure GitHub Actions secrets for CD

Under **Settings → Secrets and variables → Actions** (or the `production`
Environment):

```
DATABASE_URL       = <owner DSN, sslmode=require>           # CD migrate job
DEPLOY_HEALTH_URL  = https://<api-public-host>/readyz       # CD smoke gate
FLY_API_TOKEN      = $(fly tokens create deploy)            # CD flyctl rollout
```

Until all three exist the CD pipeline still builds + pushes the image but
no-ops migrate / deploy / smoke (each emits a `::notice::`).

### (f) First deploy

The web app currently has no published image, so build it from source once
(add a Next.js standalone `apps/web/Dockerfile` referenced by
`apps/web/fly.toml`), then let CD own the API:

```sh
# Web (from source, first time):
flyctl deploy --config apps/web/fly.toml --app fuelgrid-web

# API: push to main → CD runs build-push → migrate → deploy (flyctl rollout)
#      → smoke automatically. Or roll out manually for the very first cut:
flyctl deploy --config services/api/fly.toml \
  --image ghcr.io/<owner>/<repo>-api:sha-<commit-sha>
```

### (g) Verify

```sh
curl -fsS https://<api-public-host>/readyz        # expect 200 {"status":"ready"}
# Login smoke against the web BFF (no token leaves the server):
curl -i -X POST https://<web-host>/api/bff/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"<user>","password":"<pw>"}'
```

**Tenant seed decision:** seeding is **prod-guarded** — `services/api/cmd/seed`
refuses to run unless `NODE_ENV=development` *or* `ALLOW_SEED=true` is set
explicitly. Do **not** seed demo data into a real production tenant. Provision
real tenants instead via `POST /api/v1/platform/tenants` (authenticated with
`PLATFORM_ADMIN_TOKEN`). Only run the seeder against a throwaway staging DB, and
only with `ALLOW_SEED=true` set deliberately.

## Defer list

- Replication / read-replica config on Fly Postgres.
- A published web image + image-based web rollout in CD (today the web app deploys from source per the runbook; the CD `deploy` job already skips the web rollout cleanly until a `…-web` image exists in GHCR).

These land alongside scale-out, not the first launch.
