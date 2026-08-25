import { Params } from '@angular/router';

import { RitualFilters, RitualSort } from '../../../core/models/ritual.model';

export type RitualHotkeyFilter = 'all' | 'with' | 'without';

export interface RitualViewFilters {
  query: string;
  personalityId: string;
  personalityIds: string[];
  globalOnly: boolean;
  hasHotkey: RitualHotkeyFilter;
  sort: RitualSort;
  minDate: string;
  maxDate: string;
}

export const DEFAULT_RITUAL_VIEW_FILTERS: RitualViewFilters = {
  query: '',
  personalityId: '',
  personalityIds: [],
  globalOnly: false,
  hasHotkey: 'all',
  sort: 'name_asc',
  minDate: '',
  maxDate: '',
};

export function parseQueryParams(params: Params): RitualViewFilters {
  const personalityIds = normalizePersonalityIds(params['personality_ids']);
  const legacyPersonalityId = String(params['personality_id'] ?? '').trim();
  const globalOnly = normalizeBoolean(params['global_only']);
  return {
    query: String(params['query'] ?? '').trim(),
    personalityId: legacyPersonalityId,
    personalityIds: personalityIds.length > 0 ? personalityIds : legacyPersonalityId ? [legacyPersonalityId] : [],
    globalOnly,
    hasHotkey: normalizeHotkeyFilter(params['has_hotkey']),
    sort: normalizeSort(params['sort']),
    minDate: String(params['min_date'] ?? '').trim(),
    maxDate: String(params['max_date'] ?? '').trim(),
  };
}

export function serializeFilters(filters: RitualViewFilters): Params {
  const params: Params = {};
  if (filters.query.trim()) params['query'] = filters.query.trim();
  if (filters.globalOnly) {
    params['global_only'] = 'true';
  } else {
    if (filters.personalityIds.length > 0) params['personality_ids'] = filters.personalityIds;
    if (filters.personalityIds.length === 0 && filters.personalityId.trim()) {
      params['personality_id'] = filters.personalityId.trim();
    }
  }
  if (filters.hasHotkey !== 'all') params['has_hotkey'] = filters.hasHotkey;
  if (filters.sort !== 'name_asc') params['sort'] = filters.sort;
  if (filters.minDate.trim()) params['min_date'] = filters.minDate.trim();
  if (filters.maxDate.trim()) params['max_date'] = filters.maxDate.trim();
  return params;
}

export function toApiFilters(filters: RitualViewFilters): RitualFilters {
  const api: RitualFilters = {};
  if (filters.query.trim()) {
    api.search = filters.query.trim();
  }

  const personalityIds = filters.personalityIds.length
    ? filters.personalityIds
    : filters.personalityId.trim()
      ? [filters.personalityId.trim()]
      : [];

  if (filters.globalOnly) {
    api.global_only = true;
  } else if (personalityIds.length > 0) {
    api.personality_ids = personalityIds;
  }
  if (filters.hasHotkey === 'with') {
    api.has_hotkeys = true;
  }
  if (filters.hasHotkey === 'without') {
    api.has_hotkeys = false;
  }
  if (filters.sort) {
    api.sort = filters.sort;
  }
  if (filters.minDate.trim()) {
    api.min_date = filters.minDate.trim();
  }
  if (filters.maxDate.trim()) {
    api.max_date = filters.maxDate.trim();
  }
  return api;
}

function normalizeHotkeyFilter(raw: unknown): RitualHotkeyFilter {
  if (raw === 'with' || raw === 'without') return raw;
  return 'all';
}

function normalizeSort(raw: unknown): RitualSort {
  if (raw === 'name_asc' || raw === 'created_desc' || raw === 'updated_desc') return raw;
  return 'name_asc';
}

function normalizeBoolean(raw: unknown): boolean {
  const value = String(raw ?? '').trim().toLowerCase();
  return value === 'true' || value === '1';
}

function normalizePersonalityIds(raw: unknown): string[] {
  if (Array.isArray(raw)) {
    return raw
      .map(value => String(value ?? '').trim())
      .filter(value => value.length > 0);
  }

  const value = String(raw ?? '').trim();
  if (!value) return [];
  return value
    .split(',')
    .map(item => item.trim())
    .filter(item => item.length > 0);
}
