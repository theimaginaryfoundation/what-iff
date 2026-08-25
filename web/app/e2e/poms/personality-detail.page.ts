import type { Locator, Page } from '@playwright/test';
import { ConfirmationModal } from './confirmation.modal';

/** A single personality's detail page (`/personality/<id>`). */
export class PersonalityDetailPage {
  readonly confirmation: ConfirmationModal;

  constructor(private readonly page: Page) {
    this.confirmation = new ConfirmationModal(page);
    this.autoPinToggle = this.page.getByRole('switch', { name: 'Auto-pin new User memories' });
    this.promptEditor = this.page.getByLabel('System prompt editor');
    this.editPromptButton = this.promptEditor.getByRole('button', { name: 'Edit' });
    this.promptNameInput = this.promptEditor.getByLabel('Name');
    this.promptTextarea = this.promptEditor.getByLabel('Prompt');
    this.savePromptButton = this.promptEditor.getByRole('button', { name: 'Save' });
    this.cancelPromptButton = this.promptEditor.getByRole('button', { name: 'Cancel' });
    this.attachments = this.page.getByLabel('Personality attachments');
    this.uploadInput = this.page.locator('#personality-attachments-file-input');
    this.deleteButton = this.page.getByRole('button', { name: 'Delete', exact: true });
    this.makeDefaultButton = this.page.getByRole('button', { name: 'Make default' });
    this.expressions = this.page.getByLabel('Personality expressions');
    this.addExpressionButton = this.expressions.getByRole('button', { name: '+ Add expression' });
    this.expressionsEmptyMessage = this.expressions.getByText('No expression slots yet.');
    this.expressionKeyDialog = this.page.getByRole('dialog', { name: 'Add expression key' });
    this.expressionKeyInput = this.expressionKeyDialog.getByPlaceholder('e.g. mischievous');
    this.expressionKeySubmit = this.expressionKeyDialog.getByRole('button', { name: 'Add', exact: true });
    this.expressionKeyCancel = this.expressionKeyDialog.getByRole('button', { name: 'Cancel', exact: true });
    this.expressionKeyError = this.expressionKeyDialog.getByRole('alert');
  }

  heading(name: string) {
    return this.page.getByRole('heading', { name });
  }

  async navigateTo(id: string): Promise<void> {
    await this.page.goto(`/personality/${id}`);
  }

  async useInNewChat(): Promise<void> {
    await this.page.getByRole('button', { name: 'Use in new chat' }).click();
  }

  // --- auto-pin memories -----------------------------------------------------

  readonly autoPinToggle: Locator;

  async toggleAutoPinMemories(): Promise<void> {
    await this.autoPinToggle.click();
  }

  // --- system prompt editor ---------------------------------------------------

  readonly promptEditor: Locator;

  readonly editPromptButton: Locator;

  readonly promptNameInput: Locator;

  readonly promptTextarea: Locator;

  readonly savePromptButton: Locator;

  readonly cancelPromptButton: Locator;

  async editPrompt(details: { name?: string; systemPrompt?: string }): Promise<void> {
    await this.editPromptButton.click();
    if (details.name !== undefined) {
      await this.promptNameInput.fill(details.name);
    }
    if (details.systemPrompt !== undefined) {
      await this.promptTextarea.fill(details.systemPrompt);
    }
  }

  async savePrompt(): Promise<void> {
    await this.savePromptButton.click();
  }

  async cancelPrompt(): Promise<void> {
    await this.cancelPromptButton.click();
  }

  // --- attachments -------------------------------------------------------------

  readonly attachments: Locator;

  readonly uploadInput: Locator;

  async uploadAttachment(file: { name: string; mimeType: string; buffer: Buffer }): Promise<void> {
    await this.uploadInput.setInputFiles(file);
  }

  attachmentRow(name: string): Locator {
    return this.attachments.locator('li').filter({ hasText: name });
  }

  async deleteAttachment(name: string): Promise<void> {
    await this.attachments.getByRole('button', { name: `Delete ${name}` }).click();
  }

  // --- header actions ------------------------------------------------------

  readonly makeDefaultButton: Locator;

  // --- expression slots ------------------------------------------------------

  /** The "Expressions" panel on the detail page. */
  readonly expressions: Locator;

  readonly addExpressionButton: Locator;

  /** Shown while the personality has no persisted slots. */
  readonly expressionsEmptyMessage: Locator;

  /** The "Add expression key" modal opened by `addExpressionButton`. */
  readonly expressionKeyDialog: Locator;

  readonly expressionKeyInput: Locator;

  readonly expressionKeySubmit: Locator;

  readonly expressionKeyCancel: Locator;

  /** The modal's inline validation message (`role="alert"`). */
  readonly expressionKeyError: Locator;

  /**
   * The image picker the component opens immediately after a key is accepted
   * (`submitCustomKey()` calls `openGallery()`), so an "add" flow ends here
   * rather than back on the page. Labelled for the slot it is filling
   * (`Replace "<key>"`), so it takes the key rather than being a bare locator.
   */
  expressionImagePicker(key: string): Locator {
    return this.page.getByRole('dialog', { name: `Replace "${key}"` });
  }

  async openAddExpression(): Promise<void> {
    await this.addExpressionButton.click();
  }

  /** Types a key into the open modal and submits it. */
  async submitExpressionKey(key: string): Promise<void> {
    await this.expressionKeyInput.fill(key);
    await this.expressionKeySubmit.click();
  }

  /** A slot tile in the expressions grid, by its expression key. */
  expressionSlot(key: string): Locator {
    return this.expressions.getByRole('listitem').filter({ hasText: key });
  }

  async makeDefault(): Promise<void> {
    await this.makeDefaultButton.click();
  }

  /** Header "Delete" button — opens the shared confirmation modal. */
  readonly deleteButton: Locator;

  async requestDelete(): Promise<void> {
    await this.deleteButton.click();
  }

  async delete(): Promise<void> {
    await this.requestDelete();
    await this.confirmation.confirm('Delete');
  }
}
