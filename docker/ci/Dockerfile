# CI base image for the frontend e2e workflows. Bakes the OS packages and
# (for the `e2e-ollama` target) the Ollama binary + model weights that those
# workflows otherwise install at test time. Why: every install here was a source of CI flakiness (apt archive
# blips, Ollama installer/registry blips) unrelated to the code under test.
#
# Built and tagged by scripts/ci-image.sh; never build or reference this
# Dockerfile directly with a hand-picked tag, or the image a workflow pulls
# can silently diverge from the Playwright/Ollama pins it was meant to match.

ARG PLAYWRIGHT_VERSION

FROM mcr.microsoft.com/playwright:v${PLAYWRIGHT_VERSION}-noble AS e2e

# postgresql-client: e2e workflows create the pgvector extension against the
#   `db` service container directly (no volume-mounted initdb on a GH
#   service container).
# make, jq, python3: local-llm workflow needs make (Ollama install target),
#   jq (PR comment step), python3 (the e2e-status-comment composite action).
#   Included here too (not just the e2e-ollama target) so both targets share
#   one apt layer and neither drifts from the other.
# zstd: the official Ollama installer extracts its archive with it — only
#   used by e2e-ollama, but harmless and small enough to keep in one layer.
# curl: the e2e-ollama target's installer step depends on it. The Playwright
#   base image already ships curl, but it's listed explicitly here so this
#   layer doesn't silently break if a future base-image bump drops it.
RUN apt-get update \
    && apt-get install -y --no-install-recommends \
        postgresql-client make jq python3 zstd curl \
    && rm -rf /var/lib/apt/lists/*

FROM e2e AS e2e-ollama

ARG OLLAMA_VERSION
ARG LOCAL_LLM_MODEL

# Trusts ollama.com's installer script as-is (no pin/checksum available for
# it upstream) — acceptable here since this only ever runs in a CI image
# build, never in a production runtime image.
RUN curl -fsSL https://ollama.com/install.sh | OLLAMA_VERSION="${OLLAMA_VERSION}" sh

# Ollama resolves its model store from $HOME/.ollama/models by default, but
# GitHub Actions container jobs override HOME to /github/home — a directory
# that doesn't exist in this image and has nothing baked into it — which
# would make the pulled weights below invisible at test time. Pin the model
# store to a fixed path so it's unaffected by whatever HOME the runner sets.
ENV OLLAMA_MODELS=/root/.ollama/models

# Bake the model weights into the image so a test run never has to reach the
# Ollama registry: start the server just long enough to pull, then stop it.
# 120s (not 60s) to give slower CI hosts room before this is treated as a
# hung build. `pkill` is allowed to fail — the layer only needs the pull to
# have finished, not the server to still be running when it exits.
RUN (ollama serve > /tmp/ollama-build.log 2>&1 &) \
    && timeout 120 bash -c 'until curl -sf http://127.0.0.1:11434 >/dev/null; do sleep 1; done' \
    && ollama pull "${LOCAL_LLM_MODEL}" \
    && (pkill ollama || true)
