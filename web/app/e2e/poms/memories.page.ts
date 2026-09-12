import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/** Sidebar association filters for memories (All / Global). */
export type MemoryFilter = 'All' | 'Global';

/**
 * Memories list (`/memories`) — features/memory/memories-list-tab under the
 * Memory Manager shell (memories-page.component).
 */
export class MemoriesPage {
  private readonly shell: AppShell;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.emptyMessage = this.page.getByText('No memories found.');
    this.deleteDialogHeading = this.page.locator('#delete-memory-title');
    this.previousPageButton = this.page.getByRole('button', {
      name: 'Previous',
    });
    this.nextPageButton = this.page.getByRole('button', {
      name: 'Next',
    });
    this.pageIndicator = this.page.getByText(/^Page \d+ of \d+$/);
    this.heading = this.page.getByRole('heading', {
      name: 'Memory Manager',
      level: 1,
    });
    this.subtitle = this.page.getByText(
      'Review and correct saved context that informs your conversations.',
      { exact: true },
    );
    this.header = this.page.locator('.memories-shell__header');
    this.headerCopy = this.page.locator('.memories-shell__copy');
    this.headerActions = this.page.locator('.memories-shell__tabs');
    this.mergeHistoryTab = this.page.getByRole('tab', { name: 'Merge history', exact: true });
    this.compactionLogTab = this.page.getByRole('tab', { name: 'Compaction log', exact: true });
    this.memoriesTab = this.page.getByRole('tab', { name: 'Memories', exact: true });
    this.mobileMenuButton = this.page.getByRole('button', { name: 'Open navigation menu', exact: true });
    this.mainContent = this.page.locator('#main-content');
    this.filtersRow = this.page.locator('.memories-list__filters');
    this.statusTabs = this.page.getByRole('tablist', { name: 'Memory status' });
    this.activeStatusTab = this.statusTabs.getByRole('tab', { name: 'Active', exact: true });
    this.archivedStatusTab = this.statusTabs.getByRole('tab', { name: 'Archived', exact: true });
    this.summariesStatusTab = this.statusTabs.getByRole('tab', { name: 'Summaries', exact: true });
    this.sortSelect = this.page.locator('.memories-list__filters .memories-list__sort-wrap select');
    this.searchInput = this.page.getByPlaceholder('Search memories…');
    this.minDateInput = this.page.locator('.memories-list__date-wrap input[type="date"]').first();
    this.maxDateInput = this.page.locator('.memories-list__date-wrap input[type="date"]').last();
    this.openMinDateButton = this.page.getByRole('button', { name: 'Open start date calendar', exact: true });
    this.openMaxDateButton = this.page.getByRole('button', { name: 'Open end date calendar', exact: true });
    this.grid = this.page.getByRole('list', { name: 'Memories' });
    this.cards = this.page.locator('article.memory-card');
    this.editingCard = this.cards.filter({
      has: this.page.locator('.memory-card__editor'),
    });
    this.bulkBar = this.page.getByRole('toolbar', { name: 'Bulk memory actions' });
    this.focusPanel = this.page.getByRole('complementary', { name: 'Memory details' });
    this.focusDialog = this.page.getByRole('dialog', { name: 'Memory details' });
    this.focusModalBackdrop = this.page.locator('.ui-modal');
  }

  /** `query` is an optional leading-`?` query string, for deep-linking (e.g. `?status=inactive`). */
  async navigateTo(query = ''): Promise<void> {
    await this.page.goto(`/memories${query}`);
    await this.shell.dismissAnnouncementIfPresent();
  }

  readonly heading: Locator;

  readonly subtitle: Locator;

  readonly header: Locator;

  readonly headerCopy: Locator;

  readonly headerActions: Locator;

  readonly memoriesTab: Locator;

  readonly mergeHistoryTab: Locator;

  readonly compactionLogTab: Locator;

  /** @deprecated Use mergeHistoryTab — kept for older specs during the tab migration. */
  get mergeHistoryLink(): Locator {
    return this.mergeHistoryTab;
  }

  /** @deprecated Use compactionLogTab — kept for older specs during the tab migration. */
  get compactionLogLink(): Locator {
    return this.compactionLogTab;
  }

  readonly mobileMenuButton: Locator;

  readonly mainContent: Locator;

  readonly filtersRow: Locator;

  /** @deprecated Level chip row removed — use filtersRow / sidebar All·Global. */
  get toolbar(): Locator {
    return this.filtersRow;
  }

  /** @deprecated Level chip row removed — use statusTabs or sidebar filters. */
  get filterTabs(): Locator {
    return this.statusTabs;
  }

  readonly statusTabs: Locator;

  readonly activeStatusTab: Locator;

  readonly archivedStatusTab: Locator;

  readonly summariesStatusTab: Locator;

  readonly searchInput: Locator;

  readonly minDateInput: Locator;

  readonly maxDateInput: Locator;

  readonly openMinDateButton: Locator;

  readonly openMaxDateButton: Locator;

  readonly bulkBar: Locator;

  /** Desktop right-rail focus panel (`aside`). Hidden on narrow viewports. */
  readonly focusPanel: Locator;

  /** Mobile focus popup (`ui-modal` labelled "Memory details"). */
  readonly focusDialog: Locator;

  /**
   * The mobile focus modal's own backdrop (`ui-modal`'s `backdropClass()`).
   * Click a corner rather than the center — the center falls on the centered
   * dialog panel, which stops click propagation.
   */
  readonly focusModalBackdrop: Locator;

  async openFocus(content: string): Promise<void> {
    await this.card(content).click();
  }

  async closeFocus(): Promise<void> {
    const dialogClose = this.focusDialog.getByRole('button', { name: 'Close', exact: true });
    if (await dialogClose.isVisible().catch(() => false)) {
      await dialogClose.click();
      return;
    }
    await this.focusPanel.getByRole('button', { name: 'Close details', exact: true }).click();
  }

  async filterBy(filter: MemoryFilter): Promise<void> {
    const name = filter === 'All' ? 'Show all memories' : 'Show global memories only';
    await this.page.getByRole('button', { name, exact: true }).click();
  }

  async showArchived(): Promise<void> {
    await this.archivedStatusTab.click();
  }

  async showActive(): Promise<void> {
    await this.activeStatusTab.click();
  }

  async showSummaries(): Promise<void> {
    await this.summariesStatusTab.click();
  }

  readonly sortSelect: Locator;

  async sortBy(value: 'created_desc' | 'created_asc' | 'updated_desc'): Promise<void> {
    await this.sortSelect.selectOption(value);
  }

  readonly grid: Locator;

  readonly cards: Locator;

  /**
   * A memory's card. Cards are `role="listitem"` with no accessible name, so
   * they're located by their visible excerpt text.
   */
  card(content: string): Locator {
    return this.cards.filter({ hasText: content });
  }

  /** "No memories found." — rendered by memory-card-grid when the list is empty. */
  readonly emptyMessage: Locator;

  async openCardMenu(content: string): Promise<void> {
    await this.card(content).getByRole('button', { name: 'More actions' }).click();
  }

  async startEdit(content: string): Promise<void> {
    await this.openCardMenu(content);
    await this.page.getByRole('menuitem', { name: 'Edit', exact: true }).click();
  }

  /**
   * The card currently in edit mode. Located by the editor rather than by
   * content: entering edit mode swaps the excerpt for a textarea, and a
   * textarea's *value* is not matched by `hasText`, so `card(content)` stops
   * resolving the moment editing starts.
   */
  readonly editingCard: Locator;

  async saveEdit(newContent: string): Promise<void> {
    const card = this.editingCard;
    await card.locator('textarea').fill(newContent);
    await card.getByRole('button', { name: 'Save', exact: true }).click();
  }

  async cancelEdit(): Promise<void> {
    await this.editingCard.getByRole('button', { name: 'Cancel', exact: true }).click();
  }

  /** Opens the delete confirmation modal for one card. */
  async requestDelete(content: string): Promise<void> {
    await this.openCardMenu(content);
    await this.page.getByRole('menuitem', { name: 'Delete', exact: true }).click();
  }

  readonly deleteDialogHeading: Locator;

  async confirmDelete(): Promise<void> {
    await this.page.getByRole('dialog').getByRole('button', { name: 'Delete', exact: true }).click();
  }

  async cancelDelete(): Promise<void> {
    await this.page.getByRole('dialog').getByRole('button', { name: 'Cancel', exact: true }).click();
  }

  async delete(content: string): Promise<void> {
    await this.requestDelete(content);
    await this.confirmDelete();
  }

  async selectCard(content: string): Promise<void> {
    await this.card(content).getByLabel('Select memory').check();
  }

  /**
   * The bulk bar's own "Select all" checkbox — only rendered once the bulk
   * bar itself is visible (`selectCard` at least one card first).
   */
  async selectAll(): Promise<void> {
    await this.bulkBar.getByLabel('Select all').check();
  }

  async archiveFromMenu(content: string): Promise<void> {
    await this.openCardMenu(content);
    await this.page.getByRole('menuitem', { name: 'Archive', exact: true }).click();
  }

  async unarchiveFromMenu(content: string): Promise<void> {
    await this.openCardMenu(content);
    await this.page.getByRole('menuitem', { name: 'Unarchive', exact: true }).click();
  }

  async moveFromMenu(content: string, destination: string): Promise<void> {
    await this.openCardMenu(content);
    await this.page.getByRole('menuitem', { name: 'Move', exact: true }).click();
    await this.page.getByRole('dialog', { name: 'Move memories' }).getByRole('button', {
      name: destination,
      exact: true,
    }).click();
  }

  async bulkArchive(): Promise<void> {
    await this.bulkBar.getByRole('button', { name: /^(Archive|Unarchive)$/ }).click();
  }

  async bulkDelete(): Promise<void> {
    await this.bulkBar.getByRole('button', { name: 'Delete', exact: true }).click();
  }

  async bulkMove(destination: string): Promise<void> {
    await this.bulkBar.getByRole('button', { name: 'Move', exact: true }).click();
    await this.page.getByRole('dialog', { name: 'Move memories' }).getByRole('button', {
      name: destination,
      exact: true,
    }).click();
  }

  // --- pagination ----------------------------------------------------------

  readonly previousPageButton: Locator;

  readonly nextPageButton: Locator;

  readonly pageIndicator: Locator;

  async nextPage(): Promise<void> {
    await this.nextPageButton.click();
  }

  async previousPage(): Promise<void> {
    await this.previousPageButton.click();
  }
}
