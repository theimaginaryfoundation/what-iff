#!/usr/bin/env bash
# check-public-hygiene.sh — Layer A of the public/private boundary check.
#
# This is the *generic* half: patterns that are self-evidently wrong in any
# public repo, with nothing project-specific or secret in the list itself.
# That is deliberate — a second, private-name denylist (real hostnames,
# tracker IDs, sibling repo names) is maintained and run entirely outside
# this tree, so this script stays safe to ship and read.
#
# Uses `git grep`, which searches tracked files only and is portable across
# macOS and CI. Do not switch to `xargs grep` — xargs exits 123 when any
# batch finds nothing, which silently turns every check into a pass.
#
# Deliberately no `set -e`: this script accumulates every violation into
# `fail` and reports them all in one pass rather than stopping at the first
# one, and `git grep`/`git ls-files` finding nothing is a normal, expected
# outcome (nonzero exit) for most of the patterns below, not an error to
# abort on.
#
# Usage: scripts/check-public-hygiene.sh [--cached]

set -uo pipefail

cd "$(dirname "$0")/.."

MODE_ARGS=()
[[ "${1:-}" == "--cached" ]] && MODE_ARGS=(--cached)
# Expanded below as "${MODE_ARGS[@]+"${MODE_ARGS[@]}"}", not the plainer
# "${MODE_ARGS[@]}" — under `set -u`, bash 3.2 (macOS's system bash, which
# this repo's tooling targets) treats expanding an empty array as an unbound
# variable. The `+` guard makes an empty MODE_ARGS expand to nothing instead
# of erroring.

fail=0
problem() { printf '\n\033[31m✖ %s\033[0m\n' "$1"; fail=1; }

# ---------------------------------------------------------------------------
# Forbidden tracked-file globs: things that should never be committed at all.
# ---------------------------------------------------------------------------
GLOBS=(
  '*.tfvars'
  '*.pem'
  '*.key'
  '*.p12'
  'id_rsa*'
  '*credentials*.json'
  'service-account*.json'
  '*.tfstate'
)

tracked=$(git ls-files "${MODE_ARGS[@]+"${MODE_ARGS[@]}"}" 2>/dev/null || git ls-files)
for g in "${GLOBS[@]}"; do
  matches=""
  while IFS= read -r f; do
    [[ -z "$f" ]] && continue
    case "$(basename "$f")" in
      $g) matches="$matches$f"$'\n' ;;
    esac
  done <<< "$tracked"
  if [[ -n "$matches" ]]; then
    problem "Secret-shaped file tracked: $g"
    printf '%s' "$matches" | sed 's/^/  /'
  fi
done

# .env* is tracked at all except .env.example
env_matches=$(printf '%s\n' "$tracked" | grep -E '(^|/)\.env(\..+)?$' | grep -v '\.env\.example$') || env_matches=""
if [[ -n "$env_matches" ]]; then
  problem "Tracked .env file (only .env.example is allowed)"
  printf '%s\n' "$env_matches" | sed 's/^/  /'
fi

# ---------------------------------------------------------------------------
# Forbidden content patterns. The secret-shaped ones are post-filtered to drop
# doc placeholders written as a run of repeated x/X (e.g. `whsec_xxxxxxx...`
# in README.md) — a real generated secret never repeats one character; a
# doc example commonly does. Narrow further here if a new placeholder style
# false-positives; do not delete the pattern.
# ---------------------------------------------------------------------------
PATTERNS=(
  '-----BEGIN [A-Z ]*PRIVATE KEY|Embedded private key'
  'arn:aws:[a-z0-9-]+:[a-z0-9-]*:[0-9]{12}:|AWS account-ID-shaped ARN'
  'AKIA[0-9A-Z]{16}|AWS access key ID'
  'sk_live_[0-9A-Za-z]+|Stripe live secret key'
  'whsec_[0-9A-Za-z]{20,}|Stripe webhook signing secret'
  'ghp_[0-9A-Za-z]{30,}|GitHub personal access token'
  'sk-ant-[0-9A-Za-z_-]{20,}|Anthropic API key'
  'xox[bpars]-[0-9A-Za-z-]{10,}|Slack token'
  'claude\.ai/|AI attribution — session URL'
  'Claude-Session|AI attribution — session identifier'
  'Generated with Claude|AI attribution footer'
  'Co-Authored-By: *Claude|AI attribution co-author trailer'
  'noreply@anthropic\.com|AI attribution email'
  '/Users/[a-z][a-zA-Z0-9_.-]*|Absolute macOS home path'
  '/home/[a-z][a-zA-Z0-9_.-]*|Absolute Linux home path'
)

