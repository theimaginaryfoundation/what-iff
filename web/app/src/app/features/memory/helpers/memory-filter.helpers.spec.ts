import { DEFAULT_MEMORY_VIEW_FILTERS, parseQueryParams, serializeFilters, toApiFilters, } from './memory-filter.helpers';

describe('memory-filter.helpers', () => {
    it('parses query params into filter model', () => {
        const parsed = parseQueryParams({
            scope: 'chat',
            level: 'summary',
            sort: 'updated_desc',
            query: 'retro',
            personality_id: 'p-1',
            chat: 'c-1',
            min_date: '2026-01-01',
            max_date: '2026-01-31',
        });
        expect(parsed.scope).toBe('chat');
        expect(parsed.level).toBe('summary');
        expect(parsed.sort).toBe('updated_desc');
        expect(parsed.chatId).toBe('c-1');
    });

    it('serializes non-default filters', () => {
        const params = serializeFilters({
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            scope: 'user',
            sort: 'created_asc',
            query: 'insight',
            chatId: 'chat-1',
        });
        expect(params['scope']).toBe('user');
        expect(params['sort']).toBe('created_asc');
        expect(params['query']).toBe('insight');
        expect(params['chat']).toBe('chat-1');
    });

    it('maps view filters to api filters', () => {
        const api = toApiFilters({
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            scope: 'chat',
            sort: 'updated_desc',
            query: 'thread note',
            personalityId: 'persona-1',
            chatId: 'chat-3',
        });
        expect(api.level).toBe('thread');
        expect(api.sort).toBe('updated_desc');
        expect(api.query).toBe('thread note');
        expect(api.pinned_personality_ids).toEqual(['persona-1']);
        expect(api.chat_id).toBe('chat-3');
    });
});
