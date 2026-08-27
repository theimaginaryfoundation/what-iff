import { test, expect } from '../../../fixtures';
import { sendChatMessage } from '../../../sdk/client';
import { IMMEDIATE_UI_UPDATE_TIMEOUT, UI_REACTION_TIMEOUT } from '../../../timeouts';

/**
 * Bookmark toggle and navigator smoke coverage.
 *
 * A user message is injected directly through the API before navigation so it
 * is present on the initial page load — no need to wait for an LLM reply or
 * use the composer. The navigator (ThreadBookmarksComponent) only appears once
 * the chat has at least one bookmark, so its presence is used to confirm that
 * the bookmark was persisted and synced to the listing endpoint.
 */

test('starring a message bookmarks it and shows the navigator pill', async ({
  chatPage,
  seed,
  userWithPersonality,
  page,
  apiClient,
}) => {
  const thread = await seed.thread();

  // Inject a user message via the API before navigating so it arrives on initial load.
  await sendChatMessage(apiClient, thread.id!, 'please star me');

  await chatPage.navigateTo(thread.id!);

  // Wait for the message to be visible.
  const bubble = chatPage.messageText('please star me');
  await expect(bubble).toBeVisible({ timeout: UI_REACTION_TIMEOUT });

  // Hover to reveal the meta row (opacity: 0 → 1 on hover via CSS).
  await bubble.hover();

  const saveButton = bubble.getByRole('button', { name: 'Bookmark this message' });
  await expect(saveButton).toBeVisible({ timeout: UI_REACTION_TIMEOUT });
  await saveButton.click();

  // Optimistic update: button reflects the new state immediately.
  const removeButton = bubble.getByRole('button', { name: 'Remove bookmark' });
  await expect(removeButton).toBeVisible({ timeout: IMMEDIATE_UI_UPDATE_TIMEOUT });

  // Navigator pill appears once the bookmarks list endpoint confirms the bookmark.
  const navigatorPill = page.getByRole('button', { name: 'Jump to a bookmark' });
  await expect(navigatorPill).toBeVisible({ timeout: UI_REACTION_TIMEOUT });
});

test('bookmark navigator opens a dropdown with the snippet and selecting it closes the menu', async ({
  chatPage,
  seed,
  userWithPersonality,
  page,
  apiClient,
}) => {
  const thread = await seed.thread();
  await sendChatMessage(apiClient, thread.id!, 'navigator target');
  await chatPage.navigateTo(thread.id!);

  const bubble = chatPage.messageText('navigator target');
  await expect(bubble).toBeVisible({ timeout: UI_REACTION_TIMEOUT });
  await bubble.hover();

  await bubble.getByRole('button', { name: 'Bookmark this message' }).click();

  const navigatorPill = page.getByRole('button', { name: 'Jump to a bookmark' });
  await expect(navigatorPill).toBeVisible({ timeout: UI_REACTION_TIMEOUT });

  // Open the dropdown.
  await navigatorPill.click();
  await expect(page.getByRole('menu', { name: 'Bookmarks' })).toBeVisible();

  // The snippet for our message should appear in the list.
  const menuItem = page.getByRole('menuitem').filter({ hasText: 'navigator target' });
  await expect(menuItem).toBeVisible();

  // Clicking a bookmark emits `jump` and closes the dropdown.
  await menuItem.click();
  await expect(page.getByRole('menu', { name: 'Bookmarks' })).toBeHidden({ timeout: UI_REACTION_TIMEOUT });
});

test('un-starring a message removes it from the navigator', async ({
  chatPage,
  seed,
  userWithPersonality,
  page,
  apiClient,
}) => {
  const thread = await seed.thread();
  await sendChatMessage(apiClient, thread.id!, 'toggle off');
  await chatPage.navigateTo(thread.id!);

  const bubble = chatPage.messageText('toggle off');
  await expect(bubble).toBeVisible({ timeout: UI_REACTION_TIMEOUT });
  await bubble.hover();

  // Bookmark it.
  await bubble.getByRole('button', { name: 'Bookmark this message' }).click();
  const navigatorPill = page.getByRole('button', { name: 'Jump to a bookmark' });
  await expect(navigatorPill).toBeVisible({ timeout: UI_REACTION_TIMEOUT });

  // Un-bookmark it.
  await bubble.hover();
  await bubble.getByRole('button', { name: 'Remove bookmark' }).click();

  // Navigator disappears when no bookmarks remain.
  await expect(navigatorPill).toBeHidden({ timeout: UI_REACTION_TIMEOUT });
});
