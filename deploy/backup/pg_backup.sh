#!/usr/bin/env bash
# =============================================================================
# FuelGrid OS — nightly Postgres logical backup (self-hosted droplet)
# =============================================================================
# Dumps the fuelgrid database from the running `postgres` compose container in
# custom format (pg_dump -Fc), gzips it, rotates local copies, and OPTIONALLY
# pushes a copy to DigitalOcean Spaces (s3-compatible) when SPACES_BUCKET is set.
#
# This is a LOGICAL backup (a consistent snapshot at dump time). It does NOT
# give point-in-time recovery — see deploy/backup/RESTORE.md "If you need PITR".
#
# Idempotent + safe to re-run. Designed to be invoked by a systemd timer (see
# fuelgrid-backup.{service,timer} in this directory) or cron. Reads config from
# the same .env that the compose stack uses.
#
# Required on the droplet: docker (with the compose plugin), and — only if
# pushing offsite — the `aws` CLI (or s3cmd; this script uses `aws`).
# =============================================================================
set -euo pipefail

# --- Resolve paths -----------------------------------------------------------
# DEPLOY_DIR holds docker-compose.prod.yml + .env. Override via env if needed.
DEPLOY_DIR="${DEPLOY_DIR:-/opt/fuelgrid}"
COMPOSE_FILE="${COMPOSE_FILE:-${DEPLOY_DIR}/docker-compose.prod.yml}"
ENV_FILE="${ENV_FILE:-${DEPLOY_DIR}/.env}"
BACKUP_DIR="${BACKUP_DIR:-/var/backups/fuelgrid}"
LOG_PREFIX="[fuelgrid-backup $(date -u +%Y-%m-%dT%H:%M:%SZ)]"

log() { echo "${LOG_PREFIX} $*"; }

# --- Load config from .env (only the keys we need) ---------------------------
if [[ -f "${ENV_FILE}" ]]; then
  # shellcheck disable=SC1090
  set -a
  # Source only KEY=VALUE lines, ignore comments/blanks.
  # shellcheck disable=SC2046
  eval "$(grep -E '^[A-Z0-9_]+=' "${ENV_FILE}" | sed 's/[[:space:]]*$//')"
  set +a
else
  log "WARN: ${ENV_FILE} not found; relying on inherited environment."
fi

PGUSER="${POSTGRES_USER:-fuelgrid}"
PGDB="${POSTGRES_DB:-fuelgrid}"
KEEP_DAYS="${BACKUP_KEEP_DAYS:-14}"
REQUIRE_OFFSITE_BACKUP="${REQUIRE_OFFSITE_BACKUP:-0}"

mkdir -p "${BACKUP_DIR}"

TIMESTAMP="$(date -u +%Y%m%dT%H%M%SZ)"
DUMP_FILE="${BACKUP_DIR}/fuelgrid-${TIMESTAMP}.dump.gz"
TMP_FILE="${DUMP_FILE}.partial"

log "Starting backup of database '${PGDB}' (user '${PGUSER}') -> ${DUMP_FILE}"

# --- Dump from the running container -----------------------------------------
# pg_dump -Fc (custom format) is compressed + supports selective/parallel
# restore via pg_restore. We additionally gzip the stream for offsite economy.
# Run inside the postgres container so we don't need a host psql client.
docker compose -f "${COMPOSE_FILE}" exec -T postgres \
  pg_dump -U "${PGUSER}" -d "${PGDB}" -Fc \
  | gzip -c >"${TMP_FILE}"

# Atomic publish: only rename in once the full dump succeeded.
mv "${TMP_FILE}" "${DUMP_FILE}"
SIZE="$(du -h "${DUMP_FILE}" | cut -f1)"

# A successful pg_dump process is not enough: verify both gzip integrity and
# that pg_restore can parse the custom archive before it can gate a deploy.
gzip -t "${DUMP_FILE}"
gzip -dc "${DUMP_FILE}" \
  | docker compose -f "${COMPOSE_FILE}" exec -T postgres pg_restore --list >/dev/null
log "Backup complete and verified: ${DUMP_FILE} (${SIZE})"

# --- Optional offsite push to DigitalOcean Spaces ----------------------------
if [[ -n "${SPACES_BUCKET:-}" ]]; then
  if ! command -v aws >/dev/null 2>&1; then
    if [[ "${REQUIRE_OFFSITE_BACKUP}" == "1" ]]; then
      log "ERROR: offsite backup is required but the aws CLI is unavailable."
      exit 1
    fi
    log "WARN: SPACES_BUCKET set but 'aws' CLI not found; keeping the verified local backup only."
  else
    if [[ -z "${SPACES_ACCESS_KEY_ID:-}" || -z "${SPACES_SECRET_ACCESS_KEY:-}" ]]; then
      log "ERROR: SPACES_BUCKET is set but Spaces credentials are missing."
      exit 1
    fi
    ENDPOINT="${SPACES_ENDPOINT:-https://nyc3.digitaloceanspaces.com}"
    REMOTE_KEY="postgres/$(basename "${DUMP_FILE}")"
    REMOTE="s3://${SPACES_BUCKET}/${REMOTE_KEY}"
    log "Pushing offsite to ${REMOTE} via ${ENDPOINT}"
    AWS_ACCESS_KEY_ID="${SPACES_ACCESS_KEY_ID:-}" \
      AWS_SECRET_ACCESS_KEY="${SPACES_SECRET_ACCESS_KEY:-}" \
      aws s3 cp "${DUMP_FILE}" "${REMOTE}" --endpoint-url "${ENDPOINT}"
    AWS_ACCESS_KEY_ID="${SPACES_ACCESS_KEY_ID}" \
      AWS_SECRET_ACCESS_KEY="${SPACES_SECRET_ACCESS_KEY}" \
      aws s3api head-object \
        --bucket "${SPACES_BUCKET}" \
        --key "${REMOTE_KEY}" \
        --endpoint-url "${ENDPOINT}" >/dev/null
    log "Offsite push verified."
  fi
else
  if [[ "${REQUIRE_OFFSITE_BACKUP}" == "1" ]]; then
    log "ERROR: SPACES_BUCKET is unset; production deployment requires an offsite backup."
    exit 1
  fi
  log "SPACES_BUCKET unset; keeping backup local-only."
fi

# --- Rotate local copies -----------------------------------------------------
log "Rotating local backups older than ${KEEP_DAYS} days in ${BACKUP_DIR}"
find "${BACKUP_DIR}" -name 'fuelgrid-*.dump.gz' -type f -mtime "+${KEEP_DAYS}" -print -delete \
  | while read -r removed; do log "Rotated out: ${removed}"; done

log "Done."
