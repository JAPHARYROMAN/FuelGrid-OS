#!/usr/bin/env bash
# Generate FuelGrid's first shared-droplet environment without printing secrets.

set -euo pipefail

ENV_FILE="${ENV_FILE:-/opt/fuelgrid/.env}"
TEMPLATE_FILE="${TEMPLATE_FILE:-/opt/fuelgrid/.env.production.example}"
DOMAIN="${DOMAIN:-itembagrouptz.com}"

if [ -e "$ENV_FILE" ]; then
  echo "ERROR: ${ENV_FILE} already exists; refusing to replace production secrets." >&2
  exit 1
fi
if [ ! -f "$TEMPLATE_FILE" ]; then
  echo "ERROR: environment template not found: ${TEMPLATE_FILE}" >&2
  exit 1
fi

install -m 0600 "$TEMPLATE_FILE" "$ENV_FILE"

set_env() {
  local key="$1"
  local value="$2"
  if grep -q "^${key}=" "$ENV_FILE"; then
    sed -i "s#^${key}=.*#${key}=${value}#" "$ENV_FILE"
  else
    printf '%s=%s\n' "$key" "$value" >> "$ENV_FILE"
  fi
}

owner_password="$(openssl rand -hex 32)"
app_password="$(openssl rand -hex 32)"

set_env WEB_DOMAIN "fuelgrid.${DOMAIN}"
set_env API_DOMAIN "api.fuelgrid.${DOMAIN}"
set_env SHARED_EDGE_NETWORK itemba_shared_edge
set_env API_CORS_ALLOWED_ORIGINS "https://fuelgrid.${DOMAIN}"
set_env APP_BASE_URL "https://fuelgrid.${DOMAIN}"
set_env MPESA_CALLBACK_URL "https://api.fuelgrid.${DOMAIN}/api/v1/payments/mpesa/callback"
set_env SMTP_FROM "no-reply@${DOMAIN}"

set_env POSTGRES_USER fuelgrid
set_env POSTGRES_DB fuelgrid
set_env POSTGRES_PASSWORD "$owner_password"
set_env DATABASE_APP_PASSWORD "$app_password"
set_env DATABASE_URL "postgres://fuelgrid:${owner_password}@postgres:5432/fuelgrid?sslmode=disable"
set_env DATABASE_APP_URL "postgres://fuelgrid_app:${app_password}@postgres:5432/fuelgrid?sslmode=disable"

set_env AUTH_PASSWORD_PEPPER "$(openssl rand -hex 48)"
set_env PLATFORM_ADMIN_TOKEN "$(openssl rand -hex 48)"
set_env ALLOW_INITIAL_DATABASE_BOOTSTRAP true

# Immutable image references are replaced by the CD workflow before Compose is
# evaluated. Keep syntactically valid placeholders for the pre-deploy file.
set_env API_IMAGE pending-first-release
set_env WEB_IMAGE pending-first-release
set_env MIGRATE_IMAGE pending-first-release

chmod 600 "$ENV_FILE"
unset owner_password app_password
echo "Created ${ENV_FILE}. Back it up to a password manager before deployment."
