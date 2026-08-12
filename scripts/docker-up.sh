#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker with the Compose plugin is required." >&2
  exit 1
fi

./scripts/certs.sh

database=${DATABASE_NAME:-patient_dashboard}
app_port=${APP_PORT:-8443}

# Bring shared infrastructure up first so a non-default logical database can
# be provisioned before the application starts.
docker compose up -d --wait postgres redis
./scripts/database.sh ensure "$database"

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
