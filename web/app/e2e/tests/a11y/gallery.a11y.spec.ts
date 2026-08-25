import { test, expect } from '../../fixtures';

/**
 * Gallery accessibility — the first spec in this directory.
 *
 * Scope note: this is a *state exposure* assertion, not an axe scan. The axe
 * harness this directory's README describes (`@axe-core/playwright`, failing
 * on serious/critical with a shared allowlist) is still unbuilt; the
 * dependency is not in package.json yet.
 *
 * These assertions live here rather than in
 * `functional/gallery/gallery.spec.ts` because what they protect is not the
 * feature's behaviour — it is whether the feature's state is perceivable by
 * anything other than a sighted user. The gallery's segmented controls
 * originally signalled the chosen option through a CSS class alone, which
 * looks correct on screen and is invisible to assistive tech. A functional
 * test would not have caught that, and did not.
 */

test('the gallery segmented controls expose which option is selected', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  // Source: exactly one pressed, and pressing another moves it.
  await expect(galleryPage.sourceSegment('All')).toHaveAttribute('aria-pressed', 'true');
  await galleryPage.filterBySource('Imported');
  await expect(galleryPage.sourceSegment('Imported')).toHaveAttribute('aria-pressed', 'true');
  await expect(galleryPage.sourceSegment('All')).toHaveAttribute('aria-pressed', 'false');

  // Sort: same contract.
  await expect(galleryPage.sortSegment('Created')).toHaveAttribute('aria-pressed', 'true');
  await galleryPage.sortBy('Last used');
  await expect(galleryPage.sortSegment('Last used')).toHaveAttribute('aria-pressed', 'true');
  await expect(galleryPage.sortSegment('Created')).toHaveAttribute('aria-pressed', 'false');
});

test('the gallery mode switch exposes which mode is active', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  await expect(galleryPage.modeButton('Gallery')).toHaveAttribute('aria-pressed', 'true');
  await expect(galleryPage.modeButton('Expression Manager')).toHaveAttribute('aria-pressed', 'false');

  await galleryPage.setMode('Expression Manager');
  await expect(galleryPage.modeButton('Expression Manager')).toHaveAttribute('aria-pressed', 'true');
  await expect(galleryPage.modeButton('Gallery')).toHaveAttribute('aria-pressed', 'false');
});

test('the gallery page and its controls carry accessible names', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  // Each of these was unnamed before, so none of them could be found or
  // described by a screen reader — and the POM had to reach for CSS classes.
  await expect(galleryPage.root).toBeVisible();
  await expect(galleryPage.modeSwitch).toBeVisible();
  await expect(galleryPage.sourceGroup).toBeVisible();
  await expect(galleryPage.sortGroup).toBeVisible();
  // A placeholder is not an accessible name; this resolves by label.
  await expect(galleryPage.searchInput).toBeVisible();
});
