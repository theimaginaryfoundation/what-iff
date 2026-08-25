import { test, expect, seedName } from '../../../fixtures';

/**
 * Composer mode picker (`chat-composer.component.ts`'s "+" menu "Mode" item
 * and the `/mode` slash command). Before PR #261 there was no way to pin a
 * mode from the composer at all — the "+" menu had no entry for it, the
 * slash command was registered as `/mood` while the picker only handled
 * `mode`, and Enter in the composer only ever ran a slash command, nothing
 * else. None of this had e2e coverage.
 */

test('picks a mode from the "+" menu and updates the active-mode indicator', async ({
  chatPage,
  modeEditModalPage,
  seed,
  userWithPersonality,
}) => {
  // Regression test for PR #261: the composer's "+" menu previously had no
  // way to open the mode picker at all.
  const modeName = seedName('mode');

  await modeEditModalPage.navigateTo();
  await modeEditModalPage.openCreate();
  await modeEditModalPage.fillForm({ name: modeName, description: 'Concise and direct.' });
  await modeEditModalPage.save();
  await expect(modeEditModalPage.panel).toBeHidden();
  await modeEditModalPage.addPersonality(modeName, userWithPersonality.personality.name);

  const chat = await seed.thread(seedName('thread'), { personalityId: userWithPersonality.personality.id });
  await chatPage.navigateTo(chat.id);

  await chatPage.openModePickerFromMenu();
  await expect(chatPage.modePickerDialog).toBeVisible();
  await chatPage.pickMode(modeName);
  await expect(chatPage.modePickerDialog).toBeHidden();

  // The menu item's label (`modeMenuLabel()`) switches from "Mode · Auto" to
  // "Mode · <name>" once `ChatSessionService.setActiveMood` round-trips.
  await chatPage.openPlusMenu();
  await expect(chatPage.modeMenuItem).toContainText(modeName);
});

test('opens the mode picker via the /mode slash command and Enter', async (
  { chatPage, modeEditModalPage, seed, userWithPersonality },
  testInfo,
) => {
  // Regression test for PR #261: the slash command was registered as
  // `/mood` while the picker's own `case` only handled the `mode` command
  // id, so `/mood` could never resolve to it — and separately, Enter in the
  // composer only ever ran a slash command, so a command reached via Enter
  // (rather than a menu click) had no coverage at all.
  //
  // Touch/mobile projects intentionally don't send on Enter — the composer's
  // `softKeyboard()` check (`detectSoftKeyboard()` in
  // chat-composer.component.ts) treats `(pointer: coarse) and (hover: none)`
  // devices as needing a real newline key, so Enter never resolves the slash
  // command there. That's the app being correct for touch keyboards, not a
  // gap this test should paper over.
  /* eslint-disable-next-line playwright/no-skipped-test -- a conditional, per-project skip; the rule can't tell it from a blanket skip. */
  test.skip(testInfo.project.name !== 'chromium-desktop', 'Enter-to-send is disabled on soft-keyboard (mobile) projects.');

  const modeName = seedName('mode');

  await modeEditModalPage.navigateTo();
  await modeEditModalPage.openCreate();
  await modeEditModalPage.fillForm({ name: modeName, description: 'Warm and encouraging.' });
  await modeEditModalPage.save();
  await expect(modeEditModalPage.panel).toBeHidden();
  await modeEditModalPage.addPersonality(modeName, userWithPersonality.personality.name);

  const chat = await seed.thread(seedName('thread'), { personalityId: userWithPersonality.personality.id });
  await chatPage.navigateTo(chat.id);

  await chatPage.openModePickerViaSlashCommand();
  await expect(chatPage.modePickerDialog).toBeVisible();
  // The `/mode` text is cleared from the draft once the command runs
  // (`clearSlashFromDraft` in chat-composer.component.ts).
  await expect(chatPage.composerInput).toHaveValue('');

  await chatPage.pickMode(modeName);
  await expect(chatPage.modePickerDialog).toBeHidden();

  await chatPage.openPlusMenu();
  await expect(chatPage.modeMenuItem).toContainText(modeName);

  // Switching back to Auto is reachable the same way.
  await chatPage.modeMenuItem.click();
  await expect(chatPage.modePickerDialog).toBeVisible();
  await chatPage.pickAutoMode();
  await expect(chatPage.modePickerDialog).toBeHidden();
  await chatPage.openPlusMenu();
  await expect(chatPage.modeMenuItem).toContainText('Auto');
});
