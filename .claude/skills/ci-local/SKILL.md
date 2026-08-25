---
name: ci-local
description: Replay the PR CI checks locally with one command (make ci-local) and post the aggregated results as a sticky PR comment (make ci-local-post). TEMPORARY — exists because GitHub Actions minutes are scarce. Use when asked to "run CI locally", "check the PR without burning Actions minutes", or "post local CI results to the PR".
---

# ci-local: run the PR CI checks locally, post the results to the PR

**Temporary.** This skill, `scripts/ci-local.sh`, and the `ci-local` /
`ci-local-post` Makefile targets exist only because GitHub Actions minutes
are a constraint. Remove all three together once that stops being true.
The workflows in `.github/workflows/` remain the source of truth for what CI
checks; this script just calls the same `make` targets and `npm` scripts
on the developer machine.

## What it replays

| Stage | CI job | Opt-in? |
| --- | --- | --- |
| `go` | pr-validation.yml → Validate Go Code (generate, fmt, vet, tidy, check-no-local-models, test-ci + coverage total, build); also skills-check.yml's check-skill-symlinks | default |
| `mock-e2e` | pr-validation.yml → Mock E2E (hermetic, coverage) via `scripts/mock-e2e.sh` | default |
| `frontend` | frontend-pr-validation.yml (npm ci, SDK schema drift, lint:e2e, e2e:check-configs, tsc, web-unit-coverage + lcov total, build:prod) | default |
| `e2e` | e2e-mock.yml browser (chromium-desktop only) + API Playwright suites against a real API on :8080 with an isolated Postgres | `--e2e` |

Not replayed: Codecov upload/validation, the visual-regression project
(baselines only match the CI container renderer), other browser shards, and
the label-gated real-provider jobs (`run-integration`, `run-local-model`).
The report footer says so.

## Commands

Run the default stages:

```bash
make ci-local
```

Pass flags through `ARGS`:

```bash
make ci-local ARGS="--only go"
make ci-local ARGS="--skip frontend"
make ci-local ARGS="--e2e"            # adds the Playwright stage (needs docker, port 8080 free)
make ci-local ARGS="--verbose"        # stream step output instead of just logging it
```

Run and post (or update) the sticky PR comment for the current branch's PR:

```bash
make ci-local-post
make ci-local-post ARGS="--pr 123"    # explicit PR number
```

Preview the comment without posting, or re-post the last report without
re-running anything:

```bash
./scripts/ci-local.sh --post --dry-run
./scripts/ci-local.sh --post-only --pr 123
```

## Outputs

- `.dev/ci-local/report.md` — the Markdown that gets posted (gitignored).
- `.dev/ci-local/logs/<stage>-<step>.log` — full output per step.
- `.dev/ci-local/e2e-browser.json` / `e2e-api.json` — Playwright JSON when `--e2e` ran.

Exit status is non-zero when any step failed, so it is safe to chain.

## Workflow for an agent

1. Make sure the work is committed. Posting refuses on a dirty tree or when
   local `HEAD` differs from the PR head (override with `--force-post`, but
   that means the comment describes something other than what the PR shows
   — say so in the comment if you do).
2. `make ci-local` (add `ARGS="--e2e"` when the change touches the frontend
   or the API surface the Playwright suites exercise).
3. If something failed, open the matching log under `.dev/ci-local/logs/`
   — the report only embeds the last 60 lines. Fix, commit, re-run. Use
   `--only <stage>` to iterate on one stage instead of re-running everything.
4. `./scripts/ci-local.sh --post --dry-run` to eyeball the comment, then
   `make ci-local-post` (or `--post-only` if nothing changed since the last
   run). The comment is sticky: the script finds the earlier one by its
   `<!-- ci-local-report -->` marker and updates it in place.

## Things to know

- The first `frontend` run does `npm ci`, which takes a few minutes.
- `mock-e2e` and `e2e` each start their own Postgres via
  `docker compose -p <unique project>` and tear it down afterwards. `e2e`
  also needs port 8080 free because the Angular environment hardcodes
  `http://localhost:8080/api`.
- Paths in embedded logs are scrubbed (`$PWD` → `.`, `$HOME` → `~`) so the
  comment does not leak local usernames into a public repo. Still read the
  dry-run output before posting.
- `go` runs `make generate` first; if it fails, the rest of the stage is
  skipped (same for `npm ci` in `frontend`).
