import { test, expect, waitForJobComplete } from './fixtures';
import { createChat, sendChatMessage, listChatMessages, deleteChat, type Chat } from '../sdk/client';
import { API_TEST_TIMEOUT } from '../timeouts';

/**
 * Posting a chat message and reading back the reply, straight against the
 * API. The mock LLM (this suite always runs against `make run-mock`)
 * generates a reply asynchronously via a background job — see
 * `waitForJobComplete` in ./fixtures, which mirrors `poll_job()` in
 * scripts/mock-e2e.sh.
 */

test.describe('messages', () => {
  let chat: Chat;

  test.beforeEach(async ({ apiClient }) => {
    chat = await createChat(apiClient, { name: 'e2e-api-messages' });
  });

  test.afterEach(async ({ apiClient }) => {
    await deleteChat(apiClient, chat.id!);
  });

  test('posting a message eventually produces an assistant reply', async ({ apiClient }) => {
    const posted = await sendChatMessage(apiClient, chat.id!, 'hello from the api-tests suite');
    expect(posted.job_id).toBeTruthy();

    const job = await waitForJobComplete(apiClient, posted.job_id!, API_TEST_TIMEOUT - 5_000);
    expect(job.status).toBe('complete');

    const messages = await listChatMessages(apiClient, chat.id!);
    const userMessage = messages.find(m => m.origin === 'User');
    const assistantMessage = messages.find(m => m.origin === 'Assistant');
    expect(userMessage?.message).toBe('hello from the api-tests suite');
    expect(assistantMessage).toBeTruthy();
    expect(assistantMessage!.message?.length ?? 0).toBeGreaterThan(0);
  });

  test('message history is returned newest-first', async ({ apiClient }) => {
    const first = await sendChatMessage(apiClient, chat.id!, 'first message');
    await waitForJobComplete(apiClient, first.job_id!, API_TEST_TIMEOUT - 5_000);

    const second = await sendChatMessage(apiClient, chat.id!, 'second message');
    await waitForJobComplete(apiClient, second.job_id!, API_TEST_TIMEOUT - 5_000);

    // ListChatMessages orders by sent_at descending (internal/datastore/chatmessage.go).
    const messages = await listChatMessages(apiClient, chat.id!);
    const userMessages = messages.filter(m => m.origin === 'User').map(m => m.message);
    expect(userMessages).toStrictEqual(['second message', 'first message']);
  });
});
