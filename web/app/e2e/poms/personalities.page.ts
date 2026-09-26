import type { Page } from '@playwright/test';

/** The personality list page (`/personalities`). */
export class PersonalitiesPage {
  constructor(private readonly page: Page) {}

  async navigateTo(): Promise<void> {
    await this.page.goto('/personalities');
  }

  /**
   * A personality's card in the grid. The card is an `<article>` labelled by
   * its own `<h3>` title (personality-card.component.ts), so the accessible
   * name is the personality name.
   */
  card(name: string) {
    return this.page.getByRole('article', { name });
  }

  /**
   * The "Default" badge shown in place of the star button once a card's
   * personality is the account default (personality-card.component.ts).
   */
  defaultBadge(name: string) {
    return this.card(name).getByText('Default', { exact: true });
  }

  /**
   * Star button that sets a (non-default) personality as the account
   * default, directly from the list — this is the affordance PR #138 added
   * so switching the default no longer requires leaving for Settings. Only
   * rendered for cards that aren't already the default.
   */
  makeDefaultButton(name: string) {
    return this.card(name).getByRole('button', { name: `Make ${name} default` });
  }

  async makeDefault(name: string): Promise<void> {
    await this.makeDefaultButton(name).click();
  }

  async openCreateManually(): Promise<void> {
    // The page header and the sidebar both have this button; on mobile the sidebar's copy is
    // off-canvas, so pick whichever is visible rather than the first in DOM order.
    await this.page.getByRole('button', { name: 'Create Manually' }).filter({ visible: true }).first().click();
  }

  /**
   * Fills and submits the manual-create form. Deliberately not the AI
   * "Generate Personality" wizard — see e2e/README.md.
   */
  async createManually(name: string, description: string): Promise<void> {
    await this.page.getByPlaceholder('e.g. Vera Calder, Filbolt Pottsworth').fill(name);
    await this.page.getByPlaceholder('Describe how this personality should behave, talk, and react…').fill(description);
    await this.page.getByRole('button', { name: 'Create', exact: true }).click();
  }
}
