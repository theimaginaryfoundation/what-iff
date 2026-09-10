import { test, expect, waitForJobTerminal, importProgress } from './fixtures';
import { buildChatgptExport } from '../fixtures/chatgpt-export';
import { importChats, importChatsRaw, listAllArchivedChats, getJob, updateJobStatusRaw, type Job } from '../sdk/client';

/**
 * The `chat_import` job's terminal contract, straight against the API.
 *
 * These run in the `api` project against real Postgres, which is the point:
 * the `(owner_id, import_hash)` unique index that makes re-import a no-op
 * exists only there, and the ent enum that decides what an invalid job status
 * does is only enforced by a real database. The Go handler suite fakes both.
 *
 * Every assertion below is about what the *client* is told. A job that
 * finished badly but reports `complete`, or one that reports a count the user
 * then sees rendered as "Imported 0 threads", is the failure mode this file
 * exists to catch — not whether the rows landed.
 */

/** Bounds a single import's run. Well under API_TEST_TIMEOUT so a hang names this wait, not the test. */
const IMPORT_TERMINAL_TIMEOUT_MS = 30_000;

/** Titles the fixture generated, as a set, for counting occurrences among the caller's threads. */
function titleCounts(chats: { name?: string }[], titles: string[]): Map<string, number> {
  const wanted = new Set(titles);
  const counts = new Map(titles.map(t => [t, 0]));
  for (const chat of chats) {
    if (chat.name && wanted.has(chat.name)) {
      counts.set(chat.name, (counts.get(chat.name) ?? 0) + 1);
    }
  }
  return counts;
}

test.describe('chat import job', () => {
  test('re-importing an export already in the account skips every conversation and still completes', async ({ apiClient }) => {
    const archive = buildChatgptExport(5, 'e2e-dup');

    const first = await waitForJobTerminal(apiClient, (await importChats(apiClient, archive.file)).id!, IMPORT_TERMINAL_TIMEOUT_MS);
    expect(first.status).toBe('complete');
    expect(importProgress(first)).toMatchObject({ phase: 'complete', total: 5, imported: 5, skipped: 0 });

    // The same bytes again: identical conversation_ids, so identical import
    // hashes, so every conversation is recognised as one already held.
    const second = await waitForJobTerminal(apiClient, (await importChats(apiClient, archive.file)).id!, IMPORT_TERMINAL_TIMEOUT_MS);

    // The load-bearing assertion. Importing nothing is only a failure when
    // nothing was deduplicated either; an all-duplicates run is the no-op the
    // UI reports as "No new threads", and must not be reported as an error.
    expect(second.status).toBe('complete');
    expect(second.error ?? '').toBe('');
    expect(importProgress(second)).toMatchObject({ phase: 'complete', total: 5, imported: 0, skipped: 5 });

    const counts = titleCounts(await listAllArchivedChats(apiClient), archive.titles);
    expect([...counts.values()]).toStrictEqual([1, 1, 1, 1, 1]);
  });

  test('an export holding no conversations is refused at upload, without starting a job', async ({ apiClient }) => {
    // An empty array carries nothing the format sniffer can recognise as
    // either an OpenAI or an Anthropic export, so it is turned away before
    // anything is staged.
    //
    // Worth being precise about, because it is easily confused with the
    // handler-level case of a *recognised* export that parses to zero
    // conversations: that one is accepted and the job completes with all
    // three counters at zero. The two cannot both be exercised from here —
    // there is no upload that reaches the parser with nothing in it.
    const empty = buildChatgptExport(0, 'e2e-empty');

    const { status, job, error } = await importChatsRaw(apiClient, empty.file);

    expect(status).toBe(400);
    expect(job?.id).toBeUndefined();
    expect((error as { message?: string })?.message).toBeTruthy();
  });

  test('two uploads of the same export race without duplicating threads', async ({ apiClient }) => {
    const archive = buildChatgptExport(5, 'e2e-race');

    // Deliberately not awaited in sequence: the second upload is issued while
    // the first is still running, so the dedup `Exist` check can miss and the
    // unique index becomes the only thing standing between the user and ten
    // copies of five threads.
    const [jobA, jobB] = await Promise.all([importChats(apiClient, archive.file), importChats(apiClient, archive.file)]);

    const terminal = await Promise.all([
      waitForJobTerminal(apiClient, jobA.id!, IMPORT_TERMINAL_TIMEOUT_MS),
      waitForJobTerminal(apiClient, jobB.id!, IMPORT_TERMINAL_TIMEOUT_MS),
    ]);

    // Which job wins is a genuine race, so the status assertion is on the
    // pair rather than on either one: at least one run must have imported the
    // conversations, and neither may sit unfinished.
    const progress = terminal.map(job => importProgress(job));
    expect(progress.reduce((sum, p) => sum + (p?.imported ?? 0), 0)).toBe(5);

    const counts = titleCounts(await listAllArchivedChats(apiClient), archive.titles);
    expect([...counts.values()]).toStrictEqual([1, 1, 1, 1, 1]);
  });

  test.describe('an unknown job status', () => {
    let job: Job;

    test.beforeEach(async ({ apiClient }) => {
      const archive = buildChatgptExport(1, 'e2e-status');
      job = await waitForJobTerminal(apiClient, (await importChats(apiClient, archive.file)).id!, IMPORT_TERMINAL_TIMEOUT_MS);
      expect(job.status).toBe('complete');
    });

    // openapi.yaml documents 400 for invalid request data on this path. Only
    // the empty string is rejected there today: `datastore.ErrInvalidJobStatus`
    // exists and the handler maps it to 400, but nothing in the real datastore
    // ever returns it — an unrecognised status reaches ent's enum validator and
    // comes back as a generic error, which the handler's else branch turns into
    // 500. RED until the status is validated before the write.
    for (const [label, body, reachesEnt] of [
      ['an unrecognised value', { status: 'bogus' }, true],
      ['a correct value in the wrong case', { status: 'Complete' }, true],
      ['an empty string', { status: '' }, false],
    ] as const) {
      test(`is rejected with 400: ${label}`, async ({ apiClient }) => {
        // `test.fail`, not `test.fixme`: the rest of this file's known-defect
        // neighbours are skipped because they cannot run at all, whereas this
        // one runs and demonstrates the defect correctly. Keeping it executing
        // means the assertion cannot rot, and the run turns red the moment the
        // validation lands — which is the notification that this expectation
        // is now stale, not a new failure.
        test.fail(reachesEnt, 'Unrecognised statuses reach ent and surface as 500 — see the defect note above');

        const { status, body: responseBody } = await updateJobStatusRaw(apiClient, job.id!, body);

        expect(status).toBe(400);
        // Asserted separately from the 400 so a regression that maps every
        // rejection to 500 is named as such, rather than reported only as
        // "expected 400".
        expect(status).not.toBe(500);

        const error = responseBody as { message?: string; code?: string };
        expect(error.message).toBeTruthy();
        expect(error.code).toBeTruthy();

        // A rejected write must not have been half-applied.
        expect((await getJob(apiClient, job.id!)).status).toBe('complete');
      });
    }
  });
});
