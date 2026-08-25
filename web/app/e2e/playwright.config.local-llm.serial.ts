import { defineConfig } from '@playwright/test';
import parallelConfig from './playwright.config.local-llm';
import { serialCompanion } from './playwright.config.base';

/**
 * The `@serial` tests against a local real model — one worker, everything else inherited
 * from `playwright.config.local-llm.ts`.
 *
 * As with mock-llm, accounts are per-test here; this exists so the recipe is identical across backends.
 *
 * See `serialCompanion()` for what "serial" buys and why the bar for tagging
 * a test `@serial` is high.
 */
export default defineConfig(serialCompanion(parallelConfig));
