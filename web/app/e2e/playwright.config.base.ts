import { devices, type PlaywrightTestConfig } from '@playwright/test';
import { ASSERTION_TIMEOUT } from './timeouts';

/**
 * The app's `environment.ts` bakes `apiUrl` in as an absolute
 * `http://localhost:8080/api` at build time (no runtime override). That's
 * fine when the browser itself runs on the host, but when Chromium runs
 * *inside* the Playwright Docker container (`e2e:mock-llm:visual:docker*`), its own
 * `localhost` is the container, not the host — so the app's XHRs would try
 * to hit a backend that isn't there.
 *
 * `E2E_CHROMIUM_HOST_RESOLVER_RULES`, when set, is passed straight through
 * to Chromium's `--host-resolver-rules` flag so the container can remap just
 * that one hostname:port (typically `MAP localhost:8080
 * host.docker.internal:8080`) without touching app source or navigation
 * URLs. Unset outside the Docker recipe, so normal runs are unaffected.
 */
const hostResolverRules = process.env['E2E_CHROMIUM_HOST_RESOLVER_RULES']?.trim() || undefined;
/**
 * Chromium compares the verb case-insensitively (`EqualsCaseInsensitiveASCII`)
 * but splits tokens on a literal space, so this matches on both counts: the
 * `i` flag below, and ` +` rather than `\s+` — a tab-separated rule passes a
 * `\s+` check and is then silently ignored by Chromium, which is exactly the
 * failure this guard exists to catch.
 */
const HOST_RESOLVER_RULE = String.raw`(MAP +[^\s,]+ +[^\s,]+|EXCLUDE +[^\s,]+)`;
if (hostResolverRules !== undefined && !new RegExp(`^${HOST_RESOLVER_RULE}( *, *${HOST_RESOLVER_RULE})*$`, 'i').test(hostResolverRules)) {
  // Chromium silently ignores a malformed --host-resolver-rules value, which
  // turns a typo here into a confusing "backend unreachable" failure inside
  // the container. Fail at config load instead, where the message is obvious.
  throw new Error(
    `E2E_CHROMIUM_HOST_RESOLVER_RULES is set but not a valid rule list (expected e.g. "MAP localhost:8080 host.docker.internal:8080"): ${hostResolverRules}`,
  );
}
export const chromiumHostResolverUse = hostResolverRules ? { launchOptions: { args: [`--host-resolver-rules=${hostResolverRules}`] } } : {};

/**
 * Settings shared by every environment config (mock/local). Env configs spread
 * this and override `use.baseURL`, tag filters, and whether a dev server is
 * started. See web/app/e2e/README.md. A downstream
 * consumer running this suite against its own deployment does the same: spread
 * `baseConfig`, point `use.baseURL` at the deployment, and skip
 * `localWebServer`.
 */
