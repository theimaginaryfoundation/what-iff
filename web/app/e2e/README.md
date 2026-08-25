# Frontend E2E (Playwright)

Browser-level tests against the real Angular app. The local configs'
`webServer` block starts the Angular dev server (`npm start`, `:4200`)
automatically — it does **not** start the backend API. You must have the
backend already running on `:8080` first, e.g.:

```bash
make db-up
make dev-up   # or: make run-mock / make run-local
```

`LLM_BACKEND=mock` or `LLM_BACKEND=local` both work for these tests. Personality
creation here uses the "Create Manually" form (a plain REST call), not the
AI "Generate Personality" wizard — that wizard calls the real LLM generation
path, which is intentionally disabled under `LLM_BACKEND=mock`/`local`
(`internal/agent/generate_personality.go`). Chat replies will be the mock's
deterministic echo or a real (if small) local-model reply depending on which
backend is running; tests only assert that a non-empty assistant reply
appears, not on specific wording.

> **Deciding where a new test belongs, or wondering why it passes on one
> config and not another?** Read
> [`docs/what-runs-where.md`](docs/what-runs-where.md) — which config runs a
> test against which backend, what each tag protects, and what CI runs on a
> PR versus nightly. This README is the how-to; that file is the why.

## Go coverage from this suite

The API these tests drive can be built with `-cover` and report Go coverage
under `$GOCOVERDIR`, the same mechanism CI uses to upload the `go-e2e`
Codecov flag (`.github/workflows/e2e-mock.yml`). `GOCOVERDIR` is honoured by
any `-cover` build, including locally:

```bash
make db-up
make dev-up-cover   # instead of dev-up — builds with -cover, prints GOCOVERDIR
# ...run the suite against it as usual...
make dev-down       # SIGTERM flushes counters; go tool covdata textfmt to inspect
```

`GOCOVERDIR` must exist before the binary launches or Go silently writes
nothing ("coverage meta-data emit failed"); `dev-up-cover` creates it for you.

The same mechanism covers the API-test suite in CI, uploaded separately under
its own `go-api` flag — see `.github/workflows/e2e-mock.yml`'s `api` job.

## Frontend coverage from this suite

Set `E2E_COVERAGE=1` and the suite also collects V8 coverage of the Angular
app, which becomes the `web-e2e-pr` / `web-e2e-nightly` Codecov flags (one per
run type — see `what-runs-where.md`). It is off by default: it costs roughly
10% wall clock and is pure overhead for anyone debugging a test.

```bash
E2E_COVERAGE=1 npm run e2e -- --project=chromium-desktop
node e2e/scripts/merge-coverage.mjs   # -> coverage/web-e2e/report/lcov.info
node e2e/scripts/check-coverage.mjs   # acceptance checks on the merged report
```

Three things about how it works are worth knowing before changing it:

- **Chromium only.** `page.coverage` is a CDP API; the flag is inert on
  `webkit-mobile`, so a full-suite number needs a Chromium project.
- **The dev server changes.** With `E2E_COVERAGE=1`, `webServer` runs
  `npm run serve:coverage` (`ng serve` with live-reload and HMR off) — both
  inject a client bundle and can re-serve a chunk mid-test, which V8 reports as
  a second script for the same URL. Sourcemaps come from `ng serve`'s default
  `development` configuration either way, and are what make the merge
  resolvable at all.
- **Counts do not survive a full-document navigation.**
  `Profiler.takePreciseCoverage` only reports scripts V8 still holds, so
  `resetOnNavigation: false` is not enough; `fixtures/coverage.ts` banks and
  restarts the recording around `goto`/`reload`/`goBack`/`goForward`.
  Client-side (router) navigation is invisible to V8 and needs nothing.

  That only catches navigations a *test* starts. A page-initiated one — a link
  click, a form submit, a `location.href =` — cannot be caught: the boundary
  where the outgoing document is still alive is the paused document request,
  and pausing it blocks the renderer, so the profiler call needed to bank the
  coverage deadlocks against the pause. Those are counted instead, via CDP
  `Page.frameNavigated`, and the merge job reports the total as a qualifier on
  the percentage — a document dropped before its counts were taken looks
  exactly like one that was never visited. If that number is not zero, find the
  navigation and make it a `routerLink`.

