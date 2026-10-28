import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it } from 'vitest';

import { environment } from '../../../environments/environment';
import { CurrentTimezoneService } from '../../core/services/current-timezone.service';
import { TimezonePausedNoticeComponent } from './timezone-paused-notice.component';

describe('TimezonePausedNoticeComponent', () => {
  let timezone: CurrentTimezoneService;
  let http: HttpTestingController;

  const pause = (at: string) =>
    timezone.start({ id: 'u', username: 'u', email: 'u@example.com', created_at: '', updated_at: '', timezone_following_paused_at: at });

  const render = async () => {
    const fixture = TestBed.createComponent(TimezonePausedNoticeComponent);
    fixture.detectChanges();
    await fixture.whenStable();
    return fixture;
  };

  beforeEach(() => {
    try {
      localStorage.clear();
    } catch {
      // ignore
    }
    TestBed.configureTestingModule({
      imports: [TimezonePausedNoticeComponent],
      providers: [provideZonelessChangeDetection(), provideHttpClient(), provideHttpClientTesting()],
    });
    timezone = TestBed.inject(CurrentTimezoneService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => timezone.stop());

  it('stays hidden while following is on', async () => {
    const fixture = await render();
    expect((fixture.nativeElement as HTMLElement).querySelector('[data-testid="timezone-paused-notice"]')).toBeNull();
  });

  it('shows while paused and resumes on request', async () => {
    pause('2026-10-28T12:00:00Z');
    const fixture = await render();
    const host = fixture.nativeElement as HTMLElement;
    expect(host.querySelector('[data-testid="timezone-paused-notice"]')).not.toBeNull();

    fixture.componentInstance.resume();
    http.expectOne(`${environment.apiUrl}/user/current-timezone/resume`).flush(null);
    http.match(`${environment.apiUrl}/user/current-timezone`).forEach(req => req.flush({}));
    fixture.detectChanges();
    await fixture.whenStable();
    expect(host.querySelector('[data-testid="timezone-paused-notice"]')).toBeNull();
  });

  it('dismisses for this pause only', async () => {
    pause('2026-10-28T12:00:00Z');
    const fixture = await render();
    const host = fixture.nativeElement as HTMLElement;
    fixture.componentInstance.dismiss();
    fixture.detectChanges();
    await fixture.whenStable();
    expect(host.querySelector('[data-testid="timezone-paused-notice"]')).toBeNull();

    // A later pause shows again.
    pause('2026-10-30T09:00:00Z');
    fixture.detectChanges();
    await fixture.whenStable();
    expect(host.querySelector('[data-testid="timezone-paused-notice"]')).not.toBeNull();
  });
});
