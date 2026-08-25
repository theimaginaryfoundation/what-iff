import { test, expect } from './fixtures';
import {
  createChat,
  createMemory,
  createPersonality,
  createRitual,
  deleteChat,
  deleteMemory,
  deletePersonality,
  deleteRitual,
  search,
} from '../sdk/client';
import { uniqueId } from '../fixtures/unique';

/**
 * GET /search — the cross-resource command-palette query.
 *
 * Ported from the Newman regression suite's "09 CrossResourceSearch"
 * folder. That version searched for whatever the LLM happened to have
 * written into a memory earlier in the journey; here the four searchable
 * resources are all seeded directly, so the run is deterministic and needs
 * no provider.
 *
 * The image section is asserted to be *present*, not populated: gallery
 * images only exist once a provider has generated one, and the endpoint
 * emits every section (with `results: []`) precisely so clients can render
 * stable headers.
 */

/** Sections are documented as always returned, in this order. */
const CANONICAL_SECTIONS = ['chat', 'personality', 'ritual', 'memory', 'image'] as const;

const ROUTE_PATTERNS: Record<string, RegExp> = {
  chat: /^\/chat\/[0-9a-f-]+$/,
  personality: /^\/personality\/[0-9a-f-]+$/,
  ritual: /^\/ritual\/[0-9a-f-]+$/,
  memory: /^\/memory\/[0-9a-f-]+$/,
};

test.describe('search', () => {
  // One marker per test, embedded in every seeded resource, so a search for
  // it can only match this test's own rows even under parallel workers.
  let marker: string;
  let cleanup: Array<() => Promise<void>>;

  test.beforeEach(async ({ apiClient }) => {
    marker = uniqueId();
    cleanup = [];

    const personality = await createPersonality(apiClient, {
      name: `e2e-api-search personality ${marker}`,
      systemPrompt: 'Search regression personality.',
    });
    cleanup.push(() => deletePersonality(apiClient, personality.id!));

    const chat = await createChat(apiClient, { name: `e2e-api-search chat ${marker}` });
    cleanup.push(() => deleteChat(apiClient, chat.id!));

    const ritual = await createRitual(apiClient, {
      name: `e2e-api-search ritual ${marker}`,
      description: 'Cross-resource search marker ritual.',
      content: `Marker: ${marker}`,
    });
    cleanup.push(() => deleteRitual(apiClient, ritual.id!));

    const memory = await createMemory(apiClient, {
      content: `e2e-api-search memory marker ${marker}`,
      level: 'global',
      type: 'Context',
      starred: false,
    });
    cleanup.push(() => deleteMemory(apiClient, memory.id!));
  });

  test.afterEach(async () => {
    // Reverse order so a resource is never deleted before something that
    // referenced it, and keep going if one delete fails.
    await Promise.allSettled(cleanup.reverse().map(fn => fn()));
  });

  test('aggregates a marker across every resource section', async ({ apiClient }) => {
    const body = await search(apiClient, { query: marker, limitPerType: 10 });

    expect(body.query).toBe(marker);
    expect(body.sections.map(s => s.type)).toStrictEqual([...CANONICAL_SECTIONS]);

    for (const type of ['chat', 'personality', 'ritual', 'memory'] as const) {
      const section = body.sections.find(s => s.type === type)!;
      const hit = section.results.find(r => `${r.label} ${r.snippet ?? ''}`.includes(marker));
      expect(hit, `${type} section should surface the seeded marker`).toBeTruthy();
      expect(hit!.route).toMatch(ROUTE_PATTERNS[type]!);
      expect(hit!.icon_type).toBe(type);
    }
  });

  test('narrows to one section when types is set', async ({ apiClient }) => {
    const body = await search(apiClient, { query: marker, types: 'chat', limitPerType: 10 });

    expect(body.sections).toHaveLength(1);
    expect(body.sections[0]!.type).toBe('chat');
    expect(body.sections[0]!.results.some(r => r.label.includes(marker))).toBe(true);
  });

  test('rejects an empty query with 400', async ({ apiClient }) => {
    // Below the documented 2-rune minimum, so this is a validation failure
    // rather than a search that happens to match nothing.
    const { response } = await apiClient.GET('/search', { params: { query: { query: '' } } });
    expect(response.status).toBe(400);
  });
});
