import { Mood } from '../../../core/models/mood.model';
import { Personality } from '../../../core/models/personality.model';

export interface ModePersonalityVm {
  id: string;
  name: string;
  accentColor: string;
  coverUrl: string | null;
  initials: string;
}

export interface ModeCardVm {
  mood: Mood;
  title: string;
  description: string;
  toolsSilencedLabel: string;
  skillsLabel: string;
  jobsLabel: string;
  personalities: ModePersonalityVm[];
}

export function filterMoodsBySelectedPersonalities(moods: Mood[], selectedPersonalityIds: readonly string[]): Mood[] {
  if (selectedPersonalityIds.length === 0) return moods;
  const selectedSet = new Set(selectedPersonalityIds);
  return moods.filter(mood =>
    (mood.personality_ids ?? []).some(personalityID => selectedSet.has(personalityID)),
  );
}

export function moodSkillsChipText(mood: Mood): string {
  const count = mood.ritual_ids?.length ?? 0;
  return count === 0 ? 'All skills on' : `${count} skills configured`;
}

export function moodJobsChipText(): string {
  return 'Jobs on';
}

export function toolSilencedCountLabel(count: number): string {
  return `${count} silenced`;
}

export function initialsForName(name: string): string {
  return name
    .trim()
    .split(/\s+/)
    .filter(Boolean)
    .slice(0, 2)
    .map(part => part[0]?.toUpperCase() ?? '')
    .join('') || '?';
}

export function filterAssociationOptions(personalities: Personality[], mood: Mood, query: string): Personality[] {
  const selected = new Set(mood.personality_ids ?? []);
  const normalized = query.trim().toLowerCase();
  return personalities
    .filter(personality => !selected.has(personality.id))
    .filter(personality => !normalized || personality.name.toLowerCase().includes(normalized));
}
