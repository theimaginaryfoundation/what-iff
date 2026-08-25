import { Personality } from '../../../core/models/personality.model';
import { personalityAccent, personalityAccentSurface, toPersonalityCardVm, toPersonalityDetailVm, usageBadge, } from './personality-vm.helpers';

function makePersonality(overrides: Partial<Personality> = {}): Personality {
    return {
        id: 'p-id-default',
        name: 'Test Persona',
        system_prompt: 'You are a deterministic assistant. Keep replies short.',
        auto_pin_memories: false,
        expressions_enabled: true,
        image_style: 'auto', cover_image_id: null,
        cover_image_url: null,
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
        stats: { chat_count: 0, last_used_at: null },
        ...overrides,
    };
}

describe('personalityAccent', () => {
    it('uses accent_color when provided', () => {
        const accent = personalityAccent({ id: 'abc-123', name: 'X', accent_color: '#C2572A' });
        expect(accent).toBe('#C2572A');
    });

    it('returns a stable HSL accent for the same id', () => {
        const accent1 = personalityAccent({ id: 'abc-123', name: 'X', accent_color: null });
        const accent2 = personalityAccent({ id: 'abc-123', name: 'X', accent_color: null });
        expect(accent1).toBe(accent2);
        expect(accent1).toMatch(/^hsl\(\d+ \d+%/);
    });

    it('falls back to the name when id is missing', () => {
        const accent = personalityAccent({ id: '', name: 'Vera', accent_color: null });
        expect(accent).toMatch(/^hsl\(/);
    });

    it('returns a sensible default for empty inputs', () => {
        const accent = personalityAccent({ id: '', name: '', accent_color: null });
        expect(accent).toMatch(/^hsl\(/);
    });
});

describe('personalityAccentSurface', () => {
    it('produces a transparent color-mix expression', () => {
        expect(personalityAccentSurface('hsl(200 50% 50%)')).toContain('color-mix');
        expect(personalityAccentSurface('hsl(200 50% 50%)')).toContain('transparent');
    });
});

describe('usageBadge', () => {
    it('shows "New thread" when stats are missing or zero', () => {
        expect(usageBadge(undefined)).toBe('New thread');
        expect(usageBadge(null)).toBe('New thread');
        expect(usageBadge({ chat_count: 0, last_used_at: null })).toBe('New thread');
    });

    it('uses singular for exactly one chat', () => {
        expect(usageBadge({ chat_count: 1, last_used_at: null })).toBe('1 thread');
    });

    it('uses plural for more than one chat', () => {
        expect(usageBadge({ chat_count: 3, last_used_at: null })).toBe('3 threads');
    });

    it('clamps negative chat counts to zero', () => {
        expect(usageBadge({ chat_count: -5, last_used_at: null })).toBe('New thread');
    });
});

describe('toPersonalityCardVm', () => {
    it('truncates the system prompt preview', () => {
        const longPrompt = 'A'.repeat(500);
        const vm = toPersonalityCardVm(makePersonality({ system_prompt: longPrompt }));
        expect(vm.systemPromptPreview.length).toBeLessThanOrEqual(140);
        expect(vm.systemPromptPreview.endsWith('…')).toBe(true);
    });

    it('marks isDefault when defaultPersonalityId matches', () => {
        const personality = makePersonality({ id: 'p-1' });
        expect(toPersonalityCardVm(personality, { defaultPersonalityId: 'p-1' }).isDefault).toBe(true);
        expect(toPersonalityCardVm(personality, { defaultPersonalityId: 'p-other' }).isDefault).toBe(false);
    });

    it('uses the cover override when provided', () => {
        const personality = makePersonality({ cover_image_url: '/api/image-gallery/abc' });
        const vm = toPersonalityCardVm(personality, { coverImageUrl: '/override' });
        expect(vm.coverImageUrl).toBe('/override');
    });

    it('falls back to personality.cover_image_url when no override is given', () => {
        const personality = makePersonality({ cover_image_url: '/api/image-gallery/abc' });
        expect(toPersonalityCardVm(personality).coverImageUrl).toBe('/api/image-gallery/abc');
    });
});

describe('toPersonalityDetailVm', () => {
    it('hydrates derived fields from the personality and stats', () => {
        const personality = makePersonality({
            id: 'p-2',
            name: 'Eli Drake',
            mood_ids: ['m-1', 'm-2'],
            stats: { chat_count: 4, last_used_at: '2026-04-27T00:00:00Z' },
        });
        const vm = toPersonalityDetailVm(personality);
        expect(vm.shortName).toBe('Eli D.');
        expect(vm.moodIds).toEqual(['m-1', 'm-2']);
        expect(vm.stats.chat_count).toBe(4);
        expect(vm.accent).toMatch(/^hsl\(/);
    });
});
