import { DEFAULT_MEMORY_VIEW_FILTERS, normalizeDateRange, parseQueryParams, serializeFilters, toApiFilters, } from './memory-filter.helpers';

describe('memory-filter.helpers', () => {
    it('parses query params into filter model', () => {
        const parsed = parseQueryParams({
            scope: 'chat',
            level: 'thread',
            sort: 'updated_desc',
            query: 'retro',
            personality_id: 'p-1',
            chat: 'c-1',
            min_date: '2026-01-01',
            max_date: '2026-01-31',
        });
        expect(parsed.scope).toBe('chat');
        expect(parsed.level).toBe('thread');
        expect(parsed.sort).toBe('updated_desc');
        expect(parsed.chatId).toBe('c-1');
    });

    it('coerces legacy level=summary into Summaries status tab', () => {
        const parsed = parseQueryParams({ level: 'summary' });
        expect(parsed.status).toBe('summaries');
        expect(parsed.level).toBe('all');
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

    it('serializes summaries status without level param', () => {
        const params = serializeFilters({
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            status: 'summaries',
        });
        expect(params['status']).toBe('summaries');
        expect(params['level']).toBeUndefined();
    });

    it('maps view filters to api filters', () => {
        const api = toApiFilters({
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            scope: 'chat',
            sort: 'updated_desc',
            query: 'thread note',
            personalityId: 'persona-1',
            chatId: 'chat-3',
            status: 'inactive',
        });
        expect(api.level).toBe('thread');
        expect(api.sort).toBe('updated_desc');
        expect(api.query).toBe('thread note');
        expect(api.pinned_personality_ids).toEqual(['persona-1']);
        expect(api.chat_id).toBe('chat-3');
        expect(api.status).toBe('inactive');
    });

    it('maps summaries status to level=summary for the API', () => {
        const api = toApiFilters({
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            status: 'summaries',
            level: 'personality',
        });
        expect(api.level).toBe('summary');
        expect(api.status).toBe('active');
    });

    it('clamps inverted date ranges and drops invalid dates', () => {
        const inverted = normalizeDateRange('2026-05-10', '2026-05-01');
        expect(inverted.minDate).toBe('2026-05-10');
        expect(inverted.maxDate).toBe('2026-05-10');
        expect(inverted.error).toContain('before start date');

        const invalid = normalizeDateRange('not-a-date', '2026-01-01');
        expect(invalid.minDate).toBe('');
        expect(invalid.maxDate).toBe('2026-01-01');
        expect(invalid.error).toContain('Start date');
    });

    it('normalizes inverted dates from query params', () => {
        const parsed = parseQueryParams({
            min_date: '2026-06-10',
            max_date: '2026-06-01',
        });
        expect(parsed.minDate).toBe('2026-06-10');
        expect(parsed.maxDate).toBe('2026-06-10');
    });

    it('defaults status to active and serializes inactive', () => {
        expect(parseQueryParams({}).status).toBe('active');
        expect(serializeFilters({ ...DEFAULT_MEMORY_VIEW_FILTERS, status: 'inactive' })['status']).toBe('inactive');
        expect(serializeFilters(DEFAULT_MEMORY_VIEW_FILTERS)['status']).toBeUndefined();
    });
});
