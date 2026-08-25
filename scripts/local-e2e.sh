#!/usr/bin/env bash
# local-e2e.sh — real-inference smoke test under LLM_BACKEND=local.
#
# Sibling of mock-e2e.sh: same isolated-Postgres / ephemeral-port / `env -i`
# allowlist harness, but drives assistant generation through a real local
# OpenAI-compatible model server (Ollama by default) instead of the
# in-process MockAdapter. Requires the server already running with
# LOCAL_LLM_MODEL pulled — this script does not manage the model server's
# lifecycle (the CI job pulls the model as a prior step).
#
#   * registers + logs in through the real auth path (same as mock-e2e.sh)
#   * sends a chat message and polls the job to completion
#   * asserts a non-empty assistant reply that is NOT a deterministic echo
#     of the user message (proves real inference happened, not a fallback)
#
# Requires: docker (compose v2), go, curl, jq, openssl, and a reachable
# LOCAL_LLM_BASE_URL (default http://localhost:11434/v1) serving LOCAL_LLM_MODEL.
set -euo pipefail

cd "$(dirname "$0")/.."

: "${LOCAL_LLM_MODEL:?LOCAL_LLM_MODEL must be set (e.g. qwen2.5:0.5b)}"
LOCAL_LLM_BASE_URL="${LOCAL_LLM_BASE_URL:-http://localhost:11434/v1}"

for tool in docker go curl jq openssl; do
  command -v "$tool" >/dev/null 2>&1 || { echo "❌ required tool missing: $tool"; exit 1; }
done

RUN_ID="$(date +%s)-$$"
PROJECT="chatapp-local-e2e-${RUN_ID}"
WORKDIR="$(mktemp -d)"
API_PID=""
API_LOG="${WORKDIR}/api.log"

cleanup() {
  status=$?
  set +e
  if [ -n "$API_PID" ] && kill -0 "$API_PID" 2>/dev/null; then
    kill "$API_PID" 2>/dev/null
    wait "$API_PID" 2>/dev/null
  fi
  if [ "$status" -ne 0 ] && [ -f "$API_LOG" ]; then
    echo "--- api log (last 50 lines) ---"
    tail -n 50 "$API_LOG"
    echo "-------------------------------"
  fi
  docker compose -p "$PROJECT" down -v --remove-orphans >/dev/null 2>&1
  rm -rf "$WORKDIR"
  if [ "$status" -eq 0 ]; then
    echo "✅ local-e2e passed"
  else
    echo "❌ local-e2e failed (exit $status)"
  fi
  exit "$status"
}
trap cleanup EXIT

step() { echo; echo "── $*"; }

step "checking local model server is reachable (${LOCAL_LLM_BASE_URL})"
curl -fsS "${LOCAL_LLM_BASE_URL%/}/models" >/dev/null 2>&1 || {
  echo "❌ local model server not reachable at ${LOCAL_LLM_BASE_URL} — start it and pull ${LOCAL_LLM_MODEL} first"
  exit 1
}

# ── Per-run ephemeral secrets (never reused, never from the ambient env) ─────
JWT_SECRET="$(openssl rand -hex 32)"
JWT_REFRESH_SECRET="$(openssl rand -hex 32)"
TOKEN_ENCRYPTION_SECRET="$(openssl rand -hex 32)"
DB_PASSWORD="$(openssl rand -hex 16)"
USER_PASSWORD="$(openssl rand -hex 12)"

# ── Isolated Postgres: unique compose project, ephemeral volume, random port ─
step "starting isolated Postgres (compose project ${PROJECT})"
CHATAPP_DB_USER=locale2e \
CHATAPP_DB_PASSWORD="$DB_PASSWORD" \
CHATAPP_DB_NAME=locale2e \
CHATAPP_DB_EXPOSED_PORT=0 \
  docker compose -p "$PROJECT" up -d --wait db
DB_PORT="$(docker compose -p "$PROJECT" port db 5432 | awk -F: '{print $NF}')"
[ -n "$DB_PORT" ] || { echo "❌ could not resolve mapped db port"; exit 1; }
echo "db on 127.0.0.1:${DB_PORT}"

# ── Build the API, then pick a free port ─────────────────────────────────────
step "building API"
go build -o "${WORKDIR}/api-server" ./cmd/api-server

API_PORT=""
for _ in $(seq 1 20); do
  candidate=$(( (RANDOM % 20000) + 20000 ))
  if ! (exec 3<>"/dev/tcp/127.0.0.1/${candidate}") 2>/dev/null; then
    API_PORT="$candidate"
    break
  fi
done
[ -n "$API_PORT" ] || { echo "❌ could not find a free API port"; exit 1; }
BASE="http://127.0.0.1:${API_PORT}"

