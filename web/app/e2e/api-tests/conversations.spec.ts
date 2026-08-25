import { test, expect, createSecondUser } from './fixtures';
import { createChat, listChats, updateChat, deleteChat, type Chat, ApiError } from '../sdk/client';

/**
 * Chat-thread CRUD, straight against the API: create / list / rename /
 * delete, plus cross-user access. The backend scopes chats by the
 * authenticated user's ID at the datastore level (internal/handlers/chat),
 * so a foreign chat ID reads as 404, not 403.
 */

test.describe('conversations', () => {
  let chat: Chat;

  test.beforeEach(async ({ apiClient }) => {
    chat = await createChat(apiClient, { name: 'e2e-api-conversation' });
  });

  // Tolerates the chat already being gone: the delete test below deletes it
  // itself as part of the assertion, so this is a no-op there, not a second
  // failure on top of a passing test.
  test.afterEach(async ({ apiClient }) => {
    try {
      await deleteChat(apiClient, chat.id!);
    } catch (err) {
      if (!(err instanceof ApiError && err.status === 404)) throw err;
    }
  });

  test('create, list, rename, and delete a chat', async ({ apiClient }) => {
    expect(chat.id).toBeTruthy();
    expect(chat.name).toBe('e2e-api-conversation');

    const listed = await listChats(apiClient);
    expect(listed.some(c => c.id === chat.id)).toBe(true);

    const renamed = await updateChat(apiClient, chat.id!, { name: 'e2e-api-renamed' });
    expect(renamed.name).toBe('e2e-api-renamed');

    await deleteChat(apiClient, chat.id!);
    const afterDelete = await listChats(apiClient);
    expect(afterDelete.some(c => c.id === chat.id)).toBe(false);
  });

  test('a chat can be archived without being deleted', async ({ apiClient }) => {
    const archived = await updateChat(apiClient, chat.id!, { archived: true });
    expect(archived.archived).toBe(true);
  });

  test.describe('cross-user access', () => {
    let other: Awaited<ReturnType<typeof createSecondUser>>;

    test.beforeEach(async () => {
      other = await createSecondUser();
    });

    test.afterEach(async () => {
      await other.cleanup();
    });

    test("one user cannot read, rename, or delete another user's chat", async () => {
      const getResult = await other.apiClient.GET('/chat/{id}', { params: { path: { id: chat.id! } } });
      expect(getResult.response.status).toBe(404);

      const patchResult = await other.apiClient.PATCH('/chat/{id}', {
        params: { path: { id: chat.id! } },
        body: { name: 'stolen' },
      });
      expect(patchResult.response.status).toBe(404);

      const deleteResult = await other.apiClient.DELETE('/chat/{id}', { params: { path: { id: chat.id! } } });
      expect(deleteResult.response.status).toBe(404);
    });
  });
});
