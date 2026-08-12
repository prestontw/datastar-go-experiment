#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

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

compose() {
  docker compose "$@"
}

if ! compose ps --status running postgres 2>/dev/null | grep -q postgres; then
  echo "Shared PostgreSQL is not running; run 'just infra-up' first." >&2
  exit 1
fi

exists() {
  compose exec -T postgres psql -U dashboard -d postgres -Atqc \
    "SELECT 1 FROM pg_database WHERE datname = '$database'" | grep -qx 1
}

create() {
  if ! exists; then
    if compose exec -T postgres createdb -U dashboard "$database"; then
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
    compose exec -T postgres dropdb --force -U dashboard "$database"
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
