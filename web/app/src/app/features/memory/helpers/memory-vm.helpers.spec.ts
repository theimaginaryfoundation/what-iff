import { Memory } from '../../../core/models/memory.model';
import {
    associationLabel,
    confidenceBucketLabel,
    excerpt,
    isUserScopedMemoryLevel,
    levelBadgeText,
    toMemoryCardVm,
} from './memory-vm.helpers';

function makeMemory(partial: Partial<Memory> = {}): Memory {
    return {
        id: 'm-1',
        content: 'Memory content for testing',
        level: 'thread',
        type: 'Context',
        status: 'active',
        confidence: 0.6,
        starred: false,
        created_at: '2026-05-01T00:00:00Z',
        updated_at: '2026-05-01T00:00:00Z',
        ...partial,
    };
}

describe('memory-vm.helpers', () => {
    it('maps memory into card vm', () => {
        const vm = toMemoryCardVm(makeMemory({ level: 'summary', chat_name: 'Roadmap Chat' }));
        expect(vm.levelLabel).toBe('Summary');
        expect(vm.chatName).toBe('Roadmap Chat');
    });

    it('maps merged_from ids onto the card vm', () => {
        const vm = toMemoryCardVm(
            makeMemory({
                chain_metadata: {
                    duplicate_count: 2,
                    merged_from_memory_ids: ['m-a', 'm-b'],
                },
            }),
        );
        expect(vm.mergedFromIds).toEqual(['m-a', 'm-b']);
        expect(vm.verifiedCount).toBe(2);
    });

    it('formats level badge text', () => {
        expect(levelBadgeText('global')).toBe('Global');
        expect(levelBadgeText('thread')).toBe('Thread');
    });

    it('maps confidence into percent and bucket label', () => {
        const vm = toMemoryCardVm(makeMemory({ confidence: 0.9 }));
        expect(vm.confidencePercent).toBe(90);
        expect(vm.confidenceLabel).toBe('High');
    });

    it('detects user-scoped memory levels', () => {
        expect(isUserScopedMemoryLevel('global')).toBe(true);
        expect(isUserScopedMemoryLevel('personality')).toBe(true);
        expect(isUserScopedMemoryLevel('thread')).toBe(false);
        expect(isUserScopedMemoryLevel('summary')).toBe(false);
    });

    it('truncates excerpts with ellipsis', () => {
        expect(excerpt('abc', 10)).toBe('abc');
        expect(excerpt('abcdefghij', 6)).toBe('abcde…');
    });

    it('does not truncate content exactly at the excerpt length boundary', () => {
        expect(excerpt('abcde', 5)).toBe('abcde');
    });

    it('trims surrounding whitespace before measuring excerpt length', () => {
        expect(excerpt('  abc  ', 10)).toBe('abc');
    });

    it('returns an empty excerpt for empty content', () => {
        expect(excerpt('', 10)).toBe('');
    });

    it('resolves a pinned personality name from the lookup map', () => {
        const vm = toMemoryCardVm(
            makeMemory({ level: 'personality', pinned_personality_id: 'p-1' }),
            220,
            { 'p-1': 'Muse' },
        );
        expect(vm.pinnedPersonalityId).toBe('p-1');
        expect(vm.pinnedPersonalityName).toBe('Muse');
    });

    it('leaves the pinned personality name null when absent from the lookup map', () => {
        const vm = toMemoryCardVm(makeMemory({ pinned_personality_id: 'p-unknown' }), 220, {});
        expect(vm.pinnedPersonalityName).toBeNull();
    });

    it('leaves the pinned personality name null when the memory has no pin', () => {
        const vm = toMemoryCardVm(makeMemory(), 220, { 'p-1': 'Muse' });
        expect(vm.pinnedPersonalityId).toBeNull();
        expect(vm.pinnedPersonalityName).toBeNull();
    });

    it('defaults chatName/chatId to null when the memory has none', () => {
        const vm = toMemoryCardVm(makeMemory());
        expect(vm.chatName).toBeNull();
        expect(vm.chatId).toBeNull();
    });

    it('respects a custom excerpt length', () => {
        const vm = toMemoryCardVm(makeMemory({ content: 'abcdefghij' }), 4);
        expect(vm.excerpt).toBe('abc…');
    });

    it('does not surface verifiedCount when duplicate_count is 1 or absent', () => {
        const singleDuplicate = toMemoryCardVm(
            makeMemory({ chain_metadata: { duplicate_count: 1, merged_from_memory_ids: [] } }),
        );
        expect(singleDuplicate.verifiedCount).toBeNull();

        const noChainMetadata = toMemoryCardVm(makeMemory());
        expect(noChainMetadata.verifiedCount).toBeNull();
        expect(noChainMetadata.mergedFromIds).toEqual([]);
        expect(noChainMetadata.chainMetadata).toBeNull();
    });

    it('clamps out-of-range confidence into a 0-100 percent', () => {
        expect(toMemoryCardVm(makeMemory({ confidence: -0.5 })).confidencePercent).toBe(0);
        expect(toMemoryCardVm(makeMemory({ confidence: 1.5 })).confidencePercent).toBe(100);
    });

    it('formats every level badge, including the default fallback', () => {
        expect(levelBadgeText('personality')).toBe('Personality');
        expect(levelBadgeText('summary')).toBe('Summary');
        expect(levelBadgeText('unknown-level' as Memory['level'])).toBe('unknown-level');
    });

    it('buckets confidence at both boundaries', () => {
        expect(confidenceBucketLabel(0.44)).toBe('Low');
        expect(confidenceBucketLabel(0.45)).toBe('Medium');
        expect(confidenceBucketLabel(0.74)).toBe('Medium');
        expect(confidenceBucketLabel(0.75)).toBe('High');
    });

    it('prefers pinned personality name, then chat name, then level label for the association label', () => {
        const pinned = toMemoryCardVm(
            makeMemory({ level: 'personality', pinned_personality_id: 'p-1', chat_name: 'Roadmap Chat' }),
            220,
            { 'p-1': 'Muse' },
        );
        expect(associationLabel(pinned)).toBe('Muse');

        const chatOnly = toMemoryCardVm(makeMemory({ level: 'thread', chat_name: 'Roadmap Chat' }));
        expect(associationLabel(chatOnly)).toBe('Roadmap Chat');

        const neither = toMemoryCardVm(makeMemory({ level: 'global' }));
        expect(associationLabel(neither)).toBe('Global');
    });
});
