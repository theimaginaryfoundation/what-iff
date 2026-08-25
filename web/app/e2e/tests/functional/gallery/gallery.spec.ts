import { test, expect, shortId } from '../../../fixtures';

/**
 * Image gallery (`/gallery`) — the page had no e2e coverage at all.
 *
 * Everything here is reachable without an image in the library, which is the
 * only thing a hermetic run can assume: importing proxies the file to the
 * vendor Files API with no mock/local bypass (see the note on `GalleryPage`),
 * and generation is disabled on the same backends, so a mock-run account's
 * gallery is always empty. These cover the page's own controls rather than its
 * contents, which also keeps them honest on a shared deployed account.
 */

test('switches between Gallery and Expression Manager modes', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  await expect(galleryPage.heading).toHaveText('Gallery');
  await expect(galleryPage.searchInput).toBeVisible();

  await galleryPage.setMode('Expression Manager');
  await expect(galleryPage.heading).toHaveText('Expression Manager');
  // The search row and the source/sort segments live inside the gallery-mode
  // branch of the template, so they go away entirely rather than just disable.
  await expect(galleryPage.searchInput).toBeHidden();
  await expect(galleryPage.sourceSegment('All')).toBeHidden();

  await galleryPage.setMode('Gallery');
  await expect(galleryPage.heading).toHaveText('Gallery');
  await expect(galleryPage.searchInput).toBeVisible();
});

test('offers the source and sort segments in gallery mode', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  for (const source of ['All', 'Generated', 'Imported'] as const) {
    await expect(galleryPage.sourceSegment(source)).toBeVisible();
  }
  for (const sort of ['Created', 'Last used'] as const) {
    await expect(galleryPage.sortSegment(sort)).toBeVisible();
  }

  // Selecting a segment must not tear the page down — the results summary is
  // rendered from the same branch, so it standing is the signal the view
  // re-filtered rather than errored. That the *selected* segment is exposed
  // to assistive tech is a separate claim, asserted in tests/a11y/.
  await galleryPage.filterBySource('Imported');
  await expect(galleryPage.resultsSummary).toBeVisible();
  await galleryPage.sortBy('Last used');
  await expect(galleryPage.resultsSummary).toBeVisible();
});

test('a search that matches nothing shows the empty state', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  // A UUID nothing can match, so this holds on a shared account whose library
  // is full of other runs' images.
  await galleryPage.search(`no-such-image-${shortId()}`);

  await expect(galleryPage.emptyMessage).toBeVisible();
  await expect(galleryPage.grid).toBeHidden();
});
