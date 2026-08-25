import type { Locator, Page } from '@playwright/test';

/**
 * The app-wide confirmation dialog driven by `ConfirmationService`
 * (core/components/confirmation-modal). Thread delete, skill delete and
 * "discard changes?" all route through this one component, so every POM that
 * triggers a destructive action reuses this rather than re-deriving the
 * buttons.
 *
 * The title/message are per-caller strings; only the footer buttons are
 * stable, and their labels are configurable too (`confirmText`/`cancelText`),
 * hence `confirm(label)`.
 */
export class ConfirmationModal {
  constructor(private readonly page: Page) {
    this.dialog = this.page.getByRole('dialog').filter({ has: this.page.locator('#confirmation-modal-title') });
    this.headingText = this.page.locator('#confirmation-modal-title');
    this.message = this.page.locator('#confirmation-modal-message');
  }

  /**
   * The dialog itself. Buttons are scoped to it because the confirm label is
   * often the same word as the row action that opened it ("Delete", "Revoke"),
   * which would otherwise be a strict-mode violation.
   */
  readonly dialog: Locator;

  readonly headingText: Locator;

  readonly message: Locator;

  /** Clicks the confirm button. `label` defaults to the common danger wording. */
  async confirm(label = 'Delete'): Promise<void> {
    await this.dialog.getByRole('button', { name: label, exact: true }).click();
  }

  async cancel(label = 'Cancel'): Promise<void> {
    await this.dialog.getByRole('button', { name: label, exact: true }).click();
  }
}