export const baseConfig: PlaywrightTestConfig = {
  testDir: './tests',
  // e2e/api-tests/ is a separate, browserless suite (playwright.config.mock-llm.api.ts)
  // with its own single project. Every project below is picked up by
  // `testDir: './tests'` and sharded; without this, an API spec placed there
  // by mistake would run three times over, once per browser project, in a
  // project with no `page` fixture to give it.
  testIgnore: /api-tests[\\/]/,
  fullyParallel: true,
  forbidOnly: !!process.env['CI'],
  // Retry in CI only. CI has contention a developer's machine doesn't, so one
  // retry there keeps an infrastructure blip from failing an otherwise good
  // run. Locally the opposite is wanted: a flaky test should fail in front of
  // the person who can still reproduce it, rather than going green on a rerun
  // and being discovered later from a CI report.
  retries: process.env['CI'] ? 1 : 0,
  workers: process.env['CI'] ? 1 : undefined,
  reporter: 'list',
  // Every environment inherits this; individual assertions still override with
  // a named budget from e2e/timeouts.ts where they wait on something specific.
  expect: { timeout: ASSERTION_TIMEOUT },
  // No `timeout` here on purpose: how long a test may take depends on the
  // environment behind it (real model, network hop), so each env config sets
  // its own from e2e/timeouts.ts rather than inheriting a default that suits
  // none of them. Same for `webServer.timeout` below.
  // No `use.trace`/`use.video`/`use.screenshot` here on purpose. Traces record
  // request headers and `fill()` values — Authorization bearers, the
  // `/user/login` body, typed passwords — and Playwright has no redaction, so
  // the right retention is a property of *which backend the run touched*, not
  // of the suite. Each env config sets all three explicitly.
  use: {},
  projects: [
    {
      name: 'chromium-desktop',
      use: { ...devices['Desktop Chrome'], ...chromiumHostResolverUse },
    },
    {
      name: 'chromium-mobile',
      use: { ...devices['Pixel 7'], ...chromiumHostResolverUse },
    },
    // Visual baselines (tests/visual/) are Chromium-only — WebKit's own font
    // rasterization would need its own baseline set, and the suite doesn't
    // maintain one. Excluded here (config-level) rather than per-test so a
    // new visual spec can't forget the skip.
    {
      name: 'webkit-mobile',
      use: { ...devices['iPhone 15'] },
      testIgnore: /tests[\\/]visual[\\/]/,
    },
  ],
};

/**
 * Frontend dev server, started for the local-backend configs only — a config
 * targeting an already-running deployment omits this and lets that deployment
 * serve its own frontend. `cwd` is the app root because
 * these configs live one level down in e2e/. URL follows `E2E_BASE_URL` (same
 * variable `baseURL` uses in each env config) so a container run can point
 * this at a host-side dev server instead of `localhost` — see
 * `e2e/scripts/visual-docker.sh`.
 *
 * No `timeout`: the configs that use this set it explicitly from
 * e2e/timeouts.ts, so how long a boot may take is visible in the config you
 * are reading rather than one import away.
 */
export const localWebServer = {
  // `serve:coverage` is `ng serve` with live-reload and HMR off. Both inject a
  // client bundle and can re-serve a chunk mid-test, which V8 reports as a
  // second script for the same URL — noise the coverage merge would have to
  // reconcile. Sourcemaps come from `ng serve`'s default `development`
  // configuration either way; that is what makes the merge resolvable at all.
  command: process.env['E2E_COVERAGE'] === '1' ? 'npm run serve:coverage' : 'npm start',
  url: process.env['E2E_BASE_URL'] ?? 'http://localhost:4200',
  cwd: '..',
  reuseExistingServer: !process.env['CI'],
};

/**
 * Name of a one-login-per-run authentication project, for environment configs
 * that authenticate once in a `setup` project and hand its saved session to
 * every browser project via `storageState` (Playwright's documented auth
 * pattern, https://playwright.dev/docs/auth). A constant because two places
 * must agree on the name: the config declares it and lists it in each browser
 * project's `dependencies`, while `serialCompanion` must recognise it to
 * leave it unfiltered. A typo in either would silently drop authentication.
 */
export const SETUP_PROJECT = 'setup';

/**
 * The `@serial` tag: this test cannot share its backend state with a test
 * running beside it.
 *
 * The bar is deliberately high. Almost nothing needs it — every seeded entity
 * carries a UUID, so a spec that follows "assert only about entities your own
 * test created" (see e2e/README.md) is parallel-safe even on a config where
 * every test shares one pre-provisioned account. Reach for this only
 * when a test genuinely cannot be scoped: it asserts a global count, drives a
 * bulk action over "everything", or changes an account-wide setting that
 * another test would read.
 *
 * Prefer narrowing the assertion. A `@serial` test is one that no longer runs
 * in the main suite, which is a real cost in feedback time and in how easily
 * it is forgotten.
 */
export const SERIAL_TAG = /@serial/;

