import { Mood } from '../../../core/models/mood.model';
import { Personality } from '../../../core/models/personality.model';
import {
  filterAssociationOptions,
  filterMoodsBySelectedPersonalities,
  initialsForName,
  moodJobsChipText,
  moodSkillsChipText,
  toolSilencedCountLabel,
} from './mode-vm.helpers';

function makeMood(overrides: Partial<Mood> = {}): Mood {
  return {
    id: 'mode-1',
    name: 'Focused',
    description: '',
    prompt_snippet: '',
    image_ids: [],
    ritual_ids: [],
    personality_ids: [],
    created_at: '',
    updated_at: '',
    ...overrides,
  };
}

function makePersonality(overrides: Partial<Personality> = {}): Personality {
  return {
    id: 'persona-1',
    name: 'Ada',
    system_prompt: 'system',
    auto_pin_memories: false,
    expressions_enabled: true,
    image_style: 'auto',    cover_image_id: null,
    cover_image_url: null,
    accent_color: null,
    thumbnail_circle: null,
    created_at: '',
    updated_at: '',
    stats: {
      chat_count: 0,
      last_used_at: null,
    },
    ...overrides,
  };
}

describe('mode-vm helpers', () => {
  it('filters moods by selected personality IDs', () => {
    const moods = [
      makeMood({ id: 'm1', personality_ids: ['p1'] }),
      makeMood({ id: 'm2', personality_ids: ['p2'] }),
      makeMood({ id: 'm3', personality_ids: [] }),
    ];

    expect(filterMoodsBySelectedPersonalities(moods, []).map(item => item.id)).toEqual(['m1', 'm2', 'm3']);
    expect(filterMoodsBySelectedPersonalities(moods, ['p2']).map(item => item.id)).toEqual(['m2']);
  });

  it('builds chip labels', () => {
    expect(moodSkillsChipText(makeMood({ ritual_ids: [] }))).toBe('All skills on');
    expect(moodSkillsChipText(makeMood({ ritual_ids: ['r1', 'r2'] }))).toBe('2 skills configured');
    expect(moodJobsChipText()).toBe('Jobs on');
    expect(toolSilencedCountLabel(3)).toBe('3 silenced');
  });

  it('creates initials with fallback', () => {
    expect(initialsForName('Ada Lovelace')).toBe('AL');
    expect(initialsForName('  ')).toBe('?');
  });

  it('filters association options by selected and query', () => {
    const mood = makeMood({ personality_ids: ['p1'] });
    const personalities = [
      makePersonality({ id: 'p1', name: 'Ada' }),
      makePersonality({ id: 'p2', name: 'Vera' }),
      makePersonality({ id: 'p3', name: 'Aurex' }),
    ];

    expect(filterAssociationOptions(personalities, mood, '').map(item => item.id)).toEqual(['p2', 'p3']);
    expect(filterAssociationOptions(personalities, mood, 'ver').map(item => item.id)).toEqual(['p2']);
  });
});
