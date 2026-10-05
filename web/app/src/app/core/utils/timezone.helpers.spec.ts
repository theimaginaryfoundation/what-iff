import { FormControl } from '@angular/forms';

import {
  detectBrowserTimezone,
  isValidTimezone,
  listTimezones,
  resolveDefaultTimezone,
  timezoneValidator,
} from './timezone.helpers';

describe('timezone helpers', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('lists sorted IANA zones including UTC', () => {
    const zones = listTimezones();
    expect(zones).toContain('UTC');
    expect(zones).toContain('America/New_York');
    expect(zones).toEqual([...zones].sort());
  });

  it('detects the browser timezone', () => {
    vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({ timeZone: 'Europe/Berlin' } as Intl.ResolvedDateTimeFormatOptions);
    expect(detectBrowserTimezone()).toBe('Europe/Berlin');
  });

  describe('resolveDefaultTimezone', () => {
    it('prefers the saved timezone', () => {
      expect(resolveDefaultTimezone('  Asia/Tokyo ')).toBe('Asia/Tokyo');
    });

    it('falls back to the browser timezone when nothing is saved', () => {
      vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({ timeZone: 'Europe/Berlin' } as Intl.ResolvedDateTimeFormatOptions);
      expect(resolveDefaultTimezone(undefined)).toBe('Europe/Berlin');
      expect(resolveDefaultTimezone('   ')).toBe('Europe/Berlin');
    });

    it('falls back to UTC when the browser reports nothing', () => {
      vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockReturnValue({ timeZone: undefined } as unknown as Intl.ResolvedDateTimeFormatOptions);
      expect(resolveDefaultTimezone(null)).toBe('UTC');
    });
  });

  it('validates timezone names', () => {
    expect(isValidTimezone('America/New_York')).toBe(true);
    expect(isValidTimezone('UTC')).toBe(true);
    expect(isValidTimezone('Mars/Olympus_Mons')).toBe(false);
  });

  it('timezoneValidator allows empty and rejects unknown zones', () => {
    expect(timezoneValidator(new FormControl(''))).toBeNull();
    expect(timezoneValidator(new FormControl('Europe/London'))).toBeNull();
    expect(timezoneValidator(new FormControl('Nope/Nowhere'))).toEqual({ timezone: true });
  });
});
