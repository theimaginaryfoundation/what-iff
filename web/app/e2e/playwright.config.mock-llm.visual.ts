import { defineConfig, devices } from '@playwright/test';
import mockConfig from './playwright.config.mock-llm';
import { chromiumHostResolverUse } from './playwright.config.base';

/**
 * Pixel baselines. Extends the mock config — visual comparison needs the
 * deterministic in-process LLM, so everything about the backend, timeouts and
 * artifact retention is inherited rather than restated; only what makes a run
 * *reproducible at the pixel level* is overridden here.
 *
 * Reduced motion is set on the projects below rather than called per-spec
 * (the old `prepareForScreenshot()` helper). A context-level option applies
 * before the first paint, whereas a helper could only run after the page had
 * already been navigated — and, being opt-in, was one forgotten line away from
 * a silently flaky baseline. It is not possible to forget a project option.
 *
 * WebKit is absent on purpose: its font rasterization differs enough to need
 * its own baseline set, which the suite doesn't maintain.
 *
 * `chromiumHostResolverUse` has to be carried through explicitly: the
 * `e2e:mock-llm:visual:docker*` recipes run Chromium inside the Playwright container,
 * where `localhost:8080` is the container rather than the host, and without
 * that remap every request in a baseline run would hit nothing.
 */
export default defineConfig({
  ...mockConfig,
  // Only the visual specs. The mock config excludes them for the same reason,
  // so the two selections are complements and no spec runs twice.
  //
  // Selected with `testMatch` rather than by narrowing `testDir` to
  // `./tests/visual`: `testDir` is recorded in every blob report, and
  // `playwright merge-reports` refuses to merge blobs whose recorded test
  // directories disagree ("Blob reports being merged were recorded with
  // different test directories"). Sharded CI runs the visual project and the
  // functional projects in separate jobs and merges their blobs, so both must
  // report the same root. Keeping the inherited `./tests` and filtering here
  // makes that true by construction.
  testMatch: /tests[\\/]visual[\\/].*\.spec\.ts$/,
  grepInvert: undefined,
  projects: [
    {
      name: 'chromium-desktop',
      use: {
        ...devices['Desktop Chrome'],
        ...chromiumHostResolverUse,
        reducedMotion: 'reduce',
      },
    },
    {
      name: 'chromium-mobile',
      use: {
        ...devices['Pixel 7'],
        ...chromiumHostResolverUse,
        reducedMotion: 'reduce',
      },
    },
  ],
});
