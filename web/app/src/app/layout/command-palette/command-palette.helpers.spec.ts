import { SearchSection } from '../../core/models/search.model';
import { mergeSections, rankResults, SCORE_EXACT, SCORE_NO_MATCH, SCORE_PREFIX, SCORE_SUBSTRING, SCORE_WORD_BOUNDARY, scoreItem, scoreText, } from './command-palette.helpers';

describe('command-palette.helpers', () => {
    describe('scoreText', () => {
        it('returns EXACT when text equals query case-insensitively', () => {
            expect(scoreText('atlas', 'Atlas')).toBe(SCORE_EXACT);
        });

        it('returns PREFIX when text starts with query', () => {
            expect(scoreText('atl', 'Atlas roadmap')).toBe(SCORE_PREFIX);
        });

        it('returns WORD_BOUNDARY when query starts a non-leading word', () => {
            expect(scoreText('road', 'Atlas roadmap')).toBe(SCORE_WORD_BOUNDARY);
        });

        it('treats hyphens and slashes as word boundaries', () => {
            expect(scoreText('docs', 'project-docs')).toBe(SCORE_WORD_BOUNDARY);
            expect(scoreText('src', 'web/src')).toBe(SCORE_WORD_BOUNDARY);
        });

        it('returns SUBSTRING for mid-word matches', () => {
            expect(scoreText('atal', 'Catalyst')).toBe(SCORE_SUBSTRING);
        });

        it('returns NO_MATCH for non-matches and empty inputs', () => {
            expect(scoreText('xyz', 'Atlas')).toBe(SCORE_NO_MATCH);
            expect(scoreText('', 'Atlas')).toBe(SCORE_NO_MATCH);
            expect(scoreText('atlas', '')).toBe(SCORE_NO_MATCH);
            expect(scoreText('   ', 'Atlas')).toBe(SCORE_NO_MATCH);
        });
    });

    describe('scoreItem', () => {
        it('takes the maximum tier across label and description', () => {
            expect(scoreItem('atlas', { label: 'Project Catalyst', description: 'Atlas roadmap' })).toBe(SCORE_PREFIX);
        });

        it('handles missing description gracefully', () => {
            expect(scoreItem('cat', { label: 'Catalyst' })).toBe(SCORE_PREFIX);
        });
    });

    describe('rankResults', () => {
        it('drops non-matches and orders by descending score', () => {
            const ranked = rankResults([
                { label: 'Atlas roadmap' }, // PREFIX 80
                { label: 'unrelated' }, // 0 -> dropped
                { label: 'World atlas' }, // WORD_BOUNDARY 60
                { label: 'Catalyst', description: 'atlas of designs' }, // PREFIX 80 (description)
            ], 'atlas');
            expect(ranked.map(r => r.label)).toEqual([
                'Atlas roadmap',
                'Catalyst',
                'World atlas',
            ]);
            expect(ranked.every(r => r.score > 0)).toBe(true);
        });

        it('returns empty list when query is empty or whitespace only', () => {
            expect(rankResults([{ label: 'Atlas' }], '')).toEqual([]);
            expect(rankResults([{ label: 'Atlas' }], '   ')).toEqual([]);
        });
    });

    describe('mergeSections', () => {
        const make = (type: any, count: number): SearchSection => ({
            type,
            results: Array.from({ length: count }, (_, i) => ({
                id: `${type}-${i}`,
                label: `${type} ${i}`,
                description: '',
                route: `/x/${type}-${i}`,
                icon_type: type,
                score: 50,
            })),
        });

        it('reorders into canonical chat/personality/ritual/memory/image order', () => {
            const out = mergeSections([
                make('image', 1),
                make('chat', 1),
                make('memory', 1),
                make('personality', 1),
                make('ritual', 1),
            ]);
            expect(out.map(s => s.type)).toEqual(['chat', 'personality', 'ritual', 'memory', 'image']);
        });

        it('drops unknown section types silently', () => {
            const out = mergeSections([
                make('chat', 1),
                make('something-else' as any, 1),
                make('memory', 1),
            ]);
            expect(out.map(s => s.type)).toEqual(['chat', 'memory']);
        });

        it('omits sections that were not provided rather than padding with empties', () => {
            const out = mergeSections([make('chat', 1), make('memory', 1)]);
            expect(out.map(s => s.type)).toEqual(['chat', 'memory']);
        });
    });
});
