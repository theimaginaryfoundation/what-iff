#!/usr/bin/env bash
# check-compose-defaults.sh — insecure-default guardrails for docker-compose.yml
# and the Go config it feeds. Regressing any of these silently reopens a real
# defect: forgeable JWTs on a default deployment, or a destructive migration
# running by default against whatever database is configured.
#
# The secret-fallback check below uses an explicit variable list rather than
# a generic SECRET|PASSWORD|KEY|TOKEN pattern: a generic pattern also matches
# CHATAPP_DB_PASSWORD (a compose-internal Postgres password, not a forgeable
# session/auth secret) and STRIPE_PUBLISHABLE_KEY (designed to be public), and
# a check that cries wolf on legitimate defaults gets ignored. Add a new
# security-sensitive var to the list explicitly rather than broadening the
# pattern.
set -uo pipefail

cd "$(dirname "$0")/.."

fail=0

echo "1/5 no non-empty compose fallback on a security-sensitive secret..."
SECRET_VARS="JWT_SECRET JWT_REFRESH_SECRET TOKEN_ENCRYPTION_SECRET OPENAI_API_KEY ANTHROPIC_API_KEY STRIPE_SECRET_KEY STRIPE_WEBHOOK_SECRET"
bad=""
for v in $SECRET_VARS; do
  # Matches `VAR: ${VAR:-anything-non-empty}` — a bare `VAR:` or `VAR: ${VAR:-}`
  # (empty fallback) both pass; only a *value* shipped in the file is a leak.
  if grep -qE "^\s*${v}: \\\$\\{${v}:-[^}]+\\}" docker-compose.yml; then
    bad="$bad $v"
  fi
done
if [ -n "$bad" ]; then
  echo "❌ compose ships a non-empty fallback for:$bad — a shipped value is public the moment this repo is public; remove the fallback"
  fail=1
else
  echo "✅ no secret-shaped compose fallbacks"
fi

echo "2/5 DESTRUCTIVE_MIGRATION defaults to false..."
if grep -qE '^\s*DESTRUCTIVE_MIGRATION: \$\{DESTRUCTIVE_MIGRATION:-false\}' docker-compose.yml; then
  echo "✅ DESTRUCTIVE_MIGRATION defaults false"
else
  echo "❌ DESTRUCTIVE_MIGRATION does not default to false in docker-compose.yml"
  fail=1
fi

echo "3/5 ENV has no compose-level default..."
if grep -qE '^\s*ENV: \$\{ENV:-' docker-compose.yml; then
  echo "❌ ENV has a compose fallback — this silently satisfies the explicit-local-env gate (IsExplicitLocalEnv); use a bare 'ENV:' passthrough instead"
  fail=1
elif grep -qE '^\s*ENV:\s*$' docker-compose.yml; then
  echo "✅ ENV is a bare passthrough, no default"
else
  echo "❌ no bare 'ENV:' line found in docker-compose.yml"
  fail=1
fi

echo "4/5 DB_SSL_MODE defaults to require in Go..."
if grep -qE 'sslMode = "require"' internal/database/db.go; then
  echo "✅ DB_SSL_MODE defaults to require"
else
  echo "❌ internal/database/db.go's DB_SSL_MODE default is not \"require\""
  fail=1
fi

# .env.example is copied verbatim by anyone starting out, so its defaults are
# shipped defaults in every sense that matters — but until now nothing watched
# it, and it drifted: it was the only place in the repo setting
# AUTO_MIGRATE=false, and it shipped DESTRUCTIVE_MIGRATION=true next to
# ENV=development, which is what satisfies the fail-closed gate. Checking one
# surface and not the other is how the two disagreed for so long.
echo "5/5 .env.example migration defaults are safe..."
env_fail=0
if ! grep -qE '^AUTO_MIGRATE=true$' .env.example; then
  echo "❌ .env.example must set AUTO_MIGRATE=true — a fresh database is unusable without it (the seeder fails with 'relation ... does not exist')"
  env_fail=1
fi
if ! grep -qE '^DESTRUCTIVE_MIGRATION=false$' .env.example; then
  echo "❌ .env.example must set DESTRUCTIVE_MIGRATION=false — it ships beside ENV=development, so a true here means 'cp .env.example .env' drops columns and indexes"
  env_fail=1
fi
if (( env_fail )); then
  fail=1
else
  echo "✅ .env.example migration defaults are safe"
fi

if (( fail )); then
  echo
  echo "❌ check-compose-defaults FAILED"
  exit 1
fi

echo
echo "✅ compose defaults: clean"
