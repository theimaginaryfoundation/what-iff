import { galleryDisplayBaseName, galleryFileExtension, galleryFilenameFromBaseName, } from './gallery-filename.helpers';

describe('gallery-filename helpers', () => {
    it('strips extension for display', () => {
        expect(galleryDisplayBaseName('cute_fox.png')).toBe('cute_fox');
        expect(galleryFileExtension('cute_fox.png')).toBe('.png');
    });

    it('reapplies extension when saving a new base name', () => {
        expect(galleryFilenameFromBaseName('cute_fox.png', 'fox')).toBe('fox.png');
    });

    it('does not double-append extension', () => {
        expect(galleryFilenameFromBaseName('cute_fox.png', 'fox.png')).toBe('fox.png');
    });

    it('leaves extensionless names unchanged', () => {
        expect(galleryFilenameFromBaseName('untitled', 'renamed')).toBe('renamed');
    });
});
