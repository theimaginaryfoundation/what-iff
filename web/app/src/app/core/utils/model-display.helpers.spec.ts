import {
    compareModelsByName,
    formatModelTierDisplay,
    modelTierCompactLabel,
    modelTierName,
    modelTierRank,
    sortedModelsByName,
} from './model-display.helpers';
import { Model } from '../models/model.model';

describe('model-display.helpers', () => {
    it('formats model tiers for display', () => {
        expect(formatModelTierDisplay('low')).toBe('Tier 1');
        expect(formatModelTierDisplay('high')).toBe('Tier 3');
        expect(formatModelTierDisplay('ultra')).toBe('Tier 4');
        expect(formatModelTierDisplay(undefined)).toBe('—');
    });

    it('maps ultra tier to display helpers', () => {
        expect(modelTierName('ultra')).toBe('Ultra');
        expect(modelTierCompactLabel('ultra')).toBe('T4');
        expect(modelTierName('unknown')).toBe('—');
        expect(modelTierCompactLabel('unknown')).toBe('');
    });

    // The tier switch previously spelled out '', undefined and null alongside the
    // default arm. All four returned null, so they collapsed into the default —
    // pinned here so that stays true rather than resting on the reader's reading.
    it('treats absent, empty and unrecognised tiers alike', () => {
        expect(modelTierRank(undefined)).toBeNull();
        expect(modelTierRank(null)).toBeNull();
        expect(modelTierRank('')).toBeNull();
        expect(modelTierRank('weird')).toBeNull();
    });

    it('is case-insensitive about the tier', () => {
        expect(modelTierRank('HIGH')).toBe(3);
        expect(modelTierName('Ultra')).toBe('Ultra');
    });

    it('sorts models alphabetically by display name', () => {
        const models: Model[] = [
            { id: '2', name: 'z', display_name: 'Zeta', description: '', tool_support: false },
            { id: '1', name: 'a', display_name: 'Alpha', description: '', tool_support: false },
        ];
        expect(sortedModelsByName(models).map((m) => m.display_name)).toEqual(['Alpha', 'Zeta']);
        expect(compareModelsByName(models[0], models[1])).toBeGreaterThan(0);
    });

    it('does not mutate the array it is given', () => {
        const models: Model[] = [
            { id: '2', name: 'z', display_name: 'Zeta', description: '', tool_support: false },
            { id: '1', name: 'a', display_name: 'Alpha', description: '', tool_support: false },
        ];
        sortedModelsByName(models);
        expect(models.map((m) => m.display_name)).toEqual(['Zeta', 'Alpha']);
    });
});
