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
.github/workflows/deploy.yml     # gated CD: release images, backup, rollout, verification, rollback
deploy/images/                   # hardened Caddy/Postgres and digest-pinned Redis definitions
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

The release gate first requires the **CI**, **E2E**, and **Security Scan**
workflows to pass for the exact commit. Five image build/push jobs then run on
GitHub-hosted runners (GHCR auth via the automatic `GITHUB_TOKEN`). The deploy
job runs on a dedicated
self-hosted runner installed on the production droplet with the label
`fuelgrid-prod`; that runner polls GitHub over outbound HTTPS, so SSH can remain
locked down to operator IPs. The smoke job still runs from GitHub-hosted infra
and fails closed when `DEPLOY_HEALTH_URL` is absent.

### Jobs

1. **release-gate** — waits for CI, E2E, and Security Scan to succeed for the
   exact source commit. A failed or missing gate prevents image publication.
2. **build-push** — builds `services/api/Dockerfile` and pushes to GHCR at
   `ghcr.io/<owner>/<repo>-api`.
3. **build-push-web** — builds `apps/web/Dockerfile` → `…-web`.
4. **build-push-migrate** — builds `services/api/Dockerfile.migrate` (golang-migrate
   + the `services/api/migrations` SQL baked in) → `…-migrate`. This is what lets
   the droplet apply the schema.
5. **build-push-caddy / build-push-postgres** — build the pinned, hardened
   infrastructure images from `deploy/images/`. The deploy records their exact
   registry digests, so production never relies on a mutable base-image tag.

   Application images tag: `sha-<full-sha>` (immutable, per commit), `latest` (main branch
   only), and `<tag>` on `v*` tags. GHCR auth is the automatic `GITHUB_TOKEN` —
   no manually-created registry secret needed.

6. **deploy** — runs on the production droplet's self-hosted runner
   (`runs-on: [self-hosted, Linux, X64, fuelgrid-prod]`) as the `deploy` user
   and, from `/opt/fuelgrid`, runs:
   - installs the versioned compose file, Caddyfile, and backup script,
   - captures the current application image set for rollback,
   - creates a verified logical database backup and verifies its offsite object,
   - optional `docker login ghcr.io` (only if `GHCR_PULL_TOKEN` is set — needed
     only for a private package),
   - pins application SHA tags and Caddy/Postgres digests into `.env`,
   - pulls the five published release images plus the digest-pinned Redis image,
   - `docker compose -f docker-compose.prod.yml run --rm -T migrate` (schema first),
   - normalizes persistent Caddy volume ownership for its non-root runtime,
   - `docker compose -f docker-compose.prod.yml up -d`,
   - verifies API readiness and the public web login page,
   - restores the prior application images automatically if rollout verification fails.

7. **smoke** — curls the deployed `/readyz` from GitHub-hosted infrastructure and
   **fails the deploy unless it returns `200` with `{"status":"ready"}`** (postgres
   + redis ok). Polls up to 30× with 5s backoff.

The deploy and smoke jobs use `environment: production`, so GitHub Environment
protection rules apply at the point production can change.

### Required secrets / configuration

| Name | Where | Purpose | Behavior if unset |
|---|---|---|---|
| `GITHUB_TOKEN` | automatic | Push the five images to GHCR | Always present |
| `DEPLOY_HEALTH_URL` | repo/environment secret | `https://<API_DOMAIN>/readyz` for rollout and external smoke gates | deployment fails closed |
| `GHCR_PULL_TOKEN` | repo/environment secret (optional) | PAT w/ `read:packages` if the GHCR package is private | compose pull runs unauthenticated (public package) |

The droplet `.env` must also contain `SPACES_BUCKET`, `SPACES_ENDPOINT`,
`SPACES_ACCESS_KEY_ID`, and `SPACES_SECRET_ACCESS_KEY`. The `aws` CLI must be
installed on the runner host. Production migration does not start until the
backup archive and uploaded Spaces object have both been verified.

Infrastructure requirement: register one repo self-hosted runner on the droplet,
running as `deploy`, with labels `self-hosted`, `Linux`, `X64`, and
`fuelgrid-prod`. The runner service must be enabled and the `deploy` user must
belong to the `docker` group.

### Deploy flow

```
push to main / v* tag
  → release-gate (CI + E2E + Security Scan for this SHA)
  → five parallel image builds (api, web, migrate, hardened Caddy, hardened Postgres)
  → deploy (install manifests → verified offsite backup → pull → migrate → up)
  → verify API + web (automatic app-image rollback on failure)
  → external smoke (/readyz must be ready)
```

## Database migrations on deploy

Migrations run **on the droplet inside the deploy**, not from a GitHub-hosted
runner (the self-hosted Postgres is not publicly reachable). The
`build-push-migrate` job publishes a dedicated migrate image — `migrate/migrate`
with the
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
- [ ] DigitalOcean Spaces credentials are configured and an upload/restore drill has passed.
- [ ] `aws` CLI is installed for the mandatory pre-deploy offsite backup.
- [ ] GHCR pull works on the droplet (`docker compose pull` succeeds).
- [ ] GitHub Actions self-hosted runner is online with label `fuelgrid-prod`.
- [ ] GitHub Actions CD smoke secret set: `DEPLOY_HEALTH_URL`.

### (a) Create the Droplet

