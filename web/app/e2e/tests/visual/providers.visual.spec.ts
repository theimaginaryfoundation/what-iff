import { test, expect } from '../../fixtures';
import { ProvidersTab } from '../../poms';
import { commonMasks } from './visual.helpers';

/**
 * @functional-coverage tests/functional/profile/providers.spec.ts
 *
 * Connecting, renaming and removing providers, and the first-run prompt, are
 * covered behaviourally by the functional providers spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

/**
 * A fresh e2e account has an OpenAI key stored as part of registration, so the
 * tab shows one deterministic row: the fixture key's last four characters.
 */
test(
  'providers tab, OpenAI connected',
  { tag: ['@visual', '@mock-only'] },
  async ({ authenticatedPage: page, profileSettingsModal }) => {
    const providers = new ProvidersTab(page);
    await profileSettingsModal.open();
    await providers.tab.click();
    await expect(providers.row('openai')).toContainText('••••0000');

    await expect(page).toHaveScreenshot('providers-tab.png', {
      animations: 'disabled',
      mask: commonMasks(page),
    });
  },
);

/**
 * The add form on a cloud preset. OpenAI's address is the same wherever the API
 * runs; a local preset's default depends on whether it runs in a container, so
 * it would differ between this baseline and CI.
 */
test(
  'providers form, adding a cloud provider',
  { tag: ['@visual', '@mock-only'] },
  async ({ authenticatedPage: page, profileSettingsModal }) => {
    const providers = new ProvidersTab(page);
    await profileSettingsModal.open();
    await providers.tab.click();
    await providers.addButton.click();
    await providers.fill({ provider: 'OpenRouter' });
    await expect(providers.form.getByLabel('Address', { exact: true })).toHaveValue('https://openrouter.ai/api/v1');

    await expect(page).toHaveScreenshot('providers-form-cloud.png', {
      animations: 'disabled',
      mask: commonMasks(page),
    });
  },
);
