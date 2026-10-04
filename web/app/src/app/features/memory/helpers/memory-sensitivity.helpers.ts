import { MEMORY_SENSITIVITIES, MemorySensitivity } from '../../../core/models/memory.model';

/** Level assumed when the backend omits one: every memory saved before sensitivity existed. */
export const DEFAULT_MEMORY_SENSITIVITY: MemorySensitivity = 'personal';

export interface MemorySensitivityOption {
  value: MemorySensitivity;
  label: string;
  /** One-line explanation, shown as a tooltip and under the pickers. */
  description: string;
}

export const MEMORY_SENSITIVITY_OPTIONS: readonly MemorySensitivityOption[] = [
  {
    value: 'public',
    label: 'Public',
    description: 'Fine to use anywhere, including in threads limited to public memories',
  },
  {
    value: 'personal',
    label: 'Personal',
    description: 'Everyday details about you; used in most threads (the default)',
  },
  {
    value: 'sensitive',
    label: 'Sensitive',
    description: 'Private or high-stakes details; only used in threads with full memory access',
  },
];

/** Narrows an unknown value (URL param, API field) to a known level, or null. */
export function normalizeSensitivity(raw: unknown): MemorySensitivity | null {
  return typeof raw === 'string' && (MEMORY_SENSITIVITIES as readonly string[]).includes(raw) ? (raw as MemorySensitivity) : null;
}

export function sensitivityLabel(sensitivity: MemorySensitivity | null | undefined): string {
  return optionFor(sensitivity).label;
}

/** Tooltip for a sensitivity badge or option. */
export function sensitivityDescription(sensitivity: MemorySensitivity | null | undefined): string {
  return optionFor(sensitivity).description;
}

function optionFor(sensitivity: MemorySensitivity | null | undefined): MemorySensitivityOption {
  const level = normalizeSensitivity(sensitivity) ?? DEFAULT_MEMORY_SENSITIVITY;
  return MEMORY_SENSITIVITY_OPTIONS.find(option => option.value === level)!;
}
