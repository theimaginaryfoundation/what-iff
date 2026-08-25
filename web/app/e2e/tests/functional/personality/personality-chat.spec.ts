import { test, expect, shortId } from '../../../fixtures';
import { LLM_REPLY_TIMEOUT } from '../../../timeouts';

/**
 * End-to-end: create a personality manually (works under any LLM_BACKEND —
 * see e2e/README.md for why "Generate Personality" isn't used here), start a
 * new chat with it, send a message, and confirm a reply comes back.
 */
test('create a personality and send/receive a chat message with it', async ({
  authenticatedPage: page,
  chatPage,
  personalitiesPage,
  personalityDetailPage,
  shell,
}) => {
  const personalityName = `E2E Persona ${shortId()}`;

  // Via the `authenticatedPage` fixture rather than driving the login form: this
  // spec is about personalities and chat, not authentication. Against the
  // deployed configs the form is not even reachable — they preload a shared
  // session through `storageState`, so navigating to /auth/login redirects
  // straight back out and the form never renders. Every extra UI login also
  // costs a real Cognito authentication, which is what the shared-session
  // setup exists to avoid.
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

    // Successful create navigates to the personality detail page.
    await expect(page).toHaveURL(/\/personality\/[^/]+$/);
    await expect(personalityDetailPage.heading(personalityName)).toBeVisible();
  });

  await test.step('start a new chat with this personality', async () => {
    await personalityDetailPage.useInNewChat();
    await expect(page).toHaveURL(/\/chat\/[^/]+$/);
  });

  await test.step('send a message and receive a reply', async () => {
    const messageText = 'Hello from the Playwright end-to-end test!';
    await chatPage.sendMessage(messageText);

    await expect(chatPage.messageText(messageText)).toBeVisible();

    // Both waits carry LLM_REPLY_TIMEOUT: the bubble is a streaming
    // placeholder that appears at once, while the body fills in as the model
    // generates — see the note in new-user-lifecycle.spec.ts.
    const assistantBubble = chatPage.lastAssistantBubble;
    await expect(assistantBubble).toBeVisible({ timeout: LLM_REPLY_TIMEOUT });
    await expect(assistantBubble.locator('.bubble__body')).not.toBeEmpty({
      timeout: LLM_REPLY_TIMEOUT,
    });
  });
});
