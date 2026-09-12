import { test, expect } from '../../fixtures';
import { commonMasks } from './visual.helpers';

/**
 * @functional-coverage tests/functional/chat/emoji-shortcodes.spec.ts
 *
 * Suggestion matching, click and Enter insertion, and the two cases that must
 * NOT trigger the popup are covered behaviourally by the emoji shortcode spec.
 *
 * A baseline pins how this looks; it cannot tell you it still works. See
 * e2e/scripts/check-visual-coverage.mjs.
 */

/**
 * The Slack-style `:shortcode` popup added by #50. The existing
 * `chat.visual.spec.ts` baseline is an *empty* composer, so it can never
 * render this menu — its geometry (anchor point above the textarea, row
 * height, glyph column width) had no pixel contract at all.
 */
test(
  'emoji shortcode autocomplete popup, anchored above the composer',
  { tag: ['@visual', '@mock-only'] },
  async ({ authenticatedPage: page, chatPage, personalitiesPage, personalityDetailPage, shell }) => {
    // Created through the UI with a fixed name rather than taken from the
    // `userWithPersonality` fixture, whose names carry a random suffix. The
    // open context panel renders "<NAME>'S SCRATCHPAD" in the screenshot, so a
    // random name would change the baseline on every run. Same reasoning, and
    // the same fixed-name approach, as chat.visual.spec.ts.
    const personalityName = 'E2E Visual Emoji Persona';

    await shell.dismissAnnouncementIfPresent();
    await personalitiesPage.navigateTo();
    await shell.dismissAnnouncementIfPresent();
    await personalitiesPage.openCreateManually();
    await personalitiesPage.createManually(
      personalityName,
      'You are a calm, precise assistant used only for visual regression testing. Your answers are always short.',
    );
    await expect(personalityDetailPage.nameInput).toHaveValue(personalityName);

    await personalityDetailPage.useInNewChat();
    await expect(page).toHaveURL(/\/chat\/[^/]+$/);
    await expect(chatPage.composerInput).toBeVisible();

    // `:fox` is a deliberately narrow query: the resolver returns a small,
    // stable suggestion set, so the popup's height is fixed rather than
    // depending on how many emoji happen to match a broader prefix.
    await chatPage.composerInput.pressSequentially('Hello :fox');
    await expect(chatPage.emojiAutocomplete).toBeVisible();
    await expect(chatPage.emojiSuggestion(':fox_face:')).toBeVisible();

    // The first row is highlighted on open, and that highlight is part of
    // what the baseline pins — assert it rather than trusting the paint.
    await expect(chatPage.emojiAutocomplete.getByRole('option', { selected: true })).toHaveCount(1);

    // No assistant content may reach this screen: the mock backend's reply
    // timing is not something a pixel baseline should encode.
    await expect(chatPage.lastAssistantBubble).toHaveCount(0);

    await expect(page).toHaveScreenshot('chat-emoji-autocomplete.png', {
      animations: 'disabled',
      mask: [...commonMasks(page), chatPage.personalityNameButton, shell.recentThreadsSection],
      // Same masked-row height race as chat.visual.spec.ts — see the comment there.
      maxDiffPixelRatio: 0.02,
    });
  },
);
