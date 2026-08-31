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

    // pinIdentityToShortName() clicks Save Changes. The profile content has its
    // own overflow container inside the shared modal body, and the taller form
    // causes that inner container to scroll the button into view. Hide the new
    // fieldset for this legacy snapshot, then normalize both scroll owners.
    await page.evaluate(() => {
      const active = document.activeElement;
      if (active instanceof HTMLElement) active.blur();
    });
    await chatDefaults.evaluate(element => { element.style.display = 'none'; });

    const modalBody = page.locator('.ui-modal__body');
    const profileBody = page.locator('.profile-settings__body');
    await modalBody.evaluate(element => { element.scrollTop = 0; });
    await profileBody.evaluate(element => { element.scrollTop = 0; });
    await page.evaluate(() => new Promise<void>(resolve => {
      requestAnimationFrame(() => requestAnimationFrame(() => resolve()));
    }));

    // Assert the exact state the screenshot depends on. The previous regression
    // passed the outer-body assertion while the nested profile body remained
    // scrolled, so keep both explicit.
    await expect.poll(() => modalBody.evaluate(element => element.scrollTop)).toBe(0);
    await expect.poll(() => profileBody.evaluate(element => element.scrollTop)).toBe(0);

    await expect(page).toHaveScreenshot('profile-settings-modal.png', {
      animations: 'disabled',
      // Identity card carries the per-account email, initials, and
      // "Member since" date; the email form field mirrors the same email.
      mask: [...commonMasks(page), profileSettingsModal.identityCard, profileSettingsModal.emailInput],
    });
  },
);
