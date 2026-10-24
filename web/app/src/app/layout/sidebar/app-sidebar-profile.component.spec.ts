import {
  PROFILE_EMAIL_DISPLAY_MAX,
  profileFullName,
  profileShortName,
  truncateEmailForDisplay,
} from './app-sidebar-profile.component';

describe('sidebar profile names', () => {
  const base = { username: 'jdoe', email: 'jane@example.com' };

  describe('truncateEmailForDisplay', () => {
    it('leaves short emails alone', () => {
      expect(truncateEmailForDisplay('jane@example.com')).toBe('jane@example.com');
    });

    it('keeps the domain and ellipsizes the local-part when over the max', () => {
      const email = 'thisisareallyreallylongemailname@domain.com';
      const shown = truncateEmailForDisplay(email);
      // 24 total: 12 local + … + @domain.com
      expect(shown).toBe('thisisareall…@domain.com');
      expect(shown.length).toBe(PROFILE_EMAIL_DISPLAY_MAX);
      expect(shown.endsWith('@domain.com')).toBe(true);
      expect(shown).not.toBe(email);
    });

    it('head-truncates when the domain alone cannot fit', () => {
      const email = 'a@' + 'd'.repeat(30) + '.com';
      const shown = truncateEmailForDisplay(email);
      expect(shown.endsWith('…')).toBe(true);
      expect(shown.length).toBe(PROFILE_EMAIL_DISPLAY_MAX);
    });
  });

  describe('profileShortName', () => {
    it.each([
      ['first name and last initial', { ...base, first_name: 'Jane', last_name: 'doe' }, 'Jane D.'],
      ['trims whitespace around both names', { ...base, first_name: '  Jane ', last_name: ' Doe ' }, 'Jane D.'],
      ['first name only when there is no last name', { ...base, first_name: 'Jane' }, 'Jane'],
      ['username when there is no first name', { ...base, last_name: 'Doe' }, 'jdoe'],
      ['email when there is no username', { username: '', email: 'jane@example.com' }, 'jane@example.com'],
      [
        'truncated email when there is no username and the address is long',
        { username: '', email: 'thisisareallyreallylongemailname@domain.com' },
        'thisisareall…@domain.com',
      ],
      ['email when username is only whitespace', { username: '   ', email: 'a@b.co' }, 'a@b.co'],
      ['Guest when nothing is set', { username: '', email: '' }, 'Guest'],
    ])('uses the %s', (_label, user, expected) => {
      expect(profileShortName(user)).toBe(expected);
    });

    it('returns Guest when signed out', () => {
      expect(profileShortName(null)).toBe('Guest');
    });
  });

  describe('profileFullName', () => {
    it.each([
      ['first and last name', { ...base, first_name: 'Jane', last_name: 'Doe' }, 'Jane Doe'],
      ['last name alone', { ...base, last_name: 'Doe' }, 'Doe'],
      ['username when no name is set', base, 'jdoe'],
      [
        'full untruncated email when there is no username',
        { username: '', email: 'thisisareallyreallylongemailname@domain.com' },
        'thisisareallyreallylongemailname@domain.com',
      ],
      ['Guest when nothing is set', { username: '', email: '' }, 'Guest'],
    ])('uses the %s', (_label, user, expected) => {
      expect(profileFullName(user)).toBe(expected);
    });

    it('returns Guest when signed out', () => {
      expect(profileFullName(null)).toBe('Guest');
    });
  });
});
