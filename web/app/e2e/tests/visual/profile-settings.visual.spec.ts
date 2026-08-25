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

    await expect(page).toHaveScreenshot('profile-settings-modal.png', {
      animations: 'disabled',
      // Identity card carries the per-account email, initials, and
      // "Member since" date; the email form field mirrors the same email.
      mask: [...commonMasks(page), profileSettingsModal.identityCard, profileSettingsModal.emailInput],
    });
  },
);
