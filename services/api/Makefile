# services/api/Makefile
# Invoke from this directory or via `make -C services/api <target>`.
# The Go module is rooted at the repo root; Go finds it by walking up.

APP        ?= fuelgrid-api
VERSION    ?= dev
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
LDFLAGS    := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT)
ROOT       := $(shell git rev-parse --show-toplevel 2>/dev/null)

.PHONY: run build test test-race vet lint tidy clean docker-build help

help:
	@echo "Targets:"
	@echo "  run           — go run the API"
	@echo "  build         — produce a static binary at ./bin/$(APP)"
	@echo "  test          — run unit tests"
	@echo "  test-race     — run tests with the race detector"
	@echo "  vet           — go vet on the api packages"
	@echo "  lint          — golangci-lint on the api packages"
	@echo "  tidy          — go mod tidy (operates on the root go.mod)"
	@echo "  clean         — remove build artifacts"
	@echo "  docker-build  — build the Docker image"

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
