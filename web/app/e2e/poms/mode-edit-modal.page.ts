import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/**
 * The mode/mood editor (`/mode`, `features/mood/components/mode-edit-modal.component.ts`).
 * A hand-rolled overlay (not `ui-modal`): its own `.mode-modal-backdrop` click
 * handler and its own `document:keydown.escape` host listener, both of which
 * emit `dismissRequested` — routed through `MoodListFacade.requestCloseModeModal()`,
 * which guards the dismissal behind `ConfirmationService.confirmDiscardChanges()`
 * when the form is dirty. The header ✕ and footer "Cancel" button instead emit
 * `close` directly and dismiss immediately, with no confirmation.
 */
export class ModeEditModalPage {
  private readonly shell: AppShell;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.backdrop = this.page.locator('.mode-modal-backdrop');
    this.panel = this.page.locator('.mode-modal');
    this.nameInput = this.page.getByPlaceholder('Mode name');
    this.descriptionInput = this.page.getByPlaceholder('How should this mode feel? e.g. Concise, direct, no filler.');
    this.saveButton = this.panel.getByRole('button', { name: 'Save', exact: true });
    this.closeButton = this.panel.getByRole('button', { name: 'Close modal' });
    this.cancelButton = this.panel.getByRole('button', { name: 'Cancel', exact: true });
  }

  async navigateTo(): Promise<void> {
    await this.page.goto('/mode');
    await this.shell.dismissAnnouncementIfPresent();
  }

  /** Opens the modal in create mode via the sidebar's "Create mode" quick action. */
  async openCreate(): Promise<void> {
    await this.shell.quickAction('Create mode', 'modes');
  }

  /**
   * A mode card in the grid, located by its title. Cards carry no
   * aria-label (`mode-card.component.html`), so this filters by the `<h3>`.
   */
  card(name: string): Locator {
    return this.page.locator('article.mode-card').filter({ has: this.page.locator('h3', { hasText: name }) });
  }

  /** Opens the modal in edit mode for an existing card. */
  async openEdit(name: string): Promise<void> {
    // `openCreate()`'s quick action leaves the mobile off-canvas sidebar open
    // (nothing in `openCreateMode()` collapses it), so its backdrop button
    // keeps intercepting pointer events over the grid below until dismissed.
    await this.closeMobileSidebarIfOpen();
    await this.card(name).getByRole('button', { name: 'Edit mode basics' }).click();
  }

  private async closeMobileSidebarIfOpen(): Promise<void> {
    // `.app-layout__sidebar-backdrop` (app-layout.component.scss): fixed,
    // full-viewport, but the off-canvas sidebar sits on top of it
    // (z-index 45 vs. 44) and covers up to `min(85vw, 300px)` of that same
    // viewport. A plain `.click()` targets the backdrop's bounding-box
    // center, which on a narrow phone viewport still falls under the
    // sidebar, so this clicks near the right edge instead — always past the
    // 300px cap, and never covered.
    const backdrop = this.page.locator('.app-layout__sidebar-backdrop');
    if (await backdrop.isVisible().catch(() => false)) {
      const viewport = this.page.viewportSize();
      const x = viewport ? viewport.width - 5 : 5;
      await backdrop.click({ position: { x, y: 10 } });
    }
  }

  readonly backdrop: Locator;

  readonly panel: Locator;

  readonly nameInput: Locator;

  readonly descriptionInput: Locator;

  async fillForm(details: { name?: string; description?: string }): Promise<void> {
    if (details.name !== undefined) {
      await this.nameInput.fill(details.name);
    }
    if (details.description !== undefined) {
      await this.descriptionInput.fill(details.description);
    }
  }

  readonly saveButton: Locator;

  async save(): Promise<void> {
    await this.saveButton.click();
  }

  /** Header ✕ — dismisses immediately, no discard confirmation. */
  readonly closeButton: Locator;

  /** Footer "Cancel" — also dismisses immediately, no discard confirmation. */
  readonly cancelButton: Locator;

  /**
   * Clicks the backdrop outside the centered panel. A click at the backdrop's
   * own center would land on the panel on top of it, so this targets a corner
   * that's never covered.
   */
  async clickBackdrop(): Promise<void> {
    await this.backdrop.click({ position: { x: 10, y: 10 } });
  }

  async pressEscape(): Promise<void> {
    await this.page.keyboard.press('Escape');
  }

  /**
   * Creates a mode through the UI (create form → save) and returns its id
   * from the create response.
   *
   * Deliberately not `openEdit()` + card click for the edit half of a test:
   * `MoodListFacade.openEditMood(id, navigate=true)` both fetches the mood
   * directly *and* triggers a second fetch via the `/mode/:id` route's
   * `paramMap` subscription (`handleRouteMoodId` in mood-list.component.ts).
   * Those two `getMood()` calls race, and whichever resolves last overwrites
   * the form — including over an edit a test just made. Navigating straight
   * to `/mode/:id` (this id) triggers only the route-subscription fetch, so
   * `openEditById()` is the race-free way back into an existing mode.
   */
  async createViaUi(details: { name: string; description?: string }): Promise<string> {
    await this.openCreate();
    await this.fillForm(details);
    const [response] = await Promise.all([
      this.page.waitForResponse(res => res.request().method() === 'POST' && /\/mood$/.test(new URL(res.url()).pathname) && res.ok()),
      this.save(),
    ]);
    const body = (await response.json()) as { id: string };
    return body.id;
  }

  /** Opens the modal in edit mode by navigating straight to `/mode/:id` — see `createViaUi()`. */
  async openEditById(id: string): Promise<void> {
    await this.page.goto(`/mode/${id}`);
  }

  /**
   * Associates a mode card (in the grid, not the edit modal) with a
   * personality via its inline `app-mode-association-picker`. Scoped to the
   * button role (its accessible name is actually "+ Add personality", the
   * leading "+" glyph included) to avoid matching the `@empty`-state
   * `<span>` placeholder, which shares the same "Add personality" text but
   * isn't a button.
   */
  async addPersonality(modeName: string, personalityName: string): Promise<void> {
    // Same mobile off-canvas sidebar issue as openEdit() — see closeMobileSidebarIfOpen().
    await this.closeMobileSidebarIfOpen();
    const card = this.card(modeName);
    await card.getByRole('button', { name: 'Add personality' }).click();
    await card.getByPlaceholder('Filter personalities...').fill(personalityName);
    await card.locator('.mode-card__personality-option').filter({ hasText: personalityName }).click();
  }
}
