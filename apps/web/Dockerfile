# syntax=docker/dockerfile:1.7
#
# =============================================================================
# FuelGrid OS — Web (Next.js 15 App Router) container image
# =============================================================================
# Multi-stage, pnpm-workspace-aware build that produces the Next.js *standalone*
# server (next.config.ts sets `output: 'standalone'`). The browser never talks
# to the Go API directly: this app is the same-origin BFF proxy + UI, and the
# proxy forwards SERVER-SIDE to the API origin resolved at RUNTIME from
# API_ORIGIN (see apps/web/src/lib/server/api-origin.ts). API_ORIGIN is NOT
# baked at build — set it via Fly env/secrets (apps/web/fly.toml).
#
# Build context is the repo ROOT (CD passes `context: .`) so the whole pnpm
# workspace (root manifests + lockfile + packages/*) is available — the web app
# transpiles its sibling source packages @fuelgrid/sdk and @fuelgrid/ui via
# next.config `transpilePackages`, so no separate package build step is needed.
# =============================================================================

# Pin to Node 22 to match .nvmrc / package.json engines (>=22).
ARG NODE_VERSION=22

# ---------------------------------------------------------------------------
# build — install the workspace + produce the standalone output. In a pnpm
# workspace the standalone tracer nests the app under .next/standalone/apps/web
# (server.js lives there) and hoists the traced node_modules to
# .next/standalone/node_modules.
# ---------------------------------------------------------------------------
FROM node:${NODE_VERSION}-bookworm-slim AS build
WORKDIR /app
RUN corepack enable

ENV NODE_ENV=production
# Telemetry is a no-op in CI/containers; disable it for clean, hermetic builds.
ENV NEXT_TELEMETRY_DISABLED=1
# Skip the in-image type-check + lint (next.config reads this). Types/lint are
# the authoritative CI gate (pnpm -r typecheck / lint on every PR); the image
# build only needs to compile already-verified code.
ENV CONTAINER_BUILD=1

# Bring in the manifests + the source the web image compiles (.dockerignore
# restricts apps/ to apps/web and prunes node_modules/.next, so this stays lean).
COPY package.json pnpm-lock.yaml pnpm-workspace.yaml ./
COPY packages ./packages
COPY apps/web ./apps/web

# Install the workspace fresh IN THIS STAGE. We intentionally do NOT copy
# node_modules from a deps stage: pnpm's node_modules is a symlink farm into a
# virtual store, and copying it across build stages breaks those links (the
# Next bin then resolves to a dangling path). A frozen install here is correct
# and self-contained; the pnpm store cache-mount keeps it fast across builds.
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile

# Build the workspace topologically: @fuelgrid/sdk + @fuelgrid/ui build first so
# apps/web resolves their dist + types, then the web standalone output is produced.
RUN pnpm -r build

# ---------------------------------------------------------------------------
# runtime — slim Node image running the standalone server as a nonroot user.
# Only the standalone server, the (non-traced) static assets, and public/ are
# copied; the full node_modules / build toolchain is left behind.
# ---------------------------------------------------------------------------
FROM node:${NODE_VERSION}-bookworm-slim AS runtime
WORKDIR /app

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1
# The standalone server reads PORT/HOSTNAME from the env. Bind all interfaces
# so Fly's proxy can reach it; 3000 matches fly.toml internal_port + EXPOSE.
ENV PORT=3000
ENV HOSTNAME=0.0.0.0

# node:slim ships a `node` user (uid/gid 1000) — run unprivileged.
USER node

# 1) The standalone server + its hoisted, traced node_modules. In a monorepo
#    the app server nests under apps/web/, so this lays down /app/node_modules,
#    /app/package.json and /app/apps/web/server.js together.
COPY --from=build --chown=node:node /app/apps/web/.next/standalone ./
# 2) Static assets are NOT traced into standalone — copy them to the path the
#    server serves them from (relative to the nested app: apps/web/.next/static).
COPY --from=build --chown=node:node /app/apps/web/.next/static ./apps/web/.next/static
# 3) public/ (icons, manifest, etc.) likewise sits next to the nested server.
COPY --from=build --chown=node:node /app/apps/web/public ./apps/web/public

EXPOSE 3000

# Run the standalone server. The monorepo tracer emits server.js under the
# app's workspace path; middleware (incl. the per-request CSP nonce) runs as
# part of this Next server exactly as in `next start`.
CMD ["node", "apps/web/server.js"]
