import { test, expect, seedName, shortId } from '../../../fixtures';

/**
 * Composer skill picker (`openSkillPicker()` in chat-composer.component.ts).
 *
 * The staging-and-sending path is a journey
 * (`tests/journeys/skill-in-conversation.spec.ts`); what is left here is the
 * picker's own two edge behaviours — the second way in, and what it shows when
 * nothing matches.
 */

test('opens the skill picker via the /skill slash command and Enter', async (
  { chatPage, seed, userWithPersonality },
  testInfo,
) => {
  // Touch/mobile projects intentionally don't send on Enter — the composer's
  // `softKeyboard()` check (`detectSoftKeyboard()` in
  // chat-composer.component.ts) treats `(pointer: coarse) and (hover: none)`
  // devices as needing a real newline key, so Enter never resolves the slash
  // command there. Same reasoning as the `/mode` test in mode-picker.spec.ts:
  // the app is being correct for touch keyboards, not failing.
  /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
  test.skip(testInfo.project.name !== 'chromium-desktop', 'Enter-to-send is disabled on soft-keyboard (mobile) projects.');

  const skillName = seedName('skill');
  await seed.ritual({ name: skillName, personalityId: userWithPersonality.personality.id });
  const thread = await seed.thread(seedName('thread'), { personalityId: userWithPersonality.personality.id });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.openSkillPickerViaSlashCommand();
  await expect(chatPage.skillPickerDialog).toBeVisible();

  await chatPage.narrowSkillsTo(skillName);
  await expect(chatPage.skillOption(skillName)).toBeVisible();
});

test('the skill picker reports when nothing matches the filter', async ({ chatPage, seed, userWithPersonality }) => {
  const thread = await seed.thread(seedName('thread'), { personalityId: userWithPersonality.personality.id });
  await chatPage.navigateTo(thread.id as string);

  await chatPage.openSkillPickerFromMenu();
  await expect(chatPage.skillPickerDialog).toBeVisible();

  // A UUID nothing can match, so this holds on a shared account where other
  // workers' skills are also listed.
  await chatPage.narrowSkillsTo(`no-such-skill-${shortId()}`);

  await expect(chatPage.skillOptionList).toBeHidden();
  // The exact copy matters: a load failure renders in the same slot, and
  // "couldn't load" being read as "none match" is the confusion worth
  // catching here.
  await expect(chatPage.skillPickerStatus).toHaveText('No skills match this chat or filter.');
});
