import { avatarInitials, threadAgeLabel } from './thread-picker.helpers';

describe('thread picker helpers', () => {
  const now = Date.parse('2026-09-24T12:00:00Z');
  const ago = (ms: number) => new Date(now - ms).toISOString();
  const HOUR = 3_600_000;
  const DAY = 24 * HOUR;

  it('labels thread age like the picker design', () => {
    expect(threadAgeLabel(ago(10_000), now)).toBe('Just now');
    expect(threadAgeLabel(ago(5 * 60_000), now)).toBe('5m ago');
    expect(threadAgeLabel(ago(3 * HOUR), now)).toBe('3h ago');
    expect(threadAgeLabel(ago(DAY), now)).toBe('Yesterday');
    expect(threadAgeLabel(ago(3 * DAY), now)).toBe('3 days ago');
    expect(threadAgeLabel(ago(7 * DAY), now)).toBe('1 week ago');
    expect(threadAgeLabel(ago(14 * DAY), now)).toBe('2 weeks ago');
    expect(threadAgeLabel(ago(40 * DAY), now)).toBe('1 month ago');
    expect(threadAgeLabel(ago(800 * DAY), now)).toBe('2 years ago');
  });

  it('returns an empty label for missing or invalid dates', () => {
    expect(threadAgeLabel(undefined, now)).toBe('');
    expect(threadAgeLabel('not a date', now)).toBe('');
  });

  it('derives avatar initials', () => {
    expect(avatarInitials('Lola Tarsier')).toBe('LT');
    expect(avatarInitials('Punkapple')).toBe('PU');
    expect(avatarInitials('   ')).toBe('?');
  });
});