# Skill docs legitimately name these forbidden strings as literal examples of
# what a contributor/agent must not write (see .agents/skills/gh-pr/SKILL.md).
# Exclude skill instruction docs from the content-pattern loop rather than
# the whole file tree, so an actual leak inside a skill's generated output
# would still be caught elsewhere.
CONTENT_EXCLUDES=(
  ':(exclude)scripts/check-public-hygiene.sh'
  ':(exclude).gitleaks.toml'
  ':(exclude,glob).agents/skills/**/SKILL.md'
)

for entry in "${PATTERNS[@]}"; do
  pattern="${entry%%|*}"
  reason="${entry#*|}"
  hits=$(git grep -InE "${MODE_ARGS[@]+"${MODE_ARGS[@]}"}" -e "$pattern" -- \
           "${CONTENT_EXCLUDES[@]}" 2>/dev/null) || hits=""
  # Drop doc-placeholder matches shaped like `..._xxxxxxxxxx` (10+ repeated
  # x/X right after the prefix) — see comment above.
  hits=$(printf '%s\n' "$hits" | grep -Ev '_[xX]{10,}') || hits=""
  if [[ -n "$hits" ]]; then
    problem "$reason"
    printf '%s\n' "$hits" | head -15 | sed 's/^/  /'
    total=$(printf '%s\n' "$hits" | wc -l | tr -d ' ')
    (( total > 15 )) && printf '  … and %d more\n' "$(( total - 15 ))"
  fi
done

# ---------------------------------------------------------------------------
# gitleaks: catches shaped-secret patterns this list doesn't enumerate.
#
# Scans an archive of the tracked tree, not the working directory directly —
# `gitleaks dir .` walks the raw filesystem and does not know about nested
# git worktrees (e.g. .claude/worktrees/*, gitignored but still present on
# disk for local agent sessions), which multiplies findings and scans content
# outside this check's scope. `git archive` naturally scopes to exactly what
# `git ls-files`/`git grep` above already check.
# ---------------------------------------------------------------------------
if command -v gitleaks >/dev/null 2>&1; then
  echo "running gitleaks..."
  archive_dir=$(mktemp -d) || { echo "mktemp -d failed" >&2; exit 1; }
  trap 'rm -rf "$archive_dir"' EXIT
  if [[ "${MODE_ARGS[*]:-}" == "--cached" ]]; then
    tree=$(git write-tree)
  else
    tree=HEAD
  fi
  git archive "$tree" | tar -x -C "$archive_dir"
  if ! gitleaks dir --config .gitleaks.toml --no-banner "$archive_dir"; then
    problem "gitleaks found a likely secret"
  fi
else
  echo "⚠️  gitleaks not installed locally — CI still runs it; skipping here" >&2
fi

if (( fail )); then
  cat <<'MSG'

──────────────────────────────────────────────────────────────────────────
This check guards the public/private boundary (Layer A, generic patterns
only — a private-name denylist runs separately and does not ship in this
repo).

If this is a genuine false positive, narrow the pattern and explain why in
the same commit. Do not delete a check to make it pass.
──────────────────────────────────────────────────────────────────────────
MSG
  exit 1
fi

echo "✅ public hygiene (Layer A): clean"
