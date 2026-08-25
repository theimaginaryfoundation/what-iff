import { test, expect, seedName } from '../../../fixtures';

/** Memories list + detail smoke coverage for the MemoriesPage/MemoryDetailPage POMs. */

test('lists seeded memories and filters by level', async ({ memoriesPage, seed, userWithPersonality }) => {
  // FIXME(memory filter tabs race): each filter-tab click fires 2-3
  // concurrent load() calls with no request cancellation. When responses
  // arrive out of order the stale result overwrites the correct one,
  // showing "No memories found" after switching back to the All tab. ~50%
  // failure rate on webkit-mobile.
  test.fixme(true, 'Filter tabs fire concurrent non-cancelling API calls — see #37');
  const memories = await seed.memories(3);
  await memoriesPage.navigateTo();

  await expect(memoriesPage.heading).toBeVisible();
  for (const memory of memories) {
    await expect(memoriesPage.card(memory.content as string)).toBeVisible();
  }

  // Everything seeded here is level "global", so the Global tab keeps them and
  // the Thread tab drops them.
  await memoriesPage.filterBy('Global');
  await expect(memoriesPage.card(memories[0].content as string)).toBeVisible();

  // Asserted per-memory rather than via the grid's empty state: the empty
  // state is a claim about the *account's* whole list, and on the deployed
  // configs every test shares one Cognito account, so a thread-level memory
  // belonging to some other worker makes this fail for a reason that has
  // nothing to do with the filter. Each seeded card being hidden is the claim
  // the test actually means to make, and it holds either way.
  await memoriesPage.filterBy('Thread');
  for (const memory of memories) {
    await expect(memoriesPage.card(memory.content as string)).toBeHidden();
  }

  await memoriesPage.filterBy('All');
  await expect(memoriesPage.card(memories[0].content as string)).toBeVisible();
});

test('edits a memory inline', async ({ memoriesPage, seed, userWithPersonality }) => {
  const [memory] = await seed.memories(1);
  await memoriesPage.navigateTo();

  const original = memory.content as string;
  const updated = `${original} (edited)`;

  await memoriesPage.startEdit(original);
  await expect(memoriesPage.editingCard).toBeVisible();

  await memoriesPage.saveEdit(updated);
  await expect(memoriesPage.card(updated)).toBeVisible();
});

test('deletes a memory through the confirmation modal', async ({ memoriesPage, seed, userWithPersonality }) => {
  const [keep, remove] = await seed.memories(2);
  await memoriesPage.navigateTo();

  await memoriesPage.requestDelete(remove.content as string);
  await expect(memoriesPage.deleteDialogHeading).toContainText('Delete 1 memory?');

  await memoriesPage.confirmDelete();

  await expect(memoriesPage.card(remove.content as string)).toBeHidden();
  await expect(memoriesPage.card(keep.content as string)).toBeVisible();
});

test('opens a memory detail page and navigates back', async ({ memoryDetailPage, seed, userWithPersonality }) => {
  const [memory] = await seed.memories(1);

  // Reached by URL: memory cards carry Edit/Delete only, nothing links here.
  await memoryDetailPage.navigateTo(memory.id as string);

  await expect(memoryDetailPage.contentTextarea).toHaveValue(memory.content as string);
  await expect(memoryDetailPage.metadataHeading).toBeVisible();

  await memoryDetailPage.goBack();
  await expect(userWithPersonality.page).toHaveURL(/\/memories$/);
});

test('edits a memory content from its detail page and the change persists', async ({ memoryDetailPage, page, seed, userWithPersonality }) => {
  // The detail page's own editor. The card's inline edit and this page's
  // delete are covered above; nothing exercised saving from here — it is a
  // separate form (memory-form.component) from the card's.
  const [memory] = await seed.memories(1);

  await memoryDetailPage.navigateTo(memory.id as string);
  await expect(memoryDetailPage.contentTextarea).toHaveValue(memory.content as string);

  const updated = `${memory.content as string} (edited on the detail page)`;
  // `save()` clicks and returns while the PATCH is still in flight, and the
  // re-read below is a hard navigation, which would abort it. Wait on the
  // response rather than on the click.
  const saved = page.waitForResponse(
    r => r.request().method() === 'PATCH' && r.url().includes(`/api/memory/${memory.id as string}`) && r.ok(),
  );
  await memoryDetailPage.setContent(updated);
  await memoryDetailPage.save();
  await saved;

  // Re-read rather than assert the form we just typed into: the question is
  // whether the save round-tripped, and the filled form looks identical either way.
  await memoryDetailPage.navigateTo(memory.id as string);
  await expect(memoryDetailPage.contentTextarea).toHaveValue(updated);
});

