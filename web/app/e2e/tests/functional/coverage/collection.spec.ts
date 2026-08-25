import {
  test,
  expect,
  attachCoverage,
  coverageEntryCount,
  coverageLostNavigations,
  expectedRecordedScripts,
} from '../../../fixtures';

/**
 * Acceptance tests for the frontend coverage pipeline itself (see
 * `e2e/fixtures/coverage.ts`). They assert the properties the pipeline cannot
 * assume, each of which it got wrong at first.
 *
 * The other half of the acceptance criteria — that unvisited files land in the
 * report at 0% and that the merged report is non-empty and correctly rooted —
 * are properties of the merged lcov rather than of any one test, so they live in
 * `e2e/scripts/check-coverage.mjs` and run in the merge job.
 *
 * All are ordinary navigations, so they are cheap and still run with
 * `E2E_COVERAGE` unset — they just have nothing left to assert then.
 */

/**
 * V8 counts do not survive a full-document navigation.
 * `Profiler.takePreciseCoverage` only reports scripts V8 still holds, and the
 * previous document's are gone, so `resetOnNavigation: false` is not enough —
 * the fixture banks and restarts around `goto`/`reload`/`goBack`/`goForward`,
 * while the outgoing document is still alive.
 *
 * This executes an identifiable lazy component either side of a second
 * `page.goto()`. `check-coverage.mjs` then asserts both survive in the merged
 * lcov; without the banking, only `register.component.ts` does.
 *
 * Signed out on purpose, in a context of its own rather than the `page`
 * fixture's: `/auth/login` and `/auth/register` are behind `guestGuard`, and a
 * config with a one-login `setup` project starts every browser project from
 * the saved session, which would bounce both navigations to
 * `/chat` and fail the headings. `storageState: undefined` is what makes the
 * context signed out — `browser.newContext()` inherits the config's `use`
 * options, session included, unless a call site overrides them.
 */
test('lazy components either side of a full navigation both run', async ({ browser }) => {
  const context = await browser.newContext({ storageState: undefined });
  try {
    const page = await context.newPage();
    // Built by hand, so nothing has enrolled it in coverage — and enrolment is
    // what installs the navigation banking this test exists to exercise.
    await attachCoverage(page);

    await page.goto('/auth/login');
    await expect(page.getByRole('heading', { name: 'Sign in to your account' })).toBeVisible();

    await page.goto('/auth/register');
    await expect(page.getByRole('heading', { name: 'Create your account' })).toBeVisible();
  } finally {
    // `browser` is worker-scoped, so an unclosed context outlives the test.
    await context.close();
  }
});

/**
 * The fixtures only reach pages the `page`/`context` fixtures created, so a test
 * that builds its own context — `profile.spec.ts` does, to verify a password
 * change from a clean session — has to enrol its page by hand. This asserts that
 * `attachCoverage` on such a page actually records something.
 */
test('a page from a hand-built context is recorded once enrolled', async ({ browser, browserName }) => {
  // Signed out for the same reason as the test above: `/auth/login` is behind
  // `guestGuard`, and a deployed run would otherwise inherit its saved session.
  const context = await browser.newContext({ storageState: undefined });
  try {
    const page = await context.newPage();
    await attachCoverage(page);

    await page.goto('/auth/login');
    await expect(page.getByRole('heading', { name: 'Sign in to your account' })).toBeVisible();

    // The floor is 0 wherever collection is inert, so this stays a plain
    // second-context smoke test with `E2E_COVERAGE` unset or on webkit.
    const entries = await coverageEntryCount(page);
    expect(entries, 'first-party scripts recorded from the hand-built page').toBeGreaterThanOrEqual(
      expectedRecordedScripts(browserName),
    );
  } finally {
    await context.close();
  }
});

/**
 * The loss counter has to read zero on a run that navigates normally, or it is
 * noise rather than a signal.
 *
 * The banking only sees navigations the *test* starts, because that is the only
 * boundary at which the renderer is still alive to be profiled — see
 * `patchNavigation` in `e2e/fixtures/coverage.ts` for why intercepting the
 * document request instead deadlocks. Everything else (a link click, a form
 * submit, a `location.href =`) is counted rather than caught, and the merge job
 * reports the total as a qualifier on the percentage. That reporting is only
 * worth anything if the baseline is zero, which is what this pins.
 *
 * Deliberately not asserted here: the counter's positive path. A spec that
 * performed a page-initiated navigation would leave a permanent 1 in the
 * run-wide total, and a canary that is always tripped cannot tell anyone that
 * something started going wrong.
 */
test('navigations the test performs are banked, and nothing is counted lost', async ({
  browser,
  browserName,
}) => {
  const context = await browser.newContext({ storageState: undefined });
  try {
    const page = await context.newPage();
    await attachCoverage(page);

    await page.goto('/auth/login');
    await expect(page.getByRole('heading', { name: 'Sign in to your account' })).toBeVisible();

    // `reload` as well as `goto`: it is the wrapped method the rest of the
    // suite exercises least, and it replaces the document just as thoroughly.
    await page.reload();
    await expect(page.getByRole('heading', { name: 'Sign in to your account' })).toBeVisible();

    expect(coverageLostNavigations(), 'navigations that replaced a document unbanked').toBe(0);
    // Asserted alongside the counter: the counter alone would also read 0 if the
    // CDP session had failed to attach and no navigation were seen at all.
    const entries = await coverageEntryCount(page);
    expect(entries, 'first-party scripts recorded after the reload').toBeGreaterThanOrEqual(
      expectedRecordedScripts(browserName),
    );
  } finally {
    await context.close();
  }
});
