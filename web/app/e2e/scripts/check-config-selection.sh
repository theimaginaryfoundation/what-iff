#!/usr/bin/env bash
#
# Guards what each config actually SELECTS, which no other check covers.
#
# Two shipped bugs motivated this, both invisible to lint and to a green test
# run because a config that selects the wrong set still exits 0:
#
#   - `serialCompanion` applied its `@serial` grep config-wide, filtering out
#     the untagged `setup` project, so serial runs of a config with a login
#     setup project reused whatever stale storage state was on disk instead of
#     authenticating.
#   - Stripping `@serial` from a sole `grepInvert` produced `/(?:)/`, an empty
#     regex that matches everything, so that serial companion excluded the
#     entire suite and reported success.
#
# Asserts invariants rather than absolute counts, so adding tests does not
# require editing this file.
set -uo pipefail
cd "$(dirname "$0")/../.." || exit 1

fail=0
note() { printf '  %-58s %s\n' "$1" "$2"; }
bad() { note "$1" "FAIL — $2"; fail=1; }

# Prints the test count for a config, or "unavailable" when the config refuses
# to load (a config that requires environment variables throws at load time
# without them, which is intended behaviour, not a failure of this check).
selection() {
  local out
  if ! out=$(npx playwright test --config "e2e/playwright.config.$1.ts" --list 2>&1); then
    echo "unavailable"
    return
  fi
  echo "$out" | sed -n 's/^Total: \([0-9]*\) tests.*/\1/p' | tail -1
}

echo "Config selection invariants:"

# No config may select zero tests. A zero selection is how both bugs above
# presented: a filter that excluded everything, reported as a clean run.
for config in mock-llm mock-llm.serial mock-llm.visual mock-llm.api local-llm local-llm.serial; do
  count=$(selection "$config")
  case "$count" in
    unavailable) note "$config selects tests" "skipped (config not loadable here)" ;;
    0) bad "$config selects tests" "selected 0 tests" ;;
    *) note "$config selects tests" "ok ($count)" ;;
  esac
done

[ "$fail" -eq 0 ] && echo "All config selection invariants hold." || echo "Config selection invariants FAILED."
exit "$fail"
