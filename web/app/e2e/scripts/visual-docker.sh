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
# Prerequisites (this repo's local stack, not started by this script):
#   - Postgres + backend API reachable on the HOST at :8080 (make db-up,
#     make dev-up / make run-mock / make run-local).
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
  -e "E2E_CHROMIUM_HOST_RESOLVER_RULES=MAP localhost:8080 host.docker.internal:8080" \
  -e "PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1" \
  "${image}" \
  /bin/bash -lc "npm ci && npm run ${npm_script}"
