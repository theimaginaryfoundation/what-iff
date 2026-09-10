import { test, expect, createSecondUser, waitForJobComplete } from './fixtures';
import { createChat, sendChatMessage, exportChat, type Chat } from '../sdk/client';
import { readZipEntries, parseJsonl } from '../helpers/zip';
import { uniqueId } from '../fixtures/unique';

/**
 * `GET /chat/{id}/export`, over real HTTP.
 *
 * The endpoint streams: it commits to a 200 and the ZIP headers before it has
 * read a single message, so anything that goes wrong afterwards arrives as a
 * successful-looking response with a short body. That is only observable
 * against a real listener — a handler test asserting on a recorder sees the
 * bytes it was handed, not the bytes that survived the wire — which is why
 * every assertion below decompresses the archive rather than trusting the
 * status line.
 */

/** Bounds the mock LLM's reply. Well inside API_TEST_TIMEOUT so a stall names this wait. */
const REPLY_TIMEOUT_MS = 30_000;

const PROMPT = 'hello export';

test.describe('chat export', () => {
  let chat: Chat;

  test.beforeEach(async ({ apiClient }) => {
    chat = await createChat(apiClient, { name: `export-${uniqueId()}` });
    const sent = await sendChatMessage(apiClient, chat.id!, PROMPT);
    // The reply is generated asynchronously; exporting before it lands would
    // assert against a one-message thread and pass for the wrong reason.
    await waitForJobComplete(apiClient, sent.job_id!, REPLY_TIMEOUT_MS);
  });

  test('returns a complete two-entry archive of the thread and its messages', async ({ apiClient }) => {
    const archive = await exportChat(apiClient, chat.id!);

    expect(archive.status).toBe(200);
    expect(archive.headers.get('content-type')).toBe('application/zip');
    expect(archive.headers.get('content-disposition')).toBe(`attachment; filename="${chat.name}.zip"`);

    // Decompressing is the assertion: a stream cut short still arrives under
    // a 200 with a readable entry listing, and only fails on inflate.
    const entries = await readZipEntries(archive.bytes);
    expect(entries.map(e => e.name)).toStrictEqual(['chat.json', 'messages.jsonl']);

    const metadata = JSON.parse(entries[0]!.text) as { id?: string; name?: string; model_name?: string };
    expect(metadata.id).toBe(chat.id);
    expect(metadata.name).toBe(chat.name);

    const messages = parseJsonl<{ origin?: string; message?: string }>(entries[1]!.text);
    expect(messages).toHaveLength(2);
    expect(messages[0]!.origin).toBe('User');
    expect(messages[0]!.message).toBe(PROMPT);
    expect(messages[1]!.origin).toBe('Assistant');
  });

  test("refuses another user's thread with a JSON 404, not an empty archive", async () => {
    const peer = await createSecondUser();
    try {
      const archive = await exportChat(peer.apiClient, chat.id!);

      expect(archive.status).toBe(404);
      // The load-bearing half. A rejection that still carried the ZIP headers
      // would reach the browser as a download of an error document named
      // after someone else's thread.
      expect(archive.headers.get('content-type')).toContain('application/json');
      expect(archive.headers.get('content-disposition')).toBeNull();
      expect(archive.json).toMatchObject({ message: expect.any(String), code: expect.any(String) });
    } finally {
      await peer.cleanup();
    }
  });

  test('rejects an unparseable chat id before opening an archive', async ({ apiClient }) => {
    const archive = await exportChat(apiClient, 'not-a-uuid');

    expect(archive.status).toBe(400);
    expect(archive.headers.get('content-type')).toContain('application/json');
    expect(archive.headers.get('content-disposition')).toBeNull();
    expect(archive.json).toMatchObject({ message: expect.any(String), code: expect.any(String) });
  });
});
