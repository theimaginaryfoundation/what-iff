#!/usr/bin/env bash
# check-env.sh — validate the local development environment before bring-up.
#
# Checks: .env presence, required DB/auth settings, secret quality, tool
# availability (docker/go/node), Docker daemon reachability, and consistency
# between native DB_* settings and the docker-compose CHATAPP_DB_* overrides.
#
# NOTE: -e is intentionally omitted — the script accumulates problems in
# `fail` and reports them all instead of exiting on the first one. .env is
# parsed with grep (never sourced): shell-sourcing diverges from godotenv
# semantics and would execute arbitrary content during "validation".
set -uo pipefail

cd "$(dirname "$0")/.."

fail=0
err() { echo "❌ $*"; fail=1; }
warn() { echo "⚠️  $*"; }
ok() { echo "✅ $*"; }

# envval KEY — last value for KEY in .env (godotenv-style last-wins), with
# surrounding single/double quotes stripped. Empty when absent. This is a
# best-effort heuristic, not a full .env parser: it strips at most one layer
# of matching quotes and does not handle escaped quotes inside a quoted
# value. Good enough for the simple KEY=value / KEY="value" lines this repo's
# .env.example uses; not a substitute for godotenv if that ever changes.
envval() {
  local line
  line="$(grep -E "^${1}=" .env 2>/dev/null | tail -1)" || true
  line="${line#*=}"
  line="${line%\"}"; line="${line#\"}"
  line="${line%\'}"; line="${line#\'}"
  printf '%s' "$line"
}

# --- .env ---
if [ ! -f .env ]; then
  err ".env missing — run: cp .env.example .env (the only manual setup step)"
else
  ok ".env present"
fi

# --- tools ---
for tool in go docker node npm; do
  if command -v "$tool" >/dev/null 2>&1; then
    ok "$tool found ($(command -v "$tool"))"
  else
    err "$tool not found on PATH"
  fi
done

if command -v docker >/dev/null 2>&1; then
  if docker info >/dev/null 2>&1; then
    ok "Docker daemon reachable"
  else
    err "Docker daemon not reachable — is Docker running?"
  fi
fi

# --- database settings (native backend) ---
db_type="$(envval DB_TYPE)"
if [ "$db_type" != "postgres" ]; then
  err "DB_TYPE must be 'postgres' for local development (got '${db_type:-unset}')"
else
  ok "DB_TYPE=postgres"
fi
db_host="$(envval DB_HOST)"
db_port="$(envval DB_PORT)"
db_user="$(envval DB_USER)"
db_password="$(envval DB_PASSWORD)"
db_name="$(envval DB_NAME)"
[ -n "$db_host" ] || err "DB_HOST is not set in .env (needed by the native backend)"
[ -n "$db_port" ] || err "DB_PORT is not set in .env (needed by the native backend)"
[ -n "$db_user" ] || err "DB_USER is not set in .env (needed by the native backend)"
[ -n "$db_password" ] || err "DB_PASSWORD is not set in .env (needed by the native backend)"
[ -n "$db_name" ] || err "DB_NAME is not set in .env (needed by the native backend)"
[ -n "$db_host" ] && [ -n "$db_name" ] && ok "DB settings present (host=${db_host}, db=${db_name})"

# --- native vs docker-compose consistency ---
# docker-compose db uses CHATAPP_DB_* (defaults chatapp/chatapp/chatapp, exposed
# port 5432). The native backend connects with DB_*; mismatches cause confusing
# auth/connect failures.
compose_user="$(envval CHATAPP_DB_USER)"; compose_user="${compose_user:-chatapp}"
compose_pass="$(envval CHATAPP_DB_PASSWORD)"; compose_pass="${compose_pass:-chatapp}"
compose_name="$(envval CHATAPP_DB_NAME)"; compose_name="${compose_name:-chatapp}"
compose_port="$(envval CHATAPP_DB_EXPOSED_PORT)"; compose_port="${compose_port:-5432}"
if [ -n "$db_user" ] && [ "$db_user" != "$compose_user" ]; then
  warn "DB_USER ($db_user) != compose CHATAPP_DB_USER ($compose_user)"
fi
if [ -n "$db_password" ] && [ "$db_password" != "$compose_pass" ]; then
  warn "DB_PASSWORD differs from compose CHATAPP_DB_PASSWORD"
fi
if [ -n "$db_name" ] && [ "$db_name" != "$compose_name" ]; then
  warn "DB_NAME ($db_name) != compose CHATAPP_DB_NAME ($compose_name)"
fi
if [ -n "$db_port" ] && [ "$db_port" != "$compose_port" ]; then
  warn "DB_PORT ($db_port) != compose exposed port CHATAPP_DB_EXPOSED_PORT ($compose_port)"
fi

# --- secrets ---
# Mirrors internal/auth.ValidateSecret and datastore.ValidateTokenEncryptionSecret
# exactly: the server hard-fails at boot on any of these conditions, so this
# is "fail here instead of after a build," not merely advisory.
known_placeholder() {
  case "$1" in
    devsecret|devrefresh|changeme|changeit|secret|password|example|test| \
    your_super_secret_key_change_this_in_production| \
    change_this_secret_to_a_long_random_value) return 0 ;;
    *) return 1 ;;
  esac
}
for var in JWT_SECRET JWT_REFRESH_SECRET TOKEN_ENCRYPTION_SECRET; do
  val="$(envval "$var")"
  if [ -z "$val" ]; then
    err "$var is not set — the server refuses to boot without it"
  elif [ "${#val}" -lt 32 ]; then
    err "$var must be at least 32 characters (got ${#val}) — the server refuses to boot otherwise"
  elif known_placeholder "$val"; then
    err "$var is a known placeholder value — the server refuses to boot with it. Generate a real one: openssl rand -hex 32"
  else
    ok "$var length ok"
  fi
done
if [ -n "$(envval JWT_SECRET)" ] && [ "$(envval JWT_SECRET)" = "$(envval JWT_REFRESH_SECRET)" ]; then
  warn "JWT_SECRET and JWT_REFRESH_SECRET are identical — use two distinct random values"
fi

# --- ports ---
server_port="$(envval SERVER_PORT)"; server_port="${server_port:-8080}"
if (exec 3<>"/dev/tcp/127.0.0.1/${server_port}") 2>/dev/null; then
  exec 3>&- 3<&- || true
  warn "port ${server_port} is already in use (is the API already running?)"
else
  ok "API port ${server_port} is free"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "❌ check-env found blocking problems above"
  exit 1
fi
echo "✅ Environment looks good"
