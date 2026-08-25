import { test, expect } from './fixtures';
import { createMemory, createMemoriesBatch, listMemories, deleteMemory, type Memory, type MemoryBatchCreateResponse, ApiError } from '../sdk/client';

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
      await Promise.all(batch.results.map(m => deleteMemory(apiClient, m.id!)));
    });

    test('creates all items in one call and they are all listable', async ({ apiClient }) => {
      expect(batch.created_count).toBe(2);

      const listed = await listMemories(apiClient);
      const createdIds = new Set(batch.results.map(m => m.id));
      expect(listed.filter(m => createdIds.has(m.id))).toHaveLength(2);
    });
  });
});
