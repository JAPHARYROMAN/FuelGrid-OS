# FuelGrid on the ITEMBA-R Droplet

This is the supported deployment layout when FuelGrid and ITEMBA-R share one
DigitalOcean droplet. They share only Caddy and an edge Docker network. Their
application containers, databases, Redis data, secrets, and volumes remain
separate.

## Topology

```text
Internet :80/:443
        |
ITEMBA-R Caddy
        |-- app.itembagrouptz.com ----------> ITEMBA-R frontend
        |-- api.itembagrouptz.com ----------> ITEMBA-R backend
        |-- itembagrouptz.com --------------> ITEMBA-R website
        |-- fuelgrid.itembagrouptz.com -----> fuelgrid-web:3000
        `-- api.fuelgrid.itembagrouptz.com -> fuelgrid-api:8080

fuelgrid-web -> fuelgrid-api -> FuelGrid Postgres
                            `-> FuelGrid Redis
```

FuelGrid does not join ITEMBA-R's private application network. ITEMBA-R's Caddy
does not join FuelGrid's private data network. Only `fuelgrid-web` and
`fuelgrid-api` join `itemba_shared_edge`.

## Prerequisites

- Existing ITEMBA-R production stack under `/opt/itemba-r`.
- FuelGrid deployment directory `/opt/fuelgrid`, owned by the deploy user.
- A 4 GB swap file on the 4 GB droplet.
- DNS A records pointing to the ITEMBA-R droplet:
  - `fuelgrid.itembagrouptz.com`
  - `api.fuelgrid.itembagrouptz.com`
- FuelGrid GHCR images produced by `.github/workflows/deploy.yml`.
- A repository self-hosted runner with the `fuelgrid-prod` label. Install it on
  a replacement droplet with `deploy/install-self-hosted-runner.sh` and a
  short-lived GitHub runner registration token.

Do not start `deploy/docker-compose.prod.yml` on this host. It contains a second
Caddy service and would compete with ITEMBA-R for ports 80 and 443.

## First installation

Deploy the ITEMBA-R Caddy changes first. Its deployment creates the shared edge
network and adds both FuelGrid host routes. Until FuelGrid starts, those routes
correctly return 502 without affecting ITEMBA-R.

Then prepare FuelGrid:

```bash
sudo mkdir -p /opt/fuelgrid /var/backups/fuelgrid
sudo chown -R deploy:deploy /opt/fuelgrid /var/backups/fuelgrid

cd /opt/fuelgrid
cp /path/to/FuelGrid-OS/deploy/docker-compose.shared-droplet.yml .
cp /path/to/FuelGrid-OS/deploy/backup/pg_backup.sh .
cp /path/to/FuelGrid-OS/.env.production.example .env
chmod 600 .env
chmod 755 pg_backup.sh
```

Set at least these values in `/opt/fuelgrid/.env`:

```dotenv
WEB_DOMAIN=fuelgrid.itembagrouptz.com
API_DOMAIN=api.fuelgrid.itembagrouptz.com
SHARED_EDGE_NETWORK=itemba_shared_edge

POSTGRES_USER=fuelgrid
POSTGRES_DB=fuelgrid
POSTGRES_PASSWORD=<strong owner password>
DATABASE_APP_PASSWORD=<openssl rand -hex 32>
ALLOW_INITIAL_DATABASE_BOOTSTRAP=true
DATABASE_URL=postgres://fuelgrid:<owner password>@postgres:5432/fuelgrid?sslmode=disable
DATABASE_APP_URL=postgres://fuelgrid_app:<app password>@postgres:5432/fuelgrid?sslmode=disable

API_CORS_ALLOWED_ORIGINS=https://fuelgrid.itembagrouptz.com
APP_BASE_URL=https://fuelgrid.itembagrouptz.com
MPESA_CALLBACK_URL=https://api.fuelgrid.itembagrouptz.com/api/v1/payments/mpesa/callback
```

