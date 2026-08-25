import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/** Filter tabs in the memories toolbar (`role="tablist"`, "Memory filters"). */
export type MemoryFilter = 'All' | 'Global' | 'Personality' | 'Thread' | 'Summary';

/**
 * Memories list (`/memories`) — features/memory/memories-page.component.html.
 *
 * NOTE: there is no free-text search on this page. The exploration pass
 * expected a `Search in memory content...` box; the only search input in the
 * feature lives in `memory-filter-bar.component`, which the page doesn't
 * render (it uses the toolbar tabs + a sort select instead). Filtering by
 * personality is done from the sidebar, not this page.
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
    this.nextPageButton = this.page.getByRole('button', { name: 'Next' });
    this.pageIndicator = this.page.getByText(/^Page \d+ of \d+$/);
    this.heading = this.page.getByRole('heading', {
      name: 'Memories',
      level: 1,
    });
    this.filterTabs = this.page.getByRole('tablist', {
      name: 'Memory filters',
    });
    this.sortSelect = this.page.locator('.memories-toolbar__sort select');
    this.grid = this.page.getByRole('list', { name: 'Memories' });
    this.cards = this.page.locator('article.memory-card');
    this.editingCard = this.cards.filter({
      has: this.page.locator('.memory-card__editor'),
    });
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/memories');
    await this.shell.dismissAnnouncementIfPresent();
  }

  readonly heading: Locator;

  readonly filterTabs: Locator;

  /**
   * The toolbar filters are plain `<button>`s inside a `role="tablist"` —
   * they have no `role="tab"` of their own, so they're matched as buttons
   * scoped to the tablist rather than by tab role.
   */
  filterTab(filter: MemoryFilter): Locator {
    return this.filterTabs.getByRole('button', { name: filter, exact: true });
  }

  async filterBy(filter: MemoryFilter): Promise<void> {
    await this.filterTab(filter).click();
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

  async startEdit(content: string): Promise<void> {
    await this.card(content).getByRole('button', { name: 'Edit memory' }).click();
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
    await this.card(content).getByRole('button', { name: 'Delete memory' }).click();
  }

  readonly deleteDialogHeading: Locator;

  async confirmDelete(): Promise<void> {
    await this.page.getByRole('button', { name: 'Delete', exact: true }).click();
  }

  async cancelDelete(): Promise<void> {
    await this.page.getByRole('button', { name: 'Cancel', exact: true }).click();
  }

  async delete(content: string): Promise<void> {
    await this.requestDelete(content);
    await this.confirmDelete();
  }

  // --- pagination ----------------------------------------------------------
  //
  // Rendered only when there is more than one page, and with no landmark or
  // aria-label of its own — the buttons are matched by their labels.

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
