import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/**
 * A single memory (`/memories/<id>`) — memory-detail-page.component.html plus
 * the embedded `app-memory-form`.
 *
 * Reached by URL only: the memory cards on the list page have Edit/Delete
 * actions but no "View details" link, so nothing in the UI navigates here.
 * There is likewise no Copy button — the exploration pass expected both.
 */
export class MemoryDetailPage {
  private readonly shell: AppShell;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.backButton = this.page.getByRole('button', {
      name: 'Back to memories',
    });
    this.contentTextarea = this.page.getByLabel('Content');
    this.levelSelect = this.page.getByLabel('Memory level');
    this.metadataHeading = this.page.getByRole('heading', { name: 'Metadata' });
  }

  async navigateTo(id: string): Promise<void> {
    await this.page.goto(`/memories/${id}`);
    await this.shell.dismissAnnouncementIfPresent();
  }

  /** Back link. Sentence case in the template — "Back to memories". */
  readonly backButton: Locator;

  async goBack(): Promise<void> {
    await this.backButton.click();
  }

  readonly contentTextarea: Locator;

  readonly levelSelect: Locator;

  readonly metadataHeading: Locator;

  async setContent(value: string): Promise<void> {
    await this.contentTextarea.fill(value);
  }

  async save(): Promise<void> {
    await this.page.getByRole('button', { name: 'Save changes' }).click();
  }

  /** Opens the shared delete-memory modal. */
  async requestDelete(): Promise<void> {
    await this.page.getByRole('button', { name: 'Delete', exact: true }).first().click();
  }

  async confirmDelete(): Promise<void> {
    await this.page.locator('#delete-memory-title').waitFor();
    await this.page.getByRole('button', { name: 'Delete', exact: true }).last().click();
  }
}
