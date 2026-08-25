import { Ritual } from '../../../core/models/ritual.model';
import { groupByAffinity, toRitualRowVm } from './ritual-vm.helpers';

describe('ritual-vm.helpers', () => {
    const personalityMap = new Map<string, string>([
        ['p-1', 'Planner'],
        ['p-2', 'Creative'],
    ]);

    const ritual = (overrides: Partial<Ritual>): Ritual => ({
        id: 'r-1',
        name: 'Morning brief',
        description: 'Start the day',
        content: 'Summarize priorities',
        hotkeys: '',
        personality_id: null,
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
        ...overrides,
    });

    it('maps ritual into row vm', () => {
        const vm = toRitualRowVm(ritual({ personality_id: 'p-1', hotkeys: 'ctrl+m' }), personalityMap);
        expect(vm.affinityLabel).toBe('Planner');
        expect(vm.hasHotkey).toBe(true);
        expect(vm.isSystem).toBe(false);
    });

    it('groups rituals by affinity with all-skills last', () => {
        const grouped = groupByAffinity([
            ritual({ id: 'one', name: 'Alpha', personality_id: 'p-1' }),
            ritual({ id: 'two', name: 'Beta', personality_id: null }),
            ritual({ id: 'three', name: 'Gamma', personality_id: 'p-2' }),
        ], personalityMap);

        expect(grouped.map(group => group.label)).toEqual(['Creative', 'Planner', 'All skills']);
        expect(grouped[2].rituals[0].id).toBe('two');
    });
});
