import { test, expect } from './fixtures';
import { createChat, createMemory, exportMemories, importMemories, type Chat, type Memory } from '../sdk/client';
import { readZipEntries, parseJsonl } from '../helpers/zip';
import {
  chatExportShapedArchive,
  emptyArchive,
  misnamedPersonalityArchive,
  nonZipArchive,
  unrecognisedEntryArchive,
} from '../fixtures/memory-archive';
import { uniqueId } from '../fixtures/unique';

/**
 * `GET /memory/export` and `POST /memory/import`, over real HTTP.
 *
 * The export half streams a ZIP and rate-limits per user, neither of which a
 * handler test observes. The import half routes each entry by *filename*,
 * which makes "the user uploaded the wrong ZIP" and "the user renamed a file"
 * into two very different outcomes — and those are the cases below, because
 * they are the ones a real person reaches by accident.
 *
 * Nothing here needs an embedding: every import case either carries no
 * candidate records or fails before the datastore, so all of it runs under
 * `LLM_BACKEND=mock` where the embeddings client cannot reach the network.
 */

/** The export limiter's per-user allowance, mirroring exportRateLimit in the memory handler. */
const EXPORT_RATE_LIMIT = 5;

test.describe('memory export', () => {
  let chat: Chat;
  let globalMemory: Memory;
  let threadMemory: Memory;

  test.beforeEach(async ({ apiClient }) => {
    chat = await createChat(apiClient, { name: `mem-export-${uniqueId()}` });
    globalMemory = await createMemory(apiClient, {
      content: `e2e-api memory: prefers dark mode ${uniqueId()}`,
      level: 'global',
      type: 'Context',
      starred: false,
    });
    // A second memory in a different scope, so the export has to route
    // records to two files rather than putting everything in one.
    threadMemory = await createMemory(apiClient, {
      content: `e2e-api memory: thread detail ${uniqueId()}`,
      level: 'thread',
      chat_id: chat.id,
      type: 'Context',
      starred: false,
    });
  });

  test('returns a complete archive laid out by memory scope', async ({ apiClient }) => {
    const archive = await exportMemories(apiClient);

    expect(archive.status).toBe(200);
    expect(archive.headers.get('content-type')).toBe('application/zip');
    expect(archive.headers.get('content-disposition')).toMatch(/^attachment; filename="memories-\d{8}-\d{6}\.zip"$/);

    // Decompressing every entry is the completeness assertion: the response is
    // streamed, so a run that died partway still arrives as a 200.
    const entries = await readZipEntries(archive.bytes);
    const userEntry = entries.find(e => e.name === 'user.json');
    expect(userEntry, `expected user.json among ${entries.map(e => e.name).join(', ')}`).toBeDefined();

    const userRecords = parseJsonl<{ id?: string; content?: string }>(userEntry!.text);
    expect(userRecords.some(r => r.id === globalMemory.id && r.content === globalMemory.content)).toBe(true);

    const chatEntry = entries.find(e => e.name === 'chat.json');
    expect(chatEntry, `expected chat.json among ${entries.map(e => e.name).join(', ')}`).toBeDefined();

    // The thread-scoped record must carry its chat back, or an import into a
    // fresh account has nothing to reattach it to.
    const chatRecords = parseJsonl<{ id?: string; chat_id?: string }>(chatEntry!.text);
    const restored = chatRecords.find(r => r.id === threadMemory.id);
    expect(restored, 'thread-scoped memory missing from chat.json').toBeDefined();
    expect(restored!.chat_id).toBe(chat.id);
  });

  test(`is refused after ${EXPORT_RATE_LIMIT} calls in the window`, async ({ apiClient }) => {
    // Sequential on purpose: the limiter counts calls, so firing these
    // concurrently would make which one is refused a race.
    for (let call = 1; call <= EXPORT_RATE_LIMIT; call++) {
      const allowed = await exportMemories(apiClient);
      expect(allowed.status, `call ${call} of ${EXPORT_RATE_LIMIT} should be allowed`).toBe(200);
    }

    const refused = await exportMemories(apiClient);
    expect(refused.status).toBe(429);
    expect(refused.json).toMatchObject({ message: expect.any(String), code: expect.any(String) });

    // Without this header the client has nothing to schedule a retry against,
    // and the limiter reads to the user as an outage.
    const retryAfter = Number(refused.headers.get('retry-after'));
    expect(Number.isInteger(retryAfter)).toBe(true);
    expect(retryAfter).toBeGreaterThanOrEqual(1);
    expect(retryAfter).toBeLessThanOrEqual(901);
  });
});

test.describe('memory import', () => {
  test('accepts an archive with nothing to import and reports zero of everything', async ({ apiClient }) => {
    for (const [label, archive] of [
      ['an empty archive', await emptyArchive()],
      ['an archive of unrecognised entries', await unrecognisedEntryArchive()],
    ] as const) {
      const { status, body } = await importMemories(apiClient, archive);

      expect(status, label).toBe(200);
      expect(body.imported_count, label).toBe(0);
      expect(body.duplicate_count, label).toBe(0);
      expect(body.invalid_record_count, label).toBe(0);
    }
  });

  test('reads a chat export as zero importable memories rather than refusing it', async ({ apiClient }) => {
    // Pins the server half of a known UX trap: uploading the wrong ZIP is
    // reported as a successful import of nothing. Whether the *user* can tell
    // that apart from a real import is a frontend question.
    const { status, body } = await importMemories(apiClient, await chatExportShapedArchive());

    expect(status).toBe(200);
    expect(body.imported_count).toBe(0);
  });

  test('rejects bytes that are not a ZIP', async ({ apiClient }) => {
    const { status, body } = await importMemories(apiClient, nonZipArchive());

    expect(status).toBe(400);
    expect(body.message).toBeTruthy();
    expect(body.code).toBeTruthy();
  });

  test('reports a misnamed entry as a client error, not a server fault', async ({ apiClient }) => {
    // RED. `personality-backup.json` matches the personality-file prefix, so
    // the importer tries to read `backup` as a UUID, fails, and returns the
    // error to the handler — which has no branch for "the archive was wrong"
    // and answers 500 'Failed to import memories', logged at Error level. A
    // user who renamed a file is then indistinguishable from an outage.
    test.fail(true, 'A malformed archive surfaces as 500 rather than 400');

    const archive = await misnamedPersonalityArchive(
      JSON.stringify({ id: '00000000-0000-4000-8000-000000000002', content: 'a memory', type: 'Context' }),
    );
    const { status, body } = await importMemories(apiClient, archive);

    expect(status).toBe(400);
    expect(body.message).toBeTruthy();
    expect(body.code).toBeTruthy();
  });
});
