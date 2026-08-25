#!/usr/bin/env bash
# Verifies .agents/skills/ and .claude/skills/ stay in sync per the source-of-
# truth pattern documented in AGENTS.md: every .agents/skills/<name> must have
# a .claude/skills/<name> that is a relative directory symlink into it (not a
# symlink straight to SKILL.md — .claude/skills/<name> itself is the link, and
# <name>/SKILL.md must exist through it), and the CLAUDE.md / GEMINI.md
# symlinks to AGENTS.md must exist and resolve to exactly that target. Checks
# both directions: every .agents/skills/ entry must be exposed, and every
# .claude/skills/ entry must trace back to one (or be in TEMPORARY_SKILLS
# below) — a renamed or deleted skill that leaves a stale .claude/skills/
# entry behind is exactly the kind of drift this script exists to catch.
set -euo pipefail

cd "$(dirname "$0")/.."

# Skills that are deliberately real (non-symlinked) directories under
# .claude/skills/ with no .agents/skills/ counterpart, because they are
# temporary and scoped to disappear on their own timeline rather than being
# promoted to the vendor-neutral tree. Update this alongside the removal note
# in each skill's own SKILL.md / Makefile comment when one goes away.
TEMPORARY_SKILLS="ci-local"

fail=0

is_temporary_skill() {
  case " $TEMPORARY_SKILLS " in
    *" $1 "*) return 0 ;;
    *) return 1 ;;
  esac
}

shopt -s nullglob
skill_dirs=(.agents/skills/*/)
shopt -u nullglob

if [ ${#skill_dirs[@]} -eq 0 ]; then
  echo "❌ no skills found under .agents/skills/ — expected at least one"
  fail=1
else
  # Guard the loop on the array being non-empty, not just nullglob: bash 3.2
  # (macOS's default) treats "${arr[@]}" on a genuinely empty array as an
  # unbound-variable error under set -u, even with nullglob already applied.
  for src in "${skill_dirs[@]}"; do
    name=$(basename "$src")
    link=".claude/skills/$name"
    expected="../../.agents/skills/$name"

    if [ ! -e "$link" ] && [ ! -L "$link" ]; then
      echo "❌ missing $link (no symlink for .agents/skills/$name)"
      fail=1
      continue
    fi

    if [ ! -L "$link" ]; then
      echo "❌ $link exists but is not a symlink (should link into .agents/skills/$name)"
      fail=1
      continue
    fi

    actual=$(readlink "$link")
    if [ "$actual" != "$expected" ]; then
      echo "❌ $link -> $actual, expected relative target $expected"
      fail=1
      continue
    fi

    if [ ! -f "$link/SKILL.md" ]; then
      echo "❌ $link is a symlink but does not resolve to a SKILL.md"
      fail=1
    fi
  done
fi

# Reverse pass: every .claude/skills/ entry must trace back to a real
# .agents/skills/ source (already checked above) or be a documented
# temporary exception. Catches stale symlinks left behind by a rename or
# delete under .agents/skills/, which the forward pass above can't see.
for link in .claude/skills/*; do
  # -e follows symlinks and is false for a broken one, which is exactly the
  # case this pass needs to catch — use -e or -L, not -e alone.
  [ -e "$link" ] || [ -L "$link" ] || continue
  name=$(basename "$link")

  if [ -d ".agents/skills/$name" ]; then
    continue
  fi

  if is_temporary_skill "$name"; then
    if [ -L "$link" ]; then
      echo "❌ .claude/skills/$name is in TEMPORARY_SKILLS but is a symlink, not a real directory"
      fail=1
    fi
    continue
  fi

  echo "❌ .claude/skills/$name has no matching .agents/skills/$name and is not in TEMPORARY_SKILLS (stale entry from a rename/delete, or an undeclared exception)"
  fail=1
done

for f in CLAUDE.md GEMINI.md; do
  if [ ! -L "$f" ]; then
    echo "❌ $f is missing or not a symlink (should link to AGENTS.md)"
    fail=1
    continue
  fi

  actual=$(readlink "$f")
  if [ "$actual" != "AGENTS.md" ]; then
    echo "❌ $f -> $actual, expected AGENTS.md"
    fail=1
    continue
  fi

  if [ ! -f "$f" ]; then
    echo "❌ $f is a broken symlink"
    fail=1
  fi
done

if [ "$fail" -eq 0 ]; then
  echo "✅ skills tree and AGENTS.md symlinks are in sync"
fi

exit "$fail"
