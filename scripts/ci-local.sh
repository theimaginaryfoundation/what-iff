#!/usr/bin/env bash
# ci-local.sh — run the PR CI checks on a developer machine and (optionally)
# post the results as one sticky comment on the branch's pull request.
#
# TEMPORARY. This exists because the repository is short on GitHub Actions
# minutes: it replays what .github/workflows/{pr-validation,
# frontend-pr-validation,e2e-mock}.yml run on a PR, step for step, by calling
# the same `make` targets and npm scripts those workflows call, so nothing
# here defines a check of its own. When minutes stop being a constraint this
# script, its Makefile targets (`ci-local`, `ci-local-post`) and the
# .claude/skills/ci-local skill can be deleted together.
#
# What runs (each step is one CI step; names match the workflow step names):
#
#   go         pr-validation.yml / validate:  generate, fmt, vet, tidy,
#              check-no-local-models, test-ci (race + coverage), build.
#              Also runs skills-check.yml's check-skill-symlinks — a separate
#              workflow, not part of pr-validation.yml, but fast enough to
#              fold into this stage rather than add a stage of its own.
#   mock-e2e   pr-validation.yml / mock-e2e:  make mock-e2e with coverage
#              (needs docker; the script brings up its own Postgres)
#   frontend   frontend-pr-validation.yml:    npm ci, sdk drift, lint:e2e,
#              e2e:check-configs, tsc, web-unit-coverage, build:prod
#   e2e        e2e-mock.yml (opt-in, --e2e): isolated Postgres + mock-LLM API
#              on :8080, Playwright browser suite (chromium-desktop, unsharded)
#              and the API suite. Visual specs are NOT run — their baselines
#              only match the CI container's renderer (see e2e-mock.yml).
#
# Not replayed: Codecov uploads and the codecov.yml validation (network),
# and the label-gated real-provider / local-model jobs.
#
# Usage:
#   scripts/ci-local.sh [--only a,b] [--skip a,b] [--e2e] [--post [--pr N]]
#                       [--post-only] [--dry-run] [--verbose]
#
#   --only LIST   run only these stages (go, mock-e2e, frontend, e2e)
#   --skip LIST   skip these stages
#   --e2e         include the browser/API Playwright stage (off by default)
#   --post        after running, post/update the report on the PR for the
#                 current branch (or --pr N). Uses `gh`; skipped on dirty
#                 trees or unpushed HEADs unless --force-post.
#   --post-only   don't run anything; post the last report
#   --dry-run     with --post: print the comment instead of sending it
#   --verbose     stream every step's output (default: one line per step,
#                 full logs under .dev/ci-local/logs/)
#
# Output: .dev/ci-local/report.md plus per-step logs. Exit status is non-zero
# if any step failed.
set -uo pipefail

cd "$(dirname "$0")/.."

OUT_DIR=".dev/ci-local"
LOG_DIR="$OUT_DIR/logs"
REPORT="$OUT_DIR/report.md"
WEB_DIR="web/app"
MARKER="<!-- ci-local-report -->"

ONLY=""
SKIP=""
WITH_E2E=0
POST=0
POST_ONLY=0
DRY_RUN=0
FORCE_POST=0
VERBOSE=0
PR_NUMBER=""

while [ $# -gt 0 ]; do
  case "$1" in
    --only) ONLY="$2"; shift 2 ;;
    --only=*) ONLY="${1#*=}"; shift ;;
    --skip) SKIP="$2"; shift 2 ;;
    --skip=*) SKIP="${1#*=}"; shift ;;
    --e2e) WITH_E2E=1; shift ;;
    --post) POST=1; shift ;;
    --post-only) POST=1; POST_ONLY=1; shift ;;
    --pr) PR_NUMBER="$2"; shift 2 ;;
    --pr=*) PR_NUMBER="${1#*=}"; shift ;;
    --dry-run) DRY_RUN=1; shift ;;
    --force-post) FORCE_POST=1; shift ;;
    --verbose|-v) VERBOSE=1; shift ;;
    -h|--help) sed -n '2,50p' "$0" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1 (see --help)" >&2; exit 2 ;;
  esac
done

# ── Stage selection ──────────────────────────────────────────────────────────
in_list() { case ",$2," in *",$1,"*) return 0 ;; esac; return 1; }

