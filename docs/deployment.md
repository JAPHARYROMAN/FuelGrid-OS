# Deployment

How FuelGrid OS reaches production. The CI image build + GHCR publishing is
live; the production runtime is a single DigitalOcean Droplet running
`docker compose`.

## Target: DigitalOcean Droplet + docker compose

A single Droplet runs the whole stack with `docker compose`: the Go API, the
Next.js web app, **self-hosted Postgres + Redis on the same VM**, all behind a
**Caddy** reverse proxy doing automatic HTTPS (Let's Encrypt). The production
stack lives in [`deploy/`](../deploy/):

```
deploy/docker-compose.prod.yml   # the production stack (separate from the local-dev compose)
deploy/Caddyfile                 # reverse proxy: WEB_DOMAIN -> web, API_DOMAIN -> api, auto-TLS
deploy/backup/                   # nightly pg_dump + systemd timer + restore drill
.env.production.example          # full prod config + secret inventory (copy to .env on the droplet)
services/api/Dockerfile.migrate  # golang-migrate + the SQL migrations baked in (the migrate image)
.github/workflows/deploy.yml     # gated CD: build+push 3 GHCR images, SSH deploy, smoke gate
```

### Why a Droplet + compose

| Concern | Fit |
|---|---|
| One box, low ops | A single Droplet with `docker compose` is the cheapest, simplest topology for an early-stage product; everything is one `up -d`. |
| Portable images | The API ([services/api/Dockerfile](../services/api/Dockerfile), distroless/nonroot) and web ([apps/web/Dockerfile](../apps/web/Dockerfile), Next.js standalone) images are platform-neutral — no platform-specific build step. |
| TLS without glue | Caddy obtains + renews Let's Encrypt certs automatically; no cert plumbing. |
| Data you control | Self-hosted Postgres + Redis on named volumes; you own the backups (see [deploy/backup/](../deploy/backup/)). |
| Predictable cost | Flat Droplet pricing; scale up the VM or split out DO Managed Postgres when load justifies it. |

### What we did **not** pick (and the trade-off accepted)

- **Fly.io** (the previous target) — good multi-region story, but more
  platform-specific config (`fly.toml`, `fly secrets`, managed PG add-ons) than a
  single-box launch needs.
- **Managed everything (DO App Platform / Managed PG + Redis)** — less ops, more
  cost; the natural next step once one VM is no longer enough.
- **Self-hosted PG trade-off:** the nightly `pg_dump` is a *logical* backup — no
  point-in-time recovery. This is acceptable for launch; the migration path to
  PITR (WAL archiving or DO Managed Postgres) is documented in
  [deploy/backup/RESTORE.md](../deploy/backup/RESTORE.md).

## Deploy topology

```
                          Internet
                             │  (DNS A records: WEB_DOMAIN, API_DOMAIN → droplet IP)
                             ▼
            ┌──────────────────────────────────────────────┐
            │              DigitalOcean Droplet              │
            │   DO firewall: inbound 22 / 80 / 443 only      │
            │                                                │
            │   ┌────────────────────────────────────────┐  │
            │   │  caddy  (:80/:443, auto-HTTPS)           │  │
            │   │   WEB_DOMAIN → web:3000                  │  │
            │   │   API_DOMAIN → api:8080  (M-Pesa cb)     │  │
            │   └───────┬───────────────────┬──────────────┘  │
            │           │                   │                 │
            │   ┌───────▼──────┐    ┌───────▼───────┐         │
            │   │  web :3000   │──► │   api :8080   │         │  (web BFF → api via API_ORIGIN=http://api:8080)
            │   │  Next.js BFF │    │   Go / chi    │         │
            │   └──────────────┘    └───┬───────┬───┘         │
            │                           │       │             │
            │                  ┌────────▼──┐ ┌──▼─────────┐   │
            │                  │ postgres  │ │  redis     │   │  (private compose network; not host-published)
            │                  │ 16-alpine │ │  7-alpine  │   │
            │                  │  pgdata   │ │ redisdata  │   │
            │                  └───────────┘ └────────────┘   │
            └────────────────────────────────────────────────┘
```

Only Caddy publishes host ports (80/443). The browser only ever talks to
`https://WEB_DOMAIN`; the web app's same-origin BFF forwards server-side to the
API over the private compose network at `http://api:8080` (the
`API_ORIGIN`). Postgres + Redis are never exposed to the host, so
`sslmode=disable` on the private network is correct (config.go has no sslmode
requirement).

## CI image strategy

CI ([.github/workflows/ci.yml](../.github/workflows/ci.yml)) builds + smoke-tests
the api image on every push/PR (it does not push). On push to `main` it tags the
image with the commit SHA. **Pushing images to GHCR is owned by the CD workflow.**

## Continuous Deployment (CICD-4 / OPS-5)

The CD pipeline lives in [.github/workflows/deploy.yml](../.github/workflows/deploy.yml).
It runs on push to `main` and on `v*` tags, single-flight per ref
(`concurrency: deploy-<ref>`, `cancel-in-progress: false` so a migration is
never interrupted mid-flight).

It is intentionally a **safe no-op until the deployment secrets are configured**:
the three image build/push jobs always run (GHCR auth via the automatic
`GITHUB_TOKEN`), while the SSH deploy and smoke jobs skip cleanly when their
secrets are absent.

### Jobs

1. **build-push** — builds `services/api/Dockerfile` and pushes to GHCR at
   `ghcr.io/<owner>/<repo>-api`.
2. **build-push-web** — builds `apps/web/Dockerfile` → `…-web`.
3. **build-push-migrate** — builds `services/api/Dockerfile.migrate` (golang-migrate
   + the `services/api/migrations` SQL baked in) → `…-migrate`. This is what lets
   the droplet (no repo checkout) apply the schema.

   All three tag: `sha-<full-sha>` (immutable, per commit), `latest` (main branch
   only), and `<tag>` on `v*` tags. GHCR auth is the automatic `GITHUB_TOKEN` —
   no manually-created registry secret needed.

4. **deploy** — *gated on `DEPLOY_SSH_KEY`.* SSHes into the droplet
   (`appleboy/ssh-action`, pinned) as `DEPLOY_USER` (default `deploy`) on
   `DEPLOY_HOST` and, from `/opt/fuelgrid`, runs:
   - optional `docker login ghcr.io` (only if `GHCR_PULL_TOKEN` is set — needed
     only for a private package),
   - pins `API_IMAGE` / `WEB_IMAGE` / `MIGRATE_IMAGE` = `…:sha-<sha>` into the
     on-droplet `.env`,
   - `docker compose -f docker-compose.prod.yml pull api web migrate`,
   - `docker compose -f docker-compose.prod.yml run --rm migrate` (schema first),
   - `docker compose -f docker-compose.prod.yml up -d`,
   - `docker image prune -f`.

5. **smoke** — *gated on `DEPLOY_HEALTH_URL`.* Curls the deployed `/readyz` and
   **fails the deploy unless it returns `200` with `{"status":"ready"}`** (postgres
   + redis ok). Polls up to 30× with 5s backoff.

All jobs use `environment: production`, so GitHub Environment protection rules
apply when configured.

### Gating pattern (safe no-op)

Secrets can't be used in a job-level `if:`, so each guarded job's first step
checks the secret and exports `configured=true|false`; every real step is
`if: steps.gate.outputs.configured == 'true'` and emits a `::notice::` + skips
when the secret is absent. Until `DEPLOY_SSH_KEY` exists the pipeline still
builds + pushes all three images but does not touch the droplet; until
`DEPLOY_HEALTH_URL` exists the smoke gate no-ops.

### Required secrets / configuration

| Name | Where | Purpose | Behavior if unset |
|---|---|---|---|
| `GITHUB_TOKEN` | automatic | Push the three images to GHCR | Always present |
| `DEPLOY_SSH_KEY` | repo/environment secret | Private SSH key for the droplet deploy user | deploy job no-ops |
| `DEPLOY_HOST` | repo/environment secret | Droplet public IP / hostname | (used only by the gated deploy) |
| `DEPLOY_USER` | repo/environment secret | Deploy user (default `deploy`) | falls back to `deploy` |
| `DEPLOY_HEALTH_URL` | repo/environment secret | `https://<API_DOMAIN>/readyz` for the smoke gate | smoke gate no-ops |
| `GHCR_PULL_TOKEN` | repo/environment secret (optional) | PAT w/ `read:packages` if the GHCR package is private | compose pull runs unauthenticated (public package) |

### Deploy flow

```
push to main / v* tag
  → build-push, build-push-web, build-push-migrate  (3 images → GHCR :sha-…, :latest | :<tag>)
  → deploy   (SSH to droplet: pull → migrate one-shot → up -d → prune; gated on DEPLOY_SSH_KEY)
  → smoke    (curl /readyz @ DEPLOY_HEALTH_URL must be 200; gated on DEPLOY_HEALTH_URL)
```

## Database migrations on deploy

Migrations run **on the droplet inside the deploy**, NOT from the GitHub runner
(the self-hosted Postgres is not publicly reachable). The `build-push-migrate`
job publishes a dedicated migrate image — `migrate/migrate` with the
`services/api/migrations` directory baked in (see
[services/api/Dockerfile.migrate](../services/api/Dockerfile.migrate)). The
compose `migrate` service (profile `migrate`, `restart: "no"`) runs it as a
one-shot:

```
docker compose -f docker-compose.prod.yml run --rm migrate
```

which executes `migrate -path /migrations -database "$DATABASE_URL" up`.
`DATABASE_URL` is the table **OWNER** DSN (compose interpolates it from `.env`);
`up` is idempotent (no pending migrations → exit 0). The deploy runs this BEFORE
`up -d` — schema first, then roll out. The running API uses the **non-owner**
`fuelgrid_app` pool (`DATABASE_APP_URL`) so Postgres RLS enforces tenant
isolation on every request.

## Environment variables in production

The complete config + secret inventory — every variable, where it comes from,
which are **SECRET** vs config, and which **fail-stop** the boot outside dev —
lives in [.env.production.example](../.env.production.example). On the droplet
you `cp .env.production.example .env`, fill it in (chmod 600), and place it next
to `docker-compose.prod.yml`; the `api` service loads it via `env_file` and
compose interpolates `${VAR}` references (image refs, domains, DB creds).

Key fail-stops (config.go `validate`): `API_CORS_ALLOWED_ORIGINS` must be
explicit `https://` origins (no `*`, no `http://`); `DATABASE_APP_URL` must be
set and **distinct** from `DATABASE_URL`, pointing at the non-owner
`fuelgrid_app` role. `AUTH_PASSWORD_PEPPER` rotation invalidates all password
hashes + MFA-at-rest — treat it as permanent. The redaction model, rotation
procedures, and leak response live in
[docs/security/secrets.md](security/secrets.md).

## Observability in production

| Signal | Endpoint | Scraper |
|---|---|---|
| Metrics | `GET /metrics` (Prometheus) | A Prometheus/Grafana Agent on the droplet or a remote scraper reachable over the private network |
| Traces | OTLP over gRPC | Set `OTEL_EXPORTER=otlp` + `OTEL_EXPORTER_OTLP_ENDPOINT=…` (Tempo/Honeycomb) |
| Errors | Sentry | `SENTRY_DSN=…`, sample rate per env |
| Structured logs | container stdout (JSON) → `docker compose logs` / a log shipper | Standardized field names per the API middleware |

`/metrics` is not fronted by Caddy in this stack — it is reachable only inside
the compose network (the api service is not host-published), so scrape it from a
sidecar on the droplet or add a dedicated, authenticated Caddy route if you need
remote scraping. When `OTEL_EXPORTER=otlp` the endpoint MUST be reachable or the
API refuses to boot (no silent trace loss).

## Go-live runbook

The exact ordered steps an operator runs for the **first real deploy**. Replace
every `<...>` placeholder with your values; never commit a populated `.env`.

### Pre-launch checklist (all must be true before flipping traffic on)

- [ ] DNS A records for **both** `WEB_DOMAIN` and `API_DOMAIN` resolve to the droplet IP (Caddy needs this for ACME).
- [ ] DO firewall allows inbound **80/443** and **22 restricted** to your IP(s); nothing else.
- [ ] `API_CORS_ALLOWED_ORIGINS=https://<WEB_DOMAIN>` (https only — the API fail-stops on `*`/`http://`).
- [ ] `DATABASE_APP_URL` points at the non-owner `fuelgrid_app` role with a **strong** password, distinct from `DATABASE_URL`.
- [ ] `AUTH_PASSWORD_PEPPER` generated **once** and recorded safely (rotation invalidates every password hash + MFA enrollment).
- [ ] `/readyz` returns `200 {"status":"ready"}` (postgres + redis ok) and a login smoke passes.
- [ ] Nightly backup timer active (`systemctl list-timers fuelgrid-backup.timer`).
- [ ] GHCR pull works on the droplet (`docker compose pull` succeeds).
- [ ] GitHub Actions CD secrets set: `DEPLOY_SSH_KEY`, `DEPLOY_HOST`, `DEPLOY_USER`, `DEPLOY_HEALTH_URL`.

### (a) Create the Droplet

- Create an Ubuntu LTS (24.04) Droplet. A **2 vCPU / 4 GB** Droplet is a sane
  starting size for API + web + Postgres + Redis + Caddy on one box; scale up
  later. Add your **deploy SSH public key** at creation.
- Create a DO **Cloud Firewall** and attach it: allow inbound **TCP 22** (from
  your admin IPs only), **TCP 80**, **TCP 443** (and **UDP 443** for HTTP/3);
  deny everything else inbound.

### (b) Install Docker engine + compose plugin

```sh
ssh root@<droplet-ip>
curl -fsSL https://get.docker.com | sh        # installs engine + compose plugin
docker compose version                        # confirm the plugin is present
```

### (c) Create the deploy user + dirs, copy the stack files

```sh
# As root on the droplet:
adduser --disabled-password --gecos '' deploy
usermod -aG docker deploy
mkdir -p /opt/fuelgrid /var/backups/fuelgrid
chown -R deploy:deploy /opt/fuelgrid /var/backups/fuelgrid
# Authorize the CD deploy key for the deploy user:
mkdir -p /home/deploy/.ssh && cp ~/.ssh/authorized_keys /home/deploy/.ssh/ \
  && chown -R deploy:deploy /home/deploy/.ssh && chmod 700 /home/deploy/.ssh
```

From your workstation, copy the three files onto the droplet:

```sh
scp deploy/docker-compose.prod.yml deploy/Caddyfile \
    deploy:<droplet-ip>:/opt/fuelgrid/
# Create the .env from the example, fill it in, then copy it (0600):
cp .env.production.example .env   # edit it locally, NEVER commit it
scp .env deploy@<droplet-ip>:/opt/fuelgrid/.env
ssh deploy@<droplet-ip> 'chmod 600 /opt/fuelgrid/.env'
```

### (d) Point DNS at the droplet

Create A records for `WEB_DOMAIN` and `API_DOMAIN` → the droplet's public IP.
Wait for them to resolve (`dig +short <WEB_DOMAIN>`) before bringing Caddy up,
or ACME issuance will fail.

### (e) First migrate, then set the fuelgrid_app password

```sh
ssh deploy@<droplet-ip>
cd /opt/fuelgrid

# Pull images first (set API_IMAGE/WEB_IMAGE/MIGRATE_IMAGE in .env to the
# :sha-<sha> refs CD built, or :latest for a manual first cut):
docker compose -f docker-compose.prod.yml pull

# Apply migrations as the OWNER (creates the fuelgrid_app role with a WEAK default):
docker compose -f docker-compose.prod.yml run --rm migrate

# Rotate the non-owner role to a strong password and put it in DATABASE_APP_URL:
docker compose -f docker-compose.prod.yml exec postgres \
  psql -U fuelgrid -c "ALTER ROLE fuelgrid_app PASSWORD '<STRONG_APP_ROLE_PASSWORD>';"
# Then edit /opt/fuelgrid/.env so DATABASE_APP_URL uses <STRONG_APP_ROLE_PASSWORD>.
```

> The `migrate` one-shot only depends on postgres being healthy — compose starts
> just postgres (+ its deps) for the `run`. If postgres isn't up yet, run
> `docker compose -f docker-compose.prod.yml up -d postgres` first.

### (f) Bring the stack up

```sh
docker compose -f docker-compose.prod.yml up -d
docker compose -f docker-compose.prod.yml ps      # all healthy/running
docker compose -f docker-compose.prod.yml logs -f caddy   # watch ACME issue certs
```

### (g) Verify

```sh
curl -fsS https://<API_DOMAIN>/readyz             # expect 200 {"status":"ready"} (postgres+redis ok)
# Login smoke against the web BFF (no token leaves the server):
curl -i -X POST https://<WEB_DOMAIN>/api/bff/auth/login \
  -H 'content-type: application/json' \
  -d '{"email":"<user>","password":"<pw>"}'
```

### (h) Configure GitHub Actions secrets for CD

Under **Settings → Secrets and variables → Actions** (or the `production`
Environment):

```
DEPLOY_SSH_KEY     = <private key whose public half is in deploy's authorized_keys>
DEPLOY_HOST        = <droplet public IP / hostname>
DEPLOY_USER        = deploy
DEPLOY_HEALTH_URL  = https://<API_DOMAIN>/readyz
# Optional, only if the GHCR package is private:
GHCR_PULL_TOKEN    = <PAT with read:packages>
```

Until `DEPLOY_SSH_KEY` exists the CD pipeline still builds + pushes all three
images but no-ops the deploy + smoke jobs.

### (i) Enable nightly backups

```sh
# As root (or via sudo) on the droplet:
cp /opt/fuelgrid/pg_backup.sh /opt/fuelgrid/pg_backup.sh 2>/dev/null || true
scp deploy/backup/pg_backup.sh deploy@<droplet-ip>:/opt/fuelgrid/pg_backup.sh
ssh root@<droplet-ip> '
  chmod +x /opt/fuelgrid/pg_backup.sh
  cp /opt/fuelgrid/fuelgrid-backup.service /etc/systemd/system/ 2>/dev/null || true
'
scp deploy/backup/fuelgrid-backup.service deploy/backup/fuelgrid-backup.timer \
    root@<droplet-ip>:/etc/systemd/system/
ssh root@<droplet-ip> '
  systemctl daemon-reload
  systemctl enable --now fuelgrid-backup.timer
  systemctl list-timers fuelgrid-backup.timer
'
```

Backups are **logical** (`pg_dump -Fc`), rotated `BACKUP_KEEP_DAYS` days, with an
optional push to DigitalOcean Spaces when `SPACES_BUCKET` is set. Restore drill +
the PITR caveat: [deploy/backup/RESTORE.md](../deploy/backup/RESTORE.md).

### (j) Seeding note

Seeding is **prod-guarded** — `services/api/cmd/seed` refuses to run unless
`NODE_ENV=development` *or* `ALLOW_SEED=true`. Do **not** seed demo data into a
real production tenant. Provision real tenants via
`POST /api/v1/platform/tenants` (authenticated with `PLATFORM_ADMIN_TOKEN`).

## Branch protection (one-time setup)

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

## Defer list

- DigitalOcean Managed Postgres + PITR (or self-hosted WAL archiving) once the RPO requires it (see RESTORE.md).
- Horizontal scale-out (multiple Droplets / a load balancer) and remote `/metrics` scraping behind an authenticated route.
- Offsite-backup verification automation (a scheduled restore-drill job).
