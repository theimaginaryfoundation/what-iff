import { branchBannerText, branchPlanFor, groupBranchesByMessage } from './branch.helpers';

describe('groupBranchesByMessage', () => {
    it('groups by branch point, newest first, skipping branches without one', () => {
        const map = groupBranchesByMessage([
            { id: 'b1', name: 'one', forked_from_message_id: 'm1', created_at: '2026-01-01T00:00:00Z' },
            { id: 'b2', name: 'two', forked_from_message_id: 'm1', created_at: '2026-01-03T00:00:00Z' },
            { id: 'b3', name: 'three', forked_from_message_id: 'm2', created_at: '2026-01-02T00:00:00Z' },
            { id: 'b4', name: 'orphan', created_at: '2026-01-04T00:00:00Z' },
        ]);
        expect(map.get('m1')?.map(b => b.id)).toEqual(['b2', 'b1']);
        expect(map.get('m2')?.map(b => b.id)).toEqual(['b3']);
        expect(map.size).toBe(2);
    });

    it('handles empty input', () => {
        expect(groupBranchesByMessage(undefined).size).toBe(0);
        expect(groupBranchesByMessage([]).size).toBe(0);
    });
});

describe('branchPlanFor', () => {
    it('branches before a user message and offers its text back as a draft', () => {
        const plan = branchPlanFor({ id: 'm1', origin: 'User', message: 'Should we go to Lisbon?' });
        expect(plan.request).toEqual({ message_id: 'm1', include_message: false });
        expect(plan.draft).toBe('Should we go to Lisbon?');
    });

    it('does not offer an empty user message as a draft', () => {
        expect(branchPlanFor({ id: 'm1', origin: 'User', message: '   ' }).draft).toBeNull();
    });

    it('branches through an assistant reply with no draft', () => {
        const plan = branchPlanFor({ id: 'm2', origin: 'Assistant', message: 'Lisbon is lovely.' });
        expect(plan.request).toEqual({ message_id: 'm2', include_message: true });
        expect(plan.draft).toBeNull();
    });
});

describe('branchBannerText', () => {
    it('is null for original threads', () => {
        expect(branchBannerText(null)).toBeNull();
        expect(branchBannerText({ parent: null, branches: [] })).toBeNull();
    });

    it('names the parent', () => {
        expect(branchBannerText({ parent: { id: 'p', name: ' Trip ', deleted: false }, branches: [] }))
            .toBe('Branched from “Trip”');
    });

    it('explains a deleted parent', () => {
        expect(branchBannerText({ parent: { id: 'p', deleted: true }, branches: [] }))
            .toContain('deleted');
    });
});
