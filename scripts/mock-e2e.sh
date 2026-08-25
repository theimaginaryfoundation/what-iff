#!/usr/bin/env bash
# mock-e2e.sh — hermetic backend end-to-end test under LLM_BACKEND=mock.
#
# Runs an isolated Postgres (own compose project + ephemeral volume + random
# host port), builds the API, launches it under `env -i` with an explicit
# environment allowlist and per-run generated secrets, then exercises the real
# register → login → chat → job pipeline:
#
#   * registers a user through the public register endpoint (no auth backdoor,
#     no SUPERADMIN_* reuse) and logs in through the real login path
#   * sends a chat message and polls the returned job id to completion
#     (bounded timeout — the save is NOT assumed done when the request returns)
#   * asserts the mock echo reply, that the job did not fail, and that the
#     message persists on re-fetch
#   * sends an image-ritual message and asserts a genuine PNG attachment
#
# Requires: docker (compose v2), go, curl, jq, openssl.
set -euo pipefail

cd "$(dirname "$0")/.."

for tool in docker go curl jq openssl; do
  command -v "$tool" >/dev/null 2>&1 || { echo "❌ required tool missing: $tool"; exit 1; }
done

RUN_ID="$(date +%s)-$$"
PROJECT="chatapp-mock-e2e-${RUN_ID}"
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
  # Convert after the kill/wait above (SIGTERM lets the -cover binary flush
  # counters via its normal graceful-shutdown path) and before the workdir
  # holding $GOCOVERDIR is removed below. A conversion failure must not mask
  # a passing test run, and a passing conversion must not mask a failing one
  # — $status was captured before any of this ran and nothing here touches
  # it; failures here only print a warning.
  if [ "${MOCK_E2E_COVERAGE:-}" = "1" ]; then
    mkdir -p coverage
    if [ -n "$(ls -A "${WORKDIR}/gocov" 2>/dev/null)" ]; then
      if go tool covdata textfmt -i="${WORKDIR}/gocov" -o coverage/go-mock-e2e.out; then
        echo "✅ coverage written to coverage/go-mock-e2e.out"
      else
        echo "⚠️  covdata textfmt failed; coverage not written"
      fi
    else
      echo "⚠️  GOCOVERDIR (${WORKDIR}/gocov) is empty; no coverage written"
    fi
  fi
  if [ "$status" -ne 0 ] && [ -f "$API_LOG" ]; then
    echo "--- api log (last 50 lines) ---"
    tail -n 50 "$API_LOG"
    echo "-------------------------------"
  fi
  docker compose -p "$PROJECT" down -v --remove-orphans >/dev/null 2>&1
  rm -rf "$WORKDIR"
  if [ "$status" -eq 0 ]; then
    echo "✅ mock-e2e passed"
  else
    echo "❌ mock-e2e failed (exit $status)"
  fi
  exit "$status"
}
trap cleanup EXIT

step() { echo; echo "── $*"; }

# ── Per-run ephemeral secrets (never reused, never from the ambient env) ─────
JWT_SECRET="$(openssl rand -hex 32)"
JWT_REFRESH_SECRET="$(openssl rand -hex 32)"
TOKEN_ENCRYPTION_SECRET="$(openssl rand -hex 32)"
DB_PASSWORD="$(openssl rand -hex 16)"
USER_PASSWORD="$(openssl rand -hex 12)"

# ── Isolated Postgres: unique compose project, ephemeral volume, random port ─
step "starting isolated Postgres (compose project ${PROJECT})"
CHATAPP_DB_USER=mocke2e \
CHATAPP_DB_PASSWORD="$DB_PASSWORD" \
CHATAPP_DB_NAME=mocke2e \
CHATAPP_DB_EXPOSED_PORT=0 \
  docker compose -p "$PROJECT" up -d --wait db
DB_PORT="$(docker compose -p "$PROJECT" port db 5432 | awk -F: '{print $NF}')"
[ -n "$DB_PORT" ] || { echo "❌ could not resolve mapped db port"; exit 1; }
echo "db on 127.0.0.1:${DB_PORT}"

