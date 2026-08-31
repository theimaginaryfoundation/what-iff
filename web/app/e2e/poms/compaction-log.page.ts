import type { Locator, Page } from '@playwright/test';

/** The compaction and personality prompt audit page (`/memories/compaction-log`). */
export class CompactionLogPage {
  readonly heading: Locator;
  readonly promptChangesToggle: Locator;
  readonly promptChangesList: Locator;

  constructor(private readonly page: Page) {
    this.heading = this.page.getByRole('heading', { name: 'Compaction log' });
    this.promptChangesToggle = this.page.getByTestId('prompt-change-toggle');
    this.promptChangesList = this.page.getByRole('list', { name: 'Personality prompt changes' });
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/memories/compaction-log');
  }

  async expandPromptChanges(): Promise<void> {
    if ((await this.promptChangesToggle.getAttribute('aria-expanded')) !== 'true') {
      await this.promptChangesToggle.click();
    }
  }

  promptChangeCard(personalityName: string): Locator {
    return this.promptChangesList.getByRole('listitem').filter({ hasText: personalityName });
  }

  promptChangeMetadata(personalityName: string): Locator {
    return this.promptChangeCard(personalityName).locator('.compaction-card__meta');
  }
}
