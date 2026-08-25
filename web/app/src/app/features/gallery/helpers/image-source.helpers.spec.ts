import { FileAttachment } from '../../../core/models/file-attachment.model';
import { formatSource, sourceForImage } from './image-source.helpers';

function makeAttachment(partial: Partial<FileAttachment> = {}): FileAttachment {
    return {
        id: 'img-1',
        user_id: 'user-1',
        name: 'image.png',
        file_type: 'image/png',
        created_at: '2026-05-01T00:00:00Z',
        ...partial,
    };
}

describe('image-source.helpers', () => {
    it('maps chat attachments to generated source', () => {
        const image = makeAttachment({ chat_message_id: 'msg-1' });
        expect(sourceForImage(image)).toBe('generated');
    });

    it('maps personality attachments to uploaded source', () => {
        const image = makeAttachment({ personality_id: 'pers-1' });
        expect(sourceForImage(image)).toBe('uploaded');
    });

    it('maps expression grid personality images to generated source', () => {
        const image = makeAttachment({
            personality_id: 'pers-1',
            name: 'expression-thinking.png',
            file_type: 'image/png',
        });
        expect(sourceForImage(image)).toBe('generated');
    });

    it('maps reference-like keys to reference source', () => {
        const image = makeAttachment({ s3_key: 'users/u1/reference/image.png' });
        expect(sourceForImage(image)).toBe('reference');
    });

    it('formats source labels', () => {
        expect(formatSource('generated')).toBe('Generated');
        expect(formatSource('unknown')).toBe('Unknown');
    });
});
