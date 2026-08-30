import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

test(
  'profile & settings modal, profile tab open',
  { tag: ['@visual', '@mock-only'] },
  async ({ authenticatedPage: page, profileSettingsModal }) => {
    await profileSettingsModal.open();
    await expect(profileSettingsModal.heading).toBeVisible();
    await expect(profileSettingsModal.emailInput).not.toBeEmpty();
    // A fresh e2e user has no first/last name, so the identity card's name
    // line falls back to the full `username` (~40 random chars) — right at
    // this viewport's wrap boundary, so it flips between one and two lines
    // depending on that run's random glyph widths. Pin it to a short, fixed
    // name so the card's height (and therefore everything below it) is
    // deterministic — see pinIdentityToShortName().
    await profileSettingsModal.pinIdentityToShortName();

    const chatDefaults = page.getByTestId('chat-defaults');
    await expect(chatDefaults.getByText('Default model', { exact: true })).toBeVisible();
    await expect(chatDefaults.getByText('Default personality', { exact: true })).toBeVisible();

    // Chat defaults are an intentional addition covered by the assertions
    // above. Remove the entire added fieldset from layout, rather than only
    // its two labels, so this legacy profile snapshot still compares the
    // pre-existing profile/password UI at the same geometry.
    await chatDefaults.evaluate(element => { element.style.display = 'none'; });
    await page.locator('.ui-modal__body').evaluate(element => { element.scrollTop = 0; });
    await page.evaluate(() => {
      window.scrollTo(0, 0);
      document.documentElement.scrollTop = 0;
      document.body.scrollTop = 0;
    });
    await page.evaluate(() => new Promise<void>(resolve => requestAnimationFrame(() => resolve())));

    await expect(page).toHaveScreenshot('profile-settings-modal.png', {
      animations: 'disabled',
      // Identity card carries the per-account email, initials, and
      // "Member since" date; the email form field mirrors the same email.
      mask: [...commonMasks(page), profileSettingsModal.identityCard, profileSettingsModal.emailInput],
    });
  },
);
