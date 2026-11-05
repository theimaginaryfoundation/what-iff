import { matchesQuery } from './panel-search.helpers';

describe('matchesQuery', () => {
  it('matches everything for an empty or whitespace query', () => {
    expect(matchesQuery('', 'Web Search')).toBe(true);
    expect(matchesQuery('   ', 'Web Search')).toBe(true);
  });

  it('ignores case in both the query and the fields', () => {
    for (const query of ['image', 'IMAGE', 'ImAgE']) {
      expect(matchesQuery(query, 'Generate Image')).toBe(true);
    }
    expect(matchesQuery('web', 'WEB SEARCH')).toBe(true);
  });

  it('matches on any field and trims the query', () => {
    expect(matchesQuery(' memory ', 'Create Memory', 'Save useful information')).toBe(true);
    expect(matchesQuery('useful', 'Create Memory', 'Save useful information')).toBe(true);
  });

  it('does not match when no field contains the query', () => {
    expect(matchesQuery('zzz', 'Create Memory', 'Save useful information')).toBe(false);
  });

  it('tolerates missing fields', () => {
    expect(matchesQuery('a', undefined, null, 'abc')).toBe(true);
    expect(matchesQuery('a', undefined, null)).toBe(false);
  });
});
