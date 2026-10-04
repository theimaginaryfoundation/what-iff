import { Chat } from '../../../core/models/chat.model';
import { MemorySensitivity } from '../../../core/models/memory.model';

/** A thread's memory limit when the backend sends none: unrestricted. */
export const DEFAULT_MEMORY_ACCESS: MemorySensitivity = 'sensitive';

export interface MemoryAccessOption {
  /** Stored as the thread's `memory_sensitivity_limit`. */
  value: MemorySensitivity;
  label: string;
  description: string;
}

/** Most to least access. A limit of L lets the thread use memories with sensitivity <= L. */
export const MEMORY_ACCESS_OPTIONS: readonly MemoryAccessOption[] = [
  {
    value: 'sensitive',
    label: 'Everything',
    description: 'Uses all memories, including ones marked Sensitive.',
  },
  {
    value: 'personal',
    label: 'Not sensitive',
    description: 'Uses memories marked Public or Personal, not Sensitive ones.',
  },
  {
    value: 'public',
    label: 'Public memories only',
    description: 'Uses only memories marked Public.',
  },
];

/**
 * What a restricted thread can't see or do beyond the filtered memories. Shared by the picker and
 * the header chip. Keep in step with the sandbox rules in docs/ARCHITECTURE_SUMMARY.md.
 */
export const RESTRICTED_WITHHELD_COPY =
  "Restricted threads also can't see the personality's shared scratchpad, other conversations, account-wide file and job lists, or its notebook (agent/ workspace files). They can't schedule jobs or run sub-agents as other personalities or with skills. Anything they write stays in this thread: memories they save are kept to it, and they can add notes about new people and places but can't change existing ones.";

export function memoryAccessOf(chat: Pick<Chat, 'memory_sensitivity_limit'> | null | undefined): MemorySensitivity {
  return chat?.memory_sensitivity_limit ?? DEFAULT_MEMORY_ACCESS;
}

/** True when the thread's limit is below Sensitive, i.e. it is sandboxed. */
export function isMemoryRestricted(limit: MemorySensitivity | null | undefined): boolean {
  return (limit ?? DEFAULT_MEMORY_ACCESS) !== DEFAULT_MEMORY_ACCESS;
}

export function memoryAccessLabel(limit: MemorySensitivity | null | undefined): string {
  const level = limit ?? DEFAULT_MEMORY_ACCESS;
  return MEMORY_ACCESS_OPTIONS.find(option => option.value === level)?.label ?? '';
}

/** Tooltip for the header indicator on a restricted thread. */
export function restrictedTooltip(limit: MemorySensitivity | null | undefined): string {
  const level = limit ?? DEFAULT_MEMORY_ACCESS;
  const scope = level === 'public' ? 'only Public memories' : 'no Sensitive memories';
  return `Restricted thread: uses ${scope}. ${RESTRICTED_WITHHELD_COPY}`;
}
