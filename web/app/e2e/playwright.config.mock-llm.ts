import { defineConfig } from '@playwright/test';
import { MOCK_TEST_TIMEOUT } from './timeouts';
import { baseConfig, localWebServer } from './playwright.config.base';

// Set by e2e/scripts/visual-docker.sh on a non-amd64 host, where it serves the
// app on the host and points the container's Chromium at it (see the webServer
// note below). It requires `baseURL` to stay http://localhost:4200 so the app
// origin — and thus CORS/cookies and the committed baselines — is unchanged;
// that only holds while E2E_BASE_URL is unset, so guard the contradiction
// rather than silently rewriting the origin.
const reuseHostWebServer = process.env['E2E_REUSE_HOST_WEBSERVER'] === '1';
if (reuseHostWebServer && process.env['E2E_BASE_URL']) {
  throw new Error(
    'E2E_REUSE_HOST_WEBSERVER=1 expects E2E_BASE_URL to be unset so baseURL stays ' +
      'http://localhost:4200 (the app origin the host-served dev server and CORS rely on).',
  );
}

/**
 * Default config: local backend running with LLM_BACKEND=mock (deterministic
 * replies), Playwright starting only the Angular dev server. The backend
 * itself must already be up on :8080 — see e2e/README.md.
 *
 * Everything except the pixel baselines. Those need context-level reduced
 * motion to be stable, which is a property of the whole run rather than of a
 * spec, so they live in playwright.config.mock-llm.visual.ts — which extends this file
 * and selects exactly what the grepInvert below excludes. Run both
 * (`npm run e2e && npm run e2e:mock-llm:visual`) to cover the suite against the mock
 * backend.
 */
export default defineConfig({
  ...baseConfig,
  // One test, end to end. Replies are echoed in-process here, so nothing is
  // slow but the browser. 45s is 1.5x the slowest test measured in the CI
  // container (30.3s — the register-through-chat journey), against a 12.1s
  // median. This and the local config are the tightest of the four, so they
  // bound the shared call-level budgets in e2e/timeouts.ts.
  timeout: MOCK_TEST_TIMEOUT,
  use: {
    ...baseConfig.use,
    baseURL: process.env['E2E_BASE_URL'] ?? 'http://localhost:4200',
    // Full retention: everything here is a throwaway local account against a
    // local stack, so artifacts are cheap to keep and the most useful to have.
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  // Complement of playwright.config.mock-llm.visual.ts — see the note above.
  grepInvert: /@visual/,
  // A cold `npm start` compiles the whole app, which is far slower than
  // anything the tests themselves wait on — hence the outlier value.
  //
  // `E2E_REUSE_HOST_WEBSERVER=1` omits the managed dev server entirely: the
  // caller has already started (and waited on) an Angular dev server elsewhere
  // and is routing the browser to it — e.g. `visual-docker.sh` on an arm64
  // host, which serves the app natively and remaps `localhost:4200` into the
  // amd64 render container via `--host-resolver-rules` to dodge an emulated
  // build. `baseURL` stays `localhost:4200` so the app's origin (and thus
  // CORS/cookies) is unchanged. When set, `E2E_BASE_URL` must be left unset so
  // that default holds.
  ...(reuseHostWebServer ? {} : { webServer: { ...localWebServer, timeout: 120 * 1000 } }),
});
