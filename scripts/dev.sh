#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

database=${DATABASE_NAME:-patient_dashboard}
app_port=${APP_PORT:-8443}
case "$app_port" in
  *[!0-9]*)
    echo "APP_PORT must be numeric." >&2
    exit 2
    ;;
esac

./scripts/database.sh ensure "$database"

export ADDR=":$app_port"
export DATABASE_URL="postgres://dashboard:dashboard@localhost:5432/${database}?sslmode=disable"
exec go run ./cmd/server
