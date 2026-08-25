import { NULL_PERSONALITY_ID } from '../../../core/constants/app.constants';
import { Chat } from '../../../core/models/chat.model';
import { applyThreadFilters, buildThreadGroups, clearRecentOpenedThreadIds, loadRecentOpenedThreadIds, pickSidebarRecentThreads, recordRecentOpenedThreadId, uniquePersonalityOptions } from './thread-list.helpers';

function makeChat(overrides: Partial<Chat> = {}): Chat {
    return {
        id: 'chat-id',
        user_id: 'user-1',
        name: 'Thread',
        created_at: '2026-04-01T00:00:00Z',
        updated_at: '2026-04-01T00:00:00Z',
        ...overrides,
    };
}

describe('thread-list.helpers', () => {
    it('keeps active thread visible even when filtered out', () => {
        const chats = [
            makeChat({ id: 'a', name: 'Alpha' }),
            makeChat({ id: 'b', name: 'Bravo' }),
        ];
        const result = applyThreadFilters(chats, {
            query: 'zulu',
            pinnedOnly: false,
            selectedTag: null,
            selectedPersonalityId: null,
            sidebarPersonalityIds: [],
            sort: 'recent',
            activeThreadId: 'b',
        });

        expect(result.map(chat => chat.id)).toEqual(['b']);
    });

    it('groups pinned threads separately', () => {
        const now = new Date('2026-04-15T12:00:00Z');
        const chats = [
            makeChat({ id: 'pin', is_favorite: true, updated_at: '2026-04-15T08:00:00Z' }),
            makeChat({ id: 'today', updated_at: '2026-04-15T07:00:00Z' }),
        ];
        const groups = buildThreadGroups(chats, now);

        expect(groups[0].id).toBe('pinned');
        expect(groups[0].label).toBe('Starred');
        expect(groups[0].threads[0].id).toBe('pin');
        expect(groups[1].id).toBe('today');
    });

    it('filters by sidebar personality selection', () => {
        const chats = [
            makeChat({ id: 'a', personality_id: 'p-1', name: 'One' }),
            makeChat({ id: 'b', personality_id: 'p-2', name: 'Two' }),
        ];
        const result = applyThreadFilters(chats, {
            query: '',
            pinnedOnly: false,
            selectedTag: null,
            selectedPersonalityId: null,
            sidebarPersonalityIds: ['p-2'],
            sort: 'recent',
            activeThreadId: null,
        });
        expect(result.map(c => c.id)).toEqual(['b']);
    });

    it('omits nil UUID personalities from filter options', () => {
        const options = uniquePersonalityOptions([
            makeChat({ personality_id: NULL_PERSONALITY_ID, personality_name: NULL_PERSONALITY_ID }),
            makeChat({ personality_id: 'p-real', personality_name: 'Ada' }),
        ]);
        expect(options).toEqual([{ id: 'p-real', label: 'Ada' }]);
    });

    it('ranks sidebar recent threads by opened, then message time, then created time', () => {
        const chats = [
            makeChat({ id: 'old-opened', is_favorite: false, created_at: '2026-01-01T00:00:00Z' }),
            makeChat({
                id: 'messaged',
                is_favorite: false,
                last_message_time: '2026-06-20T00:00:00Z',
                created_at: '2026-01-02T00:00:00Z',
            }),
            makeChat({ id: 'newest', is_favorite: false, created_at: '2026-06-25T00:00:00Z' }),
            makeChat({ id: 'starred', is_favorite: true, created_at: '2026-06-26T00:00:00Z' }),
        ];

        const recent = pickSidebarRecentThreads(chats, ['old-opened', 'newest'], 3);

        expect(recent.map(thread => thread.id)).toEqual(['old-opened', 'newest', 'messaged']);
    });

    it('treats missing localStorage as a no-op for recent opened thread helpers', () => {
        const storageDescriptor = Object.getOwnPropertyDescriptor(globalThis, 'localStorage');
        Object.defineProperty(globalThis, 'localStorage', {
            configurable: true,
            value: undefined,
        });

        try {
            expect(loadRecentOpenedThreadIds()).toEqual([]);
            expect(recordRecentOpenedThreadId('thread-a')).toEqual(['thread-a']);
            expect(() => clearRecentOpenedThreadIds()).not.toThrow();
        }
        finally {
            if (storageDescriptor) {
                Object.defineProperty(globalThis, 'localStorage', storageDescriptor);
            }
            else {
                Reflect.deleteProperty(globalThis, 'localStorage');
            }
        }
    });
});