Fill every fail-stop secret in `.env.production.example`. Use URI-safe
alphanumeric or hexadecimal database passwords so DSNs remain valid.
`ALLOW_INITIAL_DATABASE_BOOTSTRAP=true` is a one-use authorization for the first
empty database. The deployment changes it to `false` after a successful rollout;
subsequent deployments stop if the named volume or migration history is missing.

Pin the three immutable images built for the same commit:

```dotenv
API_IMAGE=ghcr.io/japharyroman/fuelgrid-os-api:sha-<commit>
WEB_IMAGE=ghcr.io/japharyroman/fuelgrid-os-web:sha-<commit>
MIGRATE_IMAGE=ghcr.io/japharyroman/fuelgrid-os-migrate:sha-<commit>
```

Bring up the first release:

```bash
cd /opt/fuelgrid
docker network inspect itemba_shared_edge >/dev/null 2>&1 \
  || docker network create itemba_shared_edge

docker compose -f docker-compose.shared-droplet.yml config >/dev/null
docker compose -f docker-compose.shared-droplet.yml pull api web migrate
docker compose -f docker-compose.shared-droplet.yml up -d postgres redis
docker compose -f docker-compose.shared-droplet.yml run --rm migrate
docker compose -f docker-compose.shared-droplet.yml run --rm db-role-sync
docker compose -f docker-compose.shared-droplet.yml \
  up -d --remove-orphans postgres redis api web
```

`--remove-orphans` removes an obsolete FuelGrid-owned Caddy container if the
standalone stack was previously attempted. It does not touch ITEMBA-R's Caddy,
which belongs to a different Compose project.

## ITEMBA-R launcher settings

Keep these values in `/opt/itemba-r/.env.production`:

```dotenv
SHARED_EDGE_NETWORK=itemba_shared_edge
FUELGRID_APP_HOST=fuelgrid.itembagrouptz.com
FUELGRID_API_HOST=api.fuelgrid.itembagrouptz.com
FUELGRID_APP_URL=https://fuelgrid.itembagrouptz.com
FUELGRID_HEALTH_URL=https://api.fuelgrid.itembagrouptz.com/readyz
```

FuelGrid remains separately authenticated in this stage. The ITEMBA-R launcher
only checks readiness and opens FuelGrid in a new tab.

## Verification

```bash
curl -fsS https://api.itembagrouptz.com/api/v1/health/ready
curl -fsS https://app.itembagrouptz.com/api/health
curl -fsS https://api.fuelgrid.itembagrouptz.com/readyz
curl -fsS https://fuelgrid.itembagrouptz.com/api/health

docker compose -f /opt/itemba-r/docker-compose.production.yml \
  --env-file /opt/itemba-r/.env.production ps
docker compose -f /opt/fuelgrid/docker-compose.shared-droplet.yml \
  --env-file /opt/fuelgrid/.env ps
docker stats --no-stream
free -h
```

Expected API readiness contains `"status":"ready"`. The FuelGrid web health
route contains `"service":"fuelgrid-web"`.

## Deployment order and rollback

For normal releases, FuelGrid's CD workflow pulls immutable images, migrates,
synchronizes the runtime database role, and replaces only FuelGrid containers.
It takes and verifies a pre-migration database backup and does not restart
ITEMBA-R. If the persistent volume is missing, deployment fails before migration.

To roll FuelGrid back, restore the previous three `:sha-...` image values in
`/opt/fuelgrid/.env`, then run:

```bash
docker compose -f /opt/fuelgrid/docker-compose.shared-droplet.yml pull api web migrate
docker compose -f /opt/fuelgrid/docker-compose.shared-droplet.yml up -d api web
```

Do not automatically roll database migrations down. Use the migration-specific
runbook if a schema change is not backward compatible.

## Capacity guardrails

The shared Compose file caps FuelGrid at approximately 2 GB across web, API,
Postgres, Redis, and one-shot jobs. Leave Prometheus and Grafana disabled on the
4 GB droplet. Review `docker stats --no-stream`, swap usage, and disk space after
go-live. Upgrade to 8 GB before enabling the observability stack or sustained
high-volume report generation.