test('changes a memory level from its detail page', async ({ memoryDetailPage, page, seed, userWithPersonality }) => {
  // Real bug found via exploration: the level select offers all four levels,
  // but the form posts only `{content, level}` while the datastore requires a
  // scope id for every level except global — so Thread and Summary can never
  // be saved from this page, and Personality only if already pinned.
  test.fixme(true, 'Detail-page level select offers levels the save cannot satisfy — see #72');

  const [memory] = await seed.memories(1);

  await memoryDetailPage.navigateTo(memory.id as string);
  await expect(memoryDetailPage.levelSelect).toHaveValue('global');

  const saved = page.waitForResponse(
    r => r.request().method() === 'PATCH' && r.url().includes(`/api/memory/${memory.id as string}`) && r.ok(),
  );
  await memoryDetailPage.levelSelect.selectOption('summary');
  await memoryDetailPage.save();
  await saved;

  await memoryDetailPage.navigateTo(memory.id as string);
  await expect(memoryDetailPage.levelSelect).toHaveValue('summary');
});

test('deletes a memory from its detail page', async ({ memoriesPage, memoryDetailPage, seed, userWithPersonality }) => {
  const [memory] = await seed.memories(1);

  await memoryDetailPage.navigateTo(memory.id as string);
  await memoryDetailPage.requestDelete();
  await memoryDetailPage.confirmDelete();

  await expect(userWithPersonality.page).toHaveURL(/\/memories(\?|$)/);
  await expect(memoriesPage.card(memory.content as string)).toBeHidden();
});

test('sorts memories by creation time', { tag: '@serial' }, async ({ memoriesPage, seed, userWithPersonality }) => {
  // @serial: the memories page has no free-text search to narrow the grid to
  // this test's own rows (MemoriesPage's own doc comment), so `cards` below
  // is unavoidably an account-wide count/order claim — unsafe next to any
  // other worker's memories on a shared deployed account.
  //
  // Two separate calls (not seed.memories(2)) so the two rows get distinct
  // created_at timestamps to sort by, rather than one batch write that may
  // land at the same instant.
  const [older] = await seed.memories(1, { content: seedName('memory-older') });
  const [newer] = await seed.memories(1, { content: seedName('memory-newer') });
  await memoriesPage.navigateTo();

  await expect(memoriesPage.card(older.content as string)).toBeVisible();
  await expect(memoriesPage.card(newer.content as string)).toBeVisible();

  await memoriesPage.sortBy('created_desc');
  await expect(memoriesPage.sortSelect).toHaveValue('created_desc');
  await expect(memoriesPage.cards).toHaveCount(2);
  let order = await memoriesPage.cards.allTextContents();
  let newerIndex = order.findIndex(text => text.includes(newer.content as string));
  let olderIndex = order.findIndex(text => text.includes(older.content as string));
  expect(newerIndex).toBeLessThan(olderIndex);

  await memoriesPage.sortBy('created_asc');
  await expect(memoriesPage.sortSelect).toHaveValue('created_asc');
  await expect(memoriesPage.cards).toHaveCount(2);
  order = await memoriesPage.cards.allTextContents();
  newerIndex = order.findIndex(text => text.includes(newer.content as string));
  olderIndex = order.findIndex(text => text.includes(older.content as string));
  expect(olderIndex).toBeLessThan(newerIndex);
});

test('paginates the memory list', { tag: '@serial' }, async ({ memoriesPage, seed, userWithPersonality }) => {
  // @serial: same reason as "sorts memories by creation time" above — no
  // search to narrow the grid, and the page-count math below only holds if
  // this test's 25 rows are the whole account, which is only true alone.
  //
  // Page size is 24 (core/services/memory-view.service.ts) — 25 rows force a
  // second page.
  await seed.memories(25);
  await memoriesPage.navigateTo();

  await expect(memoriesPage.pageIndicator).toHaveText('Page 1 of 2');
  await expect(memoriesPage.previousPageButton).toBeDisabled();
  await expect(memoriesPage.cards).toHaveCount(24);

  await memoriesPage.nextPage();
  await expect(memoriesPage.pageIndicator).toHaveText('Page 2 of 2');
  await expect(memoriesPage.cards).toHaveCount(1);

  await memoriesPage.previousPage();
  await expect(memoriesPage.pageIndicator).toHaveText('Page 1 of 2');
});
