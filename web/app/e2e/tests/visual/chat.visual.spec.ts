import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

test(
  'chat composer screen before any message is sent',
  { tag: ['@visual', '@mock-only'] },
  async ({ authenticatedPage: page, chatPage, personalitiesPage, personalityDetailPage, shell }) => {
    // Fixed name (not a Date.now() suffix) so the avatar initials rendered
    // in the chat header stay stable across runs.
    const personalityName = 'E2E Visual Chat Persona';

    await shell.dismissAnnouncementIfPresent();
    await personalitiesPage.navigateTo();
    await shell.dismissAnnouncementIfPresent();
    await personalitiesPage.openCreateManually();
    await personalitiesPage.createManually(
      personalityName,
      'You are a calm, precise assistant used only for visual regression testing. Your answers are always short.',
    );
    await expect(personalityDetailPage.heading(personalityName)).toBeVisible();

    await personalityDetailPage.useInNewChat();
    await expect(page).toHaveURL(/\/chat\/[^/]+$/);

    // Assert the composer is ready and assert no assistant content has
    // rendered yet — this screen must stay free of any LLM output, which is
    // nondeterministic outside the mock backend.
    await expect(chatPage.composerInput).toBeVisible();
    await expect(chatPage.lastAssistantBubble).toHaveCount(0);

    await expect(page).toHaveScreenshot('chat-composer-empty.png', {
      animations: 'disabled',
      // The personality-name header button is flaky here even with a fixed
      // name — see ChatPage#personalityNameButton. The recent-threads
      // sidebar section has its own flaky status icon — see
      // AppShell#recentThreadsSection.
      mask: [...commonMasks(page), chatPage.personalityNameButton, shell.recentThreadsSection],
      // The masked recent-threads row's height isn't fixed by CSS (it grows
      // when an unread badge renders), so the mask rectangle itself shifts
      // by a few px depending on exactly when that badge lands relative to
      // the screenshot — a real layout race, not something a bigger mask
      // fixes. A small tolerance absorbs that boundary without hiding a
      // genuine visual regression elsewhere on the page.
      maxDiffPixelRatio: 0.02,
    });
  },
);
