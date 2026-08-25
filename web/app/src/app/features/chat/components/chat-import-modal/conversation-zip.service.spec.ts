import { conversationDedupKey, conversationSortTime, findSplitConversationParts, listConversationExportPaths, mergeConversationShards, } from './conversation-zip.service';

describe('listConversationExportPaths', () => {
    it('matches canonical and numbered shards, sorted alphabetically', () => {
        expect(listConversationExportPaths([
            'ChatGPT/conversations-002.json',
            'ChatGPT/manifest.json',
            'ChatGPT/conversations.json',
            'ChatGPT/conversations-000.json',
            'ChatGPT/conversations-001.json',
            'notes/conversations-backup.json',
        ])).toEqual([
            'ChatGPT/conversations-000.json',
            'ChatGPT/conversations-001.json',
            'ChatGPT/conversations-002.json',
            'ChatGPT/conversations.json',
        ]);
    });
});

describe('findSplitConversationParts', () => {
    it('detects numbered OpenAI conversation shards only', () => {
        expect(findSplitConversationParts([
            'ChatGPT/conversations-001.json',
            'ChatGPT/conversations-002.json',
            'ChatGPT/conversations.json',
            'ChatGPT/manifest.json',
        ])).toEqual(['ChatGPT/conversations-001.json', 'ChatGPT/conversations-002.json']);
    });
});

describe('mergeConversationShards', () => {
    it('dedupes by conversation_id and sorts by create_time', () => {
        const merged = mergeConversationShards([
            [
                { conversation_id: 'a', create_time: 200, title: 'later' },
                { conversation_id: 'b', create_time: 50, title: 'early-b' },
            ],
            [
                { conversation_id: 'a', create_time: 200, title: 'later-updated' },
                { id: 'c', create_time: 10, title: 'earliest' },
            ],
        ]);

        expect(merged.map(c => conversationDedupKey(c))).toEqual(['c', 'b', 'a']);
        expect((merged[2] as {
            title: string;
        }).title).toBe('later-updated');
    });

    it('appends conversations without ids after timed ones', () => {
        const merged = mergeConversationShards([
            [{ title: 'no-id' }, { id: 'x', create_time: 1 }],
        ]);
        expect(conversationDedupKey(merged[0])).toBe('x');
        expect(conversationSortTime(merged[1])).toBeNaN();
    });
});
