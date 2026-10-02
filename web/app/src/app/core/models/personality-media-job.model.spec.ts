import { parseExpressionCandidatesProgress } from './personality-media-job.model';

describe('parseExpressionCandidatesProgress', () => {
    it('returns null for empty input', () => {
        expect(parseExpressionCandidatesProgress(undefined)).toBeNull();
        expect(parseExpressionCandidatesProgress(null)).toBeNull();
        expect(parseExpressionCandidatesProgress('')).toBeNull();
    });

    it('returns null for malformed JSON', () => {
        expect(parseExpressionCandidatesProgress('{not json')).toBeNull();
    });

    it('returns null for JSON null or a non-candidates mode', () => {
        expect(parseExpressionCandidatesProgress('null')).toBeNull();
        expect(parseExpressionCandidatesProgress(JSON.stringify({ mode: 'default', expressions: [] }))).toBeNull();
    });

    it('returns null when expressions is missing or not an array', () => {
        expect(parseExpressionCandidatesProgress(JSON.stringify({ mode: 'candidates' }))).toBeNull();
        expect(parseExpressionCandidatesProgress(JSON.stringify({ mode: 'candidates', expressions: 'happy' }))).toBeNull();
    });

    it('parses a valid candidates progress payload', () => {
        const payload = {
            mode: 'candidates',
            expressions: ['happy', 'sad'],
            reference_image_id: 'ref-1',
            candidates: [{ expression_key: 'happy', image_id: 'img-1' }],
        };
        expect(parseExpressionCandidatesProgress(JSON.stringify(payload))).toEqual(payload);
    });
});
