import { Personality } from './personality.model';

/** Required Personality fields commonly omitted in older test fixtures. */
export const PERSONALITY_TEST_DEFAULTS = {
  expressions_enabled: true,
  image_style: 'auto',
} as const satisfies Pick<Personality, 'expressions_enabled' | 'image_style'>;

export function makeTestPersonality(overrides: Partial<Personality> = {}): Personality {
  return {
    id: 'p-1',
    name: 'Test Personality',
    system_prompt: 'sp',
    auto_pin_memories: false,
    cover_image_id: null,
    cover_image_url: null,
    created_at: '2026-04-28T00:00:00Z',
    updated_at: '2026-04-28T00:00:00Z',
    stats: { chat_count: 0, last_used_at: null },
    ...PERSONALITY_TEST_DEFAULTS,
    ...overrides,
  };
}
