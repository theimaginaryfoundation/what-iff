import { test, expect, seedName, shortId } from '../../fixtures';
import { sendChatMessage } from '../../sdk/client';
import { LLM_REPLY_TIMEOUT } from '../../timeouts';

/**
 * Returning user, established account: finding an existing conversation and
 * picking it back up.
 *
 * Deliberately does not navigate to `/chat/<id>` directly, which is how every
 * other chat spec reaches a thread. Arriving by URL means the app builds the
 * conversation from a cold route resolve; arriving by search means it swaps
 * threads inside an already-live workspace, and the prior turns have to
 * hydrate into a component that was showing something else a moment ago.
 * `command-palette.spec.ts` proves the navigation lands on the right URL —
 * this asserts the conversation itself came with it, and can be continued.
 */

test('a returning user resumes an existing conversation from the command palette @journey', async ({
  apiClient,
  chatPage,
  commandPalette,
  isMobile,
  page,
  seed,
  threadListPanel,
  userWithPersonality,
}) => {
  // Real bug found writing this test: on mobile the nav drawer the palette was
  // opened from stays open after the navigation, and its backdrop swallows the
  // composer click. Everything up to the send passes here — it is the
  // continuation half that cannot run until the drawer closes on navigation.
  test.fixme(isMobile, 'Nav drawer stays open after navigating, blocking the composer — see #70');

  const thread = await seed.thread(seedName('thread'), { personalityId: userWithPersonality.personality.id });
  // Seeded through the API, so this turn exists server-side only — the UI has
  // never rendered it, which is what makes it a real hydration assertion below.
  const earlierTurn = `Earlier turn ${shortId()}`;
  await sendChatMessage(apiClient, thread.id as string, earlierTurn);

  // Where a returning user actually starts: the workspace, no thread open.
  await threadListPanel.navigateTo();

  await commandPalette.open();
  await commandPalette.search(thread.name);
  await commandPalette.chooseOption(new RegExp(thread.name));

  await expect(commandPalette.dialog).toBeHidden();
  await expect(page).toHaveURL(new RegExp(`/chat/${thread.id}`));

  // The conversation came with the navigation, not just the route.
  await expect(chatPage.messageText(earlierTurn)).toBeVisible();

  // And it can be carried on from there.
  const followUp = `Follow-up ${shortId()}`;
  await chatPage.sendMessage(followUp);
  await expect(chatPage.messageText(followUp)).toBeVisible();
  await expect(chatPage.lastAssistantBody).not.toBeEmpty({ timeout: LLM_REPLY_TIMEOUT });
});
