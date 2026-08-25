import {
  DEFAULT_RITUAL_VIEW_FILTERS,
  parseQueryParams,
  serializeFilters,
  toApiFilters,
} from './ritual-filter.helpers';

describe('ritual-filter.helpers', () => {
  it('parses query params into ritual filters', () => {
    const parsed = parseQueryParams({
      query: ' summarize ',
      personality_ids: ['p-1', 'p-2'],
      global_only: 'false',
      has_hotkey: 'with',
      sort: 'updated_desc',
      min_date: '2026-01-01',
      max_date: '2026-01-31',
    });

    expect(parsed).toEqual({
      query: 'summarize',
      personalityId: '',
      personalityIds: ['p-1', 'p-2'],
      globalOnly: false,
      hasHotkey: 'with',
      sort: 'updated_desc',
      minDate: '2026-01-01',
      maxDate: '2026-01-31',
    });
  });

  it('falls back to defaults for invalid values', () => {
    const parsed = parseQueryParams({ has_hotkey: 'invalid' });
    expect(parsed).toEqual(DEFAULT_RITUAL_VIEW_FILTERS);
  });

  it('serializes non-default filters into query params', () => {
    const params = serializeFilters({
      query: 'plan',
      personalityId: '',
      personalityIds: ['p-2', 'p-3'],
      globalOnly: false,
      hasHotkey: 'without',
      sort: 'created_desc',
      minDate: '',
      maxDate: '2026-02-01',
    });

    expect(params).toEqual({
      query: 'plan',
      personality_ids: ['p-2', 'p-3'],
      has_hotkey: 'without',
      sort: 'created_desc',
      max_date: '2026-02-01',
    });
  });

  it('maps view filters to API filters', () => {
    const api = toApiFilters({
      query: 'daily standup',
      personalityId: '',
      personalityIds: ['persona-1', 'persona-2'],
      globalOnly: false,
      hasHotkey: 'with',
      sort: 'name_asc',
      minDate: '2026-01-01',
      maxDate: '2026-01-15',
    });

    expect(api).toEqual({
      search: 'daily standup',
      personality_ids: ['persona-1', 'persona-2'],
      has_hotkeys: true,
      sort: 'name_asc',
      min_date: '2026-01-01',
      max_date: '2026-01-15',
    });
  });
});
