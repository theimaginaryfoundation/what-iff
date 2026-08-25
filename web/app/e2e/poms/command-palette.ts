import type { Locator, Page } from '@playwright/test';
import { AppShell } from './app-shell.page';

/**
 * Command palette (layout/command-palette) — a global overlay mounted by
 * `app-layout`, opened from the sidebar's "Open command palette" launcher or
 * the ⌘K / Ctrl+K shortcut.
 */
export class CommandPalette {
  private readonly shell: AppShell;

  constructor(private readonly page: Page) {
    this.shell = new AppShell(page);
    this.dialog = this.page.getByRole('dialog', { name: 'Command palette' });
    this.input = this.page.getByPlaceholder('Search threads, personalities, skills, memories...');
    this.results = this.page.getByRole('listbox', { name: 'Search results' });
    this.emptyMessage = this.page.locator('.cmd-palette__empty');
    this.options = this.results.getByRole('option');
  }

  readonly dialog: Locator;

  readonly input: Locator;

  readonly results: Locator;

  readonly options: Locator;

  option(name: string | RegExp): Locator {
    return this.results.getByRole('option', { name });
  }

  readonly emptyMessage: Locator;

  /** Opens via the sidebar launcher (works on mobile through the hamburger). */
  async open(): Promise<void> {
    await this.shell.quickAction('Open command palette');
  }

  async search(query: string): Promise<void> {
    await this.input.fill(query);
  }

  async chooseOption(name: string | RegExp): Promise<void> {
    await this.option(name).first().click();
  }

  async dismiss(): Promise<void> {
    await this.input.press('Escape');
  }
}
