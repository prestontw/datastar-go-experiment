#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if [ "${INFRA_BACKEND:-}" != nix ]; then
  echo "scripts/database.sh belongs to the Nix development environment; run 'nix develop'." >&2
  exit 2
fi

usage() {
  cat >&2 <<'EOF'
Usage: scripts/database.sh ensure DATABASE_NAME
       scripts/database.sh reset DATABASE_NAME
       scripts/database.sh drop DATABASE_NAME

DATABASE_NAME must start with patient_dashboard and contain only lowercase
letters, numbers, and underscores. The database name is always explicit for
destructive operations.
EOF
  exit 2
}

[ "$#" -eq 2 ] || usage
action=$1
database=$2

case "$action" in
  ensure|reset|drop) ;;
  *) usage ;;
esac
case "$database" in
  *[!a-z0-9_]*|'')
    echo "Refusing database name '$database'; only lowercase letters, numbers, and underscores are allowed." >&2
    exit 2
    ;;
esac
case "$database" in
  patient_dashboard|patient_dashboard_*) ;;
  *)
    echo "Refusing database name '$database'; expected patient_dashboard or patient_dashboard_<worktree>." >&2
    exit 2
    ;;
esac

port=${POSTGRES_PORT:-5432}
export PGPASSWORD=dashboard
if ! pg_isready -h 127.0.0.1 -p "$port" -U dashboard -d postgres >/dev/null 2>&1; then
  echo "Shared Nix PostgreSQL is not running; run 'just infra-up' first." >&2
  exit 1
fi

psql_admin() {
  psql -h 127.0.0.1 -p "$port" -U dashboard -d postgres "$@"
}
createdb_admin() {
  createdb -h 127.0.0.1 -p "$port" -U dashboard "$@"
}
dropdb_admin() {
  dropdb -h 127.0.0.1 -p "$port" -U dashboard "$@"
}

exists() {
  psql_admin -Atqc "SELECT 1 FROM pg_database WHERE datname = '$database'" | grep -qx 1
}

create() {
  if ! exists; then
    if createdb_admin "$database"; then
      echo "Created database $database."
    elif ! exists; then
      return 1
    fi
  fi
}

drop() {
  if exists; then
    # FORCE closes stale development-server and test connections. The strict
    # name guard above prevents this helper from touching PostgreSQL internals.
    dropdb_admin --force "$database"
    echo "Dropped database $database."
  fi
}

case "$action" in
  ensure) create ;;
  reset)
    drop
    create
    ;;
  drop) drop ;;
esac
