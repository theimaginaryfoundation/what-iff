import type { Locator, Page } from '@playwright/test';

/** The compaction and personality prompt audit page (`/memories/compaction-log`). */
export class CompactionLogPage {
  readonly heading: Locator;
  readonly promptChangesHeading: Locator;
  readonly promptChangesList: Locator;

  constructor(private readonly page: Page) {
    this.heading = this.page.getByRole('heading', { name: 'Compaction log' });
    this.promptChangesHeading = this.page.getByRole('heading', { name: 'Personality prompt changes' });
    this.promptChangesList = this.page.getByRole('list', { name: 'Personality prompt changes' });
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/memories/compaction-log');
  }

  promptChangeCard(personalityName: string): Locator {
    return this.promptChangesList.getByRole('listitem').filter({ hasText: personalityName });
  }

  promptChangeMetadata(personalityName: string): Locator {
    return this.promptChangeCard(personalityName).locator('.compaction-card__meta');
  }
}
