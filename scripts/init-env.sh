#!/usr/bin/env bash
# init-env.sh — create a working .env for local development.
#
# Copies .env.example to .env and fills in the three secrets the server
# refuses to boot without (JWT_SECRET, JWT_REFRESH_SECRET,
# TOKEN_ENCRYPTION_SECRET). Those are validated by internal/auth.ValidateSecret
# and datastore.ValidateTokenEncryptionSecret: missing, shorter than 32
# characters, or default-looking values all fail startup, so leaving them for
# the reader to generate by hand is the single most common first-run stumble.
#
# Idempotent and non-destructive: an existing .env is never modified. Re-running
# reports what is already there and exits 0, so this is safe to make a
# prerequisite of other targets.
set -euo pipefail

cd "$(dirname "$0")/.."

# 32 bytes of hex. openssl is near-universal, but it is not guaranteed to be
# installed, and a bootstrap script that fails on a missing optional tool
# defeats its own purpose — fall back to /dev/urandom.
gen_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 32
  elif [ -r /dev/urandom ]; then
    od -An -tx1 -N32 /dev/urandom | tr -d ' \n'
  else
    echo "error: need openssl or /dev/urandom to generate secrets" >&2
    exit 1
  fi
}

if [ -f .env ]; then
  echo "✅ .env already exists — leaving it untouched"
  missing=""
  for key in JWT_SECRET JWT_REFRESH_SECRET TOKEN_ENCRYPTION_SECRET; do
    value="$(grep -E "^${key}=" .env 2>/dev/null | tail -1 || true)"
    value="${value#*=}"
    [ -n "$value" ] || missing="$missing $key"
  done
  if [ -n "$missing" ]; then
    echo "⚠️  but these are empty and the server will not boot without them:$missing"
    echo "   fill them with:  openssl rand -hex 32"
    exit 1
  fi
  exit 0
fi

[ -f .env.example ] || { echo "error: .env.example not found" >&2; exit 1; }
cp .env.example .env

# Only fill keys that are present and empty. Writing through a temp file keeps
# a failed run from leaving a half-substituted .env behind.
tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
while IFS= read -r line; do
  case "$line" in
    JWT_SECRET=|JWT_REFRESH_SECRET=|TOKEN_ENCRYPTION_SECRET=)
      printf '%s%s\n' "$line" "$(gen_secret)" ;;
    *)
      printf '%s\n' "$line" ;;
  esac
done < .env > "$tmp"
mv "$tmp" .env
trap - EXIT

echo "✅ created .env from .env.example"
echo "   generated: JWT_SECRET, JWT_REFRESH_SECRET, TOKEN_ENCRYPTION_SECRET"
echo
echo "Next: add a model provider key to .env (OPENAI_API_KEY=sk-...)"
echo "      then run: make up"
