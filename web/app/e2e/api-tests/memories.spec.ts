import { test, expect, createSecondUser } from './fixtures';
import {
  createApiClient,
  createMemory,
  createMemoriesBatch,
  createPersonality,
  deleteMemory,
  deleteMemoriesBatch,
  listMemories,
  patchMemoriesBatch,
  type Memory,
  type MemoryBatchCreateResponse,
  ApiError,
} from '../sdk/client';
import { shortId, uniqueId } from '../fixtures/unique';

/**
 * Memory create / filter / delete, straight against the API.
 */

test.describe('memories', () => {
  test.describe('a single memory', () => {
    let memory: Memory;

    test.beforeEach(async ({ apiClient }) => {
      memory = await createMemory(apiClient, {
        content: 'e2e-api memory: prefers dark mode',
        level: 'global',
        type: 'Context',
        starred: true,
      });
    });

    // Tolerates the memory already being gone: the delete test below deletes
    // it itself as part of the assertion.
    test.afterEach(async ({ apiClient }) => {
      try {
        await deleteMemory(apiClient, memory.id!);
      } catch (err) {
        if (!(err instanceof ApiError && err.status === 404)) throw err;
      }
    });

    test('is created, listed, and filterable by star/query', async ({ apiClient }) => {
      expect(memory.id).toBeTruthy();
      expect(memory.starred).toBe(true);

      const starred = await listMemories(apiClient, { starred: true });
      expect(starred.some(m => m.id === memory.id)).toBe(true);

      const filteredByQuery = await listMemories(apiClient, { query: 'dark mode' });
      expect(filteredByQuery.some(m => m.id === memory.id)).toBe(true);
    });

    test('no longer appears once deleted', async ({ apiClient }) => {
      await deleteMemory(apiClient, memory.id!);
      const afterDelete = await listMemories(apiClient, { starred: true });
      expect(afterDelete.some(m => m.id === memory.id)).toBe(false);
    });
  });

  test.describe('a batch of memories', () => {
    let batch: MemoryBatchCreateResponse;

    test.beforeEach(async ({ apiClient }) => {
      batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch memory one', level: 'global', type: 'Context', starred: false },
        { content: 'e2e-api batch memory two', level: 'global', type: 'Context', starred: false },
      ]);
    });

    test.afterEach(async ({ apiClient }) => {
      await Promise.all(
        (batch.results ?? []).map(async m => {
          try {
            await deleteMemory(apiClient, m.id!);
          } catch (err) {
            if (!(err instanceof ApiError && err.status === 404)) throw err;
          }
        }),
      );
    });

    test('creates all items in one call and they are all listable', async ({ apiClient }) => {
      expect(batch.created_count).toBe(2);

      const listed = await listMemories(apiClient);
      const createdIds = new Set(batch.results.map(m => m.id));
      expect(listed.filter(m => createdIds.has(m.id))).toHaveLength(2);
    });
  });

  test.describe('batch delete and patch', () => {
    test('deletes multiple memories in one call', async ({ apiClient }) => {
      const batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch-delete a', level: 'global', type: 'Context' },
        { content: 'e2e-api batch-delete b', level: 'global', type: 'Context' },
      ]);
      const ids = batch.results.map(m => m.id!);

      const result = await deleteMemoriesBatch(apiClient, ids, true);
      expect(result.deleted_count).toBe(2);

      const listed = await listMemories(apiClient);
      expect(listed.filter(m => ids.includes(m.id!))).toHaveLength(0);
    });

    test('archives multiple memories via batch patch and lists them as inactive', async ({ apiClient }) => {
      const batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch-archive a', level: 'global', type: 'Context' },
        { content: 'e2e-api batch-archive b', level: 'global', type: 'Context' },
      ]);
      const ids = batch.results.map(m => m.id!);

      const patched = await patchMemoriesBatch(apiClient, ids, { status: 'inactive' }, true);
      expect(patched.updated_count).toBe(2);

      const active = await listMemories(apiClient, { status: 'active' });
      expect(active.filter(m => ids.includes(m.id!))).toHaveLength(0);

      const inactive = await listMemories(apiClient, { status: 'inactive' });
      expect(inactive.filter(m => ids.includes(m.id!))).toHaveLength(2);

      await deleteMemoriesBatch(apiClient, ids, true);
    });

    test('moves a memory to global via batch patch', async ({ apiClient }) => {
      const memory = await createMemory(apiClient, {
        content: 'e2e-api batch-move target',
        level: 'global',
        type: 'Context',
      });

      const patched = await patchMemoriesBatch(
        apiClient,
        [memory.id!],
        { level: 'global', pinned_personality_id: null },
        true,
      );
      expect(patched.updated_count).toBe(1);
      expect(patched.results[0].level).toBe('global');

      await deleteMemory(apiClient, memory.id!);
    });
  });

  test.describe('batch delete and patch validation', () => {
    // These are request-shape checks the handlers reject before ever
    // touching the datastore (see parseBatchIDs / parseBatchPatch in
    // internal/handlers/memory/batch_actions.go), so none of these need a
    // real, owned memory to exist first — a syntactically-plausible id is
    // enough to reach the validation code.

    test('rejects an empty ids array on batch delete', async ({ apiClient }) => {
      await expect(deleteMemoriesBatch(apiClient, [], true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects an empty ids array on batch patch', async ({ apiClient }) => {
      await expect(patchMemoriesBatch(apiClient, [], { status: 'inactive' }, true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects a malformed id on batch delete', async ({ apiClient }) => {
      await expect(deleteMemoriesBatch(apiClient, ['not-a-uuid'], true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects a malformed id on batch patch', async ({ apiClient }) => {
      await expect(patchMemoriesBatch(apiClient, ['not-a-uuid'], { status: 'inactive' }, true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects more than the 100-id batch cap on delete', async ({ apiClient }) => {
      const tooMany = Array.from({ length: 101 }, () => uniqueId());
      await expect(deleteMemoriesBatch(apiClient, tooMany, true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects more than the 100-id batch cap on patch', async ({ apiClient }) => {
      const tooMany = Array.from({ length: 101 }, () => uniqueId());
      await expect(patchMemoriesBatch(apiClient, tooMany, { status: 'inactive' }, true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects a patch with no fields', async ({ apiClient }) => {
      await expect(patchMemoriesBatch(apiClient, [uniqueId()], {}, true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects an invalid status value on batch patch', async ({ apiClient }) => {
      const patch = { status: 'archived' } as unknown as Parameters<typeof patchMemoriesBatch>[2];
      await expect(patchMemoriesBatch(apiClient, [uniqueId()], patch, true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });

    test('rejects an invalid confidence value on batch patch', async ({ apiClient }) => {
      const patch = { confidence: 'certain' } as unknown as Parameters<typeof patchMemoriesBatch>[2];
      await expect(patchMemoriesBatch(apiClient, [uniqueId()], patch, true)).rejects.toMatchObject({
        status: 400,
      } satisfies Partial<ApiError>);
    });
  });

  test.describe('batch delete and patch require authentication', () => {
    test('rejects an unauthenticated batch delete', async () => {
      const anonymous = createApiClient();
      await expect(deleteMemoriesBatch(anonymous, [uniqueId()], true)).rejects.toMatchObject({
        status: 401,
      } satisfies Partial<ApiError>);
    });

    test('rejects an unauthenticated batch patch', async () => {
      const anonymous = createApiClient();
      await expect(patchMemoriesBatch(anonymous, [uniqueId()], { status: 'inactive' }, true)).rejects.toMatchObject({
        status: 401,
      } satisfies Partial<ApiError>);
    });
  });

  test.describe('batch delete and patch across users', () => {
    test('an all_or_none delete rejects and changes nothing when one id belongs to another user', async ({ apiClient }) => {
      const mine = await createMemory(apiClient, {
        content: 'e2e-api batch-cross-user delete mine',
        level: 'global',
        type: 'Context',
        starred: false,
      });
      const other = await createSecondUser();
      try {
        const theirs = await createMemory(other.apiClient, {
          content: 'e2e-api batch-cross-user delete theirs',
          level: 'global',
          type: 'Context',
          starred: false,
        });

        await expect(deleteMemoriesBatch(apiClient, [mine.id!, theirs.id!], true)).rejects.toMatchObject({
          status: 404,
        } satisfies Partial<ApiError>);

        // all_or_none runs in one transaction: my own memory must survive
        // the rollback triggered by an id I don't own.
        const stillListed = await listMemories(apiClient);
        expect(stillListed.some(m => m.id === mine.id)).toBe(true);
      } finally {
        await deleteMemory(apiClient, mine.id!);
        await other.cleanup();
      }
    });

    test('a partial-mode patch skips an id belonging to another user and patches the rest', async ({ apiClient }) => {
      const mine = await createMemory(apiClient, {
        content: 'e2e-api batch-cross-user patch mine',
        level: 'global',
        type: 'Context',
        starred: false,
      });
      const other = await createSecondUser();
      try {
        const theirs = await createMemory(other.apiClient, {
          content: 'e2e-api batch-cross-user patch theirs',
          level: 'global',
          type: 'Context',
          starred: false,
        });

        const result = await patchMemoriesBatch(apiClient, [mine.id!, theirs.id!], { status: 'inactive' }, false);
        expect(result.updated_count).toBe(1);
        expect(result.results.map(m => m.id)).toStrictEqual([mine.id]);

        const inactive = await listMemories(apiClient, { status: 'inactive' });
        expect(inactive.some(m => m.id === mine.id)).toBe(true);
      } finally {
        await deleteMemory(apiClient, mine.id!);
        await other.cleanup();
      }
    });
  });

  test.describe('batch delete and patch with missing ids', () => {
    test('a partial-mode delete skips a nonexistent id and deletes the rest', async ({ apiClient }) => {
      const batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch-missing delete a', level: 'global', type: 'Context', starred: false },
        { content: 'e2e-api batch-missing delete b', level: 'global', type: 'Context', starred: false },
      ]);
      const ids = batch.results.map(m => m.id!);

      const result = await deleteMemoriesBatch(apiClient, [ids[0]!, uniqueId(), ids[1]!], false);
      expect(result.deleted_count).toBe(2);

      const listed = await listMemories(apiClient);
      expect(listed.filter(m => ids.includes(m.id!))).toHaveLength(0);
    });

    test('a partial-mode patch skips a nonexistent id and reports only the real updates', async ({ apiClient }) => {
      const batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch-missing patch a', level: 'global', type: 'Context', starred: false },
        { content: 'e2e-api batch-missing patch b', level: 'global', type: 'Context', starred: false },
      ]);
      const ids = batch.results.map(m => m.id!);

      const result = await patchMemoriesBatch(apiClient, [ids[0]!, uniqueId(), ids[1]!], { status: 'inactive' }, false);
      expect(result.updated_count).toBe(2);
      expect(result.results.map(m => m.id!).sort()).toStrictEqual([...ids].sort());

      await deleteMemoriesBatch(apiClient, ids, true);
    });

    test('an all_or_none patch fails on a later missing id but keeps the earlier successful update', async ({ apiClient }) => {
      const batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch-missing all-or-none a', level: 'global', type: 'Context', starred: false },
        { content: 'e2e-api batch-missing all-or-none b', level: 'global', type: 'Context', starred: false },
      ]);
      const [first, second] = batch.results.map(m => m.id!);

      // MemoryBatchPatchRequest's contract: all_or_none only stops *further*
      // processing on the first failure. It is not one encompassing
      // transaction, so an id already patched earlier in the list stays
      // patched even though the overall call still fails.
      await expect(
        patchMemoriesBatch(apiClient, [first!, uniqueId(), second!], { status: 'inactive' }, true),
      ).rejects.toMatchObject({ status: 404 } satisfies Partial<ApiError>);

      const inactive = await listMemories(apiClient, { status: 'inactive' });
      expect(inactive.some(m => m.id === first)).toBe(true);

      const active = await listMemories(apiClient, { status: 'active' });
      expect(active.some(m => m.id === second)).toBe(true);

      await deleteMemoriesBatch(apiClient, [first!, second!], true);
    });
  });

  test.describe('batch patch field coverage', () => {
    test('updates content, confidence, and starred via a single batch patch', async ({ apiClient }) => {
      const batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch-fields a', level: 'global', type: 'Context', starred: false },
        { content: 'e2e-api batch-fields b', level: 'global', type: 'Context', starred: false },
      ]);
      const ids = batch.results.map(m => m.id!);

      const patched = await patchMemoriesBatch(
        apiClient,
        ids,
        { content: 'e2e-api batch-fields updated', confidence: 'high', starred: true },
        true,
      );
      expect(patched.updated_count).toBe(2);
      for (const memory of patched.results) {
        expect(memory.content).toBe('e2e-api batch-fields updated');
        expect(memory.confidence).toBe('high');
        expect(memory.starred).toBe(true);
      }

      await deleteMemoriesBatch(apiClient, ids, true);
    });
  });

  test.describe('batch patch propagates non-missing errors even in partial mode', () => {
    test("aborts entirely, changing nothing, when the patch references another user's personality", async ({ apiClient }) => {
      const batch = await createMemoriesBatch(apiClient, [
        { content: 'e2e-api batch-foreign-personality a', level: 'global', type: 'Context', starred: false },
        { content: 'e2e-api batch-foreign-personality b', level: 'global', type: 'Context', starred: false },
      ]);
      const ids = batch.results.map(m => m.id!);
      const other = await createSecondUser();
      try {
        const theirsPersonality = await createPersonality(other.apiClient, {
          name: `e2e-api-batch-foreign-personality ${shortId()}`,
          systemPrompt: 'Cross-user pin target.',
        });

        // all_or_none is false here, but pinning to a personality the caller
        // doesn't own is not a "missing id" — it's a validation failure, and
        // the whole batch aborts rather than being silently skipped.
        await expect(
          patchMemoriesBatch(apiClient, ids, { level: 'personality', pinned_personality_id: theirsPersonality.id! }, false),
        ).rejects.toMatchObject({ status: 404 } satisfies Partial<ApiError>);

        const stillGlobal = await listMemories(apiClient, { level: 'global' });
        expect(ids.every(id => stillGlobal.some(m => m.id === id))).toBe(true);
      } finally {
        await deleteMemoriesBatch(apiClient, ids, true);
        await other.cleanup();
      }
    });
  });
});
