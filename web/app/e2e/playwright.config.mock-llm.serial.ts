import { defineConfig } from '@playwright/test';
import parallelConfig from './playwright.config.mock-llm';
import { serialCompanion } from './playwright.config.base';

/**
 * The `@serial` tests against the mock LLM backend — one worker, everything else inherited
 * from `playwright.config.mock-llm.ts`.
 *
 * Each test here registers its own account, so nothing is *forced* to be serial by the backend — a `@serial` test runs serially because of what it asserts, and this config is where you reproduce that locally with the same isolation the deployed runs give it.
 *
 * See `serialCompanion()` for what "serial" buys and why the bar for tagging
 * a test `@serial` is high.
 */
export default defineConfig(serialCompanion(parallelConfig));
