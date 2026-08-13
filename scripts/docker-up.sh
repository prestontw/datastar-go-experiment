#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker with the Compose plugin is required." >&2
  exit 1
fi
if [ "${INFRA_BACKEND:-docker}" != docker ]; then
  echo "The full Docker workflow is intentionally outside the Nix development shell." >&2
  echo "Exit the shell and run ./scripts/docker-up.sh from the macOS host." >&2
  exit 2
fi

./scripts/certs.sh

# Docker Compose reads .env itself; load the same simple key/value file so this
# wrapper can provision and print the selected database/port too.
if [ -f .env ]; then
  set -a
  # shellcheck disable=SC1091
  . ./.env
  set +a
fi

database=${DATABASE_NAME:-patient_dashboard}
app_port=${APP_PORT:-8443}
case "$database" in
  patient_dashboard|patient_dashboard_*) ;;
  *) echo "DATABASE_NAME must be patient_dashboard or patient_dashboard_<name>." >&2; exit 2 ;;
esac
case "$database" in
  *[!a-z0-9_]*) echo "DATABASE_NAME contains invalid characters." >&2; exit 2 ;;
esac

# Bring PostgreSQL up first so a non-default logical database can be
# provisioned before the application starts.
docker compose up -d --wait postgres
if ! docker compose exec -T postgres psql -U dashboard -d postgres -Atqc \
  "SELECT 1 FROM pg_database WHERE datname = '$database'" | grep -qx 1; then
  docker compose exec -T postgres createdb -U dashboard "$database"
fi

# Running as the host user lets the unprivileged scratch container read the
# owner-only mkcert key without broadening its filesystem permissions.
LOCAL_UID="$(id -u)" LOCAL_GID="$(id -g)" \
  docker compose --profile app up -d --wait --build app

cat <<EOF

Patient dashboard: https://localhost:${app_port}/patients

The local Go process and Docker use the same mkcert certificate in .certs/.
After mkcert -install succeeds, Chrome and Safari should trust it without a
warning. Firefox may require the NSS tooling noted by scripts/certs.sh.
EOF
