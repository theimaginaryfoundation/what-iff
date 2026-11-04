import { provideZonelessChangeDetection } from '@angular/core';
import { HttpClient, HttpErrorResponse, provideHttpClient, withInterceptors } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { firstValueFrom, of, throwError } from 'rxjs';

import { environment } from '../../../environments/environment';
import { ExternalAuthProvider, NoopExternalAuthProvider } from '../auth/external-auth.provider';
import { AuthService } from '../services/auth.service';
import { LoggerService } from '../services/logger.service';
import { authInterceptor } from './auth.interceptor';

const API = environment.apiUrl;

/** The interceptor attaches the token asynchronously, so a request reaches the backend a tick later. */
const tick = () => new Promise(resolve => setTimeout(resolve, 0));

function configure(providers: unknown[]): void {
  TestBed.configureTestingModule({
    providers: [
      provideZonelessChangeDetection(),
      provideHttpClient(withInterceptors([authInterceptor])),
      provideHttpClientTesting(),
      { provide: ExternalAuthProvider, useClass: NoopExternalAuthProvider },
      ...(providers as never[]),
    ],
  });
}

describe('authInterceptor', () => {
  let token: string | null;
  let auth: {
    getAccessToken: ReturnType<typeof vi.fn>;
    hasExternalSession: ReturnType<typeof vi.fn>;
    refreshToken: ReturnType<typeof vi.fn>;
    logout: ReturnType<typeof vi.fn>;
  };
  let http: HttpClient;
  let backend: HttpTestingController;

  beforeEach(() => {
    token = 'old';
    auth = {
      getAccessToken: vi.fn(() => token),
      hasExternalSession: vi.fn(() => false),
      refreshToken: vi.fn(() => {
        token = 'new';
        return of({ access_token: 'new' });
      }),
      logout: vi.fn(),
    };
    configure([{ provide: AuthService, useValue: auth }]);
    http = TestBed.inject(HttpClient);
    backend = TestBed.inject(HttpTestingController);
  });

  afterEach(() => backend.verify());

  it('sends the stored token, except to login and register', async () => {
    void firstValueFrom(http.get(`${API}/chats`));
    void firstValueFrom(http.post(`${API}/user/login`, {}));
    await tick();
    const chats = backend.expectOne(`${API}/chats`);
    const login = backend.expectOne(`${API}/user/login`);
    expect(chats.request.headers.get('Authorization')).toBe('Bearer old');
    expect(login.request.headers.has('Authorization')).toBe(false);
    chats.flush([]);
    login.flush({});
  });

  it('refreshes on a 401 and retries the request with the new token', async () => {
    const result = firstValueFrom(http.get(`${API}/chats`));
    await tick();
    backend.expectOne(`${API}/chats`).flush({}, { status: 401, statusText: 'Unauthorized' });
    const retry = backend.expectOne(`${API}/chats`);
    expect(retry.request.headers.get('Authorization')).toBe('Bearer new');
    retry.flush(['ok']);

    await expect(result).resolves.toEqual(['ok']);
    expect(auth.refreshToken).toHaveBeenCalledTimes(1);
    expect(auth.logout).not.toHaveBeenCalled();
  });

  it('signs out and passes the 401 on when the refresh fails', async () => {
    auth.refreshToken.mockReturnValue(throwError(() => new Error('refresh rejected')));
    const result = firstValueFrom(http.get(`${API}/chats`));
    await tick();
    backend.expectOne(`${API}/chats`).flush({}, { status: 401, statusText: 'Unauthorized' });

    const error = await result.catch((e: unknown) => e);
    expect(error).toBeInstanceOf(HttpErrorResponse);
    expect((error as HttpErrorResponse).status).toBe(401);
    expect(auth.logout).toHaveBeenCalledTimes(1);
  });

  it.each(['/user/login', '/user/register', '/user/refresh'])('does not refresh after a 401 from %s', async path => {
    const result = firstValueFrom(http.post(`${API}${path}`, {}));
    await tick();
    backend.expectOne(`${API}${path}`).flush({}, { status: 401, statusText: 'Unauthorized' });

    await expect(result).rejects.toBeInstanceOf(HttpErrorResponse);
    expect(auth.refreshToken).not.toHaveBeenCalled();
    expect(auth.logout).not.toHaveBeenCalled();
  });

  it('passes other errors on without refreshing or signing out', async () => {
    const result = firstValueFrom(http.get(`${API}/chats`));
    await tick();
    backend.expectOne(`${API}/chats`).flush({}, { status: 503, statusText: 'Service Unavailable' });

    const error = await result.catch((e: unknown) => e);
    expect((error as HttpErrorResponse).status).toBe(503);
    expect(auth.refreshToken).not.toHaveBeenCalled();
    expect(auth.logout).not.toHaveBeenCalled();
  });
});

describe('authInterceptor with AuthService, an expired session at startup', () => {
  let backend: HttpTestingController;
  let router: { navigate: ReturnType<typeof vi.fn> };

  beforeEach(() => {
    sessionStorage.setItem('access_token', 'expired');
    sessionStorage.setItem('refresh_token', 'refresh');
    router = { navigate: vi.fn() };
    configure([
      { provide: Router, useValue: router },
      { provide: LoggerService, useValue: { error: vi.fn() } },
    ]);
    backend = TestBed.inject(HttpTestingController);
  });

  afterEach(() => {
    backend.verify();
    sessionStorage.clear();
  });

  it('refreshes, and keeps the session when the refresh succeeds', async () => {
    const auth = TestBed.inject(AuthService);
    await tick();
    await tick();
    backend.expectOne(`${API}/user/profile`).flush({}, { status: 401, statusText: 'Unauthorized' });
    await tick();
    backend.expectOne(`${API}/user/refresh`).flush({ access_token: 'fresh', refresh_token: 'refresh-2' });
    await tick();
    const retry = backend.expectOne(`${API}/user/profile`);
    expect(retry.request.headers.get('Authorization')).toBe('Bearer fresh');
    retry.flush({ id: 'u1', username: 'gori' });

    expect(sessionStorage.getItem('access_token')).toBe('fresh');
    expect(auth.isLoggedIn()).toBe(true);
  });

  it('signs out when the refresh is rejected too', async () => {
    const auth = TestBed.inject(AuthService);
    await tick();
    await tick();
    backend.expectOne(`${API}/user/profile`).flush({}, { status: 401, statusText: 'Unauthorized' });
    await tick();
    backend.expectOne(`${API}/user/refresh`).flush({}, { status: 401, statusText: 'Unauthorized' });
    await tick();

    expect(sessionStorage.getItem('access_token')).toBeNull();
    expect(sessionStorage.getItem('refresh_token')).toBeNull();
    expect(auth.isLoggedIn()).toBe(false);
    expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);
  });
});
