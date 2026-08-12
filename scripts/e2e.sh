#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

database="patient_dashboard_e2e_$(date +%s)_$$"
app_port=${APP_PORT:-8443}
cleanup() {
  docker compose exec -T postgres dropdb --if-exists --force -U dashboard "$database" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker compose exec -T postgres createdb -U dashboard "$database"

# Playwright starts the real Go server, which must migrate this empty database
# before the health check succeeds. This also keeps UI writes out of the
# developer's persistent patient_dashboard database.
ADDR=":${app_port}" \
DATABASE_URL="postgres://dashboard:dashboard@localhost:5432/${database}?sslmode=disable" \
APP_ORIGIN="https://localhost:${app_port}" \
  pnpm exec playwright test "$@"
