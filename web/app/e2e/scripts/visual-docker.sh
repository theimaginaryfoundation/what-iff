#!/usr/bin/env bash
# Runs the visual regression suite inside the same baked CI image
# (docker/ci/Dockerfile's `e2e` target, published by ci-image.yml) that
# e2e-mock.yml's visual project checks against, so baselines render
# identically locally and in CI (openMCT's visual testing workflow — see
# e2e/README.md "Updating snapshots"). Every browser behind
# `toHaveScreenshot()` is Chromium, so a single image line covers it.
#
# Usage:
#   e2e/scripts/visual-docker.sh            # check mode (fails on mismatch)
#   e2e/scripts/visual-docker.sh --update   # regenerate baselines
#
# Backend: a backend API must be reachable on the HOST at :8080. This script
# now ensures that itself — if nothing is already serving :8080 it builds (from
# current source) and starts the self-contained compose `api` service (which
# carries its own Postgres) in mock mode, so you no longer need the old
# `make db-up` + `make dev-up`/`run-mock` dance. The `--build` and mock mode
# both matter: the @visual suite is @mock-only, and a stale prebuilt image
# silently 404s any endpoint added since it was built (which shows up as a
# misleading CORS/"failed to load" error inside a baseline). An already-running
# backend is reused (with a warning that it must be current + mock), and
# anything this script starts is left running rather than torn out from under
# you. See the ensure-backend block below for the details.
#
# The Angular dev server is started *inside* the container (same as a normal
# local run — see `localWebServer` in playwright.config.base.ts) rather than
# reused from the host. Two networking wrinkles this recipe works around:
#
# 1. `--network host` doesn't work here: Docker Desktop on macOS doesn't give
#    containers the host's network namespace the way Linux does, so the
#    container can't see host-bound ports that way. The backend is reached
#    instead via the special `host.docker.internal` DNS name Docker Desktop
#    provides for exactly this (`E2E_API_BASE_URL` below — consumed by
#    `e2e/sdk/client.ts` for the Node-side test/fixture HTTP calls).
#
# 2. The app itself bakes `http://localhost:8080/api` into its bundle at
#    build time (src/environments/environment.ts), with no runtime override.
#    Chromium running *inside* the container resolves that `localhost` to
#    the container's own loopback, not the host, so its XHRs would 404/CORS-
#    fail even though the Node-side SDK calls succeed via
#    `host.docker.internal`. Rather than rebuild the app with a different
#    API URL (which would also change its Origin header and fail the
#    backend's CORS allowlist — it only trusts `http://localhost:4200`),
#    `E2E_CHROMIUM_HOST_RESOLVER_RULES` (read by playwright.config.base.ts)
#    passes Chromium a `--host-resolver-rules` flag that transparently routes
#    just `localhost:8080` to the host backend at the network layer. The
#    frontend origin stays literally `http://localhost:4200` — the container
#    serves it — so CORS and cookies behave exactly as they do outside
#    Docker.
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../../.." && pwd)"
app_dir="web/app"

# The published image (ci-image.yml) is amd64-only. On a non-amd64 host
# (Apple Silicon) `docker run`/`docker build` would otherwise silently pick
# whatever the daemon defaults to — an arm64 build renders fonts differently
# than CI's amd64, reintroducing the exact baseline-diffs-that-look-like-UI-
# regressions problem this recipe exists to avoid, or (for `docker run`
# against the published tag) just fails outright with a "no matching
# manifest" error. Force amd64 explicitly instead of trusting the default.
platform_flag=()
host_arch="$(uname -m)"
case "$host_arch" in
  x86_64 | amd64) ;;
  *)
    platform_flag=(--platform linux/amd64)
    echo "Host arch is ${host_arch} — using --platform linux/amd64 so rendering matches CI"
    ;;
esac

# Resolved by the same script CI uses (scripts/ci-image.sh) so the renderer
# that produces committed baseline PNGs and the renderer that checks them in
# CI (e2e-mock.yml's visual project) are always the same image — a mismatch
# here would silently reintroduce the class of baseline-diffs-that-look-like-
# UI-regressions this recipe exists to avoid.
image="$("$repo_root/scripts/ci-image.sh" e2e)"

