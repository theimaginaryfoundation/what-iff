import { Personality, PersonalityUsageStats } from '../../../core/models/personality.model';
import { shortName } from './short-name.helpers';

/**
 * View-model representation of a personality card on the personalities grid.
 */
export interface PersonalityCardVm {
  id: string;
  name: string;
  shortName: string;
  systemPromptPreview: string;
  accent: string;
  accentSurface: string;
  usageBadge: string;
  coverImageUrl: string | null;
  isDefault: boolean;
  stats: PersonalityUsageStats;
}

const SYSTEM_PROMPT_PREVIEW_LENGTH = 140;

/**
 * Deterministically derives an accent color (HSL) from a personality ID
 * so multiple loads always produce the same color. The hue is hashed
 * across the full 360 degree spectrum, while saturation/lightness are
 * locked to values that read well in both light and dark themes.
 */
export function personalityAccent(personality: Pick<Personality, 'id' | 'name' | 'accent_color'>): string {
  if (personality.accent_color?.trim()) {
    return personality.accent_color.trim();
  }
  const seed = (personality.id || personality.name || '').toString();
  if (!seed) {
    return 'hsl(220 70% 50%)';
  }

  let hash = 0;
  for (let i = 0; i < seed.length; i++) {
    hash = (hash * 31 + seed.charCodeAt(i)) >>> 0;
  }
  const hue = hash % 360;
  return `hsl(${hue} 65% 52%)`;
}

/**
 * Returns a tinted surface color suitable for using as a background
 * behind --text-primary. Implementations using `color-mix` give us a
 * predictable AA contrast ratio without per-personality manual tuning.
 */
export function personalityAccentSurface(accent: string): string {
  return `color-mix(in oklab, ${accent} 14%, transparent)`;
}

/**
 * Builds a copy-paste friendly string for the usage badge on a
 * personality card. Always returns a non-empty value.
 */
export function usageBadge(stats: PersonalityUsageStats | null | undefined): string {
  const count = Math.max(0, stats?.chat_count ?? 0);
  if (count === 0) {
    return 'New thread';
  }
  return `${count} ${count === 1 ? 'thread' : 'threads'}`;
}

function previewSystemPrompt(prompt: string): string {
  if (!prompt) return '';
  const collapsed = prompt.replace(/\s+/g, ' ').trim();
  if (collapsed.length <= SYSTEM_PROMPT_PREVIEW_LENGTH) return collapsed;
  return `${collapsed.slice(0, SYSTEM_PROMPT_PREVIEW_LENGTH - 1).trimEnd()}…`;
}

export interface ToCardOptions {
  defaultPersonalityId?: string | null;
  /** Optional cover override (e.g. derived from expressions); falls back to personality.cover_image_url. */
  coverImageUrl?: string | null;
}

export function toPersonalityCardVm(personality: Personality, opts: ToCardOptions = {}): PersonalityCardVm {
  const accent = personalityAccent(personality);
  const accentSurface = personalityAccentSurface(accent);
  return {
    id: personality.id,
    name: personality.name,
    shortName: shortName(personality.name),
    systemPromptPreview: previewSystemPrompt(personality.system_prompt),
    accent,
    accentSurface,
    usageBadge: usageBadge(personality.stats),
    coverImageUrl: opts.coverImageUrl ?? personality.cover_image_url ?? null,
    isDefault: !!opts.defaultPersonalityId && opts.defaultPersonalityId === personality.id,
    stats: personality.stats ?? { chat_count: 0, last_used_at: null },
  };
}

export interface PersonalityDetailVm {
  id: string;
  name: string;
  shortName: string;
  accent: string;
  accentSurface: string;
  coverImageUrl: string | null;
  systemPrompt: string;
  scratchpad: string;
  scratchpadHistory: string[];
  scratchpadUpdatePrompt: string;
  autoPinMemories: boolean;
  isDefault: boolean;
  stats: PersonalityUsageStats;
  moodIds: string[];
}

export function toPersonalityDetailVm(personality: Personality, opts: ToCardOptions = {}): PersonalityDetailVm {
  const accent = personalityAccent(personality);
  const accentSurface = personalityAccentSurface(accent);
  return {
    id: personality.id,
    name: personality.name,
    shortName: shortName(personality.name),
    accent,
    accentSurface,
    coverImageUrl: opts.coverImageUrl ?? personality.cover_image_url ?? null,
    systemPrompt: personality.system_prompt ?? '',
    scratchpad: personality.scratchpad ?? '',
    scratchpadHistory: personality.scratchpad_history ?? [],
    scratchpadUpdatePrompt: personality.scratchpad_update_prompt ?? '',
    autoPinMemories: !!personality.auto_pin_memories,
    isDefault: !!opts.defaultPersonalityId && opts.defaultPersonalityId === personality.id,
    stats: personality.stats ?? { chat_count: 0, last_used_at: null },
    moodIds: personality.mood_ids ?? [],
  };
}
