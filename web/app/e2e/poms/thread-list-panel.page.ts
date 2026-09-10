import type { Locator, Page, Response } from '@playwright/test';
import { AppShell } from './app-shell.page';
import { ConfirmationModal } from './confirmation.modal';
import { MUTATION_ACK_TIMEOUT } from '../timeouts';

/**
 * Thread Manager — the table of chat threads rendered by `/chat` whenever no
 * thread is open (chat-page.component.html falls back to
 * `app-thread-list-panel`). Everything here is inside the
 * `aria-label="Threads"` region except the modals, which portal to the body.
 */
export class ThreadListPanel {
  private readonly shell: AppShell;
  readonly confirmation: ConfirmationModal;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.confirmation = new ConfirmationModal(page);
    this.panel = this.page.getByRole('complementary', { name: 'Threads' });
    this.heading = this.page.getByRole('heading', { name: 'Thread Manager' });
    this.searchInput = this.page.getByPlaceholder('Search titles and tags…');
    this.emptyMessage = this.panel.locator('.panel__empty');
    this.activeTab = this.page.getByRole('tab', { name: 'Active' });
    this.archivedTab = this.page.getByRole('tab', { name: 'Archived' });
    this.personalityFilter = this.page.getByLabel('Filter by personality');
    this.bulkBar = this.page.getByRole('region', { name: 'Bulk actions' });
    this.bulkMenu = this.page.getByRole('listbox', { name: 'Bulk actions' });
    this.bulkPersonalityPicker = this.page.getByRole('listbox', {
      name: 'Personalities',
    });
    this.tagEditHeading = this.page.getByRole('heading', { name: 'Edit tags' });
    this.tagInput = this.page.getByLabel(/^New tag,/);
    this.selectAllCheckbox = this.page.getByLabel('Select all threads');
    this.rows = this.page.locator('tr.thread-row');
  }

  /**
   * Goes straight to the manager by URL. Preferred over the sidebar tab for
   * setup, because the Chat tab is a *toggle* that bounces back to the last
   * open thread. Also the only route that works identically on mobile.
   */
  async navigateTo(): Promise<void> {
    await this.page.goto('/chat');
    await this.shell.dismissAnnouncementIfPresent();
  }

  /** The `<aside aria-label="Threads">` wrapper — a `complementary`, not a `region`. */
  readonly panel: Locator;

  readonly heading: Locator;

  readonly searchInput: Locator;

  /** "No threads found" / "No archived threads", depending on the tab. */
  readonly emptyMessage: Locator;

  readonly activeTab: Locator;

  readonly archivedTab: Locator;

  async showActive(): Promise<void> {
    await this.activeTab.click();
  }

  async showArchived(): Promise<void> {
    await this.archivedTab.click();
  }

  async search(query: string): Promise<void> {
    await this.searchInput.fill(query);
  }

  /**
   * Narrows the table to one thread by name, so an assertion that follows is
   * about *this* test's data rather than about the account's whole list.
   *
   * On the deployed configs every test shares one Cognito account, so the
   * unfiltered table holds threads from every other test running at the same
   * time and from every previous run. `expect(row(x)).toBeVisible()` against
   * that list is really asserting "x is somewhere in a list I don't control",
   * which is a different claim than the test means to make and fails for
   * reasons that have nothing to do with x.
   *
   * Seeded names are UUID-suffixed (fixtures/unique.ts), so narrowing by name
   * leaves exactly this test's rows and makes every other worker's rows
   * structurally absent rather than incidentally off-screen. Call it before
   * asserting a row is visible *or* hidden — "hidden" is only meaningful once
   * the row would have been shown if it existed.
   *
   * No assertion here: POMs locate and interact, specs assert. Pair it with
   * `row(name)`.
   */
  async narrowTo(name: string): Promise<void> {
    const response = this.page.waitForResponse(
      candidate => {
        if (candidate.request().method() !== 'GET') return false;
        const url = new URL(candidate.url());
        return url.pathname === '/api/chat' && url.searchParams.get('search') === name;
      },
      { timeout: MUTATION_ACK_TIMEOUT },
    );
    await this.search(name);
    const completed = await response;
    if (!completed.ok()) {
      throw new Error(`Thread search rejected: GET ${new URL(completed.url()).pathname} returned ${completed.status()}`);
    }
  }

  /** Filters the table by personality (the `<select>` in the PERSONALITY header). */
  readonly personalityFilter: Locator;

  async filterByPersonality(name: string): Promise<void> {
    await this.personalityFilter.selectOption({ label: name });
  }

  /**
   * A thread's row. Rows are `role="option"` (the tbody is a listbox) with no
   * accessible name of their own, so they're located by the "Open thread
   * <name>" button they contain.
   */
  row(name: string): Locator {
    return this.page.locator('tr.thread-row').filter({
      has: this.page.getByRole('button', { name: `Open thread ${name}` }),
    });
  }

  readonly rows: Locator;

  async open(name: string): Promise<void> {
    await this.page.getByRole('button', { name: `Open thread ${name}` }).click();
  }

  /**
   * Resolves when the backend has acknowledged one thread mutation.
   *
   * Every single-thread edit here is *optimistic*: `optimisticPatch` in
   * core/services/thread-list.service.ts updates the row locally and lets the
   * `PATCH /api/chat/{id}` finish in the background. That is fine for an
   * assertion about the row on screen, and wrong for anything that goes back
   * to the server — `narrowTo` re-queries with a search term, and a tab switch
   * re-queries with `archived`. Either can run against a backend that has not
   * applied the change yet and come back without the row, and no amount of
   * retrying fixes it: the list does not re-fetch on its own, so the empty
   * result is stable until something else triggers a query.
   *
   * This is not hypothetical — it failed exactly this way on `webkit-mobile`
   * in CI, where the slower engine widened the window between the optimistic
   * update and the response.
   *
   * Must be started *before* the action that triggers the request, hence the
   * "arm it, act, await it" shape at each call site.
   *
   * Two details that are easy to get wrong and were:
   *
   * The predicate matches on method and path only, never on `response.ok()`.
   * Filtering to successful responses makes a *failed* write indistinguishable
   * from one that never happened — both surface as "timed out waiting for a
   * response", which sends the reader hunting for a missing click when the
   * real answer is a 409 the test could have named. Status is checked after
   * the wait instead, and reported.
   *
   * `expected` exists for bulk actions, which issue one PATCH per selected
   * thread; waiting for a single response would return while the rest are
   * still in flight.
   */
  private async waitForThreadPatch(expected = 1): Promise<void> {
    const seen: Response[] = [];
    await this.page.waitForResponse(
      response => {
        if (response.request().method() !== 'PATCH') return false;
        if (!/\/api\/chat\/[^/]+$/.test(new URL(response.url()).pathname)) return false;
        seen.push(response);
        return seen.length >= expected;
      },
      { timeout: MUTATION_ACK_TIMEOUT },
    );
    const failed = seen.find(response => !response.ok());
    if (failed) {
      throw new Error(`Thread mutation rejected: PATCH ${new URL(failed.url()).pathname} returned ${failed.status()}`);
    }
  }

  /** Double-click on the title swaps it for the inline rename input. */
  async rename(name: string, newName: string): Promise<void> {
    const patched = this.waitForThreadPatch();
    await this.page.getByRole('button', { name: `Open thread ${name}` }).dblclick();
    const input = this.page.getByLabel('Rename thread');
    await input.fill(newName);
    await input.press('Enter');
    await patched;
  }

  async archive(name: string): Promise<void> {
    const patched = this.waitForThreadPatch();
    await this.row(name).getByRole('button', { name: 'Archive thread' }).click();
    await patched;
  }

  async restore(name: string): Promise<void> {
    const patched = this.waitForThreadPatch();
    await this.row(name).getByRole('button', { name: 'Restore thread from archive' }).click();
    await patched;
  }

  async toggleStar(name: string): Promise<void> {
    const patched = this.waitForThreadPatch();
    await this.row(name)
      .getByRole('button', { name: /^(Star|Unstar) thread$/ })
      .click();
    await patched;
  }

  /** Opens the confirm dialog; callers finish with `confirmation.confirm()`. */
  async requestDelete(name: string): Promise<void> {
    await this.row(name)
      .getByRole('button', { name: `Delete thread ${name}` })
      .click();
  }

  async delete(name: string): Promise<void> {
    await this.requestDelete(name);
    await this.confirmation.confirm('Delete');
  }

  // --- selection & bulk actions -------------------------------------------

  async toggleSelect(name: string): Promise<void> {
    await this.page.getByLabel(`Select thread ${name}`).check();
  }

  readonly selectAllCheckbox: Locator;

  /** The bulk bar only exists while at least one row is selected. */
  readonly bulkBar: Locator;

  readonly bulkMenu: Locator;

  async openBulkMenu(): Promise<void> {
    await this.bulkBar.getByRole('button', { name: /Choose action/ }).click();
  }

  /**
   * Menu items respond to `mousedown` (so the focusout-close doesn't beat the
   * click), which `click()` still delivers. "Archive" and "Restore" are
   * mutually exclusive — whichever matches the current tab is rendered.
   *
   * Pass `expectedPatches` — the number of threads selected — for the actions
   * that write (Archive, Restore), so the call doesn't return while the
   * per-thread PATCHes are still in flight; the same optimistic-update race
   * `waitForThreadPatch` documents applies here, once per selected row. Omit
   * it for Delete (a DELETE, not a PATCH) and for "Assign to personality…",
   * which only opens the picker.
   */
  async runBulkAction(
    action: 'Archive' | 'Restore' | 'Delete' | 'Assign to personality…',
    expectedPatches?: number,
  ): Promise<void> {
    const patched = expectedPatches ? this.waitForThreadPatch(expectedPatches) : undefined;
    await this.openBulkMenu();
    await this.bulkMenu.getByRole('option', { name: action, exact: true }).click();
    await patched;
  }

  async clearSelection(): Promise<void> {
    await this.bulkBar.getByRole('button', { name: 'Clear selection' }).click();
  }

  /** Personality picker shown by the "Assign to personality…" bulk action. */
  readonly bulkPersonalityPicker: Locator;

  /**
   * Picks a personality from the bulk picker. `expectedPatches` is the number
   * of threads selected — `bulkAssignPersonality` in
   * core/services/thread-list.service.ts awaits one `optimisticPatch` per
   * thread sequentially, and the component's click handler is async, so
   * Playwright's `click()` resolving is not this write finishing. Same
   * optimistic-update race `waitForThreadPatch` documents.
   */
  async assignBulkPersonality(name: string, expectedPatches = 1): Promise<void> {
    const patched = this.waitForThreadPatch(expectedPatches);
    await this.bulkPersonalityPicker.getByRole('option', { name }).click();
    await patched;
  }

  // --- tag edit modal ------------------------------------------------------

  /** Opens the tag modal from a row ("Add tags" when it has none yet). */
  async openTagEditor(name: string): Promise<void> {
    await this.row(name).locator('.thread-row__tags').click();
  }

  readonly tagEditHeading: Locator;

  readonly tagInput: Locator;

  /** Enter commits a chip; the modal keeps them until Save. */
  async addTag(tag: string): Promise<void> {
    await this.tagInput.fill(tag);
    await this.tagInput.press('Enter');
  }

  /** Save writes the chips through as a thread PATCH — same optimistic race. */
  async saveTags(): Promise<void> {
    const patched = this.waitForThreadPatch();
    await this.page.getByRole('button', { name: 'Save', exact: true }).click();
    await patched;
  }

  async cancelTags(): Promise<void> {
    await this.page.getByRole('button', { name: 'Cancel', exact: true }).click();
  }

  /**
   * Opens the conversation-import modal. The panel's own header button is
   * `display: none` below 768px, where the sidebar quick action of the same
   * name is the only way in.
   */
  async openImport(): Promise<void> {
    const headerButton = this.panel.getByRole('button', {
      name: 'Import Conversations',
    });
    if (await headerButton.isVisible().catch(() => false)) {
      await headerButton.click();
      return;
    }
    await this.shell.quickAction('Import Conversations');
  }
}
