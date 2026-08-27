import { test, expect } from './fixtures';
import {
  createChat,
  deleteChat,
  listChatMessages,
  listChatBookmarks,
  sendChatMessage,
  setMessageBookmark,
  type Chat,
} from '../sdk/client';

test.describe('message bookmarks', () => {
  let chat: Chat;

  test.beforeEach(async ({ apiClient }) => {
    chat = await createChat(apiClient, { name: 'e2e-api-bookmarks' });
  });

  test.afterEach(async ({ apiClient }) => {
    await deleteChat(apiClient, chat.id!);
  });

  test('bookmarking a message adds it to the list', async ({ apiClient }) => {
    await sendChatMessage(apiClient, chat.id!, 'please bookmark me');

    const messages = await listChatMessages(apiClient, chat.id!);
    const userMsg = messages.find(m => m.origin === 'User');
    expect(userMsg).toBeTruthy();
    const msgId = userMsg!.id!;

    // Initially no bookmarks.
    const beforeBookmarks = await listChatBookmarks(apiClient, chat.id!);
    expect(beforeBookmarks).toHaveLength(0);

    const updated = await setMessageBookmark(apiClient, chat.id!, msgId, true);
    expect(updated.bookmarked).toBe(true);
    expect(updated.id).toBe(msgId);

    const afterBookmarks = await listChatBookmarks(apiClient, chat.id!);
    expect(afterBookmarks).toHaveLength(1);
    expect(afterBookmarks[0].id).toBe(msgId);
    expect(afterBookmarks[0].snippet).toBeTruthy();
    expect(afterBookmarks[0].origin).toBe('User');
  });

  test('un-bookmarking removes it from the list', async ({ apiClient }) => {
    await sendChatMessage(apiClient, chat.id!, 'toggle me');

    const messages = await listChatMessages(apiClient, chat.id!);
    const msgId = messages.find(m => m.origin === 'User')!.id!;

    await setMessageBookmark(apiClient, chat.id!, msgId, true);
    expect(await listChatBookmarks(apiClient, chat.id!)).toHaveLength(1);

    const removed = await setMessageBookmark(apiClient, chat.id!, msgId, false);
    expect(removed.bookmarked).toBe(false);
    expect(await listChatBookmarks(apiClient, chat.id!)).toHaveLength(0);
  });

  test('bookmarks are scoped to the owning chat', async ({ apiClient }) => {
    const otherChat = await createChat(apiClient, { name: 'e2e-api-bookmarks-other' });
    try {
      await sendChatMessage(apiClient, chat.id!, 'only mine');
      const messages = await listChatMessages(apiClient, chat.id!);
      const msgId = messages.find(m => m.origin === 'User')!.id!;
      await setMessageBookmark(apiClient, chat.id!, msgId, true);

      // The other chat should have no bookmarks.
      const otherBookmarks = await listChatBookmarks(apiClient, otherChat.id!);
      expect(otherBookmarks).toHaveLength(0);
    } finally {
      await deleteChat(apiClient, otherChat.id!);
    }
  });

  test('snippet is a single-line excerpt capped at 140 runes', async ({ apiClient }) => {
    const longText = 'A'.repeat(200);
    await sendChatMessage(apiClient, chat.id!, longText);

    const messages = await listChatMessages(apiClient, chat.id!);
    const msgId = messages.find(m => m.origin === 'User')!.id!;
    await setMessageBookmark(apiClient, chat.id!, msgId, true);

    const [bookmark] = await listChatBookmarks(apiClient, chat.id!);
    expect([...bookmark.snippet!].length).toBeLessThanOrEqual(141); // 140 + ellipsis
    expect(bookmark.snippet).not.toContain('\n');
  });
});
