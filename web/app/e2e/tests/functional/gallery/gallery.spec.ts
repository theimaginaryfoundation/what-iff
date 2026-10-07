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
  await expect(galleryPage.sortSegment('Created')).toBeVisible();
  // Images only carry a created date, so there is no "Last used" sort to offer.
  await expect(galleryPage.sortGroup.getByRole('button', { name: /last used/i })).toHaveCount(0);

  // Selecting a segment must not tear the page down — the results summary is
  // rendered from the same branch, so it standing is the signal the view
  // re-filtered rather than errored. That the *selected* segment is exposed
  // to assistive tech is a separate claim, asserted in tests/a11y/.
  await galleryPage.filterBySource('Imported');
  await expect(galleryPage.resultsSummary).toBeVisible();
  await galleryPage.sortBy('Created');
  await expect(galleryPage.resultsSummary).toBeVisible();
});

test('filters by type, with the empty state saying what was looked for', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  for (const type of ['All', 'Images', 'Files'] as const) {
    await expect(galleryPage.typeSegment(type)).toBeVisible();
  }
  await expect(galleryPage.typeSegment('All')).toHaveAttribute('aria-pressed', 'true');

  // Narrowed by a name nothing has, so the empty state shows on a shared account too.
  await galleryPage.search(`no-such-file-${shortId()}`);
  await galleryPage.filterByType('Files');
  await expect(galleryPage.typeSegment('Files')).toHaveAttribute('aria-pressed', 'true');
  await expect(galleryPage.emptyMessage).toHaveText(/^No files match/);

  await galleryPage.filterByType('Images');
  await expect(galleryPage.emptyMessage).toHaveText('No images match these filters yet.');
});

test('a search that matches nothing shows the empty state', async ({ galleryPage, userWithPersonality }) => {
  await galleryPage.navigateTo();

  // A UUID nothing can match, so this holds on a shared account whose library
  // is full of other runs' images.
  await galleryPage.search(`no-such-image-${shortId()}`);

  await expect(galleryPage.emptyMessage).toBeVisible();
  await expect(galleryPage.grid).toBeHidden();
});
