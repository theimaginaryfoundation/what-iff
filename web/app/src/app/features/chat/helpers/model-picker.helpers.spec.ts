import { Model } from '../../../core/models/model.model';
import { groupModelsByTier, modelProvider, providerLabel, sortedProviders } from './model-picker.helpers';

function makeModel(overrides: Partial<Model> = {}): Model {
    return {
        id: 'model-1',
        name: 'gpt-4o-mini',
        display_name: 'GPT-4o Mini',
        description: '',
        tool_support: true,
        provider: 'openai',
        ...overrides,
    };
}

describe('model-picker.helpers', () => {
    it('uses the backend provider field when present', () => {
        expect(modelProvider(makeModel({ provider: 'anthropic', name: 'gpt-fake' }))).toBe('anthropic');
    });

    it('falls back to legacy name heuristics when provider is missing', () => {
        expect(modelProvider(makeModel({ provider: undefined, name: 'claude-sonnet-4-6' }))).toBe('anthropic');
        expect(modelProvider(makeModel({ provider: undefined, name: 'gpt-5.1' }))).toBe('openai');
        expect(modelProvider(makeModel({ provider: undefined, name: 'glm-5.2' }))).toBe('zai');
        expect(modelProvider(makeModel({ provider: undefined, name: 'gemini-pro', display_name: 'Gemini Pro' }))).toBe('google');
    });

    it('labels known and unknown providers', () => {
        expect(providerLabel('openai')).toBe('GPT');
        expect(providerLabel('anthropic')).toBe('Claude');
        expect(providerLabel('zai')).toBe('GLM');
        expect(providerLabel('google')).toBe('Gemini');
        expect(providerLabel('other')).toBe('Other');
    });

    it('sorts known providers first and others alphabetically', () => {
        expect(sortedProviders(['google', 'anthropic', 'openai', 'zai', 'azure'])).toEqual([
            'openai',
            'anthropic',
            'zai',
            'google',
            'azure',
        ]);
    });

    describe('groupModelsByTier', () => {
        it('buckets models by tier rank, sorted by name, omitting empty tiers', () => {
            const groups = groupModelsByTier([
                makeModel({ id: 'a', display_name: 'Zeta', subscription_tier: 'low' }),
                makeModel({ id: 'b', display_name: 'Alpha', subscription_tier: 'low' }),
                makeModel({ id: 'c', display_name: 'Beta', subscription_tier: 'high' }),
            ]);

            expect(groups.map(g => g.label)).toEqual(['Tier 1', 'Tier 3']);
            expect(groups[0].models.map(m => m.display_name)).toEqual(['Alpha', 'Zeta']);
            expect(groups[1].rank).toBe(3);
        });

        it('places untiered models in a trailing "Other" bucket', () => {
            const groups = groupModelsByTier([
                makeModel({ id: 'a', display_name: 'Tiered', subscription_tier: 'medium' }),
                makeModel({ id: 'b', display_name: 'Untiered', subscription_tier: undefined }),
            ]);

            expect(groups.map(g => g.label)).toEqual(['Tier 2', 'Other']);
            expect(groups[1].rank).toBeNull();
        });
    });
});
