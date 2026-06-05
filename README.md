# FuelGrid OS

[![CI](https://github.com/JAPHARYROMAN/FuelGrid-OS/actions/workflows/ci.yml/badge.svg)](https://github.com/JAPHARYROMAN/FuelGrid-OS/actions/workflows/ci.yml)
[![CD](https://github.com/JAPHARYROMAN/FuelGrid-OS/actions/workflows/deploy.yml/badge.svg)](https://github.com/JAPHARYROMAN/FuelGrid-OS/actions/workflows/deploy.yml)

**FuelGrid OS is an operating system for fuel businesses.**

It brings station operations, fuel inventory, sales, payments, credit,
procurement, finance, reporting, risk controls, and auditability into one
traceable command platform for single stations, multi-station chains, depots,
fleet operators, distributors, and enterprise fuel organizations.

The core rule is simple:

> Every liter, every shilling, every transaction, every approval, and every user
> action must be traceable.

FuelGrid OS is not a POS skin or a generic admin dashboard. It is a
financial-grade operations platform for businesses where physical fuel movement,
cash, credit, stock, procurement, and compliance all have to reconcile.

## Current Status

This repository contains the active FuelGrid OS web/back-office implementation:

- A Go API service with domain modules, migrations, background jobs, auth,
  RBAC, audit, outbox events, reporting, and operational workflows.
- A Next.js web application for command-center operations and administration.
- A typed TypeScript SDK and shared UI package used by the web app.
- A large documentation set covering product scope, architecture, deployment,
  security, database conventions, feature readiness, and OpenAPI.

The web/back-office surface is the current product focus. Mobile, offline sync,
and hardware integrations are intentionally deferred into a separate future
phase because they need real device and field validation. See the
[feature build matrix](docs/feature-build-matrix.md) for the authoritative
implementation tracker.

## What FuelGrid OS Covers

FuelGrid OS is organized around the workflows that run a fuel business.

| Area                     | Capabilities                                                                                                                   |
| ------------------------ | ------------------------------------------------------------------------------------------------------------------------------ |
| Setup and master data    | Guided setup, companies, regions, stations, products, tanks, pumps, nozzles, suppliers, opening stock, teams, employees, users |
| Identity and access      | Login, sessions, roles, permissions, station access, MFA foundation, password reset, tenant-scoped authorization               |
| Station operations       | Operating days, shift open/close/approval, attendant assignment, meter readings, shift timeline                                |
| Fuel inventory           | Opening balances, stock movements, deliveries, tank dips, reconciliations, stock adjustments, transfers, calibration           |
| Sales and payments       | POS surface, sales list, sale details, void workflow, tenders, split payments, payment reconciliation                          |
| Credit and receivables   | Customer management, credit limits, credit sales, invoices, statements, payment allocation, aging                              |
| Procurement and payables | Suppliers, purchase orders, delivery receiving, supplier invoices, payables aging                                              |
| Finance and banking      | Chart of accounts, accounting periods, journals, cash reconciliation, bank accounts, deposits                                  |
| Reports and documents    | Operational, inventory, financial, executive, cash, customer, supplier, and exportable reports                                 |
| Risk and governance      | Risk rules, alerts, investigations, data lifecycle controls, governance and enterprise visibility                              |
| Platform controls        | Tenant isolation, audit events, outbox events, notifications, scheduler jobs, health/readiness probes                          |

## Architecture At A Glance

```mermaid
flowchart LR
  Browser[Browser] --> Web[Next.js web app]
  Web --> BFF[Next.js BFF proxy]
  BFF --> API[Go API /api/v1]

  API --> Postgres[(PostgreSQL)]
  API --> Redis[(Redis)]
  API --> Scheduler[Scheduler jobs]
  API --> Outbox[Outbox publisher]
  API --> Reports[PDF / Excel reports]

  SDK[packages/sdk] --> Web
  UI[packages/ui] --> Web
  OpenAPI[docs/openapi.yaml] --> SDK
```

The backend is a modular monolith by design. Domain packages stay inside
`internal/*`, the HTTP service lives under `services/api`, and the database is
the system of record. This gives the project strong local-development ergonomics
without giving up the option to split out services later.

For the full design, read:

- [Technical architecture](docs/architecture.md)
- [Product blueprint](docs/blueprint.md)
- [Product requirements](docs/prd.md)
- [Multi-tenancy model](docs/multi-tenancy.md)
- [Deployment guide](docs/deployment.md)

## Tech Stack

| Layer         | Stack                                                                                            |
| ------------- | ------------------------------------------------------------------------------------------------ |
| Web app       | Next.js, React, TypeScript, TanStack Query, Tailwind CSS, shared `@fuelgrid/ui`                  |
| API           | Go, chi HTTP router, pgx, Redis, slog, OpenTelemetry/Sentry hooks                                |
| Database      | PostgreSQL, SQL migrations, tenant-aware schema conventions, optional RLS app role               |
| Cache/session | Redis for sessions, rate limits, scheduler coordination, and operational state                   |
| SDK           | TypeScript API client in `packages/sdk`, backed by `docs/openapi.yaml`                           |
| Tooling       | pnpm workspaces, Vitest, Playwright, Redocly, Docker Compose, GitHub Actions                     |
| Deployment    | Docker images, DigitalOcean Droplet + compose config, GHCR image publishing, guarded CD workflow |

## Repository Layout

```text
.
|-- apps/
|   `-- web/                     # Next.js command-center UI
|-- services/
|   `-- api/                     # Go API service, migrations, Docker config
|-- internal/                    # Go domain packages and platform modules
|-- packages/
|   |-- config/                  # Shared TypeScript config
|   |-- sdk/                     # Typed API client
|   `-- ui/                      # Shared UI component library
|-- docs/                        # Architecture, product, deployment, OpenAPI, runbooks
|-- deploy/                      # Deployment/supporting infrastructure assets
|-- loadtest/                    # Load-test assets
|-- docker-compose.yml           # Local Postgres and Redis
|-- go.mod                       # Go module root
|-- package.json                 # Root pnpm workspace scripts
`-- pnpm-workspace.yaml          # JS workspace definition
```

## Prerequisites

| Tool    | Version                                        |
| ------- | ---------------------------------------------- |
| Go      | 1.25, matching [go.mod](go.mod)                |
| Node.js | 22, matching [.nvmrc](.nvmrc)                  |
| pnpm    | 10.x, via Corepack                             |
| Docker  | Docker Compose v2 for local Postgres and Redis |

Optional:

- `make`, for the API Makefile helpers.
- `golangci-lint`, for local Go linting.
- `psql` and `redis-cli`, for inspecting local services.

## Quickstart

Clone the repository and install JavaScript dependencies:

```sh
corepack enable
pnpm install
```

Create local environment files:

```sh
cp .env.example .env
```

If you run the web app through the Next.js BFF proxy, make sure the web app
points at the same API port you run locally:

```sh
# apps/web/.env.local
NEXT_PUBLIC_API_URL=http://localhost:8080
API_ORIGIN=http://localhost:8080
NEXT_PUBLIC_APP_NAME=FuelGrid OS
NEXT_PUBLIC_APP_ENV=development
```

Start Postgres and Redis:

```sh
docker compose up -d postgres redis
```

Export the variables needed by the Go commands. The Go binaries read the
process environment; they do not automatically source `.env`.

```sh
export NODE_ENV=development
export DATABASE_URL="postgres://fuelgrid:fuelgrid@localhost:5432/fuelgrid?sslmode=disable"
export REDIS_URL="redis://localhost:6379/0"
export AUTH_PASSWORD_PEPPER="dev-pepper"
```

Apply migrations and seed the development tenant:

```sh
go run ./services/api/cmd/migrate up
go run ./services/api/cmd/seed
```

Start the API:

```sh
go run ./services/api/cmd/api
```

In another terminal, start the web app:

```sh
pnpm --filter @fuelgrid/web dev
```

Open:

- Web app: [http://localhost:3000](http://localhost:3000)
- API health: [http://localhost:8080/healthz](http://localhost:8080/healthz)
- API readiness: [http://localhost:8080/readyz](http://localhost:8080/readyz)

### Demo Login

The development seed creates a demo tenant and two users:

| Role            | Tenant | Email                  | Password                       |
| --------------- | ------ | ---------------------- | ------------------------------ |
| System admin    | `demo` | `admin@fuelgrid.local` | `fuelgrid-admin-password-1234` |
| Station manager | `demo` | `demo@fuelgrid.local`  | `fuelgrid-demo-password-1234`  |

Use the same `AUTH_PASSWORD_PEPPER` when seeding and when running the API. If
you change the pepper after seeding, the existing seeded passwords will not
verify. Reset the local database volume or reseed with matching credentials.

## Common Commands

Run these from the repository root unless noted.

### JavaScript and TypeScript

```sh
pnpm lint
pnpm typecheck
pnpm test
pnpm build
pnpm format
pnpm format:check
```

Focused package commands:

```sh
pnpm --filter @fuelgrid/web dev
pnpm --filter @fuelgrid/web test
pnpm --filter @fuelgrid/web typecheck
pnpm --filter @fuelgrid/sdk test
pnpm --filter @fuelgrid/sdk typecheck
```

### Go

```sh
go test ./...
go test -race ./...
go vet ./...
go build ./...
go mod tidy
```

Focused API helpers:

```sh
go run ./services/api/cmd/migrate up
go run ./services/api/cmd/migrate version
go run ./services/api/cmd/seed
go run ./services/api/cmd/api
```

### OpenAPI

```sh
npx --yes @redocly/cli@latest lint docs/openapi.yaml
```

## Testing And CI

GitHub Actions currently includes:

- [CI](.github/workflows/ci.yml): format check, lint, typecheck, test, build,
  OpenAPI linting, Go tests, migrations/integration checks, and image smoke
  tests.
- [E2E](.github/workflows/e2e.yml): Playwright browser flows.
- [Load tests](.github/workflows/loadtest.yml): load-test workflow assets.
- [Security scan](.github/workflows/scan.yml): repository security checks.
- [CD](.github/workflows/deploy.yml): guarded deployment workflow for main/tags.

Some integration tests require real local services and use environment variables
such as `TEST_DATABASE_URL` and `TEST_REDIS_URL`. Unit tests and package
typechecks run without those integration variables.

## API And Contracts

The OpenAPI contract lives at [docs/openapi.yaml](docs/openapi.yaml). The web
app calls the API through the shared SDK in [packages/sdk](packages/sdk), and
the Next.js app can proxy browser requests through its BFF route.

Important API surfaces:

- `/api/v1/auth/*` for login, session, MFA, password, and identity flows.
- `/api/v1/setup/*` for tenant setup readiness.
- `/api/v1/stations`, `/products`, `/tanks`, `/pumps`, `/nozzles` for master data.
- `/api/v1/shifts/*`, `/operating-days/*`, and readings endpoints for station operations.
- Inventory, procurement, receivables, payables, finance, reporting, risk, and
  governance endpoints under their respective domain routes.

Operational probes:

- `/healthz`: process is alive.
- `/readyz`: configured dependencies are reachable.
- `/metrics`: Prometheus metrics.

## Documentation Map

| Document                                                                                       | Purpose                                        |
| ---------------------------------------------------------------------------------------------- | ---------------------------------------------- |
| [docs/blueprint.md](docs/blueprint.md)                                                         | Product vision and domain blueprint            |
| [docs/prd.md](docs/prd.md)                                                                     | Product requirements                           |
| [docs/architecture.md](docs/architecture.md)                                                   | Full technical architecture                    |
| [docs/openapi.yaml](docs/openapi.yaml)                                                         | API contract                                   |
| [docs/feature-build-matrix.md](docs/feature-build-matrix.md)                                   | Feature implementation tracker                 |
| [docs/feature-improvement-and-addition-plan.md](docs/feature-improvement-and-addition-plan.md) | Improvement and addition plan                  |
| [docs/implementation-checklist.md](docs/implementation-checklist.md)                           | Implementation checklist                       |
| [docs/permissions-matrix.md](docs/permissions-matrix.md)                                       | Permission coverage                            |
| [docs/audit-events-matrix.md](docs/audit-events-matrix.md)                                     | Audit-event coverage                           |
| [docs/deployment.md](docs/deployment.md)                                                       | Production deployment plan                     |
| [docs/db-conventions.md](docs/db-conventions.md)                                               | Database conventions                           |
| [docs/db-performance.md](docs/db-performance.md)                                               | Database performance notes                     |
| [docs/multi-tenancy.md](docs/multi-tenancy.md)                                                 | Tenant isolation and access model              |
| [docs/runbook-migrations.md](docs/runbook-migrations.md)                                       | Migration runbook                              |
| [docs/security/secrets.md](docs/security/secrets.md)                                           | Secret inventory and handling                  |
| [CONTRIBUTING.md](CONTRIBUTING.md)                                                             | Contribution rules and engineering conventions |

## Deployment

The current deployment target is a **DigitalOcean Droplet** running
`docker compose` (self-hosted Postgres + Redis behind a Caddy reverse proxy that
does automatic HTTPS), with Docker images, the production compose stack in
[`deploy/`](deploy/), GHCR image publishing, and a guarded SSH-based CD workflow
already in the repository.

The CD workflow is designed to be safe before production secrets are configured:
it can build and publish images, while migration and smoke-test jobs skip when
their required deployment secrets are absent.

See [docs/deployment.md](docs/deployment.md) for:

- Target platform rationale.
- API and web deploy topology.
- Required production environment variables.
- Migration strategy.
- Rollback and smoke-test flow.
- Secret and operational readiness checklist.

## Security And Data Integrity

FuelGrid OS is built around operational traceability:

- Tenant-scoped data model with explicit tenant IDs throughout the schema.
- Optional request-scoped PostgreSQL app role for RLS-backed tenant isolation.
- Role and permission checks on protected API routes.
- Station-access scoping for station-bound users.
- Append-oriented stock, payment, audit, and event records.
- Audit and outbox records for sensitive state changes.
- Password hashing with an application pepper.
- Secret redaction through typed config values.
- Login rate limiting and account lockout support.
- Health, readiness, metrics, and background job observability.

See:

- [docs/security/secrets.md](docs/security/secrets.md)
- [docs/multi-tenancy.md](docs/multi-tenancy.md)
- [docs/db-conventions.md](docs/db-conventions.md)
- [docs/audit-events-matrix.md](docs/audit-events-matrix.md)

## Contributing

Read [CONTRIBUTING.md](CONTRIBUTING.md) before changing the codebase. The key
rules are:

- Preserve tenant isolation.
- Keep ledgers append-oriented.
- Add audit and outbox coverage for sensitive writes.
- Keep migrations reversible and reviewed.
- Prefer existing domain patterns over new abstractions.
- Update OpenAPI, SDK, frontend, permissions, and tests together when adding a
  contract.

## License

Proprietary. All rights reserved.
