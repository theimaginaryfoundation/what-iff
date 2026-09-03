import { test, expect } from '../../../fixtures';

test('expands a completed emoji shortcode in the composer', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(undefined, {
    personalityId: userWithPersonality.personality.id,
  });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.composerInput.pressSequentially('Hello :fox:');

  await expect(chatPage.composerInput).toHaveValue('Hello 🦊');
});

test('leaves unknown emoji shortcodes untouched', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(undefined, {
    personalityId: userWithPersonality.personality.id,
  });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.composerInput.pressSequentially('Keep :not_a_real_emoji: literal');

  await expect(chatPage.composerInput).toHaveValue('Keep :not_a_real_emoji: literal');
});
