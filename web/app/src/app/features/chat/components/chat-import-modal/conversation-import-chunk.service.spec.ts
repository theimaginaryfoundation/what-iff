import { CHAT_IMPORT_CHUNK_BYTES, ConversationImportChunkService, parseConversationsExport, } from './conversation-import-chunk.service';

/** Small cap so split/pack tests stay fast (no multi‑MB string allocations). */
const TEST_CHUNK_BYTES = 256;

describe('parseConversationsExport', () => {
    it('accepts a top-level array (Claude / ChatGPT)', () => {
        const parsed = parseConversationsExport('[{"uuid":"1","chat_messages":[]}]');
        expect(parsed.shape).toBe('array');
        expect(parsed.conversations).toHaveLength(1);
    });

    it('accepts an OpenAI object-wrapped export', () => {
        const parsed = parseConversationsExport('{"conversations":[{"id":"c1","mapping":{}}]}');
        expect(parsed.shape).toBe('openai-wrapped');
        expect(parsed.conversations).toHaveLength(1);
    });

    it('rejects invalid JSON', () => {
        expect(() => parseConversationsExport('not json')).toThrowError(/invalid JSON/i);
    });
});

describe('ConversationImportChunkService', () => {
    let service: ConversationImportChunkService;

    beforeEach(() => {
        service = new ConversationImportChunkService();
    });

    it('passes through files within the chunk cap and still reports conversation count', async () => {
        const blob = new Blob(['[{"id":"small"},{"id":"two"}]'], { type: 'application/json' });
        const result = await service.splitExport(blob);
        expect(result.chunks).toHaveLength(1);
        expect(result.chunks[0]).toBe(blob);
        expect(result.totalConversations).toBe(2);
    });

    it('splits a large array export at conversation boundaries', async () => {
        const big = 'x'.repeat(TEST_CHUNK_BYTES / 2);
        const conversations = [
            { id: 'a', text: big },
            { id: 'b', text: big },
            { id: 'c', text: 'small' },
        ];
        const blob = new Blob([JSON.stringify(conversations)], { type: 'application/json' });

        const result = await service.splitExport(blob, { maxChunkBytes: TEST_CHUNK_BYTES });
        expect(result.chunks.length).toBeGreaterThan(1);
        expect(result.totalConversations).toBe(3);
        for (const chunk of result.chunks) {
            expect(chunk.size).toBeLessThanOrEqual(TEST_CHUNK_BYTES);
            const parsed = JSON.parse(await chunk.text());
            expect(Array.isArray(parsed)).toBe(true);
        }

        const merged = (await Promise.all(result.chunks.map(c => c.text()))).flatMap(text => JSON.parse(text) as unknown[]);
        expect(merged).toHaveLength(3);
        expect(merged.map(c => (c as {
            id: string;
        }).id)).toEqual(['a', 'b', 'c']);
    });

    it('preserves OpenAI object-wrapped shape in each chunk', async () => {
        const big = 'y'.repeat(TEST_CHUNK_BYTES / 2);
        const payload = {
            conversations: [
                { id: '1', mapping: {}, payload: big },
                { id: '2', mapping: {}, payload: big },
            ],
        };
        const blob = new Blob([JSON.stringify(payload)], { type: 'application/json' });

        const result = await service.splitExport(blob, { maxChunkBytes: TEST_CHUNK_BYTES });
        expect(result.chunks.length).toBe(2);
        for (const chunk of result.chunks) {
            const parsed = JSON.parse(await chunk.text()) as {
                conversations: unknown[];
            };
            expect(Array.isArray(parsed.conversations)).toBe(true);
            expect(chunk.size).toBeLessThanOrEqual(TEST_CHUNK_BYTES);
        }
    });

    it('rejects a single conversation larger than the chunk cap', async () => {
        const huge = 'z'.repeat(TEST_CHUNK_BYTES + 64);
        const blob = new Blob([JSON.stringify([{ id: 'only', body: huge }])], { type: 'application/json' });

        await expect(service.splitExport(blob, { maxChunkBytes: TEST_CHUNK_BYTES })).rejects.toThrowError(/larger than 60MB/i);
    });

    it('uses the production 60MB cap by default', () => {
        expect(CHAT_IMPORT_CHUNK_BYTES).toBe(60 * 1024 * 1024);
    });
});
