import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';
import { ConfirmationModal } from './confirmation.modal';

/**
 * Skills (`/skills`) — features/ritual/rituals-page.component.html. "Skill" is
 * the UI name for what the API and SDK call a ritual.
 */
export class SkillsPage {
  private readonly shell: AppShell;
  readonly confirmation: ConfirmationModal;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.confirmation = new ConfirmationModal(page);
    this.nameInput = this.page.getByPlaceholder('e.g., Source Analysis');
    this.descriptionInput = this.page.getByPlaceholder('Brief description of what this skill does');
    this.contentInput = this.page.getByPlaceholder('The prompt text that will be inserted into chat when this skill is called...');
    this.readOnlySystemMarker = this.page.getByLabel('Read-only system skill');
    this.heading = this.page.getByRole('heading', { name: 'Skills', level: 1 });
    this.searchInput = this.page.getByPlaceholder('Search skills...');
    this.emptyMessage = this.page.getByText('No personal skills match your filters.');
    this.editor = this.page.locator('.skills-editor-modal');
    this.editorTitle = this.page.locator('#skill-editor-title');
    this.cards = this.page.locator('article.skills-page__card');
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/skills');
    await this.shell.dismissAnnouncementIfPresent();
  }

  readonly heading: Locator;

  readonly searchInput: Locator;

  async search(query: string): Promise<void> {
    await this.searchInput.fill(query);
  }

  readonly cards: Locator;

  /** A skill's card, located by its title — the cards carry no aria-label. */
  card(name: string): Locator {
    return this.cards.filter({
      has: this.page.locator('.skills-page__card-title', { hasText: name }),
    });
  }

  readonly emptyMessage: Locator;

  /**
   * Row actions are icon buttons whose only accessible name is a `title`
   * attribute ("Edit" / "Delete"), so they resolve as buttons by that name.
   */
  async openEditor(name: string): Promise<void> {
    await this.card(name).getByRole('button', { name: 'Edit' }).click();
  }

  async requestDelete(name: string): Promise<void> {
    await this.card(name).getByRole('button', { name: 'Delete' }).click();
  }

  /** The Delete button is disabled for built-in system skills. */
  deleteButton(name: string): Locator {
    return this.card(name).getByRole('button', { name: 'Delete' });
  }

  async delete(name: string): Promise<void> {
    await this.requestDelete(name);
    await this.confirmation.confirm('Delete');
  }

  /** Starts a chat that calls the skill (opens the personality picker). */
  async call(name: string): Promise<void> {
    await this.card(name).getByRole('button', { name: 'Call' }).click();
  }

  // --- editor modal --------------------------------------------------------
  //
  // A hand-rolled overlay (not ui-modal): `.skills-editor-modal`, titled
  // "Create Skill" or "Edit Skill". Opened by the Edit action, or by the
  // sidebar's "Create skill" quick action (which sets ?create=1).

  /**
   * Opens the editor in create mode. There is no "new skill" button on the
   * page itself — the only affordance is the sidebar's "Create skill" quick
   * action, which navigates to `/skills?create=1`.
   */
  async openEditorForNew(): Promise<void> {
    await this.shell.quickAction('Create skill');
  }

  readonly editor: Locator;

  readonly editorTitle: Locator;

  /** Placeholders come from ritual-form.component.html, not the "Skill name" the plan assumed. */
  readonly nameInput: Locator;

  readonly descriptionInput: Locator;

  readonly contentInput: Locator;

  /** Read-only marker shown on system skills — the form is disabled for them. */
  readonly readOnlySystemMarker: Locator;

  async fillEditor(details: { name: string; description: string; content: string }): Promise<void> {
    await this.nameInput.fill(details.name);
    await this.descriptionInput.fill(details.description);
    await this.contentInput.fill(details.content);
  }

  /** Submit button is "Create Skill" when creating, "Save Changes" when editing. */
  async submitEditor(mode: 'create' | 'edit' = 'create'): Promise<void> {
    const label = mode === 'create' ? 'Create Skill' : 'Save Changes';
    await this.editor.getByRole('button', { name: label }).click();
  }

  async cancelEditor(): Promise<void> {
    await this.editor.getByRole('button', { name: 'Cancel' }).click();
  }
}