stage_enabled() {
  local s="$1"
  if [ -n "$ONLY" ]; then in_list "$s" "$ONLY" || return 1; fi
  if [ -n "$SKIP" ] && in_list "$s" "$SKIP"; then return 1; fi
  if [ "$s" = "e2e" ] && [ "$WITH_E2E" -eq 0 ] && ! { [ -n "$ONLY" ] && in_list e2e "$ONLY"; }; then
    return 1
  fi
  return 0
}

# ── Step bookkeeping (bash 3.2 compatible: no associative arrays) ────────────
# Each result is one line: stage|step|status|seconds|logfile
RESULTS=()
FAILED=0
STAGE_ABORTED=""   # set when a stage's prerequisite step failed

now() { date +%s; }

record() { RESULTS+=("$1|$2|$3|$4|$5"); }

# run_step STAGE STEP DIR COMMAND...
# Runs COMMAND (a shell string) in DIR, captures output to a log, records the
# result. Returns the command's exit status.
run_step() {
  local stage="$1" step="$2" dir="$3" cmd="$4"
  local log="$LOG_DIR/${stage}-${step}.log"
  local start status secs

  if [ "$STAGE_ABORTED" = "$stage" ]; then
    record "$stage" "$step" skipped 0 "$log"
    printf '  ⏭  %-28s skipped (earlier step in this stage failed)\n' "$step"
    return 0
  fi

  printf '  ▶  %-28s ' "$step"
  start=$(now)
  if [ "$VERBOSE" -eq 1 ]; then
    echo
    (cd "$dir" && bash -o pipefail -c "$cmd") 2>&1 | tee "$log"
    status=${PIPESTATUS[0]}
  else
    (cd "$dir" && bash -o pipefail -c "$cmd") > "$log" 2>&1
    status=$?
  fi
  secs=$(( $(now) - start ))

  if [ "$status" -eq 0 ]; then
    record "$stage" "$step" pass "$secs" "$log"
    printf '✅ %ss\n' "$secs"
  else
    record "$stage" "$step" fail "$secs" "$log"
    FAILED=1
    printf '❌ %ss (exit %s) — %s\n' "$secs" "$status" "$log"
  fi
  return "$status"
}

# Like run_step, but a failure skips the rest of the stage (e.g. npm ci).
run_prereq() {
  run_step "$@" || STAGE_ABORTED="$1"
}

stage_header() { echo; echo "── $1"; }

# ── Stages ───────────────────────────────────────────────────────────────────
stage_go() {
  stage_header "go (pr-validation.yml: Validate Go Code)"
  run_prereq go generate . "make generate"
  run_step go fmt . "make fmt"
  run_step go vet . "make vet"
  run_step go tidy . "make tidy"
  run_step go check-no-local-models . "make check-no-local-models"
  run_step go check-skill-symlinks . "make check-skill-symlinks"
  run_step go test-ci . "make test-ci && make coverage-summary PROFILE=coverage/go-unit.out"
  run_step go build . "make build"
}

stage_mock_e2e() {
  stage_header "mock-e2e (pr-validation.yml: Mock E2E)"
  run_step mock-e2e mock-e2e . \
    "MOCK_E2E_COVERAGE=1 make mock-e2e && make coverage-summary PROFILE=coverage/go-mock-e2e.out"
}

stage_frontend() {
  stage_header "frontend (frontend-pr-validation.yml: Validate Frontend Code)"
  run_prereq frontend npm-ci "$WEB_DIR" "npm ci"
  run_step frontend sdk-schema-drift "$WEB_DIR" \
    "npm run sdk:generate && git diff --exit-code -- e2e/sdk/schema.d.ts"
  run_step frontend lint-e2e "$WEB_DIR" "npm run lint:e2e"
  run_step frontend e2e-check-configs "$WEB_DIR" "npm run e2e:check-configs"
  run_step frontend tsc "$WEB_DIR" "npx tsc --noEmit -p tsconfig.app.json"
  run_step frontend web-unit-coverage . \
    "make web-unit-coverage && make lcov-summary LCOV=$WEB_DIR/coverage/what-iff/lcov.info"
  run_step frontend build-prod "$WEB_DIR" "npm run build:prod"
}

# Browser + API Playwright suites against a mock-LLM API. Mirrors the `test`
# and `api` jobs of e2e-mock.yml: isolated Postgres (own compose project,
# random port), API launched with the same env the workflow uses, on :8080
# because the Angular app's environment.ts hardcodes that API URL.
E2E_PROJECT=""
E2E_WORK=""
E2E_API_PID=""