# ── Build the API, then pick a free port ─────────────────────────────────────
step "building API"
# Not an array: `set -u` throws "unbound variable" on an empty array
# expansion under bash 3.2 (macOS's default /bin/bash), so an if/else with
# two plain invocations is used instead of a conditionally-populated flags
# array.
if [ "${MOCK_E2E_COVERAGE:-}" = "1" ]; then
  mkdir -p "${WORKDIR}/gocov"
  go build -cover -o "${WORKDIR}/api-server" ./cmd/api-server
else
  go build -o "${WORKDIR}/api-server" ./cmd/api-server
fi

# Port probing happens after the (potentially slow) build to keep the
# check-to-use window small.
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

step "launching API on :${API_PORT} (env -i allowlist, ENV=test LLM_BACKEND=mock)"
mkdir -p "${WORKDIR}/files"
API_ENV=(
  "PATH=$PATH"
  "HOME=$WORKDIR"
  "ENV=test"
  "LLM_BACKEND=mock"
  "MOCK_LLM_MODE=echo"
  "SERVER_HOST=127.0.0.1"
  "SERVER_PORT=${API_PORT}"
  "DB_TYPE=postgres"
  "DB_HOST=127.0.0.1"
  "DB_PORT=${DB_PORT}"
  "DB_USER=mocke2e"
  "DB_PASSWORD=${DB_PASSWORD}"
  "DB_NAME=mocke2e"
  # This script's own docker-compose Postgres has no TLS listener; the app
  # defaults DB_SSL_MODE to "require", so this is an explicit local opt-out.
  "DB_SSL_MODE=disable"
  "AUTO_MIGRATE=true"
  "JWT_SECRET=${JWT_SECRET}"
  "JWT_REFRESH_SECRET=${JWT_REFRESH_SECRET}"
  "TOKEN_ENCRYPTION_SECRET=${TOKEN_ENCRYPTION_SECRET}"
  "OPENAI_API_KEY=dummy-mock-key"
  "LOCAL_FILE_STORE_DIR=${WORKDIR}/files"
)
# env -i strips GOCOVERDIR like everything else not on this allowlist, so a
# -cover binary launched this way needs it added back explicitly.
if [ "${MOCK_E2E_COVERAGE:-}" = "1" ]; then
  API_ENV+=("GOCOVERDIR=${WORKDIR}/gocov")
fi
# Log the resolved environment KEYS (never values) so a debugging session can
# see exactly what the API was allowed to inherit.
echo "API environment allowlist keys:"
for kv in "${API_ENV[@]}"; do echo "  ${kv%%=*}"; done

# -env /dev/null stops the binary from loading the repo .env on top of the
# allowlist; env -i guarantees nothing else (LLM_BACKEND, MOCK_LLM_*,
# ALLOWED_EMAILS, DB_SECRET_ARN, OTEL/S3/Cognito vars, ...) leaks in from the
# caller's shell.
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
USERNAME="mocke2e$$"
EMAIL="mock-e2e-${RUN_ID}@example.test"
register_resp="$(curl -fsS -X POST "${BASE}/api/user/register" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg u "$USERNAME" --arg e "$EMAIL" --arg p "$USER_PASSWORD" \
        '{username:$u, email:$e, password:$p, terms_accepted:true}')")"
[ "$(jq -r '.access_token // empty' <<<"$register_resp")" != "" ] || { echo "❌ register returned no token: $register_resp"; exit 1; }

step "logging in via real login endpoint"
login_resp="$(curl -fsS -X POST "${BASE}/api/user/login" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg u "$USERNAME" --arg p "$USER_PASSWORD" '{username:$u, password:$p}')")"
TOKEN="$(jq -r '.access_token // empty' <<<"$login_resp" 2>/dev/null)" || TOKEN=""
[ -n "$TOKEN" ] || { echo "❌ login returned no token: $login_resp"; exit 1; }
AUTH=(-H "Authorization: Bearer ${TOKEN}")

# ── Chat + mock echo message ─────────────────────────────────────────────────
step "creating chat"
chat_resp="$(curl -fsS -X POST "${BASE}/api/chat" "${AUTH[@]}" \
  -H 'Content-Type: application/json' -d '{"name":"Mock E2E Chat"}')"
