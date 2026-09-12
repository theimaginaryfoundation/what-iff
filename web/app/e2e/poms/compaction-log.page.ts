import type { Locator, Page } from '@playwright/test';
import { ConfirmationModal } from './confirmation.modal';

/** The compaction and personality prompt audit page (`/memories/compaction-log`). */
export class CompactionLogPage {
  /**
   * "Restore previous" routes through the app-wide confirmation dialog, so a
   * bare click on the row button does nothing on its own.
   */
  readonly confirmation: ConfirmationModal;

  readonly heading: Locator;
  readonly promptChangesToggle: Locator;
  readonly promptChangesList: Locator;

  constructor(private readonly page: Page) {
    this.confirmation = new ConfirmationModal(page);
    this.heading = this.page.getByRole('heading', { name: 'Compaction log' });
    this.promptChangesToggle = this.page.getByTestId('prompt-change-toggle');
    this.promptChangesList = this.page.getByRole('list', { name: 'Personality prompt changes' });
    this.noPromptChangesMessage = this.page.getByText('No personality prompt changes logged yet.');
    this.promptChangesError = this.page.getByRole('alert');
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

  /**
   * The card below its header row — the System prompt title, the restore
   * action, and the before/after diff grid. Everything in it is derived from
   * the prompts themselves, so unlike the full card it carries no timestamp
   * and is safe to screenshot without a mask.
   */
  promptChangeBody(personalityName: string): Locator {
    return this.promptChangeCard(personalityName).locator('.compaction-card__body');
  }

  /**
   * The rendered prompt text in a card's "Before" / "After" diff pane. The one
   * place these selectors live — specs and the newest-card assertions below
   * both go through it, so a DOM change to the diff grid is a single edit.
   */
  promptChangePane(personalityName: string, side: 'Before' | 'After'): Locator {
    return this.paneWithin(this.promptChangeCard(personalityName), side);
  }

  /** Same, scoped to a card locator the caller already narrowed (e.g. `.first()`). */
  paneWithin(card: Locator, side: 'Before' | 'After'): Locator {
    return card.locator(side === 'Before' ? '.diff-pane--old' : '.diff-pane--new').locator('.diff-pane__content');
  }

  /** The card's "Edited" / "Restored" badge value. */
  promptChangeAction(personalityName: string): Locator {
    return this.promptChangeCard(personalityName)
      .locator('.compaction-card__badge')
      .filter({ hasText: 'Prompt' })
      .locator('.compaction-card__badge-value');
  }

  restorePreviousButton(personalityName: string): Locator {
    return this.promptChangeCard(personalityName).getByRole('button', {
      name: /^(Restore previous|Restoring…)$/,
    });
  }

  /**
   * Restores the newest prompt change for a personality, including the
   * confirmation step. The dialog's confirm button is labelled "Restore", not
   * the ConfirmationModal default of "Delete".
   */
  async restorePrevious(personalityName: string): Promise<void> {
    await this.restorePreviousButton(personalityName).first().click();
    await this.confirmation.confirm('Restore');
  }

  /** Shown in place of the list once expanded, when nothing has been logged. */
  readonly noPromptChangesMessage: Locator;

  readonly promptChangesError: Locator;
}