/**
 * Derives the serial companion of an environment config: same backend, same
 * timeouts, same artifact retention, but one worker and *only* the `@serial`
 * tests — the exact complement of what the parallel config excludes.
 *
 * One helper rather than four hand-written configs so a setting can never
 * apply to a backend's parallel run and not its serial one. Every backend has
 * a companion even where the hazard is mostly theoretical: on mock/local each
 * test registers its own account, so a `@serial` test there is only serial
 * because it was *designed* that way, and having the recipe present means the
 * answer to "how do I run these serially here?" is the same everywhere.
 *
 * `grep` and `grepInvert` are both derived from the parallel config rather
 * than written fresh:
 *
 * - `grepInvert` keeps the exclusions that still apply (@visual, @mock-only,
 *   @mutates-account …) and drops only `@serial`, which is what turns
 *   "exclude these" into "run exactly these".
 * - `grep` must *intersect* with any opt-in the parallel config already has,
 *   never replace it. A config aimed at a production deployment runs
 *   `grep: /@prod-safe/` — an allowlist, because the alternative is a tag
 *   typo pointing a destructive test at production. Overwriting that with
 *   `/@serial/` would do exactly what the allowlist exists to prevent, so
 *   the two are combined into a "both must be present" match instead.
 */
/**
 * Drops `@serial` from an inherited `grepInvert`, returning `undefined` when
 * nothing is left.
 *
 * Splits on the alternation rather than doing surgery on the regex source,
 * because the obvious `source.replace(/\|?@serial/, '')` has two failure modes
 * and both produce a regex that matches *everything* — which, as a
 * `grepInvert`, silently excludes the entire suite:
 *
 * - `/@serial/` alone becomes `new RegExp('')` === `/(?:)/`. This is what a
 *   `grep: /@prod-safe/` allowlist config inherits, so its serial companion
 *   could never select a test even once something was tagged
 *   `@prod-safe @serial`.
 * - `/@serial|@foo/` becomes `/|@foo/`, whose empty first branch matches the
 *   empty string.
 *
 * Neither is hypothetical-only: the first shipped. Returning `undefined` for
 * an empty result is the correct "exclude nothing" value; an empty regex is
 * the opposite.
 */
function grepInvertWithoutSerial(inherited: PlaywrightTestConfig['grepInvert']): PlaywrightTestConfig['grepInvert'] {
  if (!(inherited instanceof RegExp)) return inherited;
  const remaining = inherited.source
    .split('|')
    .filter(alternative => alternative !== SERIAL_TAG.source);
  return remaining.length ? new RegExp(remaining.join('|'), inherited.flags) : undefined;
}

export function serialCompanion(parallel: PlaywrightTestConfig): PlaywrightTestConfig {
  // Playwright matches `grep` against "<title> <tags>", so a pair of
  // lookaheads is how you say "carries both tags" with a single regex.
  const existingGrep = parallel.grep;
  const grep = existingGrep instanceof RegExp ? new RegExp(`(?=.*${existingGrep.source})(?=.*${SERIAL_TAG.source})`) : SERIAL_TAG;
  const grepInvert = grepInvertWithoutSerial(parallel.grepInvert);

  // Filter per project, not config-wide.
  //
  // A config-level `grep` applies to every project including `setup`, whose
  // login spec is deliberately untagged — so a `@serial` grep filtered the
  // authentication out of the run while the browser projects still declared
  // `dependencies: ['setup']` and still loaded its storageState. The result was
  // a serial run that reused whatever `.auth/<env>.json` happened to be on
  // disk, or none at all. It only ever appeared to work because a parallel run
  // had usually written that file first.
  //
  // Setup projects therefore keep no tag filter; everything else carries it.
  const projects = (parallel.projects ?? []).map(project =>
    project.name === SETUP_PROJECT ? project : { ...project, grep, grepInvert },
  );

  return {
    ...parallel,
    // The whole point. `fullyParallel` is set too so that a future
    // `workers: undefined` can't quietly reintroduce concurrency within a file.
    workers: 1,
    fullyParallel: false,
    projects,
    // Cleared at config level so only the per-project filters above apply.
    grep: undefined,
    grepInvert: undefined,
  };
}
