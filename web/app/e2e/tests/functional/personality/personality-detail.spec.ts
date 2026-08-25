import { test, expect } from '../../../fixtures';

/**
 * Personality detail page smoke coverage for the PersonalityDetailPage POM —
 * everything past "Use in new chat" (personality-chat.spec.ts), which is the
 * only path the detail page previously had a test against.
 */

test('edits and saves the system prompt', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  const newName = `${userWithPersonality.personality.name} v2`;
  const newPrompt = 'Updated by the e2e suite — respond tersely.';
  await personalityDetailPage.editPrompt({ name: newName, systemPrompt: newPrompt });
  await personalityDetailPage.savePrompt();

  await expect(personalityDetailPage.promptEditor).toContainText(newName);
  await expect(personalityDetailPage.promptEditor).toContainText(newPrompt);
  await expect(personalityDetailPage.editPromptButton).toBeVisible();
});

test('cancelling the system prompt editor discards the draft', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  await personalityDetailPage.editPrompt({ systemPrompt: 'this should not be saved' });
  await personalityDetailPage.cancelPrompt();

  await expect(personalityDetailPage.promptEditor).not.toContainText('this should not be saved');
  await expect(personalityDetailPage.editPromptButton).toBeVisible();
});

test('toggles auto-pin new User memories', async ({ personalityDetailPage, userWithPersonality }) => {
  await personalityDetailPage.navigateTo(userWithPersonality.personality.id);

  await expect(personalityDetailPage.autoPinToggle).toHaveAttribute('aria-checked', 'false');

  await personalityDetailPage.toggleAutoPinMemories();
  await expect(personalityDetailPage.autoPinToggle).toHaveAttribute('aria-checked', 'true');

  await personalityDetailPage.toggleAutoPinMemories();
  await expect(personalityDetailPage.autoPinToggle).toHaveAttribute('aria-checked', 'false');
});

/**
 * Attachment upload is intentionally not covered here: `CreateFileAttachment`
 * (internal/handlers/personality/fileattachment.go) always proxies the file to
 * the OpenAI Files API — internal/agent/provider/fileattachment.go — with no
 * mock/local bypass, unlike chat completions. Under `LLM_BACKEND=mock`/`local`
 * that call is blocked by the provider network-egress guard and every upload
 * 500s. Same category as the AI personality wizard and expression generation
 * (see e2e/TEST_PLAN.md's "Constraints" section) — needs the same
 * real-vendor-key-gated, opt-in suite once one exists, not a spec here.
 */

test('makes a personality the account default', { tag: '@mutates-account' }, async ({ personalityDetailPage, seed, userWithPersonality }) => {
  // @mutates-account: default_personality_id lives on the account's own
  // UserPreferences, not on the personality — same category as changing the
  // shared account's profile/password, and just as unsafe to flip mid-run
  // next to another worker on a deployed backend's one shared account.
  //
  // The fixture's own personality claimed "default" on creation, so a second,
  // freshly seeded one is guaranteed not to be — "Make default" only renders
  // for a non-default personality (personality-detail-page.component.html).
  const extra = await seed.personality();
  await personalityDetailPage.navigateTo(extra.id);

  await expect(personalityDetailPage.makeDefaultButton).toBeVisible();
  await personalityDetailPage.makeDefault();

  await expect(personalityDetailPage.makeDefaultButton).toBeHidden();
});

test('deletes a personality after confirming', async ({ personalityDetailPage, seed, userWithPersonality }) => {
  const extra = await seed.personality();
  await personalityDetailPage.navigateTo(extra.id);

  await personalityDetailPage.requestDelete();
  await expect(personalityDetailPage.confirmation.headingText).toHaveText('Delete Personality');

  await personalityDetailPage.confirmation.confirm('Delete');
  await expect(userWithPersonality.page).toHaveURL(/\/personality$/);
});
