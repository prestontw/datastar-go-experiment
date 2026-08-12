#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

database="patient_dashboard_e2e_$(date +%s)_$$"
cleanup() {
  docker compose exec -T postgres dropdb --if-exists --force -U dashboard "$database" >/dev/null 2>&1 || true
}
trap cleanup EXIT HUP INT TERM

docker compose exec -T postgres createdb -U dashboard "$database"

# Playwright starts the real Go server, which must migrate this empty database
# before the health check succeeds. This also keeps UI writes out of the
# developer's persistent patient_dashboard database.
DATABASE_URL="postgres://dashboard:dashboard@localhost:5432/${database}?sslmode=disable" \
  pnpm exec playwright test
