import { Model } from '../../../core/models/model.model';
import { groupModelsByTier, modelGroupKey, modelGroupLabel, modelProvider, providerLabel, sortedGroups, sortedProviders } from './model-picker.helpers';

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

    it('labels models from an account\'s own connections by their preset', () => {
        expect(providerLabel('openrouter')).toBe('OpenRouter');
        expect(providerLabel('ollama')).toBe('Ollama');
        expect(providerLabel('lmstudio')).toBe('LM Studio');
        expect(providerLabel('custom')).toBe('Custom');
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

    describe('groups (ADR 0x027)', () => {
        const model = (id: string, provider: string, connection?: { id: string; name?: string }): Model => ({
            id, name: id, display_name: id, description: '', provider, tool_support: true,
            ...(connection ? { connection_id: connection.id, connection_name: connection.name } : {}),
        });

        it("puts a built-in provider's catalog and connection models in one group", () => {
            const models = [model('gpt-catalog', 'openai'), model('gpt-mine', 'openai', { id: 'c-openai', name: 'OpenAI' })];
            expect(sortedGroups(models)).toEqual([{ key: 'openai', label: 'GPT' }]);
        });

        it('gives every other connection its own group, by its name, after the built-ins', () => {
            const models = [
                model('qwen', 'custom', { id: 'c2', name: 'My server' }),
                model('llama', 'ollama', { id: 'c1', name: 'Desk Ollama' }),
                model('phi', 'custom', { id: 'c3', name: 'Attic box' }),
                model('claude', 'anthropic'),
            ];
            expect(sortedGroups(models)).toEqual([
                { key: 'anthropic', label: 'Claude' },
                { key: 'connection:c3', label: 'Attic box' },
                { key: 'connection:c1', label: 'Desk Ollama' },
                { key: 'connection:c2', label: 'My server' },
            ]);
        });

        it("falls back to the preset's label when a connection's name is missing", () => {
            const m = model('x', 'openrouter', { id: 'c9' });
            expect(modelGroupKey(m)).toBe('connection:c9');
            expect(modelGroupLabel(m)).toBe('OpenRouter');
        });
    });
});

