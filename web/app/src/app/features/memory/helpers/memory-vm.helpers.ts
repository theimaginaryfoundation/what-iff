import { Memory, MemoryMergeType } from '../../../core/models/memory.model';

/** Label for memories with no pinned personality, used in filters, move menus and pickers. */
export const GLOBAL_SCOPE_LABEL = 'Global (every personality)';

/** Tooltip for a memory's confidence (merging keeps the highest; it doesn't filter retrieval). */
export const CONFIDENCE_HINT = 'How sure the personality was when saving it; merges keep the highest';

/** Tooltip for the "N×" verified count (chain_metadata.duplicate_count). */
export const VERIFIED_HINT = 'Times this fact was saved again independently and merged in';

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

/** Tooltip for a memory level badge: which threads the memory is retrieved in. */
export function levelDescription(level: Memory['level']): string {
  switch (level) {
    case 'global':
      return 'Used in threads with every personality';
    case 'personality':
      return 'Used only in threads with this personality';
    case 'thread':
      return 'Used only in the thread it came from';
    case 'summary':
      return 'Checkpoint summary of a thread';
    default:
      return '';
  }
}

/** Shared label for a merge event type (merge history, compaction log and focus panel). */
export function mergeTypeLabel(type: MemoryMergeType): string {
  switch (type) {
    case 'fold_live':
      return 'Memories merged';
    case 'link':
      return 'Linked related memories';
    default:
      return 'Created from batch';
  }
}

/** Tooltip explaining what a merge event type did. */
export function mergeTypeDescription(type: MemoryMergeType): string {
  switch (type) {
    case 'fold_live':
      return 'Duplicates folded into one memory; the others were archived';
    case 'link':
      return 'Related memories tagged as a group; nothing was removed';
    default:
      return 'New memory saved from a checkpoint extraction';
  }
}

/** Tooltip for the Undo button on a merge event, per type (see datastore undo semantics). */
export function mergeUndoDescription(type: MemoryMergeType): string {
  switch (type) {
    case 'fold_live':
      return 'Restores confidence and verified count; merged duplicates stay archived';
    case 'link':
      return 'Removes the link tag from these memories';
    default:
      return 'Permanently deletes the memory this event created';
  }
}

/**
 * Plain-language label for a raw backend memory scope ("user" / "chat"). A user-scope memory is
 * Global unless its personality auto-pins memories, so the label names both possibilities.
 */
export function memoryScopeLabel(scope: string | null | undefined): string {
  switch (scope) {
    case 'user':
      return 'Global or personality';
    case 'chat':
      return 'Thread';
    default:
      return scope ?? '';
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