# ── Launch the API under env -i with an explicit allowlist ───────────────────
step "launching API on :${API_PORT} (env -i allowlist, ENV=test LLM_BACKEND=local)"
mkdir -p "${WORKDIR}/files"
API_ENV=(
  "PATH=$PATH"
  "HOME=$WORKDIR"
  "ENV=test"
  "LLM_BACKEND=local"
  "LOCAL_LLM_BASE_URL=${LOCAL_LLM_BASE_URL}"
  "LOCAL_LLM_MODEL=${LOCAL_LLM_MODEL}"
  "SERVER_HOST=127.0.0.1"
  "SERVER_PORT=${API_PORT}"
  "DB_TYPE=postgres"
  "DB_HOST=127.0.0.1"
  "DB_PORT=${DB_PORT}"
  "DB_USER=locale2e"
  "DB_PASSWORD=${DB_PASSWORD}"
  "DB_NAME=locale2e"
  # This script's own docker-compose Postgres has no TLS listener; the app
  # defaults DB_SSL_MODE to "require", so this is an explicit local opt-out.
  "DB_SSL_MODE=disable"
  "AUTO_MIGRATE=true"
  "JWT_SECRET=${JWT_SECRET}"
  "JWT_REFRESH_SECRET=${JWT_REFRESH_SECRET}"
  "TOKEN_ENCRYPTION_SECRET=${TOKEN_ENCRYPTION_SECRET}"
  "OPENAI_API_KEY=dummy-local-e2e-key"
  "LOCAL_FILE_STORE_DIR=${WORKDIR}/files"
)
echo "API environment allowlist keys:"
for kv in "${API_ENV[@]}"; do echo "  ${kv%%=*}"; done

env -i "${API_ENV[@]}" "${WORKDIR}/api-server" -env /dev/null >"$API_LOG" 2>&1 &
API_PID=$!

for i in $(seq 1 30); do
  if curl -fsS "${BASE}/api/health" >/dev/null 2>&1; then
    break
  fi
  kill -0 "$API_PID" 2>/dev/null || { echo "❌ API process exited during startup"; exit 1; }
  [ "$i" -eq 30 ] && { echo "❌ API did not become healthy in 30s"; exit 1; }
  sleep 1
done
echo "API healthy"

# ── Register + login through the real auth path ──────────────────────────────
step "registering user via public register endpoint"
USERNAME="locale2e$$"
EMAIL="local-e2e-${RUN_ID}@example.test"
register_resp="$(curl -fsS -X POST "${BASE}/api/user/register" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg u "$USERNAME" --arg e "$EMAIL" --arg p "$USER_PASSWORD" \
        '{username:$u, email:$e, password:$p, terms_accepted:true}')")"
[ "$(jq -r '.access_token // empty' <<<"$register_resp")" != "" ] || { echo "❌ register returned no token: $register_resp"; exit 1; }

step "logging in via real login endpoint"
login_resp="$(curl -fsS -X POST "${BASE}/api/user/login" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg u "$USERNAME" --arg p "$USER_PASSWORD" '{username:$u, password:$p}')")"
TOKEN="$(jq -r '.access_token // empty' <<<"$login_resp")"
[ -n "$TOKEN" ] || { echo "❌ login returned no token: $login_resp"; exit 1; }
AUTH=(-H "Authorization: Bearer ${TOKEN}")

# ── Chat + real-inference reply ───────────────────────────────────────────────
step "creating chat"
chat_resp="$(curl -fsS -X POST "${BASE}/api/chat" "${AUTH[@]}" \
  -H 'Content-Type: application/json' -d '{"name":"Local E2E Chat"}')"
CHAT_ID="$(jq -r '.id // empty' <<<"$chat_resp")"
[ -n "$CHAT_ID" ] || { echo "❌ chat creation failed: $chat_resp"; exit 1; }

MSG="In one short sentence, what is the capital of France?"
step "sending chat message"
msg_resp="$(curl -fsS -X POST "${BASE}/api/chat/${CHAT_ID}/chat-message" "${AUTH[@]}" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg m "$MSG" '{message:$m, origin:"User"}')")"
JOB_ID="$(jq -r '.job_id // empty' <<<"$msg_resp")"
[ -n "$JOB_ID" ] || { echo "❌ message did not return a job id: $msg_resp"; exit 1; }

step "polling chat job ${JOB_ID} to completion"
for i in $(seq 1 90); do
  job="$(curl -fsS "${BASE}/api/job/${JOB_ID}" "${AUTH[@]}" || true)"
  status="$(jq -r '.status // empty' <<<"$job" 2>/dev/null || true)"
  case "$status" in
    complete)
      [ "$(jq -r '.error // empty' <<<"$job")" = "" ] || { echo "❌ job completed with error: $job"; exit 1; }
      break ;;
    failed|cancelled)
      echo "❌ job ended as ${status}: $job"; exit 1 ;;
  esac
  [ "$i" -eq 90 ] && { echo "❌ job did not complete within 90s (real inference is slower than mock; last status: ${status:-unknown})"; exit 1; }
  sleep 1
done

step "asserting a genuine (non-echo) assistant reply"
messages="$(curl -fsS "${BASE}/api/chat/${CHAT_ID}/chat-message?limit=50" "${AUTH[@]}")"
reply="$(jq -r --arg m "$MSG" '[.results[] | select(.origin == "Assistant")][0].message // empty' <<<"$messages")"
[ -n "$reply" ] || { echo "❌ no assistant reply found; messages: $messages"; exit 1; }
[ "$reply" != "$MSG" ] || { echo "❌ assistant reply is a verbatim echo of the user message — real inference did not happen"; exit 1; }
echo "assistant replied: ${reply}"
