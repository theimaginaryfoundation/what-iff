import { copySkillSlug, RITUAL_SLUG_MAX, slugifySkillName } from './ritual.model';

describe('ritual model helpers', () => {
  it('slugifies a display name', () => {
    expect(slugifySkillName('  PDF Forms! ')).toBe('pdf-forms');
  });

  it('gives a copy its own slug within the limit', () => {
    expect(copySkillSlug('pdf-forms')).toBe('pdf-forms-copy');
    expect(copySkillSlug('')).toBe('skill-copy');
    const long = copySkillSlug('a'.repeat(58) + '-bbbbb');
    expect(long.length).toBeLessThanOrEqual(RITUAL_SLUG_MAX);
    expect(long).toBe('a'.repeat(58) + '-copy');
  });
});
