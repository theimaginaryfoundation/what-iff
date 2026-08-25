import { parseChatImportProgress } from './chat-import-modal.component';

describe('parseChatImportProgress', () => {
    it('rejects the parsing payload because it has no counts', () => {
        expect(parseChatImportProgress('{"phase":"parsing","source":"openai"}')).toBeNull();
    });

    it('preserves terminal imported and skipped counts', () => {
        expect(parseChatImportProgress('{"phase":"complete","total":115,"imported":112,"skipped":3}')).toEqual(expect.objectContaining({
            phase: 'complete',
            total: 115,
            imported: 112,
            skipped: 3,
        }));
    });

    it('rejects missing, negative, and fractional counts', () => {
        expect(parseChatImportProgress('{"total":5,"imported":2}')).toBeNull();
        expect(parseChatImportProgress('{"total":5,"imported":-1,"skipped":0}')).toBeNull();
        expect(parseChatImportProgress('{"total":5,"imported":1.5,"skipped":0}')).toBeNull();
    });
});