# The resolved tag is only guaranteed to exist in GHCR once ci-image.yml has
# run on a commit that changed it (main push, or that PR's own run). A
# developer on a branch with no CI run yet — or working offline — would
# otherwise just get docker's generic "pull access denied" error. Build the
# same target locally from the same Dockerfile/build-args instead: it's the
# identical content the tag would resolve to, just produced on this machine
# rather than pulled.
if ! docker manifest inspect "${image}" >/dev/null 2>&1; then
  echo "${image} not found in GHCR — building it locally instead"
  build_args="$("$repo_root/scripts/ci-image.sh" e2e --build-args | tail -n +2)"
  build_arg_flags=()
  while IFS= read -r line; do
    build_arg_flags+=(--build-arg "$line")
  done <<<"$build_args"
  docker build \
    "${platform_flag[@]}" \
    -f "$repo_root/docker/ci/Dockerfile" \
    --target e2e \
    "${build_arg_flags[@]}" \
    -t "${image}" \
    "$repo_root"
fi

npm_script="e2e:mock-llm:visual"
if [[ "${1:-}" == "--update" ]]; then
  npm_script="e2e:mock-llm:visual:update"
fi

# Poll an HTTP endpoint until it answers (2xx/3xx), up to `attempts` tries 2s
# apart. Returns non-zero on timeout so the caller can emit a context-specific
# error. Used for both the backend and the host dev-server waits below.
wait_for_http() {
  local url="$1" attempts="$2" i
  for (( i = 1; i <= attempts; i++ )); do
    curl -fsS -o /dev/null --max-time 3 "${url}" 2>/dev/null && return 0
    sleep 2
  done
  return 1
}

# Ensure a backend is reachable on the host at :8080 (the container reaches it
# via host.docker.internal — see the networking notes above). The visual suite
# is @mock-only, so the backend it renders against MUST be:
#   * in mock mode (ENV=development LLM_BACKEND=mock) — the app refuses mock
#     unless ENV is explicitly a local env, hence both are set here; and
#   * built from the current source — a stale image silently 404s any endpoint
#     added since it was built, which surfaces as a misleading CORS/"failed to
#     load" error in a baseline rather than a clean failure.
# So when this script starts the backend it uses `--build` + mock; the compose
# `api` service carries its own `db`. It is left running on exit.
backend_health="http://localhost:8080/api/health"
if curl -fsS -o /dev/null --max-time 3 "${backend_health}" 2>/dev/null; then
  echo "Backend already reachable on :8080 — reusing it."
  echo "  NOTE: the @mock-only visual suite needs a CURRENT, mock-mode backend. If a baseline"
  echo "  fails to load data (e.g. a 404/CORS error on a newer endpoint), your backend is stale"
  echo "  or not in mock mode — stop it and re-run so this script can start a fresh one."
else
  echo "No backend on :8080 — building and starting the compose 'api' service (db + api) in mock mode…"
  ENV=development LLM_BACKEND=mock \
    docker compose -f "${repo_root}/docker-compose.yml" up -d --build api
  echo "Waiting for backend readiness on :8080 (up to ~180s)…"
  if ! wait_for_http "${backend_health}" 90; then
    echo "❌ Backend did not become ready on :8080 within ~180s." >&2
    echo >&2
    # By far the most common cause on a fresh checkout, and the one that is
    # least obvious from the outside: docker-compose.yml deliberately supplies
    # no fallback for the three secrets the server validates at boot, so
    # without a .env the container exits immediately and this loop just waits
    # out its full timeout against a port nothing is listening on.
    if [ ! -f "${repo_root}/.env" ]; then
      echo "  No .env in ${repo_root}. docker-compose.yml supplies no default for" >&2
      echo "  JWT_SECRET, JWT_REFRESH_SECRET or TOKEN_ENCRYPTION_SECRET (a shipped" >&2
      echo "  default would be public), and the server refuses to boot without them." >&2
      echo >&2
      echo "  Create one:" >&2
      echo "    cp .env.example .env" >&2
      echo "    # then set the three secrets to values of at least 32 characters," >&2
      echo "    # e.g. openssl rand -hex 32" >&2
      echo >&2
    fi
    echo "  Full logs: docker compose logs api" >&2
    exit 1
  fi
  echo "✅ Backend ready on :8080 (mock mode; left running — stop later with 'docker compose stop api db')."
fi

