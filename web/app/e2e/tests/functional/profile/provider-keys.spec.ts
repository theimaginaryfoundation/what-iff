import type { Page } from '@playwright/test';

import { test, expect, signInAs, authenticateAsNewUser, attachCoverage } from '../../../fixtures';
import { deleteUser, type TestUser } from '../../../fixtures/api';
import { ProfileSettingsModal, ProviderKeyPrompt, ProviderKeysTab } from '../../../poms';
import { getProviderAccess } from '../../../sdk/client';

/**
 * Bringing your own provider keys: the first-run prompt, the API keys tab in
 * Profile & Settings, and the step on to creating a personality. Mock-only:
 * the self-hosted build takes keys from accounts, while deployed builds supply
 * their own and never show any of this.
 */

const FAKE_KEY = 'sk-e2e-not-a-real-key-1234';

/** Registers an account with no provider key and signs it in. */
async function signInWithoutAKey(page: Page): Promise<{ user: TestUser; accessToken: string }> {
  const registered = await authenticateAsNewUser({ providerKey: false });
  await signInAs(page, registered.user);
  return registered;
}

test.describe('first run without a provider key', () => {
  let accessToken: string | undefined;

  test.afterEach(async () => {
    if (accessToken) {
      await deleteUser(accessToken);
      accessToken = undefined;
    }
  });

  test('asks for a key, takes it with the password, then points on to a personality', { tag: '@mock-only' }, async ({ page }, testInfo) => {
    const registered = await signInWithoutAKey(page);
    accessToken = registered.accessToken;
    const prompt = new ProviderKeyPrompt(page);
    const keys = new ProviderKeysTab(page);

    await test.step('the prompt holds until there is a key', async () => {
      await expect(prompt.root).toBeVisible();
      await page.keyboard.press('Escape');
      await expect(prompt.root).toBeVisible();
    });

    await test.step('its button opens settings on the API keys tab', async () => {
      await prompt.addKeyButton.click();
      await expect(keys.section).toBeVisible();
      await expect(keys.row('openai')).toContainText('Required');
      await expect(keys.row('openai')).toContainText('Not set');
    });

    await test.step('a wrong password is refused without signing out', async () => {
      await keys.addKey('openai', FAKE_KEY, 'not-the-password');
      await expect(keys.row('openai').getByRole('alert')).toHaveText("That password didn't match. Try again.");
      await expect(page).not.toHaveURL(/\/auth\/login/);
    });

    await test.step('the right password stores it, showing only the last four characters', async () => {
      await keys.row('openai').getByLabel('Your WhatIff password').fill(registered.user.password);
      await keys.row('openai').getByRole('button', { name: 'Save key' }).click();
      await expect(keys.section.getByRole('status')).toHaveText('OpenAI key saved.');
      await expect(keys.row('openai')).toContainText('••••1234');
      await expect(keys.section).not.toContainText(FAKE_KEY);
    });

    await test.step('onboarding goes on to creating a personality', async () => {
      await keys.nextStepButton.click();
      await expect(page).toHaveURL(/\/personality/);
      await expect(prompt.root).toBeHidden();
    });

    await attachCoverage(page, testInfo);
  });
});

test('an account with a key is not prompted, and can remove it', { tag: ['@mock-only', '@mutates-account'] }, async ({ page, apiClient, authenticatedPage }, testInfo) => {
  expect(await getProviderAccess(apiClient)).toMatchObject({ keys_from: 'account', providers: ['openai'] });
  const prompt = new ProviderKeyPrompt(authenticatedPage);
  const keys = new ProviderKeysTab(authenticatedPage);
  const settings = new ProfileSettingsModal(authenticatedPage);

  await expect(prompt.root).toBeHidden();

  await settings.open();
  await keys.tab.click();
  await expect(keys.row('openai')).toContainText('••••');

  await keys.row('openai').getByRole('button', { name: 'Remove' }).click();
  await page.getByRole('button', { name: 'Remove key' }).click();
  await expect(keys.row('openai')).toContainText('Not set');

  // With no key left, closing settings brings the first-run prompt back.
  await settings.close();
  await expect(prompt.root).toBeVisible();

  await attachCoverage(page, testInfo);
});
