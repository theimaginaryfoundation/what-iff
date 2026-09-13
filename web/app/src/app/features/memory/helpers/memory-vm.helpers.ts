import { Memory } from '../../../core/models/memory.model';

export interface MemoryCardVm {
  id: string;
  content: string;
  excerpt: string;
  level: Memory['level'];
  levelLabel: string;
  status: Memory['status'];
  starred: boolean;
  chatName: string | null;
  chatId: string | null;
  pinnedPersonalityId: string | null;
  pinnedPersonalityName: string | null;
  confidence: Memory['confidence'];
  confidencePercent: number;
  confidenceLabel: 'Low' | 'Medium' | 'High';
  verifiedCount: number | null;
  mergedFromIds: string[];
  chainMetadata: Memory['chain_metadata'];
  createdAt: string;
  updatedAt: string;
}

export function toMemoryCardVm(
  memory: Memory,
  excerptLength: number = 220,
  personalityNames: Record<string, string> = {},
): MemoryCardVm {
  const pinnedPersonalityId = memory.pinned_personality_id ?? null;
  const verifiedCount = memory.chain_metadata?.duplicate_count && memory.chain_metadata.duplicate_count > 1
    ? memory.chain_metadata.duplicate_count
    : null;
  const confidencePercent = Math.round(Math.max(0, Math.min(1, memory.confidence)) * 100);
  return {
    id: memory.id,
    content: memory.content,
    excerpt: excerpt(memory.content, excerptLength),
    level: memory.level,
    levelLabel: levelBadgeText(memory.level),
    status: memory.status,
    starred: memory.starred,
    chatName: memory.chat_name ?? null,
    chatId: memory.chat_id ?? null,
    pinnedPersonalityId,
    pinnedPersonalityName: pinnedPersonalityId ? (personalityNames[pinnedPersonalityId] ?? null) : null,
    confidence: memory.confidence,
    confidencePercent,
    confidenceLabel: confidenceBucketLabel(memory.confidence),
    verifiedCount,
    mergedFromIds: memory.chain_metadata?.merged_from_memory_ids ?? [],
    chainMetadata: memory.chain_metadata ?? null,
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
      return 'Global';
    case 'personality':
      return 'Personality';
    case 'thread':
      return 'Thread';
    case 'summary':
      return 'Summary';
    default:
      return level;
  }
}

export function confidenceBucketLabel(confidence: number): 'Low' | 'Medium' | 'High' {
  if (confidence >= 0.75) return 'High';
  if (confidence >= 0.45) return 'Medium';
  return 'Low';
}

export function excerpt(content: string, len: number): string {
  const normalized = content.trim();
  if (normalized.length <= len) {
    return normalized;
  }
  return `${normalized.slice(0, len - 1).trimEnd()}…`;
}

export function associationLabel(vm: MemoryCardVm): string {
  if (vm.pinnedPersonalityName) return vm.pinnedPersonalityName;
  if (vm.chatName) return vm.chatName;
  return vm.levelLabel;
}
