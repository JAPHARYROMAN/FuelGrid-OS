# FuelGrid OS

**The operating system for modern fuel businesses.**

FuelGrid OS is a full-fledged fuel business operating system for single
stations, multi-station chains, depots, fleet operators, distributors, and
enterprise fuel organizations. It unifies station operations, fuel inventory,
tank and pump management, sales, payments, finance, procurement, customer
credit, fleet fueling, risk detection, rule-based intelligence, reporting, audit, and
hardware integrations into one premium command platform.

Its core principle: **every liter and every shilling must be traceable.** Every
significant movement — fuel received, transferred, sold, adjusted, or lost; cash
collected, submitted, deposited; prices changed; shifts opened and closed —
produces an immutable record. FuelGrid OS behaves like a financial ledger for
physical fuel operations.

See [`docs/blueprint.md`](docs/blueprint.md) for the full product blueprint and
[`docs/architecture.md`](docs/architecture.md) for the technical architecture.

## Monorepo layout

This is a single repository containing the Go backend, the Next.js frontend, and
shared TypeScript packages. Workspaces are wired via
[`pnpm-workspace.yaml`](pnpm-workspace.yaml) (`apps/*`, `services/*`,
`packages/*`); the Go module is rooted at the repository root (see
[`go.mod`](go.mod)).

| Path           | Contents                                                                                                                                                                                                                                                                                                                                                                                                   |
| -------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `apps/web`     | Next.js / React web app (the command center UI)                                                                                                                                                                                                                                                                                                                                                            |
| `apps/mobile`  | Mobile field app (attendants, supervisors, delivery receivers)                                                                                                                                                                                                                                                                                                                                             |
| `services/api` | Go API service — HTTP server, migrations, and seed tooling (`cmd/api`, `cmd/migrate`, `cmd/seed`)                                                                                                                                                                                                                                                                                                          |
| `internal/*`   | Go domain packages — `identity`, `companies`, `regions`, `stations`, `tanks`, `pumps`, `nozzles`, `readings`, `operations`, `inventory`, `reconciliation`, `procurement`, `payables`, `receivables`, `revenue`, `payments`, `pricing`, `products`, `calibration`, `fleet`, `risk`, `incidents`, `expenses`, `accounting`, `banking`, `enterprise`, `audit`, `events`, `observability`, `cache`, `database` |
| `packages/*`   | Shared TypeScript packages — `config` (ESLint/TS config), `types`, `sdk` (typed API client), `ui` (component library)                                                                                                                                                                                                                                                                                      |
| `docs/*`       | Product blueprint, architecture, PRD, roadmap, DB conventions, and the OpenAPI spec                                                                                                                                                                                                                                                                                                                        |

## Prerequisites

| Tool   | Version                                              |
| ------ | ---------------------------------------------------- |
| Go     | 1.25 (matches [`go.mod`](go.mod))                    |
| Node   | 22 (LTS) — see [`.nvmrc`](.nvmrc)                    |
| pnpm   | 10.x — enabled via Corepack                          |
| Docker | with Compose v2 (`docker compose`) for the dev stack |

Optional but recommended: [`golangci-lint`](https://golangci-lint.run/) for Go
linting (see [`.golangci.yml`](.golangci.yml)) and `make` to use the API
service's [`Makefile`](services/api/Makefile) targets.

## Quickstart

```sh
# 1. Install Node toolchain + JS dependencies
corepack enable
pnpm install

# 2. Configure environment
cp .env.example .env   # adjust values as needed for local dev

# 3. Start the local dev stack (Postgres + Redis) in the background
docker compose up -d

# 4. Apply database migrations
go run ./services/api/cmd/migrate up

# (optional) seed a demo tenant/company/region/station + users
go run ./services/api/cmd/seed

# 5. Run the API (listens on :8080 by default)
go run ./services/api/cmd/api

# 6. In another terminal, run the web app (listens on :3000)
pnpm --filter @fuelgrid/web dev
```

The web app reads `NEXT_PUBLIC_API_URL` (default `http://localhost:8080`) to
reach the API. Health and readiness probes are served at `/healthz` and
`/readyz`; Prometheus metrics at `/metrics`.

> **Note:** `cmd/seed` requires `AUTH_PASSWORD_PEPPER` to be set (any value in
> dev) so seeded users get a valid password hash. The same pepper must be set
> when running the API if you intend to log in as a seeded user.

The API service [`Makefile`](services/api/Makefile) wraps the common commands —
`make -C services/api db-up`, `migrate-up`, `seed`, and `run`. Run
`make -C services/api help` for the full list.

## Common commands

### JavaScript / TypeScript (run from the repo root)

```sh
pnpm lint            # ESLint across all workspaces
pnpm typecheck       # tsc --noEmit across all workspaces
pnpm test            # run package test suites
pnpm build           # build all buildable workspaces
pnpm format          # Prettier write
pnpm format:check    # Prettier check (CI-equivalent)
```

### Go (run from the repo root)

```sh
go vet ./...                 # static analysis
golangci-lint run ./...      # lint (config in .golangci.yml)
go test -race ./...          # tests with the race detector
go build ./...               # compile everything
go mod tidy                  # keep go.mod/go.sum tidy
```

### OpenAPI

```sh
npx --yes @redocly/cli@latest lint docs/openapi.yaml
```

CI ([`.github/workflows/ci.yml`](.github/workflows/ci.yml)) runs all of the
above plus a migrations/integration job that applies migrations, seeds demo
data, boots the API, and probes `/readyz`.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for branching, Conventional Commit
conventions, code style, migration rules, and the non-negotiable domain rules
(tenant scoping, append-only ledgers, audit + outbox on sensitive writes).

## License

Proprietary. All rights reserved.
