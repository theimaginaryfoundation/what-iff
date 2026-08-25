import { Memory } from '../../../core/models/memory.model';
import { excerpt, isUserScopedMemoryLevel, levelBadgeText, toMemoryCardVm } from './memory-vm.helpers';

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

    it('formats level badge text', () => {
        expect(levelBadgeText('global')).toBe('User');
        expect(levelBadgeText('thread')).toBe('Chat');
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
});
