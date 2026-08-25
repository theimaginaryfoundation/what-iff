#!/usr/bin/env bash
# ci-image.sh — the one place the CI base image ref is resolved.
#
# Prints the full image ref for a given target (`e2e` or `e2e-ollama`) built
# from docker/ci/Dockerfile:
#
#   ghcr.io/theimaginaryfoundation/what-iff-ci:e2e-pw1.62.1-a1b2c3d4
#   ghcr.io/theimaginaryfoundation/what-iff-ci:e2e-ollama-pw1.62.1-a1b2c3d4
#
# The tag embeds the Playwright version (for humans skimming workflow logs —
# see the visual-baseline notes in playwright-version.sh) and an 8-hex-char
# hash of the Dockerfile, every resolved build arg, and the base image's
# current remote manifest content. Any change to the Dockerfile, to a pin
# (Playwright, OLLAMA_VERSION, LOCAL_LLM_MODEL), or to what the base image tag
# currently resolves to therefore produces a new tag automatically; commits
# that touch none of those resolve to the same tag, so unrelated pushes to
# main don't churn a new image or a new pull.
#
# Everything that needs this ref calls this script rather than hardcoding a
# tag, so the build workflow (ci-image.yml) and every consumer (e2e-mock.yml,
# e2e-local-llm.yml, pr-validation.yml, visual-docker.sh) can never disagree
# about which image a given set of pins maps to.
#
# Usage: ci-image.sh <e2e|e2e-ollama> [--build-args]
#
# By default, prints only the resolved ref. With --build-args, also prints
# the individual resolved values (PLAYWRIGHT_VERSION, OLLAMA_VERSION,
# LOCAL_LLM_MODEL) as KEY=VALUE lines after the ref — so a caller building
# the image (e.g. ci-image.yml) can source the exact same resolution this
# script used for the tag, instead of re-deriving the pins independently
# and risking the two falling out of sync.
#
# Exit codes: 1 = no target given, 2 = invalid/unrecognized usage
# (unknown target, or extra arguments).
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
registry="ghcr.io/theimaginaryfoundation/what-iff-ci"

if [ "$#" -eq 0 ]; then
  echo "ci-image: usage: $(basename "$0") <e2e|e2e-ollama> [--build-args]" >&2
  exit 1
fi

target="$1"
shift

print_build_args=false
if [ "$#" -gt 0 ] && [ "$1" = "--build-args" ]; then
  print_build_args=true
  shift
fi

if [ "$#" -gt 0 ]; then
  echo "ci-image: unexpected extra arguments: $*" >&2
  exit 2
fi

case "$target" in
  e2e|e2e-ollama) ;;
  *)
    echo "ci-image: unrecognized target '$target' — usage: $(basename "$0") <e2e|e2e-ollama> [--build-args]" >&2
    exit 2
    ;;
esac

playwright_version="$("$repo_root/web/app/e2e/scripts/playwright-version.sh")"

# print-local-llm-pins emits OLLAMA_VERSION=... / LOCAL_LLM_MODEL=... on two
# fixed lines — parse them explicitly rather than `eval`, since a stray
# space/quote/shell metacharacter in a future Makefile edit to these pins
# would otherwise be executed.
ollama_pins="$(make -s -C "$repo_root" print-local-llm-pins)"
OLLAMA_VERSION=""
LOCAL_LLM_MODEL=""
while IFS='=' read -r pin_key pin_value; do
  case "$pin_key" in
    OLLAMA_VERSION) OLLAMA_VERSION="$pin_value" ;;
    LOCAL_LLM_MODEL) LOCAL_LLM_MODEL="$pin_value" ;;
  esac
done <<EOF
$ollama_pins
EOF

if [ -z "$OLLAMA_VERSION" ] || [ -z "$LOCAL_LLM_MODEL" ]; then
  echo "ci-image: 'make print-local-llm-pins' did not emit both pins as expected:" >&2
  echo "$ollama_pins" >&2
  exit 1
fi

# The base image tag (mcr.microsoft.com/playwright:vX-noble) is mutable — Microsoft
# can republish it with different content (patched packages, a new Ubuntu point
# release) without changing the tag string. Fold its current manifest into the hash
# so a republish resolves to a new CI image tag instead of silently keeping stale
# content under the old one forever (the "tag already exists" check in ci-image.yml
# only compares tag strings, not content).
base_image="mcr.microsoft.com/playwright:v${playwright_version}-noble"
if ! base_manifest="$(docker manifest inspect "$base_image" 2>&1)"; then
  echo "ci-image: failed to inspect base image manifest for $base_image:" >&2
  echo "$base_manifest" >&2
  exit 1
fi

hash_input="$(cat "$repo_root/docker/ci/Dockerfile"; printf '%s\n%s\n%s\n%s\n' \
  "PLAYWRIGHT_VERSION=$playwright_version" \
  "OLLAMA_VERSION=$OLLAMA_VERSION" \
  "LOCAL_LLM_MODEL=$LOCAL_LLM_MODEL" \
  "$base_manifest")"

if command -v sha256sum >/dev/null 2>&1; then
  digest="$(printf '%s' "$hash_input" | sha256sum | cut -c1-8)"
else
  digest="$(printf '%s' "$hash_input" | shasum -a 256 | cut -c1-8)"
fi

echo "${registry}:${target}-pw${playwright_version}-${digest}"

if [ "$print_build_args" = true ]; then
  echo "PLAYWRIGHT_VERSION=$playwright_version"
  echo "OLLAMA_VERSION=$OLLAMA_VERSION"
  echo "LOCAL_LLM_MODEL=$LOCAL_LLM_MODEL"
fi
