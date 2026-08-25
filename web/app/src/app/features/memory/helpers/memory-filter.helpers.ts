import { Params } from '@angular/router';

import { MemoryFilters, MemorySort } from '../../../core/models/memory.model';

export type MemoryScopeFilter = 'all' | 'user' | 'chat';
export type MemoryLevelFilter = 'all' | 'global' | 'personality' | 'thread' | 'summary';

export interface MemoryViewFilters {
  scope: MemoryScopeFilter;
  level: MemoryLevelFilter;
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
  sort: 'created_desc',
  query: '',
  personalityId: '',
  chatId: '',
  minDate: '',
  maxDate: '',
};

export function parseQueryParams(params: Params): MemoryViewFilters {
  const scope = normalizeScope(params['scope']);
  const level = normalizeLevel(params['level']);
  return {
    scope,
    level,
    sort: normalizeSort(params['sort']),
    query: String(params['query'] ?? '').trim(),
    personalityId: String(params['personality_id'] ?? '').trim(),
    chatId: String(params['chat'] ?? params['chat_id'] ?? '').trim(),
    minDate: String(params['min_date'] ?? '').trim(),
    maxDate: String(params['max_date'] ?? '').trim(),
  };
}

export function serializeFilters(filters: MemoryViewFilters): Params {
  const params: Params = {};
  if (filters.scope !== 'all') params['scope'] = filters.scope;
  if (filters.level !== 'all') params['level'] = filters.level;
  if (filters.sort !== 'created_desc') params['sort'] = filters.sort;
  if (filters.query.trim()) params['query'] = filters.query.trim();
  if (filters.personalityId.trim()) params['personality_id'] = filters.personalityId.trim();
  if (filters.chatId.trim()) params['chat'] = filters.chatId.trim();
  if (filters.minDate.trim()) params['min_date'] = filters.minDate.trim();
  if (filters.maxDate.trim()) params['max_date'] = filters.maxDate.trim();
  return params;
}

export function toApiFilters(filters: MemoryViewFilters): MemoryFilters {
  const api: MemoryFilters = {};
  if (filters.query.trim()) api.query = filters.query.trim();
  api.sort = filters.sort;
  if (filters.personalityId.trim()) api.pinned_personality_ids = [filters.personalityId.trim()];
  if (filters.chatId.trim()) api.chat_id = filters.chatId.trim();
  if (filters.minDate.trim()) api.min_date = filters.minDate.trim();
  if (filters.maxDate.trim()) api.max_date = filters.maxDate.trim();

  const resolvedLevel = resolveLevel(filters);
  if (resolvedLevel) {
    api.level = resolvedLevel;
  }
  return api;
}

function resolveLevel(filters: MemoryViewFilters): Exclude<MemoryLevelFilter, 'all'> | undefined {
  if (filters.level !== 'all') {
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

function normalizeSort(raw: unknown): MemorySort {
  if (raw === 'created_asc' || raw === 'updated_desc') return raw;
  return 'created_desc';
}
