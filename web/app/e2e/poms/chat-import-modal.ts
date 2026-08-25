import type { Locator, Page } from '@playwright/test';

/**
 * "Import conversations" modal (features/chat/components/chat-import-modal).
 * Rendered by the Thread Manager; opened from its header Import button or the
 * sidebar's "Import Conversations" quick action (which routes to `/chat`
 * with an `?import=` param).
 */
export class ChatImportModal {
  constructor(private readonly page: Page) {
    this.fileInput = this.page.locator('input.import__file-input');
    this.dropLabel = this.page.locator('.import__drop');
    this.importButton = this.page.getByRole('button', {
      name: /^(Import|Importing…)$/,
    });
    this.cancelButton = this.page.getByRole('button', { name: 'Cancel' });
    this.closeButton = this.page.getByRole('button', {
      name: 'Close',
      exact: true,
    });
    this.completeMessage = this.page.getByText('Import complete');
    this.failedMessage = this.page.getByText('Import failed');
    this.heading = this.page.getByRole('heading', {
      name: 'Import conversations',
    });
    this.resultDetail = this.page.locator('.import__result-detail');
    this.pickerCandidateRows = this.page.locator('.import__picker-row');
    this.skipPickerButton = this.page.getByRole('button', { name: 'Skip for now' });
  }

  readonly heading: Locator;

  /** The `<input type="file">` inside the drop label; visually hidden. */
  readonly fileInput: Locator;

  readonly dropLabel: Locator;

  /** Enabled only once a file has been chosen (stage 'ready'). */
  readonly importButton: Locator;

  readonly cancelButton: Locator;

  /** Replaces Cancel/Import once the run has finished or failed. */
  readonly closeButton: Locator;

  readonly completeMessage: Locator;

  readonly failedMessage: Locator;

  /** The "Imported N threads…" / "No new threads…" line shown on the 'done' stage. */
  readonly resultDetail: Locator;

  /** One row per candidate on the post-import "pick threads to prepare" stage. */
  readonly pickerCandidateRows: Locator;

  /** Skips the post-import picker, going straight to the 'done' summary. */
  readonly skipPickerButton: Locator;

  async chooseFile(file: string | { name: string; mimeType: string; buffer: Buffer }): Promise<void> {
    await this.fileInput.setInputFiles(file);
  }

  async startImport(): Promise<void> {
    await this.importButton.click();
  }

  async cancel(): Promise<void> {
    await this.cancelButton.click();
  }

  async dismiss(): Promise<void> {
    await this.closeButton.click();
  }

  /** A picker row by the imported thread's title (`chat.name`). */
  candidateRow(title: string): Locator {
    return this.pickerCandidateRows.filter({ hasText: title });
  }

  async skipPicker(): Promise<void> {
    await this.skipPickerButton.click();
  }
}