CHAT_ID="$(jq -r '.id // empty' <<<"$chat_resp" 2>/dev/null)" || CHAT_ID=""
[ -n "$CHAT_ID" ] || { echo "❌ chat creation failed: $chat_resp"; exit 1; }

MSG="Hello from mock-e2e ${RUN_ID} — echo me back, please."
step "sending chat message"
msg_resp="$(curl -fsS -X POST "${BASE}/api/chat/${CHAT_ID}/chat-message" "${AUTH[@]}" \
  -H 'Content-Type: application/json' \
  -d "$(jq -n --arg m "$MSG" '{message:$m, origin:"User"}')")"
JOB_ID="$(jq -r '.job_id // empty' <<<"$msg_resp" 2>/dev/null)" || JOB_ID=""
[ -n "$JOB_ID" ] || { echo "❌ message did not return a job id: $msg_resp"; exit 1; }

# poll_job <job_id>: waits (bounded) for terminal success and asserts no error.
# Transient fetch failures or non-JSON bodies (502s, proxy errors) are logged
# and retried instead of killing the whole script via set -e.
#
# set -e nuance, verified empirically (not just by reading docs): a BARE
# `var="$(jq ...)"` assignment DOES trip errexit if jq fails — bash special-
# cases assignment-only simple commands to inherit the command substitution's
# exit status. It is only exempt inside an `if`/`while` condition, a `&&`/`||`
# list, or embedded as part of a larger argument like `[ "$(jq ...)" = x ]`
# (where the exit status that matters is `[`'s, not jq's). Every bare
# assignment below is therefore guarded with `|| var=""` so a malformed body
# degrades to the existing "❌ ..." assertion instead of an abrupt jq abort;
# poll_job's loop additionally needs the explicit retry (not just a fallback
# value) since it must keep polling rather than give up on one bad response.
poll_job() {
  local job_id="$1" job status
  for _ in $(seq 1 60); do
    if ! job="$(curl -fsS "${BASE}/api/job/${job_id}" "${AUTH[@]}")"; then
      echo "  (transient: job fetch failed, retrying)"
      sleep 1
      continue
    fi
    if ! status="$(jq -r '.status' <<<"$job" 2>/dev/null)"; then
      echo "  (transient: non-JSON job response, retrying): ${job}"
      sleep 1
      continue
    fi
    case "$status" in
      complete)
        [ "$(jq -r '.error // empty' <<<"$job")" = "" ] || { echo "❌ job ${job_id} completed with error: $job"; return 1; }
        return 0 ;;
      failed|cancelled)
        echo "❌ job ${job_id} ended as ${status}: $job"; return 1 ;;
    esac
    sleep 1
  done
  echo "❌ job ${job_id} did not complete within 60s (last status: ${status:-unknown})"
  return 1
}

step "polling chat job ${JOB_ID} to completion"
poll_job "$JOB_ID"

step "asserting echo reply + persistence"
# GET chat-message returns a PaginatedResponse envelope: {results: [...]}.
messages="$(curl -fsS "${BASE}/api/chat/${CHAT_ID}/chat-message?limit=50" "${AUTH[@]}")"
echo_count="$(jq --arg m "$MSG" '[.results[] | select(.origin == "Assistant" and .message == $m)] | length' <<<"$messages" 2>/dev/null)" || echo_count=0
[ "$echo_count" -ge 1 ] || { echo "❌ echoed assistant reply not found; messages: $messages"; exit 1; }
user_err="$(jq -r --arg m "$MSG" '[.results[] | select(.origin == "User" and .message == $m)][0].last_error_message // empty' <<<"$messages" 2>/dev/null)" || user_err=""
[ -z "$user_err" ] || { echo "❌ user turn carries last_error_message: $user_err"; exit 1; }
# Persistence: an independent re-fetch must return the same assistant reply.
refetch="$(curl -fsS "${BASE}/api/chat/${CHAT_ID}/chat-message?limit=50" "${AUTH[@]}")"
refetch_count="$(jq --arg m "$MSG" '[.results[] | select(.origin == "Assistant" and .message == $m)] | length' <<<"$refetch" 2>/dev/null)" || refetch_count=0
[ "$refetch_count" -ge 1 ] || { echo "❌ assistant reply missing on re-fetch"; exit 1; }
echo "echo reply persisted"

