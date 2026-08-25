import { PersonalityExpression } from '../../../core/models/personality.model';
import { DEFAULT_EXPRESSION_SUGGESTIONS, isDefaultExpressionGridComplete, isValidExpressionKey, mergeManifestWithAssignments, missingExpressions, slotsFromPersistedExpressions, } from './expressions.helpers';

function makeExpression(overrides: Partial<PersonalityExpression> = {}): PersonalityExpression {
    return {
        expression_key: 'happy',
        label: null,
        image_id: null,
        image_url: null,
        created_at: '2026-04-28T00:00:00Z',
        updated_at: '2026-04-28T00:00:00Z',
        ...overrides,
    };
}

describe('slotsFromPersistedExpressions', () => {
    it('maps rows sorted by expression_key', () => {
        const slots = slotsFromPersistedExpressions([
            makeExpression({ expression_key: 'zebra' }),
            makeExpression({ expression_key: 'apple' }),
        ]);
        expect(slots.map(s => s.expressionKey)).toEqual(['apple', 'zebra']);
        expect(slots.every(s => s.isAssigned)).toBe(true);
    });
});

describe('isDefaultExpressionGridComplete', () => {
    it('is false when there are no rows', () => {
        expect(isDefaultExpressionGridComplete([])).toBe(false);
    });

    it('is false when any grid key is missing or has no image', () => {
        const one = makeExpression({ expression_key: 'happy', image_id: 'a' });
        expect(isDefaultExpressionGridComplete([one])).toBe(false);
    });

    it('is true when every default grid key has an image', () => {
        const rows = [...DEFAULT_EXPRESSION_SUGGESTIONS].map((key, i) => makeExpression({ expression_key: key, image_id: `id-${i}` }));
        expect(isDefaultExpressionGridComplete(rows)).toBe(true);
    });
});

describe('mergeManifestWithAssignments', () => {
    it('returns one slot per manifest key when no expressions are assigned', () => {
        const merged = mergeManifestWithAssignments(['happy', 'sad'], []);
        expect(merged.map(e => e.expressionKey)).toEqual(['happy', 'sad']);
        expect(merged.every(e => !e.isAssigned)).toBe(true);
        expect(merged.every(e => e.isSuggested)).toBe(true);
    });

    it('marks suggested keys as assigned when the server returns them', () => {
        const merged = mergeManifestWithAssignments(['happy', 'sad'], [makeExpression({ expression_key: 'happy', image_id: 'img-1', image_url: '/api/image-gallery/img-1' })]);
        expect(merged[0].isAssigned).toBe(true);
        expect(merged[0].imageId).toBe('img-1');
        expect(merged[0].imageUrl).toBe('/api/image-gallery/img-1');
        expect(merged[1].isAssigned).toBe(false);
    });

    it('appends server expressions that are not in the manifest, sorted alphabetically', () => {
        const merged = mergeManifestWithAssignments(['happy'], [
            makeExpression({ expression_key: 'whaaat' }),
            makeExpression({ expression_key: 'argh' }),
            makeExpression({ expression_key: 'happy' }),
        ]);
        expect(merged.map(e => e.expressionKey)).toEqual(['happy', 'argh', 'whaaat']);
        expect(merged[1].isSuggested).toBe(false);
        expect(merged[2].isSuggested).toBe(false);
    });

    it('uses the curated default suggestions list as the manifest', () => {
        const merged = mergeManifestWithAssignments(DEFAULT_EXPRESSION_SUGGESTIONS, []);
        expect(merged.length).toBe(DEFAULT_EXPRESSION_SUGGESTIONS.length);
    });
});

describe('missingExpressions', () => {
    it('returns slots that lack an image assignment', () => {
        const merged = mergeManifestWithAssignments(['happy', 'sad'], [makeExpression({ expression_key: 'happy', image_id: 'img-1' })]);
        expect(missingExpressions(merged).map(s => s.expressionKey)).toEqual(['sad']);
    });
});

describe('isValidExpressionKey', () => {
    it('accepts URL-safe keys', () => {
        expect(isValidExpressionKey('happy')).toBe(true);
        expect(isValidExpressionKey('in-love')).toBe(true);
        expect(isValidExpressionKey('a1_b2')).toBe(true);
    });

    it('rejects keys that violate the backend pattern', () => {
        expect(isValidExpressionKey('Happy')).toBe(false);
        expect(isValidExpressionKey('-leading-dash')).toBe(false);
        expect(isValidExpressionKey('with spaces')).toBe(false);
        expect(isValidExpressionKey('')).toBe(false);
        expect(isValidExpressionKey('a'.repeat(65))).toBe(false);
    });
});
