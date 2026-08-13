#!/bin/sh
set -eu

cd "$(dirname "$0")/.."

if [ "${INFRA_BACKEND:-}" != nix ]; then
  echo "scripts/infra.sh belongs to the Nix development environment; run 'nix develop'." >&2
  exit 2
fi
for command in pg_ctl initdb pg_isready flock; do
  if ! command -v "$command" >/dev/null 2>&1; then
    echo "The Nix infrastructure contract requires '$command'; run 'nix develop'." >&2
    exit 1
  fi
done

action=${1:-}
case "$action" in
  up|down|status) ;;
  *)
    echo "Usage: scripts/infra.sh {up|down|status}" >&2
    exit 2
    ;;
esac

port=${POSTGRES_PORT:-5432}

case "$port" in
  ''|*[!0-9]*)
    echo "POSTGRES_PORT must be numeric; got '$port'." >&2
    exit 2
    ;;
esac

# One user-level PostgreSQL cluster is shared by every worktree. Worktrees are
# isolated by logical database rather than by PostgreSQL process.
user_id=$(id -u)
data_home=${XDG_DATA_HOME:-"$HOME/.local/share"}
state_home=${XDG_STATE_HOME:-"$HOME/.local/state"}
runtime_home=${XDG_RUNTIME_DIR:-"/tmp/patient-dashboard-$user_id"}
pgdata=${PATIENT_DASHBOARD_PGDATA:-"$data_home/go-datastar-patient-dashboard/postgres-18"}
pgstate=${PATIENT_DASHBOARD_PGSTATE:-"$state_home/go-datastar-patient-dashboard"}
pgruntime=${PATIENT_DASHBOARD_PGRUNTIME:-"$runtime_home/go-datastar-patient-dashboard"}

case "$pgdata$pgstate$pgruntime" in
  *" "*|*"'"*)
    echo "Nix PostgreSQL development paths may not contain spaces or single quotes." >&2
    exit 2
    ;;
esac

mkdir -p "$pgdata" "$pgstate" "$pgruntime"
chmod 0700 "$pgdata" "$pgruntime"
lock_file="$pgruntime/infrastructure.lock"
log_file="$pgstate/postgres.log"

# Serialize init/start/stop across agents. Run this script recursively under a
# flock parent process; --close prevents PostgreSQL from inheriting the lock
# descriptor and holding it for the lifetime of the server.
if [ "${PATIENT_DASHBOARD_INFRA_LOCKED:-}" != 1 ]; then
  PATIENT_DASHBOARD_INFRA_LOCKED=1 \
    flock --close "$lock_file" "$0" "$action"
  exit
fi

running() {
  pg_ctl -D "$pgdata" status >/dev/null 2>&1
}

case "$action" in
  up)
    if [ ! -s "$pgdata/PG_VERSION" ]; then
      password_file="$pgruntime/init-password"
      umask 077
      printf '%s\n' dashboard >"$password_file"
      if ! initdb \
        -D "$pgdata" \
        --username=dashboard \
        --pwfile="$password_file" \
        --auth-local=trust \
        --auth-host=scram-sha-256 >/dev/null; then
        rm -f "$password_file"
        exit 1
      fi
      rm -f "$password_file"
    fi

    if running; then
      if ! pg_isready -h 127.0.0.1 -p "$port" -U dashboard -d postgres >/dev/null; then
        echo "The shared Nix cluster is running, but not on configured POSTGRES_PORT=$port." >&2
        echo "All worktrees must use one shared PostgreSQL port." >&2
        exit 1
      fi
      echo "Shared Nix PostgreSQL is ready on 127.0.0.1:$port."
      exit
    fi

    if pg_isready -h 127.0.0.1 -p "$port" >/dev/null 2>&1; then
      echo "Port $port is already occupied by another PostgreSQL cluster/backend." >&2
      echo "Stop the process using it or configure one shared POSTGRES_PORT for all worktrees." >&2
      exit 1
    fi

    pg_ctl \
      -D "$pgdata" \
      -l "$log_file" \
      -o "-h 127.0.0.1 -p $port -k $pgruntime" \
      -w start >/dev/null
    pg_isready -h 127.0.0.1 -p "$port" -U dashboard -d postgres >/dev/null
    echo "Started shared Nix PostgreSQL on 127.0.0.1:$port (data: $pgdata)."
    ;;
  down)
    if running; then
      pg_ctl -D "$pgdata" -m fast -w stop >/dev/null
      echo "Stopped shared Nix PostgreSQL."
    else
      echo "Shared Nix PostgreSQL is not running."
    fi
    ;;
  status)
    if running; then
      pg_ctl -D "$pgdata" status
      pg_isready -h 127.0.0.1 -p "$port" -U dashboard -d postgres
    else
      echo "Shared Nix PostgreSQL is not running."
      exit 1
    fi
    ;;
esac
