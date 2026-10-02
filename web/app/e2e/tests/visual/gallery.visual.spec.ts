import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

/**
 * @functional-coverage tests/functional/gallery/gallery.spec.ts
 *
 * Mode switching, the source and sort segments, and the empty state are covered
 * behaviourally by the functional gallery spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

/**
 * The gallery header was one of the three surfaces reshaped by the mobile
 * responsive pass (composer, chat header, gallery header) and had no pixel
 * contract of any kind. The empty state is the deterministic one: a fresh
 * account has no images, so the toolbar, the mode switch and the two
 * segmented filter groups are the whole screen.
 */
test(
  'gallery, empty for a fresh account',
  { tag: ['@visual', '@mock-only'] },
  async ({ page, galleryPage, shell, userWithPersonality }) => {
    await galleryPage.navigateTo();

    await expect(galleryPage.heading).toBeVisible();
    await expect(galleryPage.modeSwitch).toBeVisible();
    await expect(galleryPage.sourceGroup).toBeVisible();
    await expect(galleryPage.sortGroup).toBeVisible();
    // Wait the loading status out rather than screenshotting a spinner.
    await expect(galleryPage.loadingStatus).toHaveCount(0);
    await expect(galleryPage.emptyMessage).toBeVisible();

    await expect(page).toHaveScreenshot('gallery-empty.png', {
      animations: 'disabled',
      mask: [...commonMasks(page), shell.recentThreadsSection],
    });
  },
);