# ── Image ritual: fixture PNG persisted through the real save path ───────────
step "sending image-ritual message"
img_resp="$(curl -fsS -X POST "${BASE}/api/chat/${CHAT_ID}/chat-message" "${AUTH[@]}" \
  -H 'Content-Type: application/json' \
  -d '{"message":"Please draw a fox","origin":"User","rituals":[{"id":"00000000-0000-0000-0000-000000000101","name":"Generate Image"}]}')"
IMG_JOB_ID="$(jq -r '.job_id // empty' <<<"$img_resp" 2>/dev/null)" || IMG_JOB_ID=""
[ -n "$IMG_JOB_ID" ] || { echo "❌ image message did not return a job id: $img_resp"; exit 1; }

step "polling image job ${IMG_JOB_ID} to completion"
poll_job "$IMG_JOB_ID"

step "asserting persisted PNG attachment"
messages="$(curl -fsS "${BASE}/api/chat/${CHAT_ID}/chat-message?limit=50" "${AUTH[@]}")"
attachment="$(jq -r '[.results[] | select(.origin == "Assistant" and .message == "Generated image.")][0].attachments[0] // empty' <<<"$messages" 2>/dev/null)" || attachment=""
[ -n "$attachment" ] || { echo "❌ image-ritual assistant message has no attachment; messages: $messages"; exit 1; }
# This block deliberately does not use the file's usual `2>/dev/null` fallback.
# It is the assertion that failed opaquely for weeks, so a parse failure and an
# absent field are kept distinguishable here even though the surrounding script
# collapses them. jq's own stderr is left visible on purpose.
if ! file_type="$(jq -r '.file_type' <<<"$attachment")"; then
  echo "❌ could not parse attachment JSON while reading file_type; raw attachment: $attachment"; exit 1
fi
[ "$file_type" = "image/png" ] || { echo "❌ attachment is not image/png: '${file_type}'"; exit 1; }
# Attachment bytes are not in the API payload. FileAttachment.FileContent is
# `json:"-"` (internal/models/fileattachment.go) — ephemeral base64 populated
# in-process only while forwarding a tool result into a vision request, never
# serialized to a client.
#
# Fetch through the gallery endpoint rather than reading the file store. GET
# /image-gallery/{id} routes through storage.ResolveAttachmentImageBytes, which
# tries the stored s3_key first (attachment_image_bytes.go:46) and falls back to
# synthesising the legacy per-chat key (:57). Reading the store by s3_key would
# only ever exercise the first branch, leaving a regression in the resolution
# path users actually hit invisible to this suite.
if ! att_id="$(jq -r '.id // empty' <<<"$attachment")"; then
  echo "❌ could not parse attachment JSON while reading id; raw attachment: $attachment"; exit 1
fi
[ -n "$att_id" ] || { echo "❌ attachment has no id; attachment: $attachment"; exit 1; }

fetched="${WORKDIR}/gallery-image.png"
if ! curl -fsS "${BASE}/api/image-gallery/${att_id}?size=full" "${AUTH[@]}" -o "$fetched"; then
  echo "❌ GET /api/image-gallery/${att_id}?size=full did not return image bytes"; exit 1
fi

# Magic-bytes probe, not decode validation: this proves the endpoint served real
# PNG bytes rather than a stub or an error page. It says nothing about whether
# the image is well-formed past its header.
#
# Length is checked first so a truncated response reports its size instead of a
# short signature string that reads like a content mismatch. No `|| true` on the
# pipeline — the length guard already assures readable bytes, and swallowing an
# od failure would turn it into a misleading signature error.
fetched_bytes="$(wc -c < "$fetched")"
[ "$fetched_bytes" -ge 8 ] || { echo "❌ gallery returned ${fetched_bytes} bytes — too short to be a PNG"; exit 1; }
sig="$(head -c 8 "$fetched" | od -An -tx1 | tr -d ' \n')"
[ "$sig" = "89504e470d0a1a0a" ] || { echo "❌ gallery did not return a PNG (signature: ${sig})"; exit 1; }
echo "PNG attachment verified via /api/image-gallery/${att_id} (${fetched_bytes} bytes)"
