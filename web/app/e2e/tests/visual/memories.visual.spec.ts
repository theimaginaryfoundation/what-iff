import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

/**
 * @functional-coverage tests/functional/memory/memories.spec.ts, tests/functional/memory/responsive-layout.spec.ts
 *
 * Listing, filtering, focus rail/modal, batch actions, and responsive layout for
 * the Memory Manager screens these baselines picture are covered by the
 * functional memories + responsive-layout specs.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

/**
 * Memory Manager shell + list tab. Dates on cards change day-to-day, so
 * populated screenshots mask `.memory-card__date`.
 *
 * Uses `userWithPersonality` so personalitySetupGuard does not bounce us
 * off `/memories` (same pattern as the functional memory suite).
 */
test.describe('memory manager screens', () => {
  test(
    'empty state',
    { tag: ['@visual', '@mock-only'] },
    async ({ userWithPersonality, memoriesPage }) => {
      const page = userWithPersonality.page;
      await memoriesPage.navigateTo();

      await expect(memoriesPage.heading).toBeVisible();
      await expect(memoriesPage.emptyMessage).toBeVisible();

      await expect(page).toHaveScreenshot('memory-manager-empty.png', {
        animations: 'disabled',
        mask: commonMasks(page),
        // Sidebar avatar mask + filter chrome can shift a few hundred AA pixels
        // between CI runners and the amd64 docker baseline image.
        maxDiffPixelRatio: 0.02,
      });
    },
  );

  test(
    'populated list with fixed seeded content',
    { tag: ['@visual', '@mock-only'] },
    async ({ userWithPersonality, memoriesPage, seed }) => {
      const page = userWithPersonality.page;
      // Fixed copy so the baseline does not churn on every run.
      await seed.memories(1, {
        content: 'E2E visual memory: prefers concise answers under 150 words.',
        level: 'global',
        starred: true,
      });
      await seed.memories(1, {
        content: 'E2E visual memory: birthday is March 3.',
        level: 'global',
        starred: false,
      });

      await memoriesPage.navigateTo();

      await expect(memoriesPage.card('prefers concise answers under 150 words')).toBeVisible();
      await expect(memoriesPage.card('birthday is March 3')).toBeVisible();

      await expect(page).toHaveScreenshot('memory-manager-list.png', {
        animations: 'disabled',
        mask: [...commonMasks(page), page.locator('.memory-card__date')],
        maxDiffPixelRatio: 0.02,
      });
    },
  );

  test(
    'merge history tab chrome',
    { tag: ['@visual', '@mock-only'] },
    async ({ userWithPersonality, memoriesPage }) => {
      const page = userWithPersonality.page;
      await memoriesPage.navigateTo();
      await memoriesPage.mergeHistoryTab.click();

      await expect(memoriesPage.mergeHistoryTab).toHaveAttribute('aria-selected', 'true');
      await expect(page.getByRole('heading', { name: 'Merge history' })).toBeVisible();

      await expect(page).toHaveScreenshot('memory-manager-merge-history.png', {
        animations: 'disabled',
        mask: commonMasks(page),
        maxDiffPixelRatio: 0.02,
      });
    },
  );
});
