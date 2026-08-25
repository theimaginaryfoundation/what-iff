import { isChatSendFailed, isChatSendSucceeded, type ChatSendMessageResult, } from './chat-send-result';

describe('ChatSendMessageResult guards', () => {
    it('narrows failed sends', () => {
        const result: ChatSendMessageResult = { status: 'failed', error: new Error('nope') };
        expect(isChatSendFailed(result)).toBe(true);
        if (isChatSendFailed(result)) {
            expect(result.error).toEqual(expect.any(Error));
        }
    });

    it('does not treat skipped as failed or sent', () => {
        const skipped: ChatSendMessageResult = { status: 'skipped' };
        expect(isChatSendFailed(skipped)).toBe(false);
        expect(isChatSendSucceeded(skipped)).toBe(false);
    });
});