A test that builds its own context (`browser.newContext()`) is outside the
`page`/`context` fixtures and has to enrol its own page:

```ts
import { attachCoverage } from '../../../fixtures';
const page = await context.newPage();
await attachCoverage(page); // no-op unless E2E_COVERAGE=1
```

`tests/functional/coverage/collection.spec.ts` and `scripts/check-coverage.mjs`
are the acceptance tests for the pipeline itself — between them they cover
navigation banking, hand-built contexts, unvisited files landing at 0%, and the
merged report being rooted in the app source tree. In CI each shard uploads a
`webcov-<shard>` artifact and the `merge` job does the single merge and upload
(`.github/workflows/e2e-mock.yml`).

### Against a deployed build

The same collection also works against an already-deployed build (a config
that points `baseURL` at a deployment instead of starting the dev server),
with two differences from the local path:

- **Sourcemaps arrive separately.** A production build typically uses
  `sourceMap: { hidden: true }`, which emits maps but strips the
  `//# sourceMappingURL` comment, so nothing resolves at collection time.
  Keep the maps from the deploy build and attach them at merge time:
  `merge-coverage.mjs --sourcemaps <dir>`. In that mode an unmapped bundle
  fails the job rather than being dropped — against a deployed build every
  bundle is mapped, so an unmapped one means the maps and the build are out
  of step and the number would be quietly wrong.
- **Line % is the only comparable metric.** The bundles are minified, so
  several statements share a line and branch counts describe the optimised
  output. Both the report title and the step summary say so.

Failure traces from a run against a real deployment hold live bearer tokens
for a real account — upload the coverage report from such a run and nothing
else.

## Layout

```
e2e/
  poms/       # Page Object Models — locators + interactions only, no assertions, no env awareness
  fixtures/   # test.extend fixtures (testUser, authenticatedPage, seed, apiClient)
  sdk/        # generated openapi types + hand-written client — the only HTTP layer
  tests/
    functional/   # per-feature specs, one directory per feature area
    journeys/     # cross-cutting flows (register→personality→chat, password rotation)
    visual/       # toHaveScreenshot specs + committed __screenshots__ baselines
    a11y/         # accessibility specs; the axe-core scan harness is still to come
  api-tests/  # browserless suite, straight against the API — see below
  docs/
    what-runs-where.md # which config runs what, against which backend, and why
  scripts/
    visual-docker.sh   # runs tests/visual inside the Playwright Docker image
    merge-coverage.mjs # E2E_COVERAGE: raw V8 from every shard -> one lcov
    check-coverage.mjs # acceptance checks on that merged lcov
  playwright.config.base.ts   # shared settings + the three browser projects
  playwright.config.{mock-llm,local-llm}.ts
  playwright.config.mock-llm.visual.ts   # extends mock-llm; pixel baselines only
  playwright.config.mock-llm.api.ts      # extends mock-llm; api-tests/, no browser
```

Every test runs in three projects: `chromium-desktop` (Desktop Chrome),
`chromium-mobile` (Pixel 7) and `webkit-mobile` (iPhone 15) — except
`tests/visual/`, which is Chromium-only (`testIgnore` on the `webkit-mobile`
project in `playwright.config.base.ts`); see "Visual regression" below.
`api-tests/` is its own single `api` project and never launches a browser at
all — see "API-test suite" below.

## API-test suite

`e2e/api-tests/` covers the API contract directly through the SDK
(`e2e/sdk/client.ts`) — no browser, no Angular dev server. It exists
alongside the browser suite (not instead of it) for cases where driving the
UI to reach an assertion would be slow or incidental to what's actually being
checked: auth token lifecycle, cross-user access scoping, and the message/job
polling contract.

```bash
make db-up
make dev-up   # or: make run-mock
npm run e2e:api
```

Kept out of `tests/` on purpose (`testIgnore: /api-tests[\\/]/` in
`playwright.config.base.ts`, plus this suite's own config clearing that
ignore for its own `testDir`) — otherwise every one of the three browser
projects would pick these specs up and run them three times over, in a
project with no `page` fixture to give them.

