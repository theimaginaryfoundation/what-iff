import { detectModelChange, extractImages, splitInlineCode } from './message-content.helpers';

describe('message-content helpers', () => {
    describe('extractImages', () => {
        it('extracts markdown images and bare image urls', () => {
            expect(extractImages('![Alt](https://example.com/a.png) https://example.com/b.webp')).toEqual([
                { alt: 'Alt', src: 'https://example.com/a.png' },
                { alt: '', src: 'https://example.com/b.webp' },
            ]);
        });

        it('does not duplicate urls already present as markdown images', () => {
            expect(extractImages('![Alt](https://example.com/a.png)')).toEqual([
                { alt: 'Alt', src: 'https://example.com/a.png' },
            ]);
        });
    });

    describe('splitInlineCode', () => {
        it('splits text around inline code runs', () => {
            expect(splitInlineCode('Use `npm test` now')).toEqual([
                { kind: 'text', value: 'Use ' },
                { kind: 'code', value: 'npm test' },
                { kind: 'text', value: ' now' },
            ]);
        });

        it('returns an empty list for empty input', () => {
            expect(splitInlineCode('')).toEqual([]);
        });
    });

    describe('detectModelChange', () => {
        it('detects changes only when both models are present and different', () => {
            expect(detectModelChange('a', 'b')).toBe(true);
            expect(detectModelChange('a', 'a')).toBe(false);
            expect(detectModelChange(undefined, 'a')).toBe(false);
            expect(detectModelChange('a', '')).toBe(false);
        });
    });
});