e2e_cleanup() {
  set +e
  if [ -n "$E2E_API_PID" ] && kill -0 "$E2E_API_PID" 2>/dev/null; then
    kill "$E2E_API_PID" 2>/dev/null
    wait "$E2E_API_PID" 2>/dev/null
  fi
  if [ -n "$E2E_PROJECT" ]; then
    docker compose -p "$E2E_PROJECT" down -v --remove-orphans >/dev/null 2>&1
  fi
  if [ -n "$E2E_WORK" ] && [ -f "$E2E_WORK/api.log" ]; then
    cp "$E2E_WORK/api.log" "$LOG_DIR/e2e-api-server.log"
  fi
  [ -n "$E2E_WORK" ] && rm -rf "$E2E_WORK"
  E2E_API_PID=""; E2E_PROJECT=""; E2E_WORK=""
}

stage_e2e() {
  stage_header "e2e (e2e-mock.yml: browser shard + api suite, chromium-desktop only)"
  trap e2e_cleanup EXIT

  run_prereq e2e port-8080-free . \
    "if (exec 3<>/dev/tcp/127.0.0.1/8080) 2>/dev/null; then echo 'port 8080 is in use — stop the API (make dev-down) first'; exit 1; fi; echo 'port 8080 free'"
  [ "$STAGE_ABORTED" = e2e ] && return

  E2E_WORK="$(mktemp -d)"
  E2E_PROJECT="chatapp-ci-local-e2e-$$"
  local db_pass db_port
  db_pass="$(openssl rand -hex 16)"

  run_prereq e2e postgres-up . \
    "CHATAPP_DB_USER=chatapp CHATAPP_DB_PASSWORD=$db_pass CHATAPP_DB_NAME=chatapp CHATAPP_DB_EXPOSED_PORT=0 docker compose -p $E2E_PROJECT up -d --wait db"
  [ "$STAGE_ABORTED" = e2e ] && return
  db_port="$(docker compose -p "$E2E_PROJECT" port db 5432 | awk -F: '{print $NF}')"

  run_prereq e2e build-api . "go build -o $E2E_WORK/api-server ./cmd/api-server"
  [ "$STAGE_ABORTED" = e2e ] && return

  mkdir -p "$E2E_WORK/files"
  ENV=test LLM_BACKEND=mock MOCK_LLM_MODE=echo MOCK_LLM_STREAM_DELAY_MS=150 \
  SERVER_HOST=127.0.0.1 SERVER_PORT=8080 \
  DB_TYPE=postgres DB_HOST=127.0.0.1 DB_PORT="$db_port" \
  DB_USER=chatapp DB_PASSWORD="$db_pass" DB_NAME=chatapp \
  DB_SSL_MODE=disable \
  AUTO_MIGRATE=true \
  JWT_SECRET="$(openssl rand -hex 32)" \
  JWT_REFRESH_SECRET="$(openssl rand -hex 32)" \
  TOKEN_ENCRYPTION_SECRET="$(openssl rand -hex 32)" \
  OPENAI_API_KEY=dummy-mock-key \
  LOCAL_FILE_STORE_DIR="$E2E_WORK/files" \
    "$E2E_WORK/api-server" -env /dev/null > "$E2E_WORK/api.log" 2>&1 &
  E2E_API_PID=$!

  run_prereq e2e api-healthy . \
    "for i in \$(seq 1 60); do curl -sf http://127.0.0.1:8080/api/health >/dev/null && { echo \"API healthy after \${i}s\"; exit 0; }; kill -0 $E2E_API_PID 2>/dev/null || { echo 'API exited'; tail -50 $E2E_WORK/api.log; exit 1; }; sleep 1; done; echo 'API did not become healthy'; tail -50 $E2E_WORK/api.log; exit 1"
  [ "$STAGE_ABORTED" = e2e ] && return

  # Playwright starts the Angular dev server itself (webServer in the config).
  run_step e2e playwright-browser "$WEB_DIR" \
    "PLAYWRIGHT_JSON_OUTPUT_NAME=$PWD/$OUT_DIR/e2e-browser.json npx playwright test --config e2e/playwright.config.mock-llm.ts --project=chromium-desktop --reporter=list,json"
  run_step e2e playwright-api "$WEB_DIR" \
    "PLAYWRIGHT_JSON_OUTPUT_NAME=$PWD/$OUT_DIR/e2e-api.json E2E_API_BASE_URL=http://127.0.0.1:8080/api npx playwright test --config e2e/playwright.config.mock-llm.api.ts --reporter=list,json"

  e2e_cleanup
  trap - EXIT
}

