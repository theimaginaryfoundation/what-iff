import { test, expect } from '../../../fixtures';

/**
 * Thread Manager smoke coverage — exercises the ThreadListPanel POM's main
 * path (list → rename → archive/restore → search → tags → bulk → delete).
 * Deeper coverage of sorting/paging is out of scope here.
 *
 * Every test names `userWithPersonality` without reading it: naming a fixture
 * is what runs it, and this one registers the account, seeds a personality and
 * lands on /chat. That last part is load-bearing — `personalitySetupGuard`
 * bounces personality-less accounts to /personality/getting-started, so an
 * account with no personality never reaches /chat at all.
 */

/**
 * Rename and archive/restore are two tests rather than one journey. Each
 * `narrowTo` is a search round-trip against the backend, and scoping every
 * assertion (which the shared deployed account requires) made the combined
 * version cost enough of them to exceed the 45s test budget on the slowest
 * project — it failed on `webkit-mobile` in CI while passing everywhere else.
 * Split, each half is comfortably inside the budget and a failure names which
 * operation broke.
 */
test('renames a thread', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  // FIXME(thread rename dblclick race): dblclick on the thread name races
  // with click-to-navigate — the click handler navigates away before the
  // browser fires the dblclick event, so edit mode is never entered.
  // Reliable on WebKit mobile, possible anywhere. Fix: PR #372.
  test.fixme(true, 'Thread-rename dblclick races with click-to-navigate — see #40');
  const thread = await seed.thread();
  await threadListPanel.navigateTo();

  await expect(threadListPanel.heading).toBeVisible();

  // Narrow before every row assertion — see narrowTo(). The unfiltered table
  // is the *account's* list, which on a deployed run also holds threads from
  // whatever else is running against the shared account.
  await threadListPanel.narrowTo(thread.name);
  await expect(threadListPanel.row(thread.name)).toBeVisible();

  const renamed = `${thread.name}-renamed`;
  await threadListPanel.rename(thread.name, renamed);
  await threadListPanel.narrowTo(renamed);
  await expect(threadListPanel.row(renamed)).toBeVisible();
});

test('archives and restores a thread', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();
  await threadListPanel.navigateTo();

  await threadListPanel.narrowTo(thread.name);
  await expect(threadListPanel.row(thread.name)).toBeVisible();

  await threadListPanel.archive(thread.name);
  await expect(threadListPanel.row(thread.name)).toBeHidden();

  // Re-narrow after each tab switch: the tab decides which set the table
  // draws from, the search box decides which of those rows are this test's.
  await threadListPanel.showArchived();
  await threadListPanel.narrowTo(thread.name);
  await expect(threadListPanel.row(thread.name)).toBeVisible();

  await threadListPanel.restore(thread.name);
  await threadListPanel.showActive();
  await threadListPanel.narrowTo(thread.name);
  await expect(threadListPanel.row(thread.name)).toBeVisible();
});

test('filters the list by title search', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const [target, other] = await seed.threads(2);
  await threadListPanel.navigateTo();

  await threadListPanel.search(target.name);
  await expect(threadListPanel.row(target.name)).toBeVisible();
  await expect(threadListPanel.row(other.name)).toBeHidden();

  await threadListPanel.search('no-thread-matches-this-query');
  await expect(threadListPanel.emptyMessage).toHaveText('No threads found');
});

test('exposes the personality filter', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  // Real bug found via exploration: the filter's options are derived from the
  // *loaded threads* (`uniquePersonalityOptions(allThreads())` in
  // core/services/thread-list.service.ts:55), not from the account's
  // personality list, so a personality with no threads — which is exactly what
  // this fixture creates — is never offered as an option.
  test.fixme(true, 'Personality filter omits thread-less personalities — see #41');

  await seed.thread();
  await threadListPanel.navigateTo();

  // Only asserts the control is populated from the account's personalities and
  // takes a selection. Not asserted: that the list actually shrinks — the
  // currently-active thread is re-prepended to the filtered result regardless
  // of the filter (`applyThreadFilters` in features/chat/helpers/
  // thread-list.helpers.ts), so a personality filter can't empty the table.
  // See the "filtering by a personality excludes the active thread" test
  // below for the bug this leads to. The options load asynchronously
  // with the personality list, so wait for the one we're about to pick
  // rather than racing `selectOption`.
  await expect(threadListPanel.personalityFilter).toContainText(userWithPersonality.personality.name);
  await threadListPanel.filterByPersonality(userWithPersonality.personality.name);
  await expect(threadListPanel.personalityFilter).toHaveValue(userWithPersonality.personality.id);
});

test(
  'filtering by a personality excludes the active thread when it does not belong',
  { tag: '@serial' },
  async ({ seed, threadListPanel, userWithPersonality }) => {
    // Real bug found via exploration: filtering the thread list by a
    // personality the *active* thread doesn't belong to should exclude that
    // thread from the results, but `applyThreadFilters` in
    // features/chat/helpers/thread-list.helpers.ts:44-52 unconditionally
    // re-prepends the active thread to the filtered list regardless of
    // whether it matches the selected personality.
    test.fixme(true, 'Personality filter never removes the active thread — see #42');

    const other = await seed.personality();
    const active = await seed.thread();
    await threadListPanel.navigateTo();
    await threadListPanel.open(active.name);
    await threadListPanel.navigateTo();

    await threadListPanel.filterByPersonality(other.name);

    await expect(threadListPanel.row(active.name)).toBeHidden();
  },
);