`sdk/client.ts` is the only file that talks HTTP to the backend directly — a
hand-written wrapper over types generated from `openapi.yaml` (`npm run
sdk:generate`). `fixtures/api.ts` is a thin user-lifecycle layer on top of it.

`e2e/test-data/` holds universal invalid-value tables
(`invalid-values.ts`, `as const` label/value tuples) for data-driven
negative cases — reach for those instead of ad-hoc bad-input literals, so
every endpoint's negative tests probe the same classes of bad input (see
the `credential validation` block in `api-tests/auth.spec.ts` for the
pattern).
### Ported from the Newman regression suite

Most of this repo's old `tests/regression/chat-api/` — a Postman/Newman
collection driven by `run-regression-e2e.sh`, since removed from the public
tree — now lives here instead, one spec per collection folder: health,
models, search, personality expressions, personality cover,
chat-creation-with-persona, and personality stats. The mapping back to the
original steps is in each spec's header comment, against that collection's
`scenario-contract.md`.

Three things did **not** come across, because this suite runs against
`LLM_BACKEND=mock` and the Newman one assumed a real provider plus a
hand-seeded paid plan:

- anything asserting on *generated* content — the tool-inventory prompt, the
  `generation_model` of a reply produced after a model switch, and the
  scratchpad/summary the agent writes into `GET /chat/{id}/context`;
- anything needing an uploaded image — assigning a real expression portrait
  or personality cover, since `POST /personality/{id}/file-attachment`
  uploads to the provider's files API and mock mode denies provider egress
  outright (`internal/agent/provider/deny_transport.go`), answering 500;
- the free-tier quota / paid-plan-upgrade journey, which needs a billing
  backend and a direct DB seed.

Those need a deployment with real provider egress and a billing backend, so
they are out of scope for this suite. The collection is kept as the reference
for that journey; it is no longer the place to add coverage for anything in the
list above.

## Configs

| Config               | Backend                    | Frontend server       | Excludes                                                                       |
| -------------------- | -------------------------- | --------------------- | ------------------------------------------------------------------------------ |
| `mock-llm` (default) | local, `LLM_BACKEND=mock`  | started by Playwright | `@visual`                                                                      |
| `mock-llm.visual`    | local, `LLM_BACKEND=mock`  | started by Playwright | everything but `tests/visual/`                                                 |
| `local-llm`          | local, `LLM_BACKEND=local` | started by Playwright | `@visual`, `@mock-only`                                                        |

Each config's npm script matches its name: `npm run e2e:mock-llm`,
`e2e:local-llm`, `e2e:mock-llm:visual`. Plain `npm run e2e` is an alias for
`e2e:mock-llm`.

What each tag protects is in
[`docs/what-runs-where.md`](docs/what-runs-where.md).

### Serial companions

Every backend has a `<config>.serial.ts` companion and a matching
`:serial` script. It runs one worker and _only_ the `@serial` tests —
everything else (backend, timeouts, artifact retention, tag exclusions) is
inherited from the parallel config via `serialCompanion()`, so a setting
can't apply to one and not the other.

**The bar for tagging a test `@serial` is high.** Almost nothing needs it:
every seeded entity carries a UUID, so a spec that follows "assert only
about entities your own test created" (below) is parallel-safe even on a
config where every test shares one account. Tag a test only when it
genuinely cannot be scoped — it
asserts a global count, drives a bulk action over "everything", or changes
an account-wide setting another test would read. Narrowing the assertion is
almost always the better fix, and a `@serial` test is one that no longer
runs in the main suite.

