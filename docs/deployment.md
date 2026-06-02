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

When the first real deploy lands, expect these additions (not part of Stage 10):

```
services/api/fly.toml         # process model, scaling, healthchecks
apps/web/fly.toml             # Next.js standalone build
.github/workflows/deploy.yml  # gated on main; uses flyctl with FLY_API_TOKEN
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

Add the secrets under **Settings → Secrets and variables → Actions** (or scope them to the `production` Environment). Until both are set the pipeline still builds and publishes the image but does not touch a database or assert on a live endpoint.

### Deploy flow

```
push to main / v* tag
  → build-push  (image → GHCR: :sha-…, :latest | :<tag>)
  → migrate     (golang-migrate up @ DATABASE_URL; gated)
  → smoke       (curl /readyz @ DEPLOY_HEALTH_URL must be 200; gated)
```

The actual rollout (pulling the new GHCR image onto the runtime, e.g. `flyctl deploy --image …`) is wired between the `migrate` and `smoke` jobs when the runtime target is provisioned; the migrate-then-smoke ordering already encodes the online-migration discipline (schema first, health-assert last).

## Environment variables in production

Every value in [.env.example](../.env.example) is set via Fly secrets:

```sh
fly secrets set --app fuelgrid-api \
    DATABASE_URL=postgres://... \
    REDIS_URL=redis://... \
    AUTH_PASSWORD_PEPPER="$(openssl rand -base64 32)" \
    SENTRY_DSN=... \
    OTEL_EXPORTER=otlp
```

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

Migrations run via `services/api/cmd/migrate up` as a pre-deploy step:

```toml
# services/api/fly.toml
[deploy]
release_command = "/api-migrate up"
```

Same binary doesn't ship in the production image (it's a separate small image). Releases that change the schema run the migration before any old API replica is replaced — the standard online-migration discipline.

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

## Defer list

- Actual `fly.toml` files for api + web.
- The `flyctl deploy --image ghcr.io/...` rollout step (the CD pipeline already builds + pushes the image and runs migrations; only the runtime rollout is deferred).
- Configuring the `DATABASE_URL` / `DEPLOY_HEALTH_URL` secrets (the pipeline no-ops cleanly until they exist).
- Replication / read-replica config on Fly Postgres.

These land alongside the first real customer onboarding, not in Phase 1.
