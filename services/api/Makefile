# services/api/Makefile
# Invoke from this directory or via `make -C services/api <target>`.
# The Go module is rooted at the repo root; Go finds it by walking up.

APP        ?= fuelgrid-api
VERSION    ?= dev
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
ROOT       := $(shell git rev-parse --show-toplevel 2>/dev/null)
MIGRATIONS := $(ROOT)/services/api/migrations

.PHONY: help run build test test-race vet lint tidy clean \
        docker-build db-up db-down db-logs db-psql db-redis \
        migrate-up migrate-down migrate-down-all migrate-version migrate-new \
        seed

help:
	@echo "App lifecycle:"
	@echo "  run                  go run the API"
	@echo "  build                produce a static binary at ./bin/$(APP)"
	@echo "  test                 run unit tests"
	@echo "  test-race            run tests with the race detector"
	@echo "  vet                  go vet"
	@echo "  lint                 golangci-lint"
	@echo "  tidy                 go mod tidy"
	@echo "  clean                remove build artifacts"
	@echo "  docker-build         build the Docker image"
	@echo ""
	@echo "Local infra (docker compose):"
	@echo "  db-up                start Postgres + Redis in the background"
	@echo "  db-down              stop them (keep volumes)"
	@echo "  db-logs              tail container logs"
	@echo "  db-psql              psql shell into Postgres"
	@echo "  db-redis             redis-cli shell into Redis"
	@echo ""
	@echo "Migrations:"
	@echo "  migrate-up           apply all pending migrations"
	@echo "  migrate-down         roll back one migration"
	@echo "  migrate-down-all     roll back every migration (dangerous)"
	@echo "  migrate-version      print current schema version"
	@echo "  migrate-new NAME=x   create a new pair of migration files"
	@echo ""
	@echo "  seed                 insert demo tenant/company/region/station"

# ---- App lifecycle ----

run:
	go run ./cmd/api

build:
	@mkdir -p ./bin
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o ./bin/$(APP) ./cmd/api

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	cd $(ROOT) && golangci-lint run ./services/api/...

tidy:
	cd $(ROOT) && go mod tidy

clean:
	rm -rf ./bin

docker-build:
	cd $(ROOT) && docker build -f services/api/Dockerfile --build-arg VERSION=$(VERSION) --build-arg COMMIT=$(COMMIT) -t $(APP):$(VERSION) .

# ---- Local infra ----

db-up:
	cd $(ROOT) && docker compose up -d postgres redis

db-down:
	cd $(ROOT) && docker compose down

db-logs:
	cd $(ROOT) && docker compose logs -f --tail=200

db-psql:
	cd $(ROOT) && docker compose exec postgres psql -U fuelgrid -d fuelgrid

db-redis:
	cd $(ROOT) && docker compose exec redis redis-cli

# ---- Migrations ----
# DATABASE_URL must be set; loaded from .env if present.

migrate-up:
	cd $(ROOT) && go run ./services/api/cmd/migrate up

migrate-down:
	cd $(ROOT) && go run ./services/api/cmd/migrate down

migrate-down-all:
	cd $(ROOT) && go run ./services/api/cmd/migrate down-all

migrate-version:
	cd $(ROOT) && go run ./services/api/cmd/migrate version

migrate-new:
	@if [ -z "$(NAME)" ]; then \
		echo "usage: make migrate-new NAME=<short_slug>"; exit 1; \
	fi
	@last=$$(ls $(MIGRATIONS)/*.up.sql 2>/dev/null | sed -E 's|.*/([0-9]+)_.*|\1|' | sort -n | tail -1); \
	next=$$(printf "%04d" $$((10#$${last:-0} + 1))); \
	touch $(MIGRATIONS)/$${next}_$(NAME).up.sql; \
	touch $(MIGRATIONS)/$${next}_$(NAME).down.sql; \
	echo "Created $${next}_$(NAME).up.sql and $${next}_$(NAME).down.sql"

# ---- Seed ----

seed:
	cd $(ROOT) && go run ./services/api/cmd/seed
