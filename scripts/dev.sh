#!/usr/bin/env bash
#
# DevDigest local bootstrap — bring the whole stack up from zero.
#
#   ./scripts/dev.sh              # full: docker → migrate → seed → API + client
#   ./scripts/dev.sh --no-seed    # skip the demo seed
#   ./scripts/dev.sh --no-client  # run only Postgres + API (no Next.js)
#   ./scripts/dev.sh --db-only    # just Postgres + migrate + seed, then exit
#   ./scripts/dev.sh --ts-api     # run the old TS API (server/) instead of Go
#
# The API is the Go server (api/), built into api/bin and run with server/.env
# loaded. Migrations and seed run through api/cmd/db, which the TS server's
# bookkeeping understands too, so --ts-api works on the same database.
#
# Idempotent: re-running installs only what's missing, migrations and seed
# both upsert. Ctrl-C stops the dev servers and leaves Postgres running.

set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

CONTAINER="devdigest-postgres"
RUN_SEED=1
RUN_CLIENT=1
DB_ONLY=0
TS_API=0

for arg in "$@"; do
  case "$arg" in
    --no-seed)   RUN_SEED=0 ;;
    --no-client) RUN_CLIENT=0 ;;
    --db-only)   DB_ONLY=1 ;;
    --ts-api)    TS_API=1 ;;
    -h|--help)   sed -n '2,16p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown flag: $arg" >&2; exit 2 ;;
  esac
done

log()  { printf '\033[1;36m▸ %s\033[0m\n' "$*"; }
warn() { printf '\033[1;33m! %s\033[0m\n' "$*"; }

# --- prerequisites -----------------------------------------------------------
command -v docker >/dev/null || { echo "docker not found"; exit 1; }
command -v go     >/dev/null || { echo "go not found (https://go.dev/dl)"; exit 1; }
if [ "$DB_ONLY" -eq 0 ] && { [ "$RUN_CLIENT" -eq 1 ] || [ "$TS_API" -eq 1 ]; }; then
  command -v pnpm >/dev/null || { echo "pnpm not found (npm i -g pnpm)"; exit 1; }
fi

# --- env files ---------------------------------------------------------------
for dir in server client; do
  if [ ! -f "$dir/.env" ] && [ -f "$dir/.env.example" ]; then
    cp "$dir/.env.example" "$dir/.env"
    warn "created $dir/.env from .env.example — add your API keys (OPENAI/ANTHROPIC/GITHUB_TOKEN) in server/.env"
  fi
done

# load_server_env exports server/.env's KEY=value lines, as the TS server's
# dotenv did: a variable the environment already has wins over the file.
# Call it in a subshell, so the settings reach only the Go commands.
load_server_env() {
  [ -f "$ROOT/server/.env" ] || return 0
  local line key value
  while IFS= read -r line || [ -n "$line" ]; do
    case "$line" in ''|'#'*) continue ;; esac
    key="${line%%=*}"
    value="${line#*=}"
    key="${key#export }"
    [[ "$key" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]] || continue
    [ -n "${!key+x}" ] && continue
    case "$value" in
      \"*\") value="${value:1:${#value}-2}" ;;
      \'*\') value="${value:1:${#value}-2}" ;;
      *)     value="${value%% #*}" ;;
    esac
    export "$key=$value"
  done < "$ROOT/server/.env"
}

# --- Postgres ----------------------------------------------------------------
# The container name is fixed (container_name: devdigest-postgres), so if one is
# already running (possibly under another compose project) we reuse it instead
# of failing on a name conflict. If it exists but is stopped, start it; else
# create it via compose.
state="$(docker inspect -f '{{.State.Status}}' "$CONTAINER" 2>/dev/null || echo "missing")"
case "$state" in
  running) log "Postgres container already running — reusing it" ;;
  exited|created) log "starting existing Postgres container"; docker start "$CONTAINER" >/dev/null ;;
  *)       log "starting Postgres (docker compose up -d)"; docker compose up -d ;;
esac

log "waiting for Postgres to be healthy"
for _ in $(seq 1 60); do
  status="$(docker inspect -f '{{.State.Health.Status}}' "$CONTAINER" 2>/dev/null || echo "starting")"
  [ "$status" = "healthy" ] && break
  sleep 1
done
[ "${status:-}" = "healthy" ] || { echo "Postgres did not become healthy in time"; exit 1; }
log "Postgres healthy"

# --- build the Go commands (api, db, review) into api/bin ---------------------
# go build is incremental: a re-run with no changes takes a second or two.
log "building the Go API"
(cd api && make build)

# --- install deps (only if missing) ------------------------------------------
install_if_needed() {
  if [ ! -d "$1/node_modules" ]; then
    log "installing deps in $1"
    (cd "$1" && pnpm install)
  fi
}
[ "$DB_ONLY" -eq 0 ] && [ "$RUN_CLIENT" -eq 1 ] && install_if_needed client
if [ "$DB_ONLY" -eq 0 ] && [ "$TS_API" -eq 1 ]; then
  install_if_needed server
  # reviewer-core's RAW source is imported by the TS API at runtime (tsconfig
  # alias); without its deps it crashes at boot with ERR_MODULE_NOT_FOUND. npm.
  [ -d reviewer-core/node_modules ] || { log "installing deps in reviewer-core"; (cd reviewer-core && npm ci); }
fi

# --- migrate + seed ----------------------------------------------------------
# From api/, where bin/db finds the migrations (../server/src/db/migrations).
log "applying migrations"
(load_server_env; cd api && ./bin/db migrate)

if [ "$RUN_SEED" -eq 1 ]; then
  log "seeding demo data"
  (load_server_env; cd api && ./bin/db seed)
fi

if [ "$DB_ONLY" -eq 1 ]; then
  log "DB ready. Postgres is running; API/client not started (--db-only)."
  exit 0
fi

# --- dev servers -------------------------------------------------------------
SERVER_PID=""
# Kill a process and all its descendants: `pnpm dev` (--ts-api) runs the real
# listener as a grandchild, which a plain kill would leave holding the port.
kill_tree() {
  local pid="$1" kid
  for kid in $(pgrep -P "$pid" 2>/dev/null || true); do kill_tree "$kid"; done
  kill "$pid" 2>/dev/null || true
}
cleanup() {
  trap - EXIT INT TERM # once, not again on the exit that follows
  log "shutting down dev servers (Postgres stays up; stop it with: docker compose down)"
  [ -n "$SERVER_PID" ] && kill_tree "$SERVER_PID"
}
trap cleanup EXIT INT TERM

if [ "$TS_API" -eq 1 ]; then
  log "starting the TS API (server/)"
  (cd server && pnpm dev) &
else
  # From server/, so relative paths in server/.env (DEVDIGEST_CLONE_DIR=./clones)
  # resolve as they did for the TS server.
  log "starting the Go API (api/bin/api); it logs the address it listens on"
  (load_server_env; cd server && exec ../api/bin/api) &
fi
SERVER_PID=$!

if [ "$RUN_CLIENT" -eq 1 ]; then
  log "starting web on :3000 (client) — Ctrl-C to stop both"
  (cd client && pnpm dev)
else
  log "API running (PID $SERVER_PID) — Ctrl-C to stop"
  wait "$SERVER_PID"
fi
