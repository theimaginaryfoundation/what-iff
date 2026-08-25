# What runs where, and why

A map of the Playwright suite: which config runs a test, against which
backend, with what parallelism, and what CI does with all of it. Read this
when you are deciding where a new test belongs, or when a test passes in one
place and fails in another.

`e2e/README.md` is the how-to (prerequisites, commands, conventions). This
file is the why — the reasoning behind the split, which the README
deliberately does not carry.

## The one-paragraph version

There is one suite of specs and several configs that point it at different
backends. A spec is written once; the config decides whether it runs at all
against a given backend, using **tags**. The local backends (mock LLM, local
LLM) throw away their data and can run anything. A config a downstream
consumer supplies for its own deployment (see the README's "Running against
your own deployment") typically shares one real account and persists
everything, so it runs a deliberately narrower subset. Each backend also has
a **serial companion** config for the handful of tests that cannot share a
browser fleet.

## The two axes

Everything follows from two independent questions.

**1. Which backend is behind the app?**

| Config      | Backend                            | Frontend                            | Data                     |
| ----------- | ---------------------------------- | ----------------------------------- | ------------------------ |
| `mock-llm`  | local API, `LLM_BACKEND=mock`      | started by Playwright (`webServer`) | throwaway local Postgres |
| `local-llm` | local API, real local model server | started by Playwright               | throwaway local Postgres |

The mock backend is the default (`npm run e2e`) because it is the only one
that is both free and deterministic: no provider keys, no token spend, and
canned replies, per ADR 0x018. The local-LLM config exists to prove the same
flows survive a real streaming model. A downstream deployment config adds a
third kind of row: a backend that is **real and persistent**, serving its own
frontend, reached over the real network.

**2. What kind of test is it?** Answered by tags, not by directory. The
directories (`tests/functional`, `tests/journeys`, `tests/visual`,
`tests/a11y`) group tests for humans; the tags decide what actually runs.

## The tags, and what each one protects

| Tag                    | Meaning                                                                             | Excluded from                                                                          |
| ---------------------- | ----------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------- |
| `@visual`              | screenshot comparison                                                               | every config except `mock-llm.visual`                                                   |
| `@mock-only`           | depends on canned mock replies                                                      | `local-llm` and any deployment config                                                   |
| `@needs-cognito-admin` | needs an identity of its own, which only Cognito admin rights can supply and remove | any config whose backend outlives the run                                               |
| `@mutates-account`     | changes the shared account itself (password, profile)                               | any static-account config                                                               |
| `@serial`              | cannot tolerate a parallel worker on the same account                               | a static-account config's **parallel** run; **required** by the serial companions       |
| `@prod-safe`           | explicitly cleared to run against production                                        | a production-facing config runs _only_ these                                            |

Two of these deserve their reasoning spelled out — one because it encodes a
mistake that has already been made, the other because its name was chosen
carefully and the choice is worth preserving:

**`@prod-safe` is opt-in, not opt-out.** A production-facing config uses
`grep: /@prod-safe/`, an allowlist. The shape used to be an exclusion list
(`grepInvert: /…|@no-prod/`), which fails in the most dangerous possible
direction: a new test that forgets the tag gets pointed at production by
default. With an allowlist, forgetting the tag means the test does not run
— visibly wrong, and safe. Any change to a production config has to preserve
this shape; `serialCompanion()` in `playwright.config.base.ts` intersects
with lookaheads rather than overwriting `grep` for exactly this reason.

**`@needs-cognito-admin` is about cleanup, not correctness.** Nothing is wrong
with these tests — they pass on every local backend. Fixtures clean up after
themselves — `testUser` deletes its account, `seed` deletes what
it created — but these specs create an account of their own that no fixture
owns. On a local backend that is fine; the database is thrown away. On a
backend that outlives the run it leaks permanently: with a Cognito-backed
deployment the identity lands in a real user pool, addressed to an
undeliverable `@example.test` address so it can never even be confirmed, and
nothing in this suite can remove it.

The tag names the **missing capability, not the symptom**, and that is
deliberate. It answers the question a reader actually has — "why is this
skipped against a deployment?" — in the tag itself, and it gives the tag an
end: when a suite can create and delete real identities in its deployment's
user pool, the tag stops being true and is deleted outright, along with the
exclusion in that deployment's config. A tag that named the symptom instead
(`@self-registers`, which this used to be) would still be true afterwards and
would linger as a label that no longer explains anything.

**When writing a new test, the question to ask is not literally "does this need
Cognito admin?"** — it is _"does this create or modify an account, rather than
using the one `testUser` hands me?"_ If yes, it needs an identity a deployment
config cannot supply on its own, and it carries this tag. Everything that seeds
threads, personalities or memories through the API and leaves the account
itself alone does not.

## Why a deployment config is different

One fact drives all of it: **against a deployment with a static test account,
every worker signs in as the same account.** There is no per-test account,
because creating and deleting real identities per test is slow, is often
impossible without admin rights on the identity provider, and leaves debris
when a run is interrupted.

Three consequences, in the order they bite:

1. **A test may only assert about entities it created.** Never about the
   account's list as a whole. The account's thread list contains every other
   worker's threads and every previous run's leftovers, so
   `expect(row(x)).toBeVisible()` against the unfiltered list is really
   asserting "x is somewhere in a list I do not control". Narrow first —
   `ThreadListPanel.narrowTo(name)` filters to the test's own UUID-suffixed
   names, which makes every other worker's rows structurally absent rather
   than incidentally off-screen. This is the rule that catches people out;
   it is stated again in the README's conventions section.
2. **No sharding, ever.** Shards are separate machines that cannot
   coordinate. On the mock backend that is harmless — every test registers
   its own account — but on a shared account two shards will interleave
   mutations with no way to serialize. `e2e-mock.yml` shards; a
   static-account config never may.
3. **Some tests still cannot run in parallel at all.** Hence the serial
   companions, below.

What such a config excludes follows mechanically from the tags: `@visual`
(baselines are rendered against the local dev build by the same renderer),
`@mock-only` (no mock adapter behind a real deployment), `@mutates-account`
and `@needs-cognito-admin` (both consequences of the one shared account, as
above), and `@serial` from the parallel run. Everything else — every test
that seeds its own data through the API and asserts only about that data —
runs unchanged.

## Serial companions

Every backend has a `*.serial.ts` config alongside its parallel one. The
companion runs _only_ `@serial`, at `workers: 1` with
`fullyParallel: false`.

**The split is real on a static-account backend and nominal on the local
ones**, and the asymmetry is deliberate:

- A config whose tests all share one account excludes `@serial` from its
  parallel run, so parallel + companion partition the suite exactly: no test
  in both, none in neither. This is the case that matters, because it is the
  only one where parallel workers contend over a shared account.
- `mock-llm` and `local-llm` do **not** exclude `@serial`. On a local
  backend every test registers its own account, so there is no contention
  for the tag to protect against and the tests are safe in parallel. They
  run in the parallel config — which is what `npm run e2e` and CI use, so
  dropping them there would silently delete coverage — and the companion
  re-runs them serially. The duplication is the price of not having a
  coverage hole in the default command.

Practically: on a local backend the serial companion is a debugging tool
(reproduce a suspected ordering problem at one worker). On a shared-account
backend it is load-bearing, and skipping it means those tests did not run.

`serialCompanion()` in `playwright.config.base.ts` derives the companion
from the parallel config rather than restating it, so the two cannot drift.
It strips `@serial` from the inherited `grepInvert` and intersects the
inherited `grep` with `@serial` using lookaheads — which is what keeps the
prod allowlist intact, as above.

The bar for applying the tag is high, and it is stated once — in the
README's "Serial companions" section. Do not restate it here.

## Visual tests

`tests/visual/` is the odd one out and has its own config
(`mock-llm.visual`), because a screenshot comparison is only meaningful
against a baseline rendered by the same renderer.

- **Chromium only.** WebKit rasterizes fonts differently and would need its
  own baseline set, which the suite does not maintain. Enforced in the base
  config's `webkit-mobile` project via `testIgnore`, not per-spec, so a new
  visual spec cannot forget it.
- **Baselines are generated in Docker, never on a host.** macOS and Linux
  render text differently, so a baseline updated on a laptop fails in CI
  forever after. `npm run e2e:mock-llm:visual:docker:update` is the only
  correct way to refresh them.
- **CI runs them in that same container image**, pinned to the Playwright
  version resolved from `package.json`. That pin is the whole reason the
  visual specs can run in CI at all.
- **Selected with `testMatch`, not by narrowing `testDir`.** `testDir` is
  recorded in every blob report, and `playwright merge-reports` refuses to
  merge blobs whose recorded directories disagree — so narrowing it here
  would break the merge job that combines the functional shards with the
  visual run.

## What CI actually runs

`.github/workflows/e2e-mock.yml` is the workflow that runs Playwright on
changes — every PR, merge, and nightly gate below is it.
`.github/workflows/e2e-local-llm.yml` runs the suite against a real (small)
local model, opt-in only.

| Trigger                     | Projects                | Visual specs                                   |
| --------------------------- | ----------------------- | ---------------------------------------------- |
| PR (frontend/backend paths) | `chromium-desktop` only | only if the frontend or `openapi.yaml` changed |
| PR labelled `e2e-full`      | all three               | same rule                                      |
| Merge to `main`             | all three               | same rule                                      |
| Nightly (07:00 UTC)         | all three               | always                                         |
| Manual dispatch             | all three               | always                                         |

**`run-local-model` runs the suite for real**, not just the backend Go smoke
test `pr-validation.yml` already gates on that label. `e2e-local-llm.yml`
opts a PR into a single `chromium-desktop` run against
`playwright.config.local-llm.ts` (which already excludes `@visual` and
`@mock-only`), backed by Ollama serving `qwen2.5:0.5b` on a bare runner —
no pinned container, since there is no baseline to match pixel-for-pixel.
Like the Go smoke test, it's `continue-on-error: true`: real inference is
slower and less deterministic than the mock backend, so a flaky run
shouldn't block the PR.

**Why the mock backend in CI streams with a delay.** `e2e-mock.yml` starts
the API with `MOCK_LLM_STREAM_DELAY_MS=150`. The mock adapter's default is
0 — every word of the echoed reply is emitted in one synchronous burst, so a
job can go from "sent" to "complete" between two Playwright polls. That is
invisible to tests asserting the final state, but any test asserting an
*intermediate* one (the stop button appearing, the composer staying
disabled while a reply streams) is racing the job's own completion. 150ms
per word gives those assertions a real window without meaningfully slowing
the suite — a handful of echoed words costs under a second. Local runs
(`make run-mock`, `make dev-up`) still default to 0; set the same env var
if reproducing a transient-state flake locally.

**Why mobile is not on every PR.** The mobile projects roughly triple the
work for coverage that changes far less often than the code does. A nightly
catches a mobile regression within a day; the `e2e-full` label gets it on a
specific PR without waiting.

**Why merges to `main` run the full matrix.** A PR only ever proves the
suite passed against _its_ merge commit, and `main` moves underneath it. The
merge run is the one that says `main` itself is green, and it is full rather
than desktop-only so a mobile regression is caught at the merge that caused
it.

**Why the visual specs are gated on paths.** A baseline is a picture of the
Angular app; a commit that touches only Go cannot move one. The `changes`
job compares the run's base and head through the compare API and reports
whether anything under `web/app/` or `openapi.yaml` moved —
`openapi.yaml` counts because it is the contract the app renders from, so a
server-supplied field or string can change a screenshot without a file under
`web/` moving.

The job **fails open**: a schedule or manual run with no base to compare
against, a force-push whose `before` is the null SHA, a compare that hits
the API's 300-file cap, or an API error all report "changed" and run the
visual specs. A wasted couple of minutes is cheaper than a missed
regression. When the specs are skipped the PR comment says so explicitly,
because a skipped visual run that goes unmentioned reads as a passing one.

**How the run is laid out.** Five machines: four functional shards and one
visual entry, all from the same matrix so they share one setup block. The
visual specs used to be a step on shard 1, which made that shard the run's
critical path — it finished about 2.5 minutes after shard 2 on every full
run, and `merge` waits for both. On a backend-only run the visual entry
still starts and then skips its test step, because a job-level `if` cannot
read `matrix`; that costs a couple of billed minutes on those runs and buys
back more than that in wall clock on the frontend ones.

Shard count went from 2 to 4 once real run data showed shards were already
balanced within a couple of percent of each other: with the fixed per-shard
setup cost (~2 minutes) small relative to test execution (~25 minutes on 2
shards), doubling the shard count roughly halves the test-execution time on
the critical path at the cost of two more machines' worth of setup overhead.

**One run per PR.** A push cancels the run its commit superseded
(`concurrency`, keyed on the PR number). Cancellation is restricted to
pull requests: the merge, nightly and dispatch runs each answer a question
about one specific commit, and a later run does not make an earlier answer
redundant.

**Why sharding is safe here specifically.** Every test on the mock backend
registers its own account through the API and names every entity with a
`crypto.randomUUID()`, so no state is shared between tests — and therefore
none between shards. This property is exactly what a static-account config
does not have, which is why the no-sharding rule above is absolute.

## The `api-tests` suite

`e2e/api-tests/` is a fourth kind of test, alongside functional/journeys/
visual: no browser, no Angular dev server, calling the API directly through
`e2e/sdk/client.ts`. It has its own config
(`playwright.config.mock-llm.api.ts`, single `api` project) and its own CI
job (`api` in `e2e-mock.yml`), run independently of the three browser
projects and the shard/merge machinery above — it is not sharded (nothing
here is slow enough to need it) and its blob report is deliberately kept out
of the browser suite's merged HTML report (see the `api` job's "Run
Playwright API suite" step).

It exists for assertions that are about the API contract itself — token
lifecycle, cross-user scoping, job-polling semantics — where driving the UI
to reach the same assertion would be slower and would couple the test to
frontend implementation details it isn't actually about. It is not a
replacement for the functional browser specs that exercise the same
endpoints through real UI interactions; both exist because they check
different things.

Response-shape checking is intentionally scoped to what the generated SDK
already buys for free: every call in `e2e/sdk/client.ts` is typed against
`schema.d.ts`, so a field rename or removal on the backend breaks the
TypeScript build before a test ever runs. This suite does not additionally
validate response bodies at runtime (e.g. via an ajv schema derived from
`openapi.yaml`) — that would be a second, larger project (a generated runtime
validator, plus a drift check keeping it in sync, mirroring the SDK's own
`frontend-pr-validation.yml` check) and is out of scope here. Call this suite
what it is: typed-client coverage of the contract, not a runtime conformance
validator.

## Go coverage from this suite (`go-e2e`)

Every matrix entry builds the API with `go build -cover` instead of a plain
build, launches it with `GOCOVERDIR` set, and stops it with `SIGTERM` (not
`SIGKILL`) so the coverage runtime gets to flush counters on the graceful
shutdown path (`cmd/api-server/main.go`'s signal handler) before the process
exits. Each shard — including the visual entry, which builds and starts the
API regardless of whether its own Playwright step ran — uploads its raw
`$GOCOVERDIR` as an artifact (`gocov-1`, `gocov-2`, `gocov-visual`). The
`merge` job downloads all three, `go tool covdata merge`s them, converts with
`covdata textfmt`, and uploads the result to Codecov under flag `go-e2e`,
tokenless via OIDC — the same pattern `pr-validation.yml` uses for `go-unit`.

This is deliberately *not* wired through `-coverpkg`: a `-cover` build on a
`main` package instruments every main-module package linked into that binary
on its own, so nothing extra is needed to get e2e-only coverage of, say,
`internal/handlers` — visible in Codecov by toggling the `go-e2e` flag on and
`go-unit` off.

`make dev-up-cover` reproduces this locally — see the README's
[Go coverage from this suite](../README.md#go-coverage-from-this-suite)
section.

## Frontend coverage from this suite (`web-e2e-pr` / `web-e2e-nightly`)

The same workflow also measures the Angular app. `E2E_COVERAGE=1` on the two
functional shards turns on the V8 collection in `fixtures/coverage.ts`; each
shard uploads its raw counts as `webcov-<shard>`, and the `merge` job merges
them into one lcov and makes a single Codecov upload, tokenless via OIDC like
the Go flags above.

That upload picks its flag from the run type: `web-e2e-pr` on a pull request,
`web-e2e-nightly` on the nightly, a manual dispatch, or a PR labelled
`e2e-full`. The two measure different suites — the second adds the mobile
projects — so a single flag would swing every few hours and read as a coverage
regression rather than a trend. Both merge into the combined number either
way.

Three deliberate limits on where it runs:

- **Not on the visual entry.** Collection adds work on every navigation, and
  screenshot baselines are the one place in this suite where extra timing
  jitter turns into false failures.
- **Chromium only**, because `page.coverage` is a CDP API. PR runs are
  `chromium-desktop` anyway (see `PROJECTS`); on a full run `chromium-mobile`
  contributes too and `webkit-mobile` runs with the flag inert.
- **Mock-LLM only.** A run against an already-deployed build measures a
  bundle whose sourcemaps are not served, so its counts cannot be resolved
  back to `src/` without supplying the sourcemaps separately — see the
  README's "Against a deployed build" section.

The `merge` job runs `scripts/check-coverage.mjs` on the merged lcov before
uploading, so a pipeline that has quietly stopped collecting fails the job
rather than silently reporting a lower number. See the README's
[Frontend coverage from this suite](../README.md#frontend-coverage-from-this-suite)
section for running it locally and for the V8 navigation caveat.

## Choosing where a new test goes

0. Is it really about the API contract — not about what the user sees or
   does in the browser? → `e2e/api-tests/`, no tag needed (it has its own
   config and never runs against a browser project).
1. Does it need a screenshot? → `tests/visual/`, tag `@visual`, Chromium
   baselines via Docker.
2. Does it depend on a canned mock reply? → tag `@mock-only`. It will run on
   the mock backend only.
3. Does it create an account of its own instead of using the `testUser`
   fixture? → tag `@needs-cognito-admin` (see the note above on why the tag is
   named for the capability rather than the behaviour).
4. Does it change the account itself — password, profile, settings? → tag
   `@mutates-account`.
5. Does it need the account to itself? → tag `@serial`, and check first that
   scoping the assertion to your own data would not have been enough.
6. Should it run against production? → tag `@prod-safe`, deliberately.
   Omitting it is the safe default.

Everything untagged runs everywhere, which is the right default for a test
that seeds its own data through the API and asserts only about that data.