# ── Report ───────────────────────────────────────────────────────────────────
# Scrub paths that would leak a username into a public PR comment.
scrub() { sed -e "s|$PWD|.|g" -e "s|$HOME|~|g"; }

playwright_summary() {
  # $1: results json. Prints a one-line stats summary, or nothing.
  [ -f "$1" ] || return 0
  jq -r '.stats | "\(.expected) passed, \(.unexpected) failed, \(.flaky) flaky, \(.skipped) skipped — \((.duration/1000)|floor)s"' "$1" 2>/dev/null
}

playwright_failures() {
  [ -f "$1" ] || return 0
  jq -r '[.. | objects | select(has("ok") and has("title") and has("tests")) | select(.ok == false) | .title] | .[]' "$1" 2>/dev/null | sed 's/^/- /'
}

write_report() {
  local sha branch dirty pass_n fail_n skip_n total_secs=0 r
  sha="$(git rev-parse --short HEAD)"
  branch="$(git branch --show-current)"
  dirty="$(git status --porcelain | wc -l | tr -d ' ')"
  pass_n=0; fail_n=0; skip_n=0
  for r in ${RESULTS[@]+"${RESULTS[@]}"}; do
    IFS='|' read -r _ _ st secs _ <<< "$r"
    case "$st" in pass) pass_n=$((pass_n+1));; fail) fail_n=$((fail_n+1));; skipped) skip_n=$((skip_n+1));; esac
    total_secs=$((total_secs + secs))
  done

  mkdir -p "$OUT_DIR"
  {
    echo "$MARKER"
    echo "### Local CI run (Actions minutes conserved)"
    echo
    if [ "$fail_n" -eq 0 ]; then
      echo "✅ **All $pass_n checks passed**, $skip_n skipped — $((total_secs/60))m$((total_secs%60))s total"
    else
      echo "❌ **$fail_n failed**, $pass_n passed, $skip_n skipped — $((total_secs/60))m$((total_secs%60))s total"
    fi
    echo
    echo "Commit \`$sha\` on \`$branch\`"
    if [ "$dirty" != "0" ]; then
      echo "⚠️ run on a working tree with **$dirty uncommitted change(s)** — results may not match the pushed commit."
    fi
    echo
    echo "| Stage | Step | Result | Time |"
    echo "|---|---|---|---|"
    for r in ${RESULTS[@]+"${RESULTS[@]}"}; do
      IFS='|' read -r stage step st secs _ <<< "$r"
      case "$st" in pass) icon="✅";; fail) icon="❌";; *) icon="⏭";; esac
      echo "| $stage | \`$step\` | $icon $st | ${secs}s |"
    done
    echo

    # Coverage totals, as the workflows print them to the step summary.
    local cov=""
    # Only for stages that ran this time — the files persist, and a stale
    # total from an earlier run would read as if it came from this one.
    stage_enabled go && [ -f coverage/go-unit.out ] && cov="$cov$(make -s coverage-summary PROFILE=coverage/go-unit.out 2>/dev/null)"$'\n'
    stage_enabled mock-e2e && [ -f coverage/go-mock-e2e.out ] && cov="$cov$(make -s coverage-summary PROFILE=coverage/go-mock-e2e.out 2>/dev/null)"$'\n'
    stage_enabled frontend && [ -f "$WEB_DIR/coverage/what-iff/lcov.info" ] && cov="$cov$(make -s lcov-summary LCOV=$WEB_DIR/coverage/what-iff/lcov.info 2>/dev/null)"$'\n'
    if [ -n "$cov" ]; then
      echo "**Coverage**"
      echo '```'
      printf '%s' "$cov"
      echo '```'
      echo
    fi

    local pw
    for pw in browser api; do
      if [ -f "$OUT_DIR/e2e-$pw.json" ]; then
        echo "**Playwright ($pw):** $(playwright_summary "$OUT_DIR/e2e-$pw.json")"
        local fails; fails="$(playwright_failures "$OUT_DIR/e2e-$pw.json")"
        if [ -n "$fails" ]; then
          echo
          echo "<details><summary>Failed tests ($pw)</summary>"; echo; echo "$fails"; echo; echo "</details>"
        fi
        echo
      fi
    done

    for r in ${RESULTS[@]+"${RESULTS[@]}"}; do
      IFS='|' read -r stage step st _ log <<< "$r"
      [ "$st" = fail ] || continue
      echo "<details><summary>❌ $stage / $step — last 60 lines</summary>"
      echo; echo '```'
      tail -n 60 "$log" | scrub | sed 's/```/` ` `/g'
      echo '```'; echo "</details>"; echo
    done

    echo "<sub>Not a GitHub Actions run. Stages: $(stages_run_list). Not replayed: Codecov upload, codecov.yml validation, visual specs, label-gated real-provider jobs. "
    echo "$(uname -s)/$(uname -m), $(go version | awk '{print $3}'), node $(node --version 2>/dev/null || echo n/a). Generated by \`make ci-local\`.</sub>"
  } > "$REPORT"
}

