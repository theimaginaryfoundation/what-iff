import { test, expect, uniqueId } from '../../fixtures';
import { AUTH_REDIRECT_TIMEOUT, LLM_REPLY_TIMEOUT } from '../../timeouts';

/**
 * Journey: a brand-new account from the registration form through to a
 * personalised, chatted-with, profile-filled state. Everything the functional
 * specs cover in isolation, in the order a real first-time user hits it — the
 * value here is the sequencing, not the individual steps.
 *
 * The account is registered through the UI rather than the `testUser`
 * fixture, so it is NOT self-cleaning, and nothing collects it afterwards —
 * no `e2e-` prefix sweep exists (the strategy doc lists one as future work).
 * Registration is local username/password (no terms-of-service or email
 * verification). Tagged @needs-cognito-admin because it creates a real account
 * via the register form that nothing here deletes, so it leaves state behind on
 * any persistent backend.
 */
test(
  'new user: register → create personality → chat → fill in profile',
  { tag: ['@journey', '@needs-cognito-admin'] },
  async ({ chatPage, page, personalitiesPage, personalityDetailPage, profileSettingsModal, registerPage, shell }) => {
    const unique = uniqueId();
    const password = 'E2ePlaywright123!';
    const personalityName = `E2E Journey Persona ${unique}`;

    await test.step('register through the UI', async () => {
      await registerPage.navigateTo();

      await registerPage.fillCredentials({
        username: `e2e-journey-${unique}`,
        email: `e2e-journey-${unique}@example.test`,
        password,
      });
      await registerPage.submit();

      await expect(page).not.toHaveURL(/\/auth\/register/, {
        timeout: AUTH_REDIRECT_TIMEOUT,
      });
    });

    await test.step('dismiss the first-run announcement', async () => {
      await shell.dismissAnnouncementIfPresent();
    });

    await test.step('create a personality manually', async () => {
      await personalitiesPage.navigateTo();
      await shell.dismissAnnouncementIfPresent();
      await personalitiesPage.openCreateManually();
      await personalitiesPage.createManually(
        personalityName,
        'You are a terse, friendly assistant used only for automated end-to-end testing.',
      );

      await expect(page).toHaveURL(/\/personality\/[^/]+$/);
      await expect(personalityDetailPage.heading(personalityName)).toBeVisible();
    });

    await test.step('chat with it', async () => {
      await personalityDetailPage.useInNewChat();
      await expect(page).toHaveURL(/\/chat\/[^/]+$/);

      const messageText = 'Hello from the Playwright journey test!';
      await chatPage.sendMessage(messageText);
      await expect(chatPage.messageText(messageText)).toBeVisible();

      // Backend-agnostic: mock echo and a real local model both satisfy this,
      // so the journey runs unchanged in every environment.
      //
      // Both waits need LLM_REPLY_TIMEOUT, not just the first. The bubble
      // appears immediately as a streaming placeholder; the *body* is what fills
      // in as tokens arrive, so it is the assertion that actually waits on the
      // model. Leaving it on the default 5s passed against the mock backend
      // (which echoes instantly) and failed against a real local model.
      await expect(chatPage.lastAssistantBubble).toBeVisible({
        timeout: LLM_REPLY_TIMEOUT,
      });
      await expect(chatPage.lastAssistantBody).not.toBeEmpty({
        timeout: LLM_REPLY_TIMEOUT,
      });
    });

    await test.step('set the profile name and confirm it survives a reload', async () => {
      await profileSettingsModal.open();
      await expect(profileSettingsModal.heading).toBeVisible();

      await profileSettingsModal.fillProfile({
        firstName: `Journey${unique}`,
        lastName: 'Rename',
        theme: 'light',
      });
      await profileSettingsModal.saveChanges();
      await expect(profileSettingsModal.profileSavedMessage).toBeVisible();

      await page.reload();
      await profileSettingsModal.open();
      await expect(profileSettingsModal.firstNameInput).toHaveValue(`Journey${unique}`);
      await expect(profileSettingsModal.lastNameInput).toHaveValue('Rename');
    });
  },
);
