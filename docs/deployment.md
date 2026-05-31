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
- `.github/workflows/deploy.yml` calling `flyctl deploy`.
- Pushing images to GHCR / Fly's registry.
- Replication / read-replica config on Fly Postgres.

These land alongside the first real customer onboarding, not in Phase 1.
