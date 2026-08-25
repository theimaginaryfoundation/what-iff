---
name: gh-run-watch
description: >-
  Wait for a GitHub Actions run to finish and report the outcome, using only
  `gh api` polling with backoff so a long build costs a handful of tiny
  requests instead of a live stream. Use when a push, PR, or workflow dispatch
  has kicked off CI and the next step depends on the result — "did the build
  pass?", "wait for CI", "watch the run", "is the workflow done yet?", "check
  on that build" — and when a run has failed and you need the failing job,
  step, and error lines without downloading whole logs. Prefer this over
  `gh run watch`, which holds a connection open and streams output you did not
  ask for.
---

# Watching a GitHub Actions run cheaply

`gh run watch` opens a long-lived connection and streams progress. That is
expensive in an agent context: the output is large, mostly noise, and it
cannot be interrupted cleanly to do something else. This skill polls instead —
a handful of small `gh api` calls, each returning a few bytes.

**Rules of thumb**

- Ask for the smallest thing that answers the question. `--jq` runs
  server-side output through a filter locally, but the field selection is what
  keeps the response small; never fetch a whole run object to read one field.
- Poll on a curve, not a constant. CI jobs take minutes, so a 5-second poll is
  ~100 wasted requests. Start at 20s, grow to 60s.
- Only reach for logs **after** a failure, and only for the job that failed.
- Never poll without a ceiling. An infinite `until` loop against a hung run
  will spin forever.

## 1. Find the run

Prefer the exact commit — branch lookups race with new pushes and can return a
run for a commit you did not make.

```bash
SHA=$(git rev-parse HEAD)
REPO=$(gh repo view --json nameWithOwner --jq .nameWithOwner)
RID=$(gh api "repos/$REPO/actions/runs?head_sha=$SHA&per_page=1" --jq '.workflow_runs[0].id')
```

Narrow to one workflow when several trigger on the same commit:

```bash
gh api "repos/$REPO/actions/runs?head_sha=$SHA&per_page=20" \
  --jq '.workflow_runs[] | select(.name == "E2E (mock backend)") | .id' | head -1
```

A freshly pushed commit may have no run for a few seconds. Poll for the run's
*existence* the same way you poll for its completion, with the same ceiling.

## 2. Wait for it

One field per request, backing off, bounded:

```bash
DEADLINE=$(( $(date +%s) + 1800 ))   # 30 min ceiling — raise for slow suites
SLEEP=20
while :; do
  STATUS=$(gh api "repos/$REPO/actions/runs/$RID" --jq '.status')
  [ "$STATUS" = "completed" ] && break
  [ "$(date +%s)" -ge "$DEADLINE" ] && { echo "timed out waiting for $RID"; break; }
  sleep "$SLEEP"
  [ "$SLEEP" -lt 60 ] && SLEEP=$(( SLEEP + 10 ))
done
gh api "repos/$REPO/actions/runs/$RID" --jq '.conclusion'
```

That is roughly 20 requests for a 15-minute build, each a few hundred bytes.

In Claude Code, run the loop with `run_in_background: true` rather than
blocking a foreground call, and check back on it — a foreground `sleep` is
blocked in that harness anyway.

## 3. Report the outcome

Per-job status, one request:

```bash
gh api "repos/$REPO/actions/runs/$RID/jobs" \
  --jq '.jobs[] | "\(.name): \(.conclusion // .status)"'
```

Wall-clock per job, when you care about which shard is the long pole:

```bash
gh api "repos/$REPO/actions/runs/$RID/jobs" \
  --jq '.jobs[] | "\(.name): \(((.completed_at // now | fromdateiso8601?) // 0) - (.started_at | fromdateiso8601))s"'
```

## 4. On failure, narrow before you read

Get the failed **step**, not the whole log:

```bash
gh api "repos/$REPO/actions/runs/$RID/jobs" --jq '
  .jobs[] | select(.conclusion == "failure")
  | "\(.name) → \(.steps[] | select(.conclusion == "failure") | .name)"'
```

Then pull only that job's log and filter it. Job logs can be megabytes; never
pipe one into context unfiltered.

```bash
JOB=$(gh api "repos/$REPO/actions/runs/$RID/jobs" \
  --jq '[.jobs[] | select(.conclusion == "failure")][0].id')
gh api "repos/$REPO/actions/jobs/$JOB/logs" 2>/dev/null \
  | grep -iE "error|failed|✘|❌|panic|exception" | head -30
```

`gh run view --log-failed` is an acceptable fallback, but it fetches every
failed job's full log — always pipe it through `grep`/`head`.

## 5. Playwright job failed? Pull its trace before reproducing anything

For an `E2E (mock backend)` or `E2E (local LLM backend)` job specifically,
grep'd log lines are rarely enough to tell a genuine regression from a slow
LLM reply or flaky selector — the log doesn't show what the page actually
looked like. **Do not start the app locally and re-run the spec to find
out.** Both workflows upload a Playwright HTML report (with embedded
trace/video/screenshot for every failed test) as a build artifact; pulling
that is faster than standing up the DB/API/model locally, and it shows you
the exact CI run instead of a fresh local attempt that may not even fail the
same way.

```bash
gh run download "$RID" -n playwright-report            # e2e-mock.yml
gh run download "$RID" -n playwright-report-local-llm  # e2e-local-llm.yml
npx playwright show-report ./playwright-report          # from web/app
```

Or, to inspect one failing test's trace directly without the full report UI:

```bash
npx playwright show-trace ./playwright-report/data/<hash>.zip
```

Only fall back to a local repro if the downloaded artifact is missing or
empty (`if-no-files-found: ignore` on the upload step means an artifact-
pipeline bug can silently produce nothing — that is itself the finding, not
a reason to route around it and reproduce locally instead).

## Gotchas

- **`status` vs `conclusion`.** `status` is the lifecycle
  (`queued`/`in_progress`/`completed`); `conclusion` is the result
  (`success`/`failure`/`cancelled`/`skipped`) and is **null until completed**.
  Branching on `conclusion` too early reads null as "not failed".
- **A green run can still be wrong.** `success` means the steps exited zero,
  not that they did what you intended — check that the run did the work you
  expected (right matrix, right test count) before reporting success.
- **`re-run` keeps the same run id** but resets `status`. Re-reading a cached
  conclusion after a re-run reports the stale result.
- **Rate limits.** Authenticated REST is 5,000 requests/hour; the backoff above
  keeps a watch far below that, but a tight loop across several runs will not.
- **`?branch=` returns runs for the branch's latest pushes**, so it can hand
  you a newer run than the commit you care about. Prefer `head_sha`.
