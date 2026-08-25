import { FileAttachment } from '../../../core/models/file-attachment.model';
import { applyGalleryFilters, DEFAULT_GALLERY_FILTERS, groupByDate, toGalleryTileVm, } from './gallery-vm.helpers';

function makeAttachment(partial: Partial<FileAttachment> = {}): FileAttachment {
    return {
        id: 'img-1',
        user_id: 'user-1',
        name: 'Sunrise',
        file_type: 'image/png',
        created_at: '2026-05-01T00:00:00Z',
        ...partial,
    };
}

describe('gallery-vm.helpers', () => {
    it('maps file attachments to tile view models', () => {
        const image = makeAttachment({ id: 'img-2', personality_id: 'pers-1', personalities: [{ id: 'pers-1', name: 'Aster' }] });
        const vm = toGalleryTileVm(image, (id, size) => `/api/image-gallery/${id}?size=${size}`, { 'pers-1': 'Aster' });
        expect(vm.id).toBe('img-2');
        expect(vm.personalityName).toBe('Aster');
        expect(vm.personalityNames).toEqual(['Aster']);
        expect(vm.thumbnailUrl).toContain('size=thumbnail');
    });

    it('filters by personality without applying local query filtering', () => {
        const images = [
            makeAttachment({ id: 'a', name: 'Sunrise', personalities: [{ id: 'pers-1', name: 'Aster' }] }),
            makeAttachment({ id: 'b', name: 'Night Sky', personalities: [{ id: 'pers-2', name: 'Vera' }] }),
        ];
        const filtered = applyGalleryFilters(images, { ...DEFAULT_GALLERY_FILTERS, query: 'sun', personalityId: 'pers-1' });
        expect(filtered.map(row => row.id)).toEqual(['a']);
    });

    it('does not hide server query matches that are not in image name', () => {
        const images = [
            makeAttachment({
                id: 'desc-match',
                name: 'Frame_001.png',
                description: 'Testing the agent expression functionality with Aurex to trigger the angry expression',
            }),
        ];
        const filtered = applyGalleryFilters(images, { ...DEFAULT_GALLERY_FILTERS, query: 'trigger' });
        expect(filtered.map(row => row.id)).toEqual(['desc-match']);
    });

    it('groups tile vms by month', () => {
        const rows = [
            toGalleryTileVm(makeAttachment({ id: 'a', created_at: '2026-05-01T00:00:00Z' }), () => '', {}),
            toGalleryTileVm(makeAttachment({ id: 'b', created_at: '2026-05-12T00:00:00Z' }), () => '', {}),
            toGalleryTileVm(makeAttachment({ id: 'c', created_at: '2026-04-12T00:00:00Z' }), () => '', {}),
        ];
        const groups = groupByDate(rows);
        expect(groups.length).toBe(2);
        expect(groups[0].items.length).toBe(2);
    });
});
