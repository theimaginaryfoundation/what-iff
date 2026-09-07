import { test, expect } from '../../../fixtures';

test('suggests and inserts an emoji from an in-progress shortcode', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(undefined, {
    personalityId: userWithPersonality.personality.id,
  });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.composerInput.pressSequentially('Hello :fox');

  await expect(chatPage.emojiAutocomplete).toBeVisible();
  await chatPage.emojiSuggestion(':fox_face:').click();

  await expect(chatPage.composerInput).toHaveValue('Hello 🦊');
  await expect(chatPage.emojiAutocomplete).toBeHidden();
});

test('accepts the highlighted suggestion with Enter without sending', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(undefined, {
    personalityId: userWithPersonality.personality.id,
  });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.composerInput.pressSequentially(':fox');
  await expect(chatPage.emojiAutocomplete).toBeVisible();
  await chatPage.composerInput.press('Enter');

  // Enter accepted the highlighted suggestion: the popup closed, the literal
  // `:fox` fragment was replaced, and the draft was neither cleared nor sent.
  await expect(chatPage.emojiAutocomplete).toBeHidden();
  await expect(chatPage.composerInput).not.toHaveValue(/:?fox/);
  await expect(chatPage.composerInput).not.toHaveValue('');
});

test('does not rewrite a completed shortcode', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(undefined, {
    personalityId: userWithPersonality.personality.id,
  });
  await chatPage.navigateTo(thread.id as string);

  // Typing the closing colon leaves the literal text untouched — no auto-replace.
  await chatPage.composerInput.pressSequentially('Keep :fox: literal');

  await expect(chatPage.composerInput).toHaveValue('Keep :fox: literal');
  await expect(chatPage.emojiAutocomplete).toBeHidden();
});

test('does not suggest for identifier-style colons', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(undefined, {
    personalityId: userWithPersonality.personality.id,
  });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.composerInput.pressSequentially('scope:value');

  await expect(chatPage.emojiAutocomplete).toBeHidden();
  await expect(chatPage.composerInput).toHaveValue('scope:value');
});
