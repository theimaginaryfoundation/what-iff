import { test, expect, seedName } from '../../fixtures';
import { LLM_REPLY_TIMEOUT, UI_REACTION_TIMEOUT } from '../../timeouts';

/**
 * Returning user, mid-conversation: staging a skill onto a message from the
 * composer.
 *
 * Starts from an account that already has a personality, a skill and a thread
 * — the state a real user is in most of the time — rather than from
 * registration, which `new-user-lifecycle.spec.ts` already covers.
 *
 * The composer's skill picker (`openSkillPicker()` in
 * chat-composer.component.ts) had no e2e coverage at all: `skills.spec.ts`
 * covers authoring at `/skills`, and nothing exercised the path that puts an
 * authored skill onto an actual message. It is also a different data source
 * from the Skills page — `getAvailableRituals(chatId)` merged with
 * `listSystemRituals()` — so a skill that lists fine at `/skills` can still be
 * missing here.
 */

test('a returning user stages a skill onto a message and sends it @journey', async ({
  chatPage,
  page,
  seed,
  userWithPersonality,
}) => {
  const skillName = seedName('skill');
  const skill = await seed.ritual({ name: skillName, personalityId: userWithPersonality.personality.id });
  const thread = await seed.thread(seedName('thread'), { personalityId: userWithPersonality.personality.id });

  await chatPage.navigateTo(thread.id as string);

  // --- discover the skill from inside the conversation ----------------------
  await chatPage.openSkillPickerFromMenu();
  await expect(chatPage.skillPickerDialog).toBeVisible();

  // Narrow before asserting: the picker lists every skill available to this
  // chat, which on a shared deployed account includes other workers' seeded
  // skills and the built-in system ones.
  await chatPage.narrowSkillsTo(skillName);
  await expect(chatPage.skillOption(skillName)).toBeVisible();

  // --- stage it, unstage it, stage it again --------------------------------
  await chatPage.attachSkill(skillName);
  await expect(chatPage.pendingSkillChip(skillName)).toBeVisible();

  await chatPage.removePendingSkill(skillName);
  await expect(chatPage.pendingSkillChip(skillName)).toBeHidden();

  // Re-open through the menu: the `/skill` slash command is the other entry
  // point into this picker, but Enter deliberately does not run slash commands
  // on soft-keyboard projects, so it gets its own desktop-only test in
  // tests/functional/chat/skill-picker.spec.ts rather than splitting this
  // journey across projects.
  await chatPage.openSkillPickerFromMenu();
  await expect(chatPage.skillPickerDialog).toBeVisible();
  await chatPage.narrowSkillsTo(skillName);
  await chatPage.attachSkill(skillName);
  await expect(chatPage.pendingSkillChip(skillName)).toBeVisible();

  // --- send, and the staged skill goes with the message --------------------
  const message = `Use the ${skillName} skill.`;
  // Assert on the outbound request rather than only on the composer clearing:
  // an empty chip row is also what a send that silently dropped the staged
  // skill looks like. `ChatSessionService.sendMessage` puts them on the body's
  // `rituals` field as whole objects (not ids), and omits the field entirely
  // when nothing is staged — so this fails loudly if that contract changes.
  const sendRequest = page.waitForRequest(
    req => req.method() === 'POST' && req.url().includes(`/chat/${thread.id}/chat-message`),
  );
  await chatPage.sendMessage(message);
  const sentRituals = ((await sendRequest).postDataJSON() as { rituals?: { id: string }[] }).rituals ?? [];
  expect(sentRituals.map(r => r.id)).toContain(skill.id);

  await expect(chatPage.messageText(message)).toBeVisible();
  // Staged skills are consumed by the send, not left on the composer.
  await expect(chatPage.pendingSkillChip(skillName)).toBeHidden({ timeout: UI_REACTION_TIMEOUT });
  await expect(chatPage.lastAssistantBody).not.toBeEmpty({ timeout: LLM_REPLY_TIMEOUT });
});
