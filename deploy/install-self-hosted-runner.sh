#!/usr/bin/env bash
# Install the FuelGrid production GitHub Actions runner on the shared droplet.

set -euo pipefail

: "${RUNNER_TOKEN:?RUNNER_TOKEN is required}"

RUNNER_VERSION="${RUNNER_VERSION:-2.336.0}"
RUNNER_USER="${RUNNER_USER:-deploy}"
RUNNER_NAME="${RUNNER_NAME:-fuelgrid-prod-1}"
RUNNER_LABELS="${RUNNER_LABELS:-fuelgrid-prod,digitalocean,production}"
RUNNER_DIR="${RUNNER_DIR:-/opt/actions-runner}"
REPOSITORY_URL="${REPOSITORY_URL:-https://github.com/JAPHARYROMAN/FuelGrid-OS}"

if ! id "$RUNNER_USER" >/dev/null 2>&1; then
  useradd --create-home --shell /bin/bash "$RUNNER_USER"
fi
usermod -aG docker "$RUNNER_USER"

mkdir -p "$RUNNER_DIR" /opt/fuelgrid /var/backups/fuelgrid
chown -R "$RUNNER_USER:$RUNNER_USER" "$RUNNER_DIR" /opt/fuelgrid /var/backups/fuelgrid

if [ ! -f "$RUNNER_DIR/config.sh" ]; then
  archive="/tmp/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz"
  curl --fail --location --silent --show-error \
    "https://github.com/actions/runner/releases/download/v${RUNNER_VERSION}/actions-runner-linux-x64-${RUNNER_VERSION}.tar.gz" \
    --output "$archive"
  tar -xzf "$archive" -C "$RUNNER_DIR"
  rm -f "$archive"
  chown -R "$RUNNER_USER:$RUNNER_USER" "$RUNNER_DIR"
fi

if [ ! -f "$RUNNER_DIR/.runner" ]; then
  runuser -u "$RUNNER_USER" -- "$RUNNER_DIR/config.sh" \
    --unattended \
    --replace \
    --url "$REPOSITORY_URL" \
    --token "$RUNNER_TOKEN" \
    --name "$RUNNER_NAME" \
    --labels "$RUNNER_LABELS" \
    --work _work
fi

cd "$RUNNER_DIR"
if ! systemctl list-unit-files --no-legend | grep -q '^actions.runner.'; then
  ./svc.sh install "$RUNNER_USER"
fi
./svc.sh start
./svc.sh status
