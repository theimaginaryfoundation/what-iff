import { defineConfig } from '@playwright/test';
import { LOCAL_TEST_TIMEOUT } from './timeouts';
import { baseConfig, localWebServer } from './playwright.config.base';

/**
 * Local backend running a real (small) model, LLM_BACKEND=local. Output is
 * non-deterministic, so @visual baselines and @mock-only tests are excluded.
 */
export default defineConfig({
  ...baseConfig,
  // Same budget as mock: a real small model, but on this machine with no
  // network hop, and a measured full run stayed inside it comfortably. This
  // and the mock config bound the shared call-level budgets.
  timeout: LOCAL_TEST_TIMEOUT,
  use: {
    ...baseConfig.use,
    baseURL: process.env['E2E_BASE_URL'] ?? 'http://localhost:4200',
    // Full retention — same reasoning as the mock config: local stack, local
    // throwaway accounts.
    trace: 'retain-on-failure',
    screenshot: 'only-on-failure',
    video: 'retain-on-failure',
  },
  grepInvert: /@visual|@mock-only/,
  // A cold `npm start` compiles the whole app, which is far slower than
  // anything the tests themselves wait on — hence the outlier value.
  webServer: { ...localWebServer, timeout: 120 * 1000 },
});
