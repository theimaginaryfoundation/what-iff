import type { Page } from '@playwright/test';

import { test, expect, signInAs, authenticateAsNewUser, attachCoverage } from '../../../fixtures';
import { deleteUser, type TestUser } from '../../../fixtures/api';
import { ProfileSettingsModal, ProviderKeyPrompt, ProvidersTab } from '../../../poms';
import { getProviderAccess } from '../../../sdk/client';

/**
 * Connecting providers (ADR 0x027): the first-run prompt, the Providers tab in
 * Profile & Settings with its one form for adding and editing a connection,
 * and the step on to creating a personality. Mock-only: the self-hosted build
 * takes keys from accounts, and under the mock backend listing a provider's
 * models answers a fixed two without leaving the server. Deployed builds
 * supply their own keys and never show any of this.
 */

/** A made-up OpenAI key; under the mock backend nothing is sent anywhere. */
const MADE_UP_OPENAI = 'sk-e2e-not-a-real-key-1234';

/** Registers an account with no provider key and signs it in. */
async function signInWithoutAKey(page: Page): Promise<{ user: TestUser; accessToken: string }> {
  const registered = await authenticateAsNewUser({ providerKey: false });
  await signInAs(page, registered.user);
  return registered;
}

test.describe('first run without a provider', () => {
  let accessToken: string | undefined;

  test.afterEach(async () => {
    if (accessToken) {
      await deleteUser(accessToken);
      accessToken = undefined;
    }
  });

  test('asks for OpenAI, connects it with the password, then points on to a personality', { tag: '@mock-only' }, async ({ page }, testInfo) => {
    const registered = await signInWithoutAKey(page);
    accessToken = registered.accessToken;
    const prompt = new ProviderKeyPrompt(page);
    const providers = new ProvidersTab(page);

    await test.step('the prompt holds until there is a key', async () => {
      await expect(prompt.root).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(prompt.root).toBeVisible();
    });

    await test.step('its button opens settings on the Providers tab, which asks for OpenAI', async () => {
      await prompt.addKeyButton.click();
      await expect(providers.section).toBeVisible();
      await providers.connectOpenAIButton.click();
      await expect(providers.form.getByLabel('Provider', { exact: true })).toHaveValue('openai');
      await expect(providers.form.getByLabel('Address', { exact: true })).toHaveValue(/api\.openai\.com/);
    });

    await test.step('a wrong password is refused, keeping the key typed, without signing out', async () => {
      await providers.fill({ key: MADE_UP_OPENAI, password: 'not-the-password' });
      await providers.submit();
      await expect(providers.form.getByRole('alert')).toHaveText("That password didn't match. Try again.");
      await expect(providers.form.getByLabel('API key', { exact: true })).toHaveValue(MADE_UP_OPENAI);
      await expect(page).not.toHaveURL(/\/auth\/login/);
    });

    await test.step('the right password connects it and lists its models', async () => {
      await providers.fill({ password: registered.user.password });
      await providers.submit();
      await expect(providers.form).toContainText('Connected to OpenAI');
      await expect(providers.form).toContainText('Found 2 models');
      await providers.done();
      await expect(providers.row('openai')).toContainText('••••1234');
      await expect(providers.section).not.toContainText(MADE_UP_OPENAI);
      await expect(providers.connectOpenAIButton).toHaveCount(0);
    });

    await test.step('onboarding goes on to creating a personality', async () => {
      await providers.nextStepButton.click();
      await expect(page).toHaveURL(/\/personality/);
      await expect(prompt.root).toBeHidden();
    });

    await attachCoverage(page, testInfo);
  });
});

test('an account adds a server on this computer, renames it, and removes OpenAI', { tag: ['@mock-only', '@mutates-account'] }, async ({ page, apiClient, authenticatedPage, testUser }, testInfo) => {
  expect(await getProviderAccess(apiClient)).toMatchObject({ keys_from: 'account', providers: ['openai'] });
  const prompt = new ProviderKeyPrompt(authenticatedPage);
  const providers = new ProvidersTab(authenticatedPage);
  const settings = new ProfileSettingsModal(authenticatedPage);

  await expect(prompt.root).toBeHidden();
  await settings.open();
  await providers.tab.click();
  await expect(providers.row('openai')).toContainText('••••');

  await test.step('add Ollama at its own address, without a key', async () => {
    await providers.addButton.click();
    await providers.fill({ provider: 'Ollama', address: 'http://localhost:11500/v1', password: testUser.password });
    await providers.submit();
    await expect(providers.form).toContainText('Found 2 models');
    await providers.done();
    await expect(providers.row('ollama')).toContainText('This computer');
    await expect(providers.row('ollama')).toContainText('localhost:11500');
    await expect(providers.row('ollama')).toContainText('No key');
  });

  await test.step('rename it without a password', async () => {
    await providers.row('ollama').getByRole('button', { name: 'Edit' }).click();
    await expect(providers.form.getByLabel('Your WhatIff password', { exact: true })).toHaveCount(0);
    await providers.fill({ name: 'Desk Ollama' });
    await providers.submit();
    await expect(providers.form).toHaveCount(0);
    await expect(providers.row('ollama')).toContainText('Desk Ollama');
  });

  await test.step('remove OpenAI, after a warning, and the tab asks for it again', async () => {
    await providers.row('openai').getByRole('button', { name: 'Remove' }).click();
    const confirm = authenticatedPage.getByRole('dialog').filter({ hasText: 'Remove OpenAI?' });
    await expect(confirm).toContainText('needs an OpenAI key');
    await confirm.getByRole('button', { name: 'Remove' }).click();
    await expect(providers.row('openai')).toHaveCount(0);
    await expect(providers.connectOpenAIButton).toBeVisible();
  });

  // With no OpenAI key left, closing settings brings the first-run prompt back.
  await settings.close();
  await expect(prompt.root).toBeVisible();

  await attachCoverage(page, testInfo);
});
