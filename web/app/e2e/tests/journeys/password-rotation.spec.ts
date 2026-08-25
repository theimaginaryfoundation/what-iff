import { test, expect, signInAs, shortId } from '../../fixtures';
import { AUTH_REDIRECT_TIMEOUT } from '../../timeouts';

/**
 * Journey: a returning user rotates their password mid-session and comes
 * back to their own data. Crosses auth, profile-settings and personality
 * ownership — the point is that the new credentials work AND that the
 * account's content is still there afterwards, which no single functional
 * spec covers.
 *
 * The account comes from the `testUser` fixture (API-registered, self-
 * cleaning). Teardown deletes it with the token issued at registration, so
 * the password change mid-test doesn't break cleanup.
 *
 * @mutates-account: that self-cleaning story only holds when `testUser` is a
 * fresh, disposable account (mock/local). On a config with a static test
 * account (see fixtures/static-account.ts), `testUser` resolves to one
 * pre-existing, shared account that is never deleted — so this test would
 * permanently change that account's real password with no revert. Such
 * configs exclude it via `grepInvert`.
 */
test(
  'returning user: change password → log out → log back in with data intact',
  { tag: ['@journey', '@mutates-account'] },
  async ({ page, personalitiesPage, personalityDetailPage, profileSettingsModal, shell, testUser }) => {
    const newPassword = 'E2eRotated789!';
    const personalityName = `E2E Rotation Persona ${shortId()}`;

    await test.step('log in with the original password', async () => {
      await signInAs(page, testUser);
      await shell.dismissAnnouncementIfPresent();
    });

    await test.step('create a personality to own before the rotation', async () => {
      await personalitiesPage.navigateTo();
      await shell.dismissAnnouncementIfPresent();
      await personalitiesPage.openCreateManually();
      await personalitiesPage.createManually(
        personalityName,
        'You are a terse, friendly assistant used only for automated end-to-end testing.',
      );

      await expect(personalityDetailPage.heading(personalityName)).toBeVisible();
    });

    await test.step('change the password', async () => {
      await profileSettingsModal.open();
      await expect(profileSettingsModal.heading).toBeVisible();

      await profileSettingsModal.changePassword(testUser.password, newPassword);
      await expect(profileSettingsModal.passwordUpdatedMessage).toBeVisible();
    });

    await test.step('log out', async () => {
      // The only logout affordance is inside this modal; changing the password
      // does not end the session on its own.
      await profileSettingsModal.logout();
      await expect(page).toHaveURL(/\/auth\/login/, {
        timeout: AUTH_REDIRECT_TIMEOUT,
      });
    });

    await test.step('log back in with the new password', async () => {
      await signInAs(page, { email: testUser.email, password: newPassword });
      await shell.dismissAnnouncementIfPresent();
    });

    await test.step('the personality created before the rotation is still listed', async () => {
      await personalitiesPage.navigateTo();
      await shell.dismissAnnouncementIfPresent();
      await expect(personalitiesPage.card(personalityName)).toBeVisible();
    });
  },
);
