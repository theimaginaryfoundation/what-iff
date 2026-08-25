import { Memory } from '../../../core/models/memory.model';

export interface MemoryCardVm {
  id: string;
  content: string;
  excerpt: string;
  level: Memory['level'];
  levelLabel: string;
  chatName: string | null;
  pinnedPersonalityId: string | null;
  pinnedPersonalityName: string | null;
  confidence: Memory['confidence'];
  verifiedCount: number | null;
  createdAt: string;
  updatedAt: string;
}

export function toMemoryCardVm(
  memory: Memory,
  excerptLength: number = 160,
  personalityNames: Record<string, string> = {},
): MemoryCardVm {
  const pinnedPersonalityId = memory.pinned_personality_id ?? null;
  const verifiedCount = memory.chain_metadata?.duplicate_count && memory.chain_metadata.duplicate_count > 1
    ? memory.chain_metadata.duplicate_count
    : null;
  return {
    id: memory.id,
    content: memory.content,
    excerpt: excerpt(memory.content, excerptLength),
    level: memory.level,
    levelLabel: levelBadgeText(memory.level),
    chatName: memory.chat_name ?? null,
    pinnedPersonalityId,
    pinnedPersonalityName: pinnedPersonalityId ? (personalityNames[pinnedPersonalityId] ?? null) : null,
    confidence: memory.confidence,
    verifiedCount,
    createdAt: memory.created_at,
    updatedAt: memory.updated_at,
  };
}

/** User-scoped memories (global or personality level) can be pinned to one personality. */
export function isUserScopedMemoryLevel(level: Memory['level']): boolean {
  return level === 'global' || level === 'personality';
}

export function levelBadgeText(level: Memory['level']): string {
  switch (level) {
    case 'global':
      return 'User';
    case 'personality':
      return 'Personality';
    case 'thread':
      return 'Chat';
    case 'summary':
      return 'Summary';
    default:
      return level;
  }
}

export function excerpt(content: string, len: number): string {
  const normalized = content.trim();
  if (normalized.length <= len) {
    return normalized;
  }
  return `${normalized.slice(0, len - 1).trimEnd()}…`;
}
