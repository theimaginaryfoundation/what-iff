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

    it('coerces an explicit status=summaries param the same way as level=summary', () => {
        const parsed = parseQueryParams({ status: 'summaries', level: 'thread' });
        expect(parsed.status).toBe('summaries');
        expect(parsed.level).toBe('all');
    });

    it('falls back to defaults for unrecognized scope/level/sort/status values', () => {
        const parsed = parseQueryParams({
            scope: 'bogus',
            level: 'bogus',
            sort: 'bogus',
            status: 'bogus',
        });
        expect(parsed.scope).toBe('all');
        expect(parsed.level).toBe('all');
        expect(parsed.sort).toBe('created_desc');
        expect(parsed.status).toBe('active');
    });

    it('prefers the chat param over chat_id, and falls back to chat_id when chat is absent', () => {
        expect(parseQueryParams({ chat: 'c-1', chat_id: 'c-2' }).chatId).toBe('c-1');
        expect(parseQueryParams({ chat_id: 'c-2' }).chatId).toBe('c-2');
        expect(parseQueryParams({}).chatId).toBe('');
    });

    it('trims whitespace from free-text query params', () => {
        const parsed = parseQueryParams({
            query: '  retro  ',
            personality_id: '  p-1  ',
            chat: '  c-1  ',
        });
        expect(parsed.query).toBe('retro');
        expect(parsed.personalityId).toBe('p-1');
        expect(parsed.chatId).toBe('c-1');
    });

    it('omits level=summary from serialized params even if set directly', () => {
        const params = serializeFilters({ ...DEFAULT_MEMORY_VIEW_FILTERS, level: 'summary' });
        expect(params['level']).toBeUndefined();
    });

    it('trims and serializes personalityId/chatId, omitting them when blank', () => {
        const params = serializeFilters({
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            personalityId: '  p-1  ',
            chatId: '  c-1  ',
        });
        expect(params['personality_id']).toBe('p-1');
        expect(params['chat']).toBe('c-1');

        const blank = serializeFilters({ ...DEFAULT_MEMORY_VIEW_FILTERS, personalityId: '   ', chatId: '   ' });
        expect(blank['personality_id']).toBeUndefined();
        expect(blank['chat']).toBeUndefined();
    });

    it('drops invalid dates when serializing filters', () => {
        const params = serializeFilters({
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            minDate: 'not-a-date',
            maxDate: '2026-01-01',
        });
        expect(params['min_date']).toBeUndefined();
        expect(params['max_date']).toBe('2026-01-01');
    });

    it('round-trips parse -> serialize -> parse for a non-default filter set', () => {
        const original = {
            ...DEFAULT_MEMORY_VIEW_FILTERS,
            scope: 'chat' as const,
            level: 'thread' as const,
            status: 'inactive' as const,
            sort: 'updated_desc' as const,
            query: 'retro',
            personalityId: 'p-1',
            chatId: 'c-1',
            minDate: '2026-01-01',
            maxDate: '2026-01-31',
        };
        const roundTripped = parseQueryParams(serializeFilters(original));
        expect(roundTripped).toEqual(original);
    });

    it('resolves scope=chat with no explicit level to thread for the API', () => {
        const api = toApiFilters({ ...DEFAULT_MEMORY_VIEW_FILTERS, scope: 'chat' });
        expect(api.level).toBe('thread');
    });

    it('omits level from api filters when scope is all/user with no explicit level', () => {
        expect(toApiFilters({ ...DEFAULT_MEMORY_VIEW_FILTERS, scope: 'all' }).level).toBeUndefined();
        expect(toApiFilters({ ...DEFAULT_MEMORY_VIEW_FILTERS, scope: 'user' }).level).toBeUndefined();
    });

    it('omits blank query/personalityId/chatId and unset dates from api filters', () => {
        const api = toApiFilters(DEFAULT_MEMORY_VIEW_FILTERS);
        expect(api.query).toBeUndefined();
        expect(api.pinned_personality_ids).toBeUndefined();
        expect(api.chat_id).toBeUndefined();
        expect(api.min_date).toBeUndefined();
        expect(api.max_date).toBeUndefined();
    });

    it('includes min_date/max_date in api filters when set', () => {
        const api = toApiFilters({ ...DEFAULT_MEMORY_VIEW_FILTERS, minDate: '2026-02-01', maxDate: '2026-02-28' });
        expect(api.min_date).toBe('2026-02-01');
        expect(api.max_date).toBe('2026-02-28');
    });

    it('treats an empty date range as valid with no error', () => {
        const result = normalizeDateRange('', '');
        expect(result).toEqual({ minDate: '', maxDate: '', error: null });
    });

    it('accepts an equal (non-inverted) date range without adjustment or error', () => {
        const result = normalizeDateRange('2026-05-10', '2026-05-10');
        expect(result).toEqual({ minDate: '2026-05-10', maxDate: '2026-05-10', error: null });
    });

    it('reports an error when both dates are invalid, prioritizing the start date message', () => {
        const result = normalizeDateRange('not-a-date', 'also-not-a-date');
        expect(result.minDate).toBe('');
        expect(result.maxDate).toBe('');
        expect(result.error).toContain('Start date');
    });

    it('rejects calendar-invalid dates like day 30 of February', () => {
        const result = normalizeDateRange('2026-02-30', '');
        expect(result.minDate).toBe('');
        expect(result.error).toContain('Start date');
    });

    it('accepts Feb 29 on a leap year and rejects it on a non-leap year', () => {
        const leap = normalizeDateRange('2024-02-29', '');
        expect(leap.minDate).toBe('2024-02-29');
        expect(leap.error).toBeNull();

        const nonLeap = normalizeDateRange('2026-02-29', '');
        expect(nonLeap.minDate).toBe('');
        expect(nonLeap.error).toContain('Start date');
    });

    it('rejects dates that are not in strict YYYY-MM-DD format', () => {
        expect(normalizeDateRange('2026-1-1', '').minDate).toBe('');
        expect(normalizeDateRange('2026/01/01', '').minDate).toBe('');
        expect(normalizeDateRange('  2026-01-01  ', '').minDate).toBe('2026-01-01');
    });
});
