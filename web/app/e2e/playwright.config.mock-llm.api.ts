import { defineConfig } from '@playwright/test';
import { baseConfig } from './playwright.config.base';
import { API_TEST_TIMEOUT } from './timeouts';

/**
 * The Playwright API-test suite (e2e/api-tests/): typed calls through
 * e2e/sdk + e2e/api-tests/fixtures.ts straight against a running Go API — no
 * browser, no Angular dev server (`webServer: undefined`). The API must
 * already be up (`make dev-up` / `make run-mock`, or started by whichever CI
 * job runs this config) — see e2e/README.md.
 *
 * Exactly one project and nothing here ever names Playwright's built-in
 * `page` fixture, so this project never launches Chromium — the whole point
 * of keeping this suite out of e2e/tests/ (see the testIgnore in
 * playwright.config.base.ts).
 *
 * `use.baseURL` is also published to `E2E_API_BASE_URL` — the variable
 * e2e/sdk/client.ts actually reads — so this config is the one place that
 * decides the target.
 */
const apiBaseUrl = process.env['E2E_API_BASE_URL'] ?? 'http://localhost:8080/api';
process.env['E2E_API_BASE_URL'] ??= apiBaseUrl;

export default defineConfig({
  ...baseConfig,
  testDir: './api-tests',
  // baseConfig excludes this directory from its own './tests' testDir (see
  // playwright.config.base.ts) — that has to be cleared here, or this
  // config's testDir would exclude the only tests it points at. It is
  // replaced rather than dropped: `api-tests/private/` is reserved for a
  // downstream overlay composing its own API specs on top of this tree, and
  // those need a backend this config does not describe (a real provider, a
  // billing service). Sweeping them up here would run them against the mock
  // and fail for reasons that have nothing to do with the code under test.
  testIgnore: /api-tests[\\/]private[\\/]/,
  timeout: API_TEST_TIMEOUT,
  use: {
    baseURL: apiBaseUrl,
  },
  projects: [{ name: 'api' }],
  webServer: undefined,
});