test('adds a tag through the tag editor', { tag: '@serial' }, async ({ isMobile, seed, threadListPanel, userWithPersonality }) => {
  // Real bug found via exploration: the TAGS cell is unreachable at mobile
  // widths. thread-list-threadListPanel.component.ts:753 hides the column headers
  // below 768px, but thread-row.component.ts:382-385 only hides some of the
  // corresponding cells — the tags cell is left rendered but non-visible, so
  // it can never be clicked.
  test.fixme(isMobile, 'Tags cell unusable at <768px — see #43');

  const thread = await seed.thread();
  await threadListPanel.navigateTo();

  await threadListPanel.openTagEditor(thread.name);
  await expect(threadListPanel.tagEditHeading).toBeVisible();

  await threadListPanel.addTag('e2etag');
  await threadListPanel.saveTags();

  await expect(threadListPanel.tagEditHeading).toBeHidden();
  await expect(threadListPanel.row(thread.name)).toContainText('e2etag');
});

test('bulk-archives the selected threads', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const [first, second] = await seed.threads(2);
  await threadListPanel.navigateTo();

  await threadListPanel.toggleSelect(first.name);
  await threadListPanel.toggleSelect(second.name);
  await expect(threadListPanel.bulkBar).toContainText('2 selected');

  await threadListPanel.runBulkAction('Archive', 2);

  await threadListPanel.narrowTo(first.name);
  await expect(threadListPanel.row(first.name)).toBeHidden();
  await threadListPanel.narrowTo(second.name);
  await expect(threadListPanel.row(second.name)).toBeHidden();

  await threadListPanel.showArchived();
  await threadListPanel.narrowTo(first.name);
  await expect(threadListPanel.row(first.name)).toBeVisible();
  await threadListPanel.narrowTo(second.name);
  await expect(threadListPanel.row(second.name)).toBeVisible();
});

test('deletes a thread after confirming', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();
  await threadListPanel.navigateTo();

  await threadListPanel.requestDelete(thread.name);
  await expect(threadListPanel.confirmation.headingText).toHaveText('Delete thread?');

  await threadListPanel.confirmation.confirm('Delete');
  await threadListPanel.narrowTo(thread.name);
  await expect(threadListPanel.row(thread.name)).toBeHidden();
});

test('cancelling the delete confirmation leaves the thread in place', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();
  await threadListPanel.navigateTo();
  await threadListPanel.narrowTo(thread.name);

  await threadListPanel.requestDelete(thread.name);
  await expect(threadListPanel.confirmation.headingText).toHaveText('Delete thread?');

  await threadListPanel.confirmation.cancel();
  await expect(threadListPanel.confirmation.dialog).toBeHidden();
  await expect(threadListPanel.row(thread.name)).toBeVisible();
});

test('stars and unstars a thread', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();
  await threadListPanel.navigateTo();
  await threadListPanel.narrowTo(thread.name);

  await threadListPanel.toggleStar(thread.name);
  await expect(threadListPanel.row(thread.name).getByRole('button', { name: 'Unstar thread' })).toBeVisible();

  await threadListPanel.toggleStar(thread.name);
  await expect(threadListPanel.row(thread.name).getByRole('button', { name: 'Star thread' })).toBeVisible();
});

test('selects all threads via the header checkbox and clears the selection', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const thread = await seed.thread();
  await threadListPanel.navigateTo();

  // Narrowed to this one row: "select all" otherwise selects the account's
  // whole list, which on a deployed run is shared with every other worker —
  // narrowing here means "select all" only ever has this test's row to
  // select, so the count assertion below is about this test's data.
  await threadListPanel.narrowTo(thread.name);
  await expect(threadListPanel.row(thread.name)).toBeVisible();

  await threadListPanel.selectAllCheckbox.check();
  await expect(threadListPanel.bulkBar).toContainText('1 selected');

  await threadListPanel.clearSelection();
  await expect(threadListPanel.bulkBar).toBeHidden();
});

test('bulk-assigns threads to a personality', { tag: '@serial' }, async ({ seed, threadListPanel, userWithPersonality }) => {
  const [first, second] = await seed.threads(2);
  await threadListPanel.navigateTo();

  await threadListPanel.toggleSelect(first.name);
  await threadListPanel.toggleSelect(second.name);

  await threadListPanel.runBulkAction('Assign to personality…');
  await expect(threadListPanel.bulkPersonalityPicker).toBeVisible();

  await threadListPanel.assignBulkPersonality(userWithPersonality.personality.name, 2);

  await threadListPanel.narrowTo(first.name);
  await expect(threadListPanel.row(first.name)).toContainText(userWithPersonality.personality.name);
  await threadListPanel.narrowTo(second.name);
  await expect(threadListPanel.row(second.name)).toContainText(userWithPersonality.personality.name);
});

test('cancelling the tag editor discards unsaved tags', { tag: '@serial' }, async ({ isMobile, seed, threadListPanel, userWithPersonality }) => {
  // Same mobile blocker as "adds a tag through the tag editor".
  test.fixme(isMobile, 'Tags cell unusable at <768px — see #43');

  const thread = await seed.thread();
  await threadListPanel.navigateTo();

  await threadListPanel.openTagEditor(thread.name);
  await expect(threadListPanel.tagEditHeading).toBeVisible();

  await threadListPanel.addTag('discard-me');
  await threadListPanel.cancelTags();

  await expect(threadListPanel.tagEditHeading).toBeHidden();
  await expect(threadListPanel.row(thread.name)).not.toContainText('discard-me');
});
