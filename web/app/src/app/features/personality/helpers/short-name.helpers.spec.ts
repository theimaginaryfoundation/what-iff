import { shortName } from './short-name.helpers';

describe('shortName', () => {
  it('returns an empty string for empty input', () => {
    expect(shortName('')).toBe('');
    expect(shortName('   ')).toBe('');
  });

  it('returns single-word names unchanged', () => {
    expect(shortName('Filbolt')).toBe('Filbolt');
  });

  it('abbreviates two-word names to first plus initial', () => {
    expect(shortName('Filbolt Pottsworth')).toBe('Filbolt P.');
  });

  it('abbreviates names with three or more words', () => {
    expect(shortName('Hugo The Magnificent')).toBe('Hugo T. M.');
  });

  it('handles unicode names correctly', () => {
    expect(shortName('Élise Côté')).toBe('Élise C.');
  });

  it('collapses excess whitespace between words', () => {
    expect(shortName('  Filbolt   Pottsworth  ')).toBe('Filbolt P.');
  });
});
