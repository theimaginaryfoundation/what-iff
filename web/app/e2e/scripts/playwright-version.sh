#!/usr/bin/env bash
# playwright-version.sh — the one place the Playwright version is resolved.
#
# Prints the exact version, e.g. `1.62.1`. Everything that needs to agree on a
# Playwright version reads it from here rather than hardcoding its own copy:
#
#   * e2e/scripts/visual-docker.sh   — picks the container image to render in
#   * .github/workflows/e2e-mock.yml — picks the container image CI runs in
#
# Why this matters more than usual: the visual baselines committed under
# `tests/visual/**-snapshots/` were rasterized by the browsers bundled in one
# specific `mcr.microsoft.com/playwright:vX.Y.Z-*` image. Checking them with a
# different image compares against a renderer that never produced them, and the
# failures look like real UI regressions. Version skew here is not a build
# annoyance, it silently invalidates the baselines.
#
# `package.json` is the source of truth and is pinned EXACTLY (no `^`) for that
# reason: a range would let `npm install` or a bot move the installed version
# while the image tags stayed literal.
set -euo pipefail

app_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

declared="$(node -p "require('${app_dir}/package.json').devDependencies['@playwright/test']" 2>/dev/null || true)"
if [[ -z "$declared" || "$declared" == "undefined" ]]; then
  echo "playwright-version: @playwright/test is not in ${app_dir}/package.json devDependencies" >&2
  exit 1
fi

# A range means the declared value can no longer identify an image tag.
if [[ ! "$declared" =~ ^[0-9]+\.[0-9]+\.[0-9]+$ ]]; then
  cat >&2 <<MSG
playwright-version: "@playwright/test" is "${declared}", which is a range, not an exact version.

It must be pinned exactly (e.g. "1.62.1"), because the container image tag and
the committed visual baselines are derived from it. A range lets the installed
version drift away from the image that rendered the baselines.
MSG
  exit 1
fi

# When dependencies are installed, make sure they actually match what is
# declared — a stale node_modules is the other way these can silently diverge.
installed="$(node -p "require('${app_dir}/node_modules/@playwright/test/package.json').version" 2>/dev/null || true)"
if [[ -n "$installed" && "$installed" != "$declared" ]]; then
  cat >&2 <<MSG
playwright-version: installed @playwright/test (${installed}) != declared (${declared}).

Run \`npm ci\` in web/app. Until they match, the container image
and the browsers actually driving the tests are different builds.
MSG
  exit 1
fi

echo "$declared"
