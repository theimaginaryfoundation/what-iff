import type { Locator, Page } from '@playwright/test';

/**
 * Compaction log tab under Memory Manager (`/memories?tab=compaction-log`).
 * Personality prompt changes used to be listed here; they now live on the
 * personality detail page (see `PersonalityDetailPage.promptHistory`).
 */
export class CompactionLogPage {
  readonly heading: Locator;

  constructor(private readonly page: Page) {
    this.heading = this.page.getByRole('heading', { name: 'Compaction log' });
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/memories?tab=compaction-log');
  }
}