# On a native amd64 host the container both builds and serves the app itself
# (the `webServer` block in playwright.config.mock-llm.ts). On any other host
# that build runs under emulation, where a cold `ng serve` is so slow it
# overruns Playwright's webServer timeout — the whole reason this used to be
# "regenerate on an x86_64 box instead". But the rendering that has to match CI
# is Chromium's rasterization inside the amd64 container, NOT the app bundle,
# which is platform-independent. So on an emulated host, serve the app natively
# on the host and route only Chromium through the container: extend the
# host-resolver remap to cover localhost:4200 as well as localhost:8080, and
# set E2E_REUSE_HOST_WEBSERVER so Playwright skips its own (emulated) dev
# server. The app's origin stays localhost:4200 — CORS, cookies and the
# committed baselines are byte-for-byte the same as the in-container path.
resolver_rules="MAP localhost:8080 host.docker.internal:8080"
reuse_webserver_env=()
# PID of the backgrounded dev-server subshell, when THIS script started one.
# Empty means we reused an existing :4200 (or never started one), so the trap
# must not signal anything.
dev_server_pid=""
stop_host_dev_server() {
  [[ -n "${dev_server_pid}" ]] || return 0
  # Guard against PID/PGID reuse: only signal if a node/npm process is still
  # alive in our process group. The subshell runs `cd && npm start`, so bash
  # does not exec-replace itself — the group leader stays a shell with node/npm
  # children sharing its pgid. If that subshell already died on its own, its PID
  # may have been recycled by an unrelated process, and blindly TERMing the
  # negative PID would take down a stranger's group. Confirm the group is still
  # ours before touching it. `ps -A -o pgid=,command=` is portable across the
  # BSD (macOS) and GNU ps this script may run under.
  if ! ps -A -o pgid=,command= 2>/dev/null \
       | awk -v pg="${dev_server_pid}" '$1 == pg' \
       | grep -Eiq 'node|npm'; then
    echo "Host Angular dev server (pgid ${dev_server_pid}) already gone — nothing to stop."
    dev_server_pid=""
    return 0
  fi
  echo "Stopping the host Angular dev server (pid/pgid ${dev_server_pid})…"
  # Started under `set -m`, so the backgrounded subshell leads its own process
  # group whose id equals its PID ($!); signalling the negative PID takes down
  # `ng serve` and its build workers together. Fall back to the bare PID if the
  # group is already gone.
  kill -TERM "-${dev_server_pid}" 2>/dev/null || kill -TERM "${dev_server_pid}" 2>/dev/null || true
  # Idempotent: the EXIT trap fires once, but clearing avoids any re-entry
  # re-signalling a PID that is no longer ours.
  dev_server_pid=""
}
if [[ ${#platform_flag[@]} -gt 0 ]]; then
  frontend_url="http://localhost:4200"
  if curl -fsS -o /dev/null --max-time 3 "${frontend_url}" 2>/dev/null; then
    echo "Angular dev server already on :4200 — reusing it (it must be bound to 0.0.0.0 to be reachable from the container)."
  else
    echo "Emulated render container — serving the app natively on the host (:4200) to skip an emulated build…"
    # `set -m` puts the backgrounded subshell in its own process group so the
    # EXIT trap can take down `ng serve` and its build workers, not just the
    # npm wrapper. Bound to 0.0.0.0 because the container reaches it via
    # host.docker.internal — a default localhost-only bind would refuse that.
    set -m
    ( cd "${repo_root}/${app_dir}" && npm start -- --host 0.0.0.0 --port 4200 --live-reload=false ) \
      >/tmp/visual-host-webserver.log 2>&1 &
    dev_server_pid=$!
    set +m
    trap stop_host_dev_server EXIT
    echo "Waiting for the host dev server on :4200 (up to ~240s — a cold native build)…"
    if ! wait_for_http "${frontend_url}" 120; then
      echo "❌ Host dev server did not come up on :4200 — see /tmp/visual-host-webserver.log" >&2
      exit 1
    fi
    echo "✅ Host dev server ready on :4200."
  fi
  resolver_rules="${resolver_rules},MAP localhost:4200 host.docker.internal:4200"
  reuse_webserver_env=(-e "E2E_REUSE_HOST_WEBSERVER=1")
fi

echo "Using image ${image}"

# The repo is bind-mounted so results (updated baselines, reports) land back
# on the host, but node_modules gets its own anonymous volume: npm ci inside
# the (Linux) container must not overwrite the host's (macOS) node_modules —
# native deps like esbuild are platform-specific and that would break the
# host toolchain afterwards.
docker run --rm \
  "${platform_flag[@]}" \
  --add-host=host.docker.internal:host-gateway \
  -v "${repo_root}:/repo" \
  -v "/repo/${app_dir}/node_modules" \
  -w "/repo/${app_dir}" \
  -e "E2E_API_BASE_URL=http://host.docker.internal:8080/api" \
  -e "E2E_CHROMIUM_HOST_RESOLVER_RULES=${resolver_rules}" \
  ${reuse_webserver_env[@]+"${reuse_webserver_env[@]}"} \
  -e "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1" \
  "${image}" \
  /bin/bash -lc "npm ci && npm run ${npm_script}"
