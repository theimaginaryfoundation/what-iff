import { TestBed } from '@angular/core/testing';

import { AnnouncerService } from './announcer.service';

describe('AnnouncerService', () => {
  afterEach(() => document.querySelectorAll('[data-testid="announcer"]').forEach(n => n.remove()));

  it('puts the message in one polite status region and reuses it', async () => {
    vi.useFakeTimers();
    const announcer = TestBed.inject(AnnouncerService);
    announcer.announce('Starred New Chat');
    vi.advanceTimersByTime(60);
    announcer.announce('Unstarred New Chat');
    vi.advanceTimersByTime(60);
    vi.useRealTimers();

    const regions = document.querySelectorAll('[data-testid="announcer"]');
    expect(regions).toHaveLength(1);
    expect(regions[0].getAttribute('role')).toBe('status');
    expect(regions[0].getAttribute('aria-live')).toBe('polite');
    expect(regions[0].textContent).toBe('Unstarred New Chat');
  });
});
