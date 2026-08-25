#!/usr/bin/env bash
# check-no-local-models.sh — enforce the C2 constraint (ADR 0x018, amended):
# this project never embeds or downloads a local model RUNTIME (llama.cpp
# linked in, gguf weights vendored, ...) in dev, tests, or CI build artifacts.
# Calling out to an external local OpenAI-compatible server (Ollama, LM
# Studio, ...) over HTTP is explicitly sanctioned as LLM_BACKEND=local — that
# path never touches go.mod, the import graph, or an embedded runtime, so it
# does not trip this audit. All LLM mocking-and-fixture behavior stays
# in-process (see ADR 0x018).
#
# Three sweeps:
#   1. go.mod / go.sum — no local-model runtime module ever declared or resolved
#   2. resolved import graph (go list -deps) — nothing pulled in transitively
#   3. source/config scan — no invocation or configuration of an embedded
#      local runtime (documentation and this script itself are excluded;
#      ADRs legitimately discuss these tools by name). "ollama" is
#      deliberately NOT in this pattern list: it's the sanctioned external
#      LLM_BACKEND=local server, so legitimate references to it (adapter
#      code, env var defaults, docs) must not fail this audit. Every other
#      token here still implies embedding or downloading a runtime/weights,
#      which stays forbidden.
set -uo pipefail

cd "$(dirname "$0")/.."

# Deliberately broad: false positives here just mean a contributor explains a
# benign match (or excludes it via the doc/self-exclusions below), while a
# false negative would silently let an embedded local-model runtime back in.
# If a specific token starts producing recurring false positives, narrow that
# token with word boundaries rather than loosening the whole set.
PATTERNS='llama[._-]cpp|llamafile|ggml|gguf|gpt4all|lm[-_]?studio|koboldcpp|exllama|localai|text-generation-inference|ctransformers'
SELF="$(basename "$0")"
fail=0

echo "1/3 checking go.mod and go.sum..."
# Match only the module-path field of go.sum — the base64 hash columns can
# randomly contain pattern substrings (e.g. "ggml") and must not fail the audit.
if grep -Ein "$PATTERNS" go.mod || awk '{print $1}' go.sum | grep -Ei "$PATTERNS"; then
  echo "❌ local-model dependency referenced in go.mod/go.sum"
  fail=1
else
  echo "✅ go.mod/go.sum clean"
fi

echo "2/3 checking resolved Go import graph..."
# Fail fast if go list itself fails (offline, flaky module proxy, build error):
# grepping partial output would silently turn resolution failures into a
# passing audit.
deps_file="$(mktemp)"
err_file="$(mktemp)"
trap 'rm -f "$deps_file" "$err_file"' EXIT
# stderr goes to its own file, not into $deps_file: folding them together meant a
# failure printed the first few *package names* as the "reason" and threw the actual
# error away, which is a confusing way to learn your module cache is cold.
if ! go list -deps ./... >"$deps_file" 2>"$err_file"; then
  echo "❌ go list -deps failed — cannot audit the import graph:"
  tail -n 10 "$err_file" >&2
  exit 1
fi
if grep -Ei "$PATTERNS" "$deps_file"; then
  echo "❌ local-model package present in the resolved import graph"
  fail=1
else
  echo "✅ import graph clean"
fi

echo "3/3 scanning source and config files..."
if grep -RIEn "$PATTERNS" \
    --exclude-dir=.git \
    --exclude-dir=node_modules \
    --exclude-dir=dist \
    --exclude-dir=.angular \
    --exclude-dir=vendor \
    --exclude-dir=.dev \
    --exclude='*.md' \
    --exclude='*.mdc' \
    --exclude="$SELF" \
    --exclude='go.sum' \
    --exclude='package-lock.json' \
    -- .; then
  echo "❌ local-model reference found in source/config"
  fail=1
else
  echo "✅ source/config scan clean"
fi

echo
if [ "$fail" -ne 0 ]; then
  echo "❌ check-no-local-models FAILED — the project must not depend on local model runners"
  exit 1
fi
echo "✅ No local-model dependencies found"
