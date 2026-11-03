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

/** What to fill in the provider form. Fields left out are left as the form has them. */
export interface ProviderFormFields {
  /** A preset's name as the dropdown shows it, when adding. */
  provider?: string;
  name?: string;
  speaks?: 'OpenAI-compatible' | 'Anthropic-compatible';
  address?: string;
  key?: string;
  password?: string;
}

/**
 * The Providers tab of Profile & Settings (providers-section.component) and
 * its one form for adding or editing a connection (provider-form.component).
 * Locates a connection by its row, so tests read "the OpenAI row".
 */
export class ProvidersTab {
  readonly tab: Locator;
  readonly section: Locator;
  readonly nextStepButton: Locator;
  readonly addButton: Locator;
  /** Shown while there is no OpenAI connection. */
  readonly connectOpenAIButton: Locator;
  /** The add/edit form, while it is open. */
  readonly form: Locator;

  constructor(private readonly page: Page) {
    this.tab = page.getByRole('button', { name: 'Providers' });
    this.section = page.getByTestId('providers');
    this.nextStepButton = this.section.getByRole('button', { name: 'Next: create a personality' });
    this.addButton = this.section.getByTestId('providers-add');
    this.connectOpenAIButton = this.section.getByRole('button', { name: 'Add OpenAI key' });
    // The form's dialog sits inside the settings dialog in the DOM, so it is
    // found by its own name (its title) rather than by what it contains.
    this.form = page.getByRole('dialog', { name: /^(Add a provider|Edit .+)$/ });
  }

  /** A connection's row, by the preset it was made from. */
  row(preset: string): Locator {
    return this.section.locator(`[data-preset="${preset}"]`);
  }

  /** Fills the open form. */
  async fill(fields: ProviderFormFields): Promise<void> {
    if (fields.provider) await this.form.getByLabel('Provider', { exact: true }).selectOption({ label: fields.provider });
    if (fields.name !== undefined) await this.form.getByLabel('Name', { exact: true }).fill(fields.name);
    if (fields.speaks) await this.form.getByLabel('Speaks', { exact: true }).selectOption({ label: fields.speaks });
    if (fields.address !== undefined) await this.form.getByLabel('Address', { exact: true }).fill(fields.address);
    if (fields.key !== undefined) await this.form.getByLabel('API key', { exact: true }).fill(fields.key);
    if (fields.password !== undefined) await this.form.getByLabel('Your WhatIff password', { exact: true }).fill(fields.password);
  }

  /** Sends the form: "Connect and list models", or "Save" for a rename. */
  async submit(): Promise<void> {
    await this.form.getByRole('button', { name: /Connect and list models|^Save$/ }).click();
  }

  /** Closes the form from its result. */
  async done(): Promise<void> {
    await this.form.getByRole('button', { name: 'Done' }).click();
  }
}
