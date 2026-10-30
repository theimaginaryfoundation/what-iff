import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';

import { environment } from '../../../environments/environment';
import { ExternalAuthProvider, NoopExternalAuthProvider } from '../auth/external-auth.provider';
import { AuthService } from './auth.service';
import { LoggerService } from './logger.service';

const PROFILE_URL = `${environment.apiUrl}/user/profile`;

describe('AuthService session check at startup', () => {
  let http: HttpTestingController;

  /** Starts the service with a stored session and returns its pending profile request. */
  async function start() {
    TestBed.inject(AuthService);
    await new Promise(resolve => setTimeout(resolve, 0));
    return http.expectOne(PROFILE_URL);
  }

  beforeEach(() => {
    sessionStorage.setItem('access_token', 'access');
    sessionStorage.setItem('refresh_token', 'refresh');
    TestBed.configureTestingModule({
      providers: [
        provideZonelessChangeDetection(),
        provideHttpClient(),
        provideHttpClientTesting(),
        { provide: Router, useValue: { navigate: vi.fn() } },
        { provide: ExternalAuthProvider, useClass: NoopExternalAuthProvider },
        { provide: LoggerService, useValue: { error: vi.fn() } },
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    http.verify();
    sessionStorage.clear();
  });

  it('keeps the session when the browser cancels the request, as navigating away does', async () => {
    (await start()).error(new ProgressEvent('abort'), { status: 0, statusText: 'Unknown Error' });
    expect(sessionStorage.getItem('access_token')).toBe('access');
    expect(sessionStorage.getItem('refresh_token')).toBe('refresh');
    expect(TestBed.inject(AuthService).isLoggedIn()).toBe(true);
  });

  it('keeps the session on a server error', async () => {
    (await start()).flush({ message: 'down' }, { status: 503, statusText: 'Service Unavailable' });
    expect(sessionStorage.getItem('access_token')).toBe('access');
  });

  it.each([401, 403])('signs out when the server rejects the session (%i)', async status => {
    (await start()).flush({ message: 'no' }, { status, statusText: 'Rejected' });
    expect(sessionStorage.getItem('access_token')).toBeNull();
    expect(sessionStorage.getItem('refresh_token')).toBeNull();
    expect(TestBed.inject(AuthService).isLoggedIn()).toBe(false);
  });
});
