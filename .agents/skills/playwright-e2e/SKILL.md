---
name: playwright-e2e
description: >-
  Work with the frontend Playwright end-to-end suite in
  web/app/e2e — running, writing, or debugging e2e tests,
  choosing which config/env to run against (mock/local/dev/prod), adding or
  extending a Page Object Model (POM), updating visual regression snapshots,
  regenerating the API SDK from openapi.yaml, or diagnosing a failing
  Playwright run. Trigger on phrasing like "run the e2e tests", "write a
  Playwright test for X", "add a POM for the Y page", "update the visual
  baselines", "why is this e2e test flaky/failing", or "which e2e config do
  I use for dev/prod".
---

# Playwright E2E

Browser-level tests against the real Angular app, in
`web/app/e2e`. This skill is a decision-tree pointer — for
full detail read `web/app/e2e/README.md` (layout, configs,
visual-regression internals, and the reasoning behind the structure) rather
than expecting it duplicated here.

All commands below run from `web/app/` and always go through
the repo's `@playwright/test` devDependency via `npx playwright` / the npm
scripts — never install or invoke a separately pinned/global Playwright.

## Which config?

| Need | Command | Notes |
|---|---|---|
| Everyday local run (default) | `npm run e2e` or `npm run e2e:mock-llm` | backend on `LLM_BACKEND=mock`, Playwright starts the Angular dev server |
| Exercise a real (small) local model | `npm run e2e:local-llm` | excludes `@visual`, `@mock-only` |
| Against your own deployment | supply your own config — see the README's "Running against your own deployment" | spread `baseConfig`, set `use.baseURL`, omit `localWebServer`; never record video, keep traces `retain-on-failure` |
| Interactive debugging | `npm run e2e -- --ui` | |
| Just the visual specs (diagnostic only, not baseline-accurate) | `npm run e2e:mock-llm:visual` | host run, will NOT match committed Linux baselines |

`mock` is the default and what you should reach for unless the task
specifically needs another environment. There is no root
`playwright.config.ts` — every invocation needs `--config` (the npm scripts
already pass it).

## Backend prerequisites

The local configs' `webServer` only starts the Angular dev server, never the
backend. Before running `e2e`/`e2e:mock-llm`/`e2e:local-llm`:

```bash
make db-up
make dev-up   # or: make run-mock / make run-local
```

`dev`/`prod` configs require `E2E_BASE_URL` and `E2E_API_BASE_URL` and throw
immediately if either is missing.

## Writing tests: POM conventions

- `e2e/poms/` holds Page Object Models: **locators and interactions only —
  no assertions, no env-awareness.** Assertions live in the spec.
- Grow an existing POM rather than reaching into a spec with a raw locator.
  If the page doesn't have a POM yet, add one following the existing files
  in `e2e/poms/` (`index.ts` re-exports them all).
- Locate by `aria-label`, role, or placeholder — the app has no
  `data-testid` attributes, and adding them isn't the established pattern
  here.
- Specs live under `e2e/tests/{functional,journeys,visual,a11y}/`, grouped
  by feature (see `functional/auth/`, `functional/personality/`,
  `functional/profile/`).

## Fixtures

Use `e2e/fixtures/index.ts`'s `test`/`expect`, not bare `@playwright/test`:

- `testUser` — a freshly registered account, self-deletes in teardown.
- `userWithPersonality` — a logged-in `page` that already owns a
  personality (needed by anything past the `personalitySetupGuard`).
- `seed` — creates chats/memories/rituals/webhook tokens/personalities for
  the current test and tears them down after.
- `apiClient` — an SDK client authenticated as `testUser`, for setup calls
  that don't need the UI.

`e2e/fixtures/api.ts` is the one file allowed to hand-roll HTTP calls
(registration/deletion); everything else should go through the generated
SDK in `e2e/sdk/`.

## SDK regeneration

If `openapi.yaml` changed, regenerate the typed client before writing tests
against new endpoints:

```bash
npm run sdk:generate
```

## Lint gate

Before considering e2e work done:

```bash
npm run lint:e2e
```

## Visual snapshots

Visual specs (`e2e/tests/visual/`, tagged `@visual @mock-only`) are
Chromium-only and cover a handful of deterministic screens. **Baselines are
only ever generated/updated inside the Playwright Docker container** —
never via `--update-snapshots` on a host machine (macOS/Linux font
rasterization differs from CI):

```bash
npm run e2e:mock-llm:visual:docker         # check mode — what CI effectively runs
npm run e2e:mock-llm:visual:docker:update  # regenerate baselines — commit these
```

`npm run e2e:mock-llm:visual:update` (host) exists only for local diagnosis; never
commit its output. See the README's "Visual regression" section for the
Docker Desktop networking details before running the `:docker` scripts.
Both `:docker` scripts run inside the CI base image resolved by
`scripts/ci-image.sh e2e` (a `ghcr.io/...` tag built from `docker/ci/Dockerfile`)
rather than a bare Playwright image — see the `playwright-version` skill for
how to upgrade it.

## More detail

- `web/app/e2e/README.md` — full layout, config matrix,
  visual-regression mechanics, Docker networking recipe, and the reasoning
  behind this structure.
- `web/app/e2e/TEST_PLAN.md` — coverage plan and known bugs.
