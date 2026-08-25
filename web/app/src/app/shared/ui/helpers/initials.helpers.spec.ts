import { initials, shortName } from './initials.helpers';

describe('initials helpers', () => {
  describe('initials', () => {
    it('returns a fallback for empty names', () => {
      expect(initials('')).toBe('?');
      expect(initials(null)).toBe('?');
      expect(initials(undefined)).toBe('?');
    });

    it('builds uppercase initials for one or more words', () => {
      expect(initials('filbolt pottsworth')).toBe('FP');
      expect(initials('Quinn')).toBe('Q');
      expect(initials('  vera  darkbloom  ')).toBe('VD');
    });

    it('handles unicode code points', () => {
      expect(initials('🦊 friend')).toBe('🦊F');
    });
  });

  describe('shortName', () => {
    it('keeps single names intact', () => {
      expect(shortName('Quinn')).toBe('Quinn');
    });

    it('shortens following names to initials', () => {
      expect(shortName('Filbolt Pottsworth Jr')).toBe('Filbolt P. J.');
    });

    it('returns an empty string for empty names', () => {
      expect(shortName('')).toBe('');
    });
  });
});
