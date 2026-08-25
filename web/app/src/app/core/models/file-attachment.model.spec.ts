import { createPendingFileAttachment, newPendingAttachmentClientKey, pendingAttachmentKey, PendingFileAttachment, } from './file-attachment.model';

describe('pendingAttachmentKey', () => {
    it('prefers clientKey when present', () => {
        const item: PendingFileAttachment = { clientKey: 'client-abc', isUploading: false };
        expect(pendingAttachmentKey(item)).toBe('client-abc');
    });

    it('falls back to attachment id', () => {
        const item: PendingFileAttachment = {
            attachment: {
                id: 'att-1',
                user_id: 'u-1',
                name: 'photo.png',
                file_type: 'image/png',
                created_at: '2026-01-01T00:00:00Z',
            },
            isUploading: false,
        };
        expect(pendingAttachmentKey(item)).toBe('attachment:att-1');
    });

    it('falls back to file name, size, and lastModified', () => {
        const file = new File(['x'], 'notes.txt', { type: 'text/plain', lastModified: 42 });
        const item: PendingFileAttachment = { file, isUploading: true };
        expect(pendingAttachmentKey(item)).toBe(`file:notes.txt:${file.size}:42`);
    });

    it('throws when no stable identifier is available', () => {
        const item: PendingFileAttachment = { isUploading: false };
        expect(() => pendingAttachmentKey(item)).toThrowError(/missing a stable identifier/);
    });
});

describe('newPendingAttachmentClientKey', () => {
    it('uses crypto.randomUUID when available', () => {
        const randomUUID = vi.fn().mockName('randomUUID').mockReturnValue('uuid-from-crypto');
        const originalCrypto = globalThis.crypto;

        Object.defineProperty(globalThis, 'crypto', {
            configurable: true,
            value: { randomUUID },
        });

        try {
            expect(newPendingAttachmentClientKey()).toBe('uuid-from-crypto');
            expect(randomUUID).toHaveBeenCalled();
        }
        finally {
            Object.defineProperty(globalThis, 'crypto', {
                configurable: true,
                value: originalCrypto,
            });
        }
    });

    it('falls back when crypto.randomUUID is unavailable', () => {
        const originalCrypto = globalThis.crypto;

        Object.defineProperty(globalThis, 'crypto', {
            configurable: true,
            value: undefined,
        });

        try {
            const key = newPendingAttachmentClientKey();
            expect(key).toMatch(/^pending-\d+-[a-z0-9]+$/);
        }
        finally {
            Object.defineProperty(globalThis, 'crypto', {
                configurable: true,
                value: originalCrypto,
            });
        }
    });
});

describe('createPendingFileAttachment', () => {
    it('assigns a clientKey when omitted', () => {
        const item = createPendingFileAttachment({ isUploading: false });
        expect(item.clientKey).toBeTruthy();
        expect(pendingAttachmentKey(item)).toBe(item.clientKey!);
    });

    it('preserves a provided clientKey', () => {
        const item = createPendingFileAttachment({ clientKey: 'fixed-key', isUploading: false });
        expect(item.clientKey).toBe('fixed-key');
    });
});
