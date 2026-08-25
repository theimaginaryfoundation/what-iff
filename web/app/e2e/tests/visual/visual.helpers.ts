import type { Locator, Page } from '@playwright/test';

/**
 * Reduced motion used to live here as a `prepareForScreenshot()` call every
 * spec had to remember. It is now a context-level `use` option on the projects
 * in playwright.config.mock-llm.visual.ts, which applies before the first paint and
 * cannot be forgotten.
 */

/** Default masks for chrome that appears on every authenticated screen. */
export function commonMasks(page: Page): Locator[] {
  return [
    // "Open profile for <username>" sidebar button — renders the account's
    // username/initials, which differ per test run.
    page.getByRole('button', { name: /^Open profile for/ }),
  ];
}
