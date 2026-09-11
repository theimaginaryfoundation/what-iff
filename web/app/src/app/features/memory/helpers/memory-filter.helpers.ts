import { Params } from '@angular/router';

import { MemoryFilters, MemorySort } from '../../../core/models/memory.model';

export type MemoryScopeFilter = 'all' | 'user' | 'chat';
export type MemoryLevelFilter = 'all' | 'global' | 'personality' | 'thread' | 'summary';
/** Active / Archived are status filters; Summaries is a dedicated view of checkpoint summaries. */
export type MemoryStatusFilter = 'active' | 'inactive' | 'summaries';

export interface MemoryViewFilters {
  scope: MemoryScopeFilter;
  level: MemoryLevelFilter;
  status: MemoryStatusFilter;
  sort: MemorySort;
  query: string;
  personalityId: string;
  chatId: string;
  minDate: string;
  maxDate: string;
}

export const DEFAULT_MEMORY_VIEW_FILTERS: MemoryViewFilters = {
  scope: 'all',
  level: 'all',
  status: 'active',
  sort: 'created_desc',
  query: '',
  personalityId: '',
  chatId: '',
  minDate: '',
  maxDate: '',
};

export function parseQueryParams(params: Params): MemoryViewFilters {
  const scope = normalizeScope(params['scope']);
  let level = normalizeLevel(params['level']);
  let status = normalizeStatus(params['status']);

  // Legacy deep links used level=summary; treat that as the Summaries status tab.
  if (status === 'summaries' || level === 'summary') {
    status = 'summaries';
    level = 'all';
  }

  const dates = normalizeDateRange(
    String(params['min_date'] ?? '').trim(),
    String(params['max_date'] ?? '').trim(),
  );

  return {
    scope,
    level,
    status,
    sort: normalizeSort(params['sort']),
    query: String(params['query'] ?? '').trim(),
    personalityId: String(params['personality_id'] ?? '').trim(),
    chatId: String(params['chat'] ?? params['chat_id'] ?? '').trim(),
    minDate: dates.minDate,
    maxDate: dates.maxDate,
  };
}

export function serializeFilters(filters: MemoryViewFilters): Params {
  const params: Params = {};
  if (filters.scope !== 'all') params['scope'] = filters.scope;
  if (filters.status === 'summaries') {
    params['status'] = 'summaries';
  } else {
    if (filters.level !== 'all' && filters.level !== 'summary') params['level'] = filters.level;
    if (filters.status !== 'active') params['status'] = filters.status;
  }
  if (filters.sort !== 'created_desc') params['sort'] = filters.sort;
  if (filters.query.trim()) params['query'] = filters.query.trim();
  if (filters.personalityId.trim()) params['personality_id'] = filters.personalityId.trim();
  if (filters.chatId.trim()) params['chat'] = filters.chatId.trim();
  const dates = normalizeDateRange(filters.minDate, filters.maxDate);
  if (dates.minDate) params['min_date'] = dates.minDate;
  if (dates.maxDate) params['max_date'] = dates.maxDate;
  return params;
}

export function toApiFilters(filters: MemoryViewFilters): MemoryFilters {
  const api: MemoryFilters = {};
  if (filters.query.trim()) api.query = filters.query.trim();
  api.sort = filters.sort;
  if (filters.personalityId.trim()) api.pinned_personality_ids = [filters.personalityId.trim()];
  if (filters.chatId.trim()) api.chat_id = filters.chatId.trim();

  const dates = normalizeDateRange(filters.minDate, filters.maxDate);
  if (dates.minDate) api.min_date = dates.minDate;
  if (dates.maxDate) api.max_date = dates.maxDate;

  if (filters.status === 'summaries') {
    api.level = 'summary';
    api.status = 'active';
    return api;
  }

  api.status = filters.status;
  const resolvedLevel = resolveLevel(filters);
  if (resolvedLevel) {
    api.level = resolvedLevel;
  }
  return api;
}

/** YYYY-MM-DD only; drops invalid values and clamps an inverted range. */
export function normalizeDateRange(
  minDate: string,
  maxDate: string,
): { minDate: string; maxDate: string; error: string | null } {
  let min = sanitizeIsoDate(minDate);
  let max = sanitizeIsoDate(maxDate);
  let error: string | null = null;

  if (minDate.trim() && !min) {
    error = 'Start date must be a valid date (YYYY-MM-DD).';
  } else if (maxDate.trim() && !max) {
    error = 'End date must be a valid date (YYYY-MM-DD).';
  }

  if (min && max && min > max) {
    max = min;
    error = 'End date can’t be before start date — adjusted To to match From.';
  }

  return { minDate: min, maxDate: max, error };
}

function sanitizeIsoDate(raw: string): string {
  const value = raw.trim();
  if (!value) return '';
  if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return '';
  const [year, month, day] = value.split('-').map(Number);
  const date = new Date(Date.UTC(year, month - 1, day));
  if (
    date.getUTCFullYear() !== year ||
    date.getUTCMonth() !== month - 1 ||
    date.getUTCDate() !== day
  ) {
    return '';
  }
  return value;
}

function resolveLevel(filters: MemoryViewFilters): Exclude<MemoryLevelFilter, 'all'> | undefined {
  if (filters.level !== 'all' && filters.level !== 'summary') {
    return filters.level;
  }
  if (filters.scope === 'chat') return 'thread';
  return undefined;
}

function normalizeScope(raw: unknown): MemoryScopeFilter {
  if (raw === 'user' || raw === 'chat') return raw;
  return 'all';
}

function normalizeLevel(raw: unknown): MemoryLevelFilter {
  if (raw === 'global' || raw === 'personality' || raw === 'thread' || raw === 'summary') return raw;
  return 'all';
}

function normalizeStatus(raw: unknown): MemoryStatusFilter {
  if (raw === 'inactive' || raw === 'summaries') return raw;
  return 'active';
}

function normalizeSort(raw: unknown): MemorySort {
  if (raw === 'created_asc' || raw === 'updated_desc') return raw;
  return 'created_desc';
}
