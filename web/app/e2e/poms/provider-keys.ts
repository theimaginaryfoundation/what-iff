import type { Locator, Page } from '@playwright/test';

/**
 * The first-run "add your OpenAI key" prompt (provider-key-prompt.component).
 * Shown, undismissable, to an account that brings its own provider keys and
 * has none yet.
 */
export class ProviderKeyPrompt {
  readonly root: Locator;
  readonly addKeyButton: Locator;

  constructor(private readonly page: Page) {
    this.root = page.getByRole('dialog').filter({ has: page.getByRole('heading', { name: 'Add your OpenAI key to get started' }) });
    this.addKeyButton = this.root.getByRole('button', { name: 'Add your OpenAI key' });
  }
}

/**
 * The API keys tab of Profile & Settings (provider-keys-section.component).
 * Locates a provider by its row, so tests read "the OpenAI row".
 */
export class ProviderKeysTab {
  readonly tab: Locator;
  readonly section: Locator;
  readonly nextStepButton: Locator;

  constructor(private readonly page: Page) {
    this.tab = page.getByRole('button', { name: 'API keys' });
    this.section = page.getByTestId('provider-keys');
    this.nextStepButton = this.section.getByRole('button', { name: 'Next: create a personality' });
  }

  row(provider: string): Locator {
    return this.section.locator(`[data-provider="${provider}"]`);
  }

  /** Opens the provider's form, fills it and saves. */
  async addKey(provider: string, key: string, password: string): Promise<void> {
    const row = this.row(provider);
    await row.getByRole('button', { name: /Add key|Replace/ }).click();
    await row.getByLabel(/API key$/).fill(key);
    await row.getByLabel('Your WhatIff password').fill(password);
    await row.getByRole('button', { name: 'Save key' }).click();
  }
}
