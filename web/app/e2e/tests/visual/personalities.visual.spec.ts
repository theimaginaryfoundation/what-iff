import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

test.describe('personalities screens', () => {
  test(
    'empty state for a fresh user',
    { tag: ['@visual', '@mock-only'] },
    async ({ authenticatedPage: page, personalitiesPage, shell }) => {
      await shell.dismissAnnouncementIfPresent();
      await personalitiesPage.navigateTo();
      await shell.dismissAnnouncementIfPresent();

      await expect(page).toHaveScreenshot('personalities-empty.png', {
        animations: 'disabled',
        mask: commonMasks(page),
      });
    },
  );

  test(
    'personality detail page',
    { tag: ['@visual', '@mock-only'] },
    async ({ authenticatedPage: page, personalitiesPage, personalityDetailPage, shell }) => {
      // Fixed, non-time-based name — a Date.now()-suffixed name (the pattern
      // used elsewhere in this suite) would break the baseline on every run.
      const personalityName = 'E2E Visual Persona';

      await shell.dismissAnnouncementIfPresent();
      await personalitiesPage.navigateTo();
      await shell.dismissAnnouncementIfPresent();
      await personalitiesPage.openCreateManually();
      await personalitiesPage.createManually(
        personalityName,
        'You are a calm, precise assistant used only for visual regression testing. Your answers are always short.',
      );

      await expect(page).toHaveURL(/\/personality\/[^/]+$/);
      await expect(personalityDetailPage.heading(personalityName)).toBeVisible();
      // The "DEFAULT" badge depends on a user-preferences fetch that trails
      // the personality-detail navigation; a fresh user's first personality
      // always ends up default, so wait for that badge rather than racing it —
      // otherwise the screenshot flakes between the badge/accent-ring being
      // rendered and not, depending on which request wins.
      await expect(page.getByText('Default', { exact: true })).toBeVisible();

      await expect(page).toHaveScreenshot('personality-detail.png', {
        animations: 'disabled',
        mask: commonMasks(page),
        // The avatar's accent ring keys off the same async default-personality
        // status as the "Default" badge (waited for above) but settles its
        // own styling slightly after the badge text renders — a small,
        // observed-in-practice trailing race rather than a masking gap.
        maxDiffPixelRatio: 0.02,
      });
    },
  );
});