- Create an Ubuntu LTS (24.04) Droplet. A **2 vCPU / 4 GB** Droplet is a sane
  starting size for API + web + Postgres + Redis + Caddy on one box; scale up
  later. Add your **operator SSH public key** at creation.
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
# Authorize your operator key for the deploy user:
mkdir -p /home/deploy/.ssh && cp ~/.ssh/authorized_keys /home/deploy/.ssh/ \
  && chown -R deploy:deploy /home/deploy/.ssh && chmod 700 /home/deploy/.ssh
```

For initial bootstrap, copy the stack files and `.env` onto the droplet. Future
CD runs install the versioned compose file, Caddyfile, and backup script from the
checked-out release before any migration.

```sh
scp deploy/docker-compose.prod.yml deploy/Caddyfile \
    deploy:<droplet-ip>:/opt/fuelgrid/
scp deploy/backup/pg_backup.sh deploy@<droplet-ip>:/opt/fuelgrid/pg_backup.sh
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

# Pull images first (set application images to immutable :sha-<sha> refs and
# hardened infrastructure images to the digest-pinned refs produced by CD):
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
  -H 'Origin: https://<WEB_DOMAIN>' \
  -H 'content-type: application/json' \
  -d '{"email":"<user>","password":"<pw>"}'
```

### (h) Register the GitHub Actions deploy runner

Create a repository self-hosted runner from **Settings → Actions → Runners**.
Install it on the droplet as the `deploy` user, add the label `fuelgrid-prod`,
and install it as a systemd service. The resulting runner must show as online
with labels `self-hosted`, `Linux`, `X64`, and `fuelgrid-prod`.

The runner service keeps an outbound connection to GitHub; no inbound GitHub SSH
access is needed. Keep firewall port 22 restricted to operator IPs.

Verify on the droplet:

```sh
systemctl is-active actions.runner.*.service
sudo -u deploy docker ps
```

Then set the smoke secret under **Settings → Secrets and variables → Actions**
(or the `production` Environment):

```
DEPLOY_HEALTH_URL  = https://<API_DOMAIN>/readyz
# Optional, only if the GHCR package is private:
GHCR_PULL_TOKEN    = <PAT with read:packages>
```

Until the runner is online, the deploy job waits for a matching runner. If
`DEPLOY_HEALTH_URL` is absent, both rollout verification and the external smoke
gate fail closed.

### (i) Enable nightly backups

```sh
# From the workstation:
scp deploy/backup/pg_backup.sh deploy@<droplet-ip>:/opt/fuelgrid/pg_backup.sh
scp deploy/backup/fuelgrid-backup.service deploy/backup/fuelgrid-backup.timer \
    root@<droplet-ip>:/etc/systemd/system/
ssh root@<droplet-ip> '
  apt-get update && apt-get install -y awscli
  chmod +x /opt/fuelgrid/pg_backup.sh
  systemctl daemon-reload
  systemctl enable --now fuelgrid-backup.timer
  systemctl list-timers fuelgrid-backup.timer
'
```

Backups are **logical** (`pg_dump -Fc`), rotated `BACKUP_KEEP_DAYS` days, with an
offsite push to DigitalOcean Spaces. The CD workflow sets
`REQUIRE_OFFSITE_BACKUP=1`; migration is blocked unless local archive validation,
upload, and remote object verification all succeed. Restore drill + the PITR
caveat: [deploy/backup/RESTORE.md](../deploy/backup/RESTORE.md).

### (j) Seeding note

Seeding is **prod-guarded** — `services/api/cmd/seed` refuses to run when
`NODE_ENV=production`. Non-development, non-production environments require
`ALLOW_SEED=true` and explicit seed passwords. Do **not** seed demo data into a
real production tenant. Provision real tenants via
`POST /api/v1/platform/tenants` (authenticated with `PLATFORM_ADMIN_TOKEN`).

## Branch protection (one-time setup)

```sh
gh api -X PUT repos/JAPHARYROMAN/FuelGrid-OS/branches/main/protection \
    -F required_status_checks.strict=true \
    -F required_status_checks.checks[][context]=Node — lint, typecheck, test, build \
    -F required_status_checks.checks[][context]=Go — vet, lint, test, build \
    -F required_status_checks.checks[][context]=Migrations — apply, seed, /readyz check \
    -F required_status_checks.checks[][context]=Docker — build and smoke API image \
    -F required_status_checks.checks[][context]=Deployment — validate workflows, compose, and scripts \
    -F required_status_checks.checks[][context]=Playwright — production build journeys \
    -F required_status_checks.checks[][context]=Playwright — real API auth smoke \
    -F required_status_checks.checks[][context]=Trivy — filesystem (vuln + misconfig + secret) \
    -F required_status_checks.checks[][context]=Trivy — api image + SBOM \
    -F required_status_checks.checks[][context]=Trivy — web image + SBOM \
    -F required_status_checks.checks[][context]=Trivy — migrate image + SBOM \
    -F required_status_checks.checks[][context]=Trivy — caddy image + SBOM \
    -F required_status_checks.checks[][context]=Trivy — postgres image + SBOM \
    -F required_status_checks.checks[][context]=Trivy — redis image + SBOM \
    -F enforce_admins=true \
    -F required_pull_request_reviews.required_approving_review_count=1 \
    -F required_pull_request_reviews.dismiss_stale_reviews=true \
    -F restrictions=null
```

## Defer list

- DigitalOcean Managed Postgres + PITR (or self-hosted WAL archiving) once the RPO requires it (see RESTORE.md).
- Horizontal scale-out (multiple Droplets / a load balancer) and remote `/metrics` scraping behind an authenticated route.
- Automated full restore drills (offsite object verification already gates every deploy).