stages_run_list() {
  local s out=""
  for s in go mock-e2e frontend e2e; do stage_enabled "$s" && out="$out${out:+, }$s"; done
  echo "${out:-none}"
}

# ── Posting ──────────────────────────────────────────────────────────────────
post_report() {
  [ -f "$REPORT" ] || { echo "❌ no report at $REPORT — run without --post-only first"; return 1; }
  command -v gh >/dev/null || { echo "❌ gh is required to post"; return 1; }

  local repo pr head_sha local_sha
  repo="$(gh repo view --json nameWithOwner -q .nameWithOwner)" || return 1
  if [ -z "$PR_NUMBER" ]; then
    PR_NUMBER="$(gh pr view --json number -q .number 2>/dev/null)" || true
    [ -n "$PR_NUMBER" ] || { echo "❌ no open PR for branch $(git branch --show-current); pass --pr N"; return 1; }
  fi
  pr="$PR_NUMBER"

  # Refuse to attach local results to a commit that isn't what the PR shows,
  # unless told otherwise — a green comment on the wrong SHA is worse than none.
  head_sha="$(gh pr view "$pr" --json headRefOid -q .headRefOid)"
  local_sha="$(git rev-parse HEAD)"
  if [ "$FORCE_POST" -eq 0 ]; then
    if [ -n "$(git status --porcelain)" ]; then
      echo "❌ working tree is dirty; commit/push first or pass --force-post"; return 1
    fi
    if [ "$head_sha" != "$local_sha" ]; then
      echo "❌ PR #$pr head is ${head_sha:0:7} but local HEAD is ${local_sha:0:7}; push first or pass --force-post"; return 1
    fi
  fi

  if [ "$DRY_RUN" -eq 1 ]; then
    echo "── dry run: would post the following to $repo PR #$pr"
    cat "$REPORT"
    return 0
  fi

  local existing
  existing="$(gh api --paginate "repos/$repo/issues/$pr/comments" \
    --jq "[.[] | select(.body | startswith(\"$MARKER\"))] | last | .id // empty")"
  if [ -n "$existing" ]; then
    gh api -X PATCH "repos/$repo/issues/comments/$existing" -F "body=@$REPORT" >/dev/null \
      && echo "✅ updated comment $existing on $repo PR #$pr"
  else
    gh api -X POST "repos/$repo/issues/$pr/comments" -F "body=@$REPORT" >/dev/null \
      && echo "✅ posted comment on $repo PR #$pr"
  fi
}

# ── Main ─────────────────────────────────────────────────────────────────────
if [ "$POST_ONLY" -eq 1 ]; then
  post_report; exit $?
fi

mkdir -p "$LOG_DIR"
rm -f "$LOG_DIR"/*.log "$OUT_DIR"/e2e-*.json

echo "ci-local: stages → $(stages_run_list)   (logs: $LOG_DIR)"
RUN_START=$(now)

stage_enabled go       && stage_go
stage_enabled mock-e2e && stage_mock_e2e
stage_enabled frontend && stage_frontend
stage_enabled e2e      && stage_e2e

write_report
echo
echo "── report: $REPORT ($(( $(now) - RUN_START ))s wall clock)"
sed -n '2,4p' "$REPORT"

if [ "$POST" -eq 1 ]; then
  echo
  post_report || FAILED=1
fi

exit "$FAILED"
