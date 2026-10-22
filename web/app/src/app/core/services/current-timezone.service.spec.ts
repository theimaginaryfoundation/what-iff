import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

import { environment } from '../../../environments/environment';
import { UserResponse } from '../models/user.model';
import { CurrentTimezoneService } from './current-timezone.service';

describe('CurrentTimezoneService', () => {
  const url = `${environment.apiUrl}/user/current-timezone`;
  let service: CurrentTimezoneService;
  let http: HttpTestingController;
  let browserZone: string;

  const user = (current_timezone?: string): UserResponse => ({
    id: 'u1',
    username: 'gori',
    email: 'gori@example.com',
    current_timezone,
    created_at: '',
    updated_at: '',
  });

  const becomeVisible = () => document.dispatchEvent(new Event('visibilitychange'));

  beforeEach(() => {
    browserZone = 'America/Los_Angeles';
    const real = Intl.DateTimeFormat.prototype.resolvedOptions;
    vi.spyOn(Intl.DateTimeFormat.prototype, 'resolvedOptions').mockImplementation(function (this: Intl.DateTimeFormat) {
      return { ...real.call(this), timeZone: browserZone };
    });
    TestBed.configureTestingModule({ providers: [provideHttpClient(), provideHttpClientTesting()] });
    service = TestBed.inject(CurrentTimezoneService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    service.stop();
    http.verify();
    vi.restoreAllMocks();
  });

  it('reports the browser timezone when the API has a different one', () => {
    service.start(user('America/New_York'));

    const req = http.expectOne(url);
    expect(req.request.method).toBe('PUT');
    expect(req.request.body).toEqual({ timezone: 'America/Los_Angeles' });
    req.flush(null);
  });

  it('stays quiet when the API already has the browser timezone', () => {
    service.start(user('America/Los_Angeles'));
    http.expectNone(url);
  });

  it('reports again when the tab comes back in another timezone, and only then', () => {
    service.start(user('America/Los_Angeles'));

    becomeVisible();
    http.expectNone(url);

    browserZone = 'America/New_York';
    becomeVisible();
    http.expectOne(url).flush(null);

    becomeVisible();
    http.expectNone(url);
  });

  it('retries after a failed report', () => {
    service.start(user());
    http.expectOne(url).flush('nope', { status: 500, statusText: 'Server Error' });

    becomeVisible();
    http.expectOne(url).flush(null);
  });

  it('does nothing after sign-out or when the browser zone is unknown', () => {
    browserZone = '';
    service.start(user('America/New_York'));
    http.expectNone(url);

    browserZone = 'Europe/London';
    service.stop();
    becomeVisible();
    http.expectNone(url);
  });
});
