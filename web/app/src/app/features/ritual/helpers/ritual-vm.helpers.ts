import { Ritual } from '../../../core/models/ritual.model';

export interface RitualRowVm {
  id: string;
  name: string;
  description: string;
  content: string;
  hotkeys: string;
  hasHotkey: boolean;
  personalityId: string | null;
  affinityLabel: string;
  createdAt: string;
  updatedAt: string;
  isSystem: boolean;
}

export interface RitualAffinityGroupVm {
  id: string;
  label: string;
  rituals: RitualRowVm[];
}

export function toRitualRowVm(
  ritual: Ritual,
  personalityNameById: ReadonlyMap<string, string>,
  isSystem: boolean = false,
): RitualRowVm {
  const affinityLabel = ritual.personality_id
    ? (personalityNameById.get(ritual.personality_id) ?? 'Unknown personality')
    : 'All skills';

  return {
    id: ritual.id,
    name: ritual.name,
    description: ritual.description,
    content: ritual.content,
    hotkeys: ritual.hotkeys,
    hasHotkey: ritual.hotkeys.trim().length > 0,
    personalityId: ritual.personality_id,
    affinityLabel,
    createdAt: ritual.created_at,
    updatedAt: ritual.updated_at,
    isSystem,
  };
}

export function groupByAffinity(
  rituals: Ritual[],
  personalityNameById: ReadonlyMap<string, string>,
): RitualAffinityGroupVm[] {
  const byGroup = new Map<string, RitualRowVm[]>();
  for (const ritual of rituals) {
    const vm = toRitualRowVm(ritual, personalityNameById, false);
    const key = vm.personalityId ?? 'all-skills';
    byGroup.set(key, [...(byGroup.get(key) ?? []), vm]);
  }

  const entries = Array.from(byGroup.entries()).map(([groupId, items]) => ({
    id: groupId,
    label: groupId === 'all-skills' ? 'All skills' : (personalityNameById.get(groupId) ?? 'Unknown personality'),
    rituals: items.sort((a, b) => a.name.localeCompare(b.name)),
  }));

  entries.sort((a, b) => {
    if (a.id === 'all-skills') return 1;
    if (b.id === 'all-skills') return -1;
    return a.label.localeCompare(b.label);
  });
  return entries;
}
