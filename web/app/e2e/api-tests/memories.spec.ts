import { test, expect } from './fixtures';
import {
  createMemory,
  createMemoriesBatch,
  deleteMemory,
  deleteMemoriesBatch,
  listMemories,
  patchMemoriesBatch,
  type Memory,
  type MemoryBatchCreateResponse,
  ApiError,
} from '../sdk/client';

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
});