On mock/local each test registers its own account, so a `@serial` test
still runs in the normal parallel suite — it stays covered by `npm run e2e`
and by CI — and the serial config is how you reproduce it in isolation. A
config whose tests all share one pre-provisioned account (see "Running
against your own deployment") is the case that must exclude `@serial` from
its parallel run.

A production-facing config should be opt-in (`grep: /@prod-safe/`), and
`serialCompanion()` _intersects_ that allowlist rather than replacing it: a
test must carry both `@prod-safe` and `@serial` to run. Replacing it would
point untagged-for-prod tests at production, which is the exact thing the
allowlist exists to prevent.

### Running against your own deployment

The framework is built so a downstream consumer can point the suite at an
already-running deployment by supplying a config of its own — none of the
configs in this repo target one. The recipe:

- **Spread `baseConfig`**, set `use.baseURL` to the deployment's frontend
  origin, and omit `localWebServer` — the deployment serves its own frontend.
- **Set artifact retention explicitly.** Failure traces hold live bearer
  tokens and typed passwords for a real account: never record video, keep
  traces `retain-on-failure` at most, and treat them as sensitive.
- **Exclude the tags that assume a throwaway backend**: `@mock-only`,
  `@visual` (baselines are rendered against the local dev build),
  `@mutates-account`, and `@needs-cognito-admin` (specs that create accounts
  outside a fixture and would leak them). A production-facing config should
  invert the model entirely and run only an opt-in allowlist
  (`grep: /@prod-safe/`).
- **Authentication.** If the deployment's login is backed by an external
  identity provider the suite cannot self-register against, replace
  `fixtures/static-account.ts` (see its doc comment) with an implementation
  that signs in one pre-provisioned, dedicated test account — never a real
  person's login, since fixtures mutate and delete things. Authenticate once
  per run in a `setup` project (`SETUP_PROJECT` in
  `playwright.config.base.ts`) and share the session via `storageState`;
  identity providers rate-limit per-user logins, and a login per test can
  lock the shared account out. `sdk/cognito.ts` is a ready-made SRP login
  helper for deployments whose build sets `authMode: 'cognito'`.
- **Exclude `@serial` from the parallel run** and use `serialCompanion()`
  for the serial one — with a shared account, `@serial` tests are the ones
  where UUID-scoping is not sufficient.
- **Never shard such a config** — see "Layer 3" under Parallelism below.

Because a shared account is never deleted, specs that mutate the _account
itself_ — not a seeded entity — would corrupt it for every other run, with
no teardown to undo the change. That is what `@mutates-account` protects:
`password-rotation.spec.ts` permanently changes the account's real password.

Keep credentials for such a config in the repo's root `.env` (gitignored)
and load it from the config via `dotenv`, so nothing needs exporting into
the shell; let values already in the environment win, so a one-off
`E2E_BASE_URL=… npm run <script>` can retarget a run at a preview
deployment without touching a file.

`E2E_BASE_URL` is the frontend origin Playwright navigates to (and, for
`mock`/`local`, the URL its own `webServer` waits on before starting tests);
it defaults to `http://localhost:4200` when unset. `E2E_API_BASE_URL` is the
backend API origin used by `fixtures/api.ts` and the generated SDK
(`e2e/sdk/client.ts`) for direct HTTP setup/teardown calls, independent of
whatever `apiUrl` the built frontend itself points at; it defaults to
`http://localhost:8080/api` when unset.

## POM and fixture conventions

**Testability means we never bypass native events.** If a Playwright action
like `dblclick()` doesn't work in a browser or viewport, the fix belongs in
the application code, not in the test. Never work around it with
`dispatchEvent('dblclick')`, `evaluate(() => el.click())`, or any other
synthetic event injection. Those approaches hide the real problem (the app
can't handle the interaction on that platform) and produce a test that
passes while the feature is broken for real users.

**Never use static timeouts to wait for app state.** `waitForTimeout`,
`setTimeout`, or any hardcoded delay is a race condition with a generous
head start, not a fix. The only acceptable static waits match a known,
constant app-owned duration like a CSS animation or a debounce timer that
is itself a defined constant.

When a test is flaky because the app's state transitions are
non-deterministic, fix in this priority order:

1. **Fix the application** to be more deterministic (e.g. cancel superseded
   API requests so only the last response writes to the view).
2. **Fix the UX** so the user can't trigger non-deterministic behavior
   (e.g. disable a button while loading, debounce rapid clicks).
3. **Wait on an observable DOM event** that is also WCAG-compliant
   (a `role="status"` loading indicator, an `aria-busy` attribute, a
   visible empty/loaded state transition).
4. **Wait for a specific network response** using `waitForResponse` with a
   URL/predicate match for the exact API call the view depends on.

If none of these are possible, that is a testability gap in the app. File
it, add a testability hook (`data-` attribute, aria role, signal-driven
class), and wait on that. Never paper over the gap with a static delay.

**Never wait for `networkidle`.** `waitForLoadState('networkidle')` and
`waitUntil: 'networkidle'` are deprecated, unreliable, and couple tests to
implementation details (how many requests the app fires, whether it has
long-polling or analytics pings). Waiting for a *specific* response
(`waitForResponse` with a URL predicate) is fine and is option 4 above;
waiting for *all network activity to stop* is not.

**Never give a POM member a name that Playwright's own API already uses.**
`goto`, `fill`, `click`, `close`, `selectOption`, `title` and friends all
exist on `Page`/`Locator`, and a POM method sharing one reads at the call
site as if it were the Playwright call — `await page.fill(details)` says
nothing about which form, and `await modal.close()` hides that it clicks a
specific button. The POM names here are `navigateTo`, `fillCredentials`,
`dismiss`, `chooseOption`, `headingText`.

**Navigate by URL unless the click path is what's under test.** Every POM
exposes `navigateTo()`, a single `page.goto()` of that page's route. It is
one hop, it can't be knocked off course by whatever the last test left on
screen, and it doesn't couple every spec to the sidebar's markup. When
reaching a page _by clicking_ is the point, the standard method is
`AppShell.clickThroughTo(section)` — used by
`tests/functional/nav/sidebar-nav.spec.ts`, the spec that exists to catch
nav-wiring regressions.

**POMs come from fixtures — never construct one in a spec.** Every POM is
exposed as a fixture named after it (`threadListPanel`, `chatPage`, `shell`,
…), so a test asks for what it drives:

```ts
test('renames a thread', async ({ seed, threadListPanel, userWithPersonality }) => {
  await threadListPanel.navigateTo();
```

The POM fixtures depend on the built-in `page`, which is the same object
`authenticatedPage` and `userWithPersonality.page` hand back — so a spec can
combine a POM with whichever auth fixture it needs and they all drive the
same tab. Playwright constructs a fixture only for the tests that name it,
so the fifteen POMs cost an unused one nothing.

Note that a spec often names `userWithPersonality` without reading it:
naming a fixture is what _runs_ it, and that one registers an account, seeds
a personality and lands on /chat. `eslint.config.mjs` exempts it (and
`authenticatedPage`) from `no-unused-vars` for exactly this reason.

**Assert only about entities your own test created — never about the
account's list as a whole.** On a config with a static test account (see
"Running against your own deployment") every test signs in as the _same_
account. Its threads, memories, skills and personalities are therefore the
union of what every test running right now created, plus the residue of
every previous run.

So an assertion phrased against the whole list is not the claim the test
means to make:

```ts
// Asserts "my thread is somewhere in a list I don't control" — fails for
// reasons that have nothing to do with this test.
await expect(threadListPanel.row(mine.name)).toBeVisible();

// Asserts "my thread is there", and nothing else.
await threadListPanel.narrowTo(mine.name);
await expect(threadListPanel.row(mine.name)).toBeVisible();
```

The mechanics that make this work are already in place: every seeded entity
is named with a UUID (`fixtures/unique.ts`), so narrowing by name leaves
exactly your rows and makes every other worker's rows _structurally_ absent
rather than incidentally off-screen. That is why the rest of the suite
survives six workers on one account — this rule is what the rest of the
suite is already implicitly doing.

Where a list POM has no narrowing affordance yet, add one
(`ThreadListPanel.narrowTo()` is the model) rather than weakening the
assertion or adding a wait. And narrow _again_ after anything that changes
which set the list draws from — switching the Active/Archived tab picks the
set, narrowing picks your rows within it.

This applies to a "hidden" assertion just as much as a "visible" one: after
archiving or deleting, narrow first, because "not present" is only
meaningful once the row would have been shown if it still existed.

**Fixture and helper names say which identity you get**, rather than naming
the mechanism: `authenticatedPage` (not `loggedInPage`),
`authenticateAsNewUser()`, `signInAsStaticAccount()`, `LoginPage.signIn()`.

**POM locators are `readonly` fields assigned in the constructor**, not
getters. A locator is already lazy — it resolves at action time, not at
construction — so a getter only rebuilds the same object on every access.

**A POM constructor assigns locators and composes other POMs. Nothing
else** — no `page.on()`, no navigation, no network, nothing awaited.
Anything that touches the page's runtime state is a method, so the test
decides when it happens.

POMs are handed to tests as fixtures, and a fixture is constructed for
every test that names it, at setup time, whether or not the test goes on
to use it. Work hidden in a constructor is therefore work the call site
can neither see nor opt out of, and it runs earlier than the reader
expects. `eslint.config.mjs` enforces this over `e2e/poms/` — a bare call
statement in a constructor body is an error.

## Parking a test on a filed bug

When a test fails because the **product** is wrong, the sequence is: file
the bug, then park the test on it. Never delete the test, never weaken the
assertion to match the broken behaviour, and never leave it failing.

```ts
test('exposes the personality filter', { tag: '@serial' }, async ({ … }) => {
  // Real bug found via exploration: the filter's options are derived from
  // the *loaded threads* (`uniquePersonalityOptions(allThreads())` in
  // core/services/thread-list.service.ts:55), not from the account's
  // personality list, so a personality with no threads is never offered.
  // Filed as #214.
  test.fixme(true, 'Personality filter omits thread-less personalities — see #214');
  …
});
```

Three parts, all required:

- **`test.fixme`, not `test.skip`.** `fixme` means "this should pass and
  doesn't". `skip` means "not applicable to this project or viewport" — a
  capability gate, like export chrome existing only above 1024px. Using
  `skip` for a defect hides it among the legitimately-inapplicable tests.
- **The issue number in the reason string.** That string is what Playwright
  prints in the report next to the skip, so it is the one place a reader
  encounters the link without going hunting. `eslint.config.mjs` enforces
  this over `e2e/tests/` — a `test.fixme` whose reason has no filed GitHub
  issue number (`#nnn`) is an error.
- **A comment with the `file:line` trace.** The ticket carries the full
  investigation; the comment carries enough of it that the next reader
  knows what is broken without leaving the file.

Leave the test body intact below the `fixme`. It is the reproduction, and
it is what verifies the fix: deleting the `fixme` line should be the whole
of the re-enabling change.

Not every filed bug parks a test. Thread Manager rendering another
session's threads under concurrent sessions is worked around in the POM
layer by `ThreadListPanel.narrowTo`, so the tests still assert their real
subject and nothing is disabled. Park a test only when the assertion itself
cannot currently hold.

## Cleanup

Fixtures clean up after themselves: `testUser` deletes its account in
teardown, and `seed` deletes every entity it created. Both log failures, and
both fail the test in CI so a systematically broken teardown cannot pass
unnoticed.

There is **no sweep** that collects strays. The `e2e-` prefix on generated
usernames and entity names is a label that makes test data recognisable — it
is not a safety net, and nothing scans for it. Anything created outside a
fixture leaks permanently, which is why the specs that create an account of
their own (`tests/functional/auth/auth.spec.ts`,
`tests/journeys/new-user-lifecycle.spec.ts`, and two in
`tests/functional/profile/profile.spec.ts`) are tagged `@needs-cognito-admin`
— the tag any config with a backend that outlives the run must exclude.

## Parallelism

Parallelism has three layers here and each has its own constraints. Getting
any of them wrong produces flakes that only appear on certain backends or
under certain concurrency.

### Layer 1: tests within a worker (`fullyParallel`)

`fullyParallel: true` in the base config means tests from the same file can
run concurrently within a single worker. Every test gets its own `BrowserContext`
(Playwright's default), so they share a worker process but not cookies, storage
or navigation state. This is safe as long as tests don't share backend state
either, which the UUID-naming convention below guarantees for the mock and
local backends.

The serial companion configs set `fullyParallel: false` and `workers: 1` to
prevent both kinds of interleaving.

### Layer 2: workers within a run

The base config sets `workers: 1` in CI and `undefined` (Playwright's
auto-detect, typically half-CPU) locally. CI uses one worker per shard
because sharding already provides the parallelism across machines; adding
workers within a shard would double the Go API's load inside a single
container, which has caused OOM kills on the service container in early
experiments.

On a static-account config every worker signs in as the **same account**.
Authentication happens once per run in a `setup` project and is shared via
`storageState`, so adding workers does not multiply logins against the
identity provider, but it does multiply the mutations against a single
account's data. This is safe because every seeded entity carries a UUID
(`fixtures/unique.ts`) and assertions use `narrowTo()` to scope to their own
data, but it is also why such a config must exclude `@serial` from its
parallel run: those are the tests where scoping was not sufficient.

### Layer 3: shards across machines (CI only)

`e2e-mock.yml` shards the mock suite across two runners. This is safe
**only** because every mock/local test registers its own throwaway account
(no shared state between tests, therefore none between shards). A
static-account config **must never be sharded**: two machines running
against the same account cannot coordinate teardown order, and interleaved
mutations will produce non-deterministic failures. This constraint is
documented in `e2e-mock.yml` and in `docs/what-runs-where.md`.

### What makes parallel-safety work (and what breaks it)

The invariant that keeps everything parallel-safe on mock/local is: **every
test creates its own account and names every entity with a UUID.** Break
either half and you get cross-test interference:

- **Shared account without scoping** produces false positives ("my thread is
  visible" passes because the _other_ worker's thread matched) and false
  negatives ("no threads found" fails because a neighbour's thread appeared).
  This is the static-account reality, and `narrowTo()` is the mitigation.
- **Non-unique names** make scoping useless: `narrowTo("test-thread")` would
  match every worker that chose the same name.

A test that creates or modifies the account itself (password, profile name,
settings) rather than a seeded entity is unsafe in parallel, because the
modification affects every other worker using the same account. Tag it
`@mutates-account` so a static-account config can exclude it.

### Teardown under crashes

Fixtures clean up in reverse order (Playwright guarantee), but a crashed
worker skips teardown entirely. On mock/local this is harmless: the database
is throwaway. On a backend that outlives the run it leaves orphan entities
on the shared account. The `e2e-` prefix makes them recognisable but nothing
sweeps for them automatically. A systematically crashing test on such a
backend will accumulate debris; file a bug and fixme the test rather than
letting it run and leak.

## Running

```bash
npm run e2e            # mock config (default), all three browser projects
npm run e2e -- --ui    # interactive UI mode
npm run e2e:mock-llm
npm run e2e:local-llm
```

There is no root `playwright.config.ts` any more — always pass `--config`
(the npm scripts do) so the environment is explicit.

First time only, install the browser binaries:

```bash
npx playwright install chromium webkit
```

## Visual regression

`tests/visual/` covers the app's few genuinely stable, deterministic screens
— login, register, the empty personalities list, a manually-created
personality's detail page, the Profile & Settings modal, and the chat
composer before any message is sent. It deliberately does **not** cover
anything downstream of an assistant reply: the local stack normally runs
`LLM_BACKEND=local`, and even the mock backend's timing/content isn't the
kind of thing a pixel baseline should pin. Every visual spec is tagged
`{ tag: ['@visual', '@mock-only'] }`, uses the shared `testUser`/`authenticatedPage`
fixtures, and drives the page exclusively through `poms/` — grow a POM
locator rather than reaching into a spec with a raw selector.

Determinism follows openMCT's approach: `animations: 'disabled'` on every
`toHaveScreenshot()` call, `page.emulateMedia({ reducedMotion: 'reduce' })`
before capturing (see `tests/visual/visual.helpers.ts`), and `mask:` over
anything account-specific or otherwise non-deterministic (sidebar
username/avatar button, the Profile modal's identity card, a couple of
elements found by trial-and-error to render inconsistently run-to-run even
with fixed test data — see the comments in `chat.visual.spec.ts` and
`personalities.visual.spec.ts`). Two specs additionally set a small
`maxDiffPixelRatio` for a masked region whose _size_ (not content) shifts by
a few pixels depending on async timing — a last resort after masking, not a
substitute for it.

### Updating snapshots

Font rasterization differs between macOS (where you're probably running
this) and the Linux image CI uses, so baselines are always generated and
checked inside the CI base image (`docker/ci/Dockerfile`'s `e2e` target,
published by the `CI base image` workflow) — never by running
`--update-snapshots` on your host. This mirrors openMCT's visual-testing
workflow.

```bash
npm run e2e:mock-llm:visual              # host run — will NOT match committed (Linux) baselines, diagnostic only
npm run e2e:mock-llm:visual:update       # host run, regenerates host-platform baselines — do not commit these

npm run e2e:mock-llm:visual:docker        # check mode inside the CI base image — same image CI uses
npm run e2e:mock-llm:visual:docker:update # regenerate baselines inside the container — commit these
```

CI runs these too. The `e2e-mock` workflow runs its shard jobs _inside_ the
same `e2e` CI base image `visual-docker.sh` resolves (both via
`scripts/ci-image.sh e2e`), so the renderer that takes a CI screenshot is the
one that produced the committed PNG. A visual regression therefore fails the
PR rather than waiting for someone to run the Docker recipe by hand. Because
both sides resolve the ref through the same script, they can never drift
apart.

Update the baselines whenever a change intentionally alters one of the six
covered screens; the container run is what actually validates the diff, not
a local eyeball on a macOS-rendered screenshot.

**Prerequisites** for the `:docker` scripts (see `e2e/scripts/visual-docker.sh`
for the fully commented version): Postgres + the backend API running on the
host at `:8080` (`make db-up`, then `make dev-up` / `make run-mock` /
`make run-local`). The Angular dev server itself is started _inside_ the
container by Playwright's normal `webServer` config — nothing extra to run
for that.

**The recipe that actually works on Docker Desktop for macOS:**
`--network host` is a no-op there (unlike native Linux Docker), so the
container reaches the host backend via the `host.docker.internal` DNS name
Docker Desktop provides, with `E2E_API_BASE_URL=http://host.docker.internal:8080/api`
picked up by `e2e/sdk/client.ts` for the suite's own HTTP calls (user
registration, cleanup, etc.). The app's _browser_ JS is a separate problem:
`src/environments/environment.ts` bakes `apiUrl` in as the literal
`http://localhost:8080/api` at build time, and Chromium running inside the
container resolves `localhost` to the container itself, not the host.
Pointing the whole page at `host.docker.internal` instead would dodge that
but breaks the backend's CORS allowlist (only `http://localhost:4200` is
trusted) and would need its own baseline set. Instead
`playwright.config.base.ts` reads `E2E_CHROMIUM_HOST_RESOLVER_RULES` and
passes it straight to Chromium as a `--host-resolver-rules` flag; the script
sets it to `MAP localhost:8080 host.docker.internal:8080`, so only that one
`host:port` gets rerouted to the real backend while the app's origin stays
exactly `http://localhost:4200`, matching CORS and cookies as if nothing
were containerized at all. `node_modules` gets its own anonymous Docker
volume (not the bind-mounted repo) so the container's `npm ci` — needed
because the platform-specific parts of `node_modules` differ from the host's
— can't clobber your host toolchain.

### Upgrading Playwright

`web/app/package.json`'s `devDependencies['@playwright/test']`
is the single source of truth for the Playwright version — it must stay an
exact version (no `^`/`~` range), because both the CI base image tag and the
committed visual baselines are derived from it. `scripts/ci-image.sh` reads it
via `e2e/scripts/playwright-version.sh`, which also fails loudly if that pin
and the installed `node_modules` version ever diverge.

To bump it:

1. Set the new exact version in `package.json`, then run `npm install` inside
   `web/app/` to update `package-lock.json` in lockstep.
2. That's the only manual step — everything downstream is automatic. The new
   version changes what `scripts/ci-image.sh` resolves the CI base image tag
   to, and `ci-image.yml` triggers on changes to `package.json`, so the same
   PR builds and publishes that new image before its own e2e jobs need it
   (`visual-docker.sh` also builds it locally on the fly if the GHCR image
   isn't published yet — see "Updating snapshots" above).
3. A Playwright bump usually changes rendering, so regenerate the visual
   baselines against the new image and commit them:
   `npm run e2e:mock-llm:visual:docker:update`.
4. Run the rest of the mock-llm suite against the new image before pushing to
   catch any other behavioral changes in the new Playwright release.
