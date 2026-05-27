# syntax=docker/dockerfile:1.7

# ---------------------------------------------------------------------------
# Builder stage — compile a static binary using BuildKit cache mounts so
# repeated builds reuse the Go module cache and build cache.
# ---------------------------------------------------------------------------
FROM golang:1.25-alpine AS builder

WORKDIR /src

# Layer the module download separately from the source copy so changes to
# .go files don't bust the module cache.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG TARGETOS=linux
ARG TARGETARCH=amd64

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build \
        -trimpath \
        -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
        -o /out/api \
        ./services/api/cmd/api

# ---------------------------------------------------------------------------
# Runtime stage — distroless static so the image carries the binary and
# little else. nonroot user (uid 65532) avoids running as root.
# ---------------------------------------------------------------------------
FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=builder /out/api /api

USER nonroot:nonroot
EXPOSE 8080

ENTRYPOINT ["/api"]
