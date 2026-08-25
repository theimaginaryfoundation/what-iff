import { test, expect } from './fixtures';
import {
  createChat,
  createPersonality,
  deleteChat,
  deletePersonality,
  listPersonalitiesPage,
  updateChat,
  type Personality,
} from '../sdk/client';
import { shortId } from '../fixtures/unique';

/**
 * The `stats` block GET /personality returns per row, and the
 * `personality_ids` filter that scopes the list to specific personalities.
 *
 * Ported from the Newman regression suite's "15 PersonalityStats" folder.
 * `chat_count` is the count of the *requesting user's* chats assigned to the
 * personality, so reassigning a chat has to move the count between two rows
 * — asserting both sides in one read is what makes this more than a getter
 * test.
 */

test.describe('personality stats', () => {
  let runId: string;
  let personalityA: Personality;
  let personalityB: Personality;
  let chatIds: string[];

  /** The filtered read both assertions share: exactly the two seeded personalities. */
  async function readStats(apiClient: Parameters<typeof listPersonalitiesPage>[0]) {
    const page = await listPersonalitiesPage(apiClient, {
      personality_ids: `${personalityA.id},${personalityB.id}`,
      limit: 10,
    });
    const byId = (id: string) => page.results.find(p => p.id === id)!;
    return { page, a: byId(personalityA.id!), b: byId(personalityB.id!) };
  }

  test.beforeEach(async ({ apiClient }) => {
    runId = shortId();
    personalityA = await createPersonality(apiClient, {
      name: `e2e-api-stats A ${runId}`,
      systemPrompt: 'Stats regression personality A.',
    });
    personalityB = await createPersonality(apiClient, {
      name: `e2e-api-stats B ${runId}`,
      systemPrompt: 'Stats regression personality B.',
    });

    // Two chats on A, one on B — an asymmetric split, so a bug that reported
    // the same count for every personality would still fail this.
    const chats = await Promise.all([
      createChat(apiClient, { name: `e2e-api-stats A1 ${runId}`, personalityId: personalityA.id! }),
      createChat(apiClient, { name: `e2e-api-stats A2 ${runId}`, personalityId: personalityA.id! }),
      createChat(apiClient, { name: `e2e-api-stats B1 ${runId}`, personalityId: personalityB.id! }),
    ]);
    chatIds = chats.map(c => c.id!);
  });

  test.afterEach(async ({ apiClient }) => {
    await Promise.allSettled(chatIds.map(id => deleteChat(apiClient, id)));
    await Promise.allSettled([
      deletePersonality(apiClient, personalityA.id!),
      deletePersonality(apiClient, personalityB.id!),
    ]);
  });

  test('personality_ids narrows the list to exactly those personalities', async ({ apiClient }) => {
    const { page } = await readStats(apiClient);

    expect(page.totalCount).toBe(2);
    expect(page.results.map(p => p.id).sort()).toStrictEqual([personalityA.id, personalityB.id].sort());
  });

  test('chat_count follows a chat when it is reassigned', async ({ apiClient }) => {
    const before = await readStats(apiClient);
    expect(before.a.stats.chat_count).toBe(2);
    expect(before.b.stats.chat_count).toBe(1);
    expect(before.a.stats.last_used_at).toStrictEqual(expect.any(String));
    expect(before.b.stats.last_used_at).toStrictEqual(expect.any(String));

    // Move one of A's two chats onto B.
    await updateChat(apiClient, chatIds[0]!, { personalityId: personalityB.id! });

    const after = await readStats(apiClient);
    expect(after.a.stats.chat_count).toBe(1);
    expect(after.b.stats.chat_count).toBe(2);
  });
});
