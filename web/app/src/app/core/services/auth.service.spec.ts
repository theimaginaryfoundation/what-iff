import type { MockedObject } from 'vitest';
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { provideHttpClient, withXhr } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { Router } from '@angular/router';
import { environment } from '@environments/environment';

import { AuthService } from './auth.service';
import { LoggerService } from './logger.service';
import { ThemeService } from './theme.service';
import { ExternalAuthProvider } from '../auth/external-auth.provider';

// Cognito flows chain several `await`s (signIn/signUp, fetchSession, then an
// HTTP call) before the next request is issued. A macrotask tick reliably
// drains all of those microtasks, unlike a fixed count of
// `await Promise.resolve()` which under/over-shoots as the chain grows.
function tick(): Promise<void> {
    return new Promise((resolve) => setTimeout(resolve, 0));
}

describe('AuthService', () => {
    let service: AuthService;
    let httpMock: HttpTestingController;
    let router: { navigate: ReturnType<typeof vi.fn> };
    let logger: Pick<MockedObject<LoggerService>, 'error'>;
    let themeService: Pick<MockedObject<ThemeService>, 'setTheme'>;
    let externalAuth: MockedObject<ExternalAuthProvider>;

    function makeExternalAuth(available: boolean): MockedObject<ExternalAuthProvider> {
        return {
            available,
            configure: vi.fn().mockName('ExternalAuthProvider.configure'),
            fetchSession: vi.fn().mockName('ExternalAuthProvider.fetchSession'),
            signUp: vi.fn().mockName('ExternalAuthProvider.signUp'),
            signIn: vi.fn().mockName('ExternalAuthProvider.signIn'),
            completeNewPassword: vi.fn().mockName('ExternalAuthProvider.completeNewPassword'),
            confirmSignUp: vi.fn().mockName('ExternalAuthProvider.confirmSignUp'),
            resendSignUpCode: vi.fn().mockName('ExternalAuthProvider.resendSignUpCode'),
            requestPasswordReset: vi.fn().mockName('ExternalAuthProvider.requestPasswordReset'),
            confirmPasswordReset: vi.fn().mockName('ExternalAuthProvider.confirmPasswordReset'),
            updatePassword: vi.fn().mockName('ExternalAuthProvider.updatePassword'),
            updateProfileAttributes: vi.fn().mockName('ExternalAuthProvider.updateProfileAttributes'),
            signOut: vi.fn().mockName('ExternalAuthProvider.signOut'),
        } as unknown as MockedObject<ExternalAuthProvider>;
    }

    function configureTestBed(cognitoAvailable = false): void {
        externalAuth = makeExternalAuth(cognitoAvailable);

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                provideHttpClient(withXhr()),
                provideHttpClientTesting(),
                AuthService,
                { provide: Router, useValue: router },
                { provide: LoggerService, useValue: logger },
                { provide: ThemeService, useValue: themeService },
                { provide: ExternalAuthProvider, useValue: externalAuth },
            ],
        });

        service = TestBed.inject(AuthService);
        httpMock = TestBed.inject(HttpTestingController);
    }

    beforeEach(() => {
        sessionStorage.clear();
        localStorage.clear();

        router = { navigate: vi.fn().mockName('Router.navigate') };
        logger = { error: vi.fn().mockName('LoggerService.error') } as unknown as Pick<MockedObject<LoggerService>, 'error'>;
        themeService = { setTheme: vi.fn().mockName('ThemeService.setTheme') } as unknown as Pick<MockedObject<ThemeService>, 'setTheme'>;
    });

    afterEach(() => {
        httpMock?.verify();
        sessionStorage.clear();
        localStorage.clear();
    });

    describe('token accessors', () => {
        it('returns null when no tokens are stored', () => {
            configureTestBed();
            expect(service.getAccessToken()).toBeNull();
            expect(service.getRefreshToken()).toBeNull();
        });

        it('reads tokens from sessionStorage', () => {
            // Set the tokens after construction, not before: an access_token
            // present at construction time makes initializeAuthState() schedule
            // a deferred (setTimeout) profile fetch that this test doesn't care
            // about and doesn't flush, which would otherwise leak an unflushed
            // HTTP request into a later test.
            configureTestBed();
            sessionStorage.setItem('access_token', 'at-1');
            sessionStorage.setItem('refresh_token', 'rt-1');
            expect(service.getAccessToken()).toBe('at-1');
            expect(service.getRefreshToken()).toBe('rt-1');
        });
    });

    describe('provider availability', () => {
        it('only local is available when cognito is not configured', () => {
            configureTestBed(false);
            expect(service.getAvailableAuthProviders()).toEqual(['local']);
            expect(service.isCognitoEnabled()).toBe(false);
            expect(service.isLocalEnabled()).toBe(true);
        });

        it('lists both providers when cognito is configured', () => {
            configureTestBed(true);
            expect(service.getAvailableAuthProviders()).toEqual(['local', 'cognito']);
            expect(service.isCognitoEnabled()).toBe(true);
        });
    });

    describe('getPreferredAuthProvider', () => {
        it('defaults to local when nothing is stored', () => {
            configureTestBed(true);
            expect(service.getPreferredAuthProvider()).toBe('local');
        });

        it('returns the stored preference when it is available', () => {
            configureTestBed(true);
            localStorage.setItem('preferred_auth_provider', 'cognito');
            expect(service.getPreferredAuthProvider()).toBe('cognito');
        });

        it('ignores a stored preference for a provider that is not available', () => {
            configureTestBed(false);
            localStorage.setItem('preferred_auth_provider', 'cognito');
            expect(service.getPreferredAuthProvider()).toBe('local');
        });

        it('ignores a garbage stored value', () => {
            configureTestBed(true);
            localStorage.setItem('preferred_auth_provider', 'not-a-provider');
            expect(service.getPreferredAuthProvider()).toBe('local');
        });
    });

    describe('setPreferredAuthProvider', () => {
        it('persists an available provider', () => {
            configureTestBed(true);
            service.setPreferredAuthProvider('cognito');
            expect(localStorage.getItem('preferred_auth_provider')).toBe('cognito');
        });

        it('throws when the provider is not enabled', () => {
            configureTestBed(false);
            expect(() => service.setPreferredAuthProvider('cognito')).toThrow(
                'Authentication provider "cognito" is not enabled in local mode',
            );
        });
    });

    describe('local login/register', () => {
        const user = {
            id: 'u1',
            username: 'fox',
            email: 'fox@example.com',
            created_at: '2026-01-01T00:00:00Z',
            updated_at: '2026-01-01T00:00:00Z',
        };

        it('logs in locally, stores tokens and sets auth state', async () => {
            configureTestBed(false);
            const promise = service.login({ username: 'fox', password: 'pw' });

            const req = httpMock.expectOne(`${environment.apiUrl}/user/login`);
            expect(req.request.method).toBe('POST');
            req.flush({ access_token: 'at', refresh_token: 'rt', user });

            await promise;

            expect(sessionStorage.getItem('access_token')).toBe('at');
            expect(sessionStorage.getItem('refresh_token')).toBe('rt');
            expect(service.isLoggedIn()).toBe(true);
            expect(service.currentUser()?.id).toBe('u1');
        });

        it('registers locally and sets auth state', async () => {
            configureTestBed(false);
            const promise = service.register({ username: 'fox', email: 'fox@example.com', password: 'pw' });

            const req = httpMock.expectOne(`${environment.apiUrl}/user/register`);
            expect(req.request.method).toBe('POST');
            req.flush({ access_token: 'at2', refresh_token: 'rt2', user });

            await promise;

            expect(sessionStorage.getItem('access_token')).toBe('at2');
            expect(service.isLoggedIn()).toBe(true);
        });

        it('propagates the API error message on failed login', async () => {
            configureTestBed(false);
            const promise = service.login({ username: 'fox', password: 'wrong' });

            const req = httpMock.expectOne(`${environment.apiUrl}/user/login`);
            req.flush({ message: 'Invalid credentials' }, { status: 401, statusText: 'Unauthorized' });

            await expect(promise).rejects.toThrow('Invalid credentials');
        });

        it('applies the user theme via ThemeService after login', async () => {
            configureTestBed(false);
            const promise = service.login({ username: 'fox', password: 'pw' });

            const req = httpMock.expectOne(`${environment.apiUrl}/user/login`);
            req.flush({ access_token: 'at', refresh_token: 'rt', user: { ...user, theme: 'dark' } });

            await promise;

            expect(themeService.setTheme).toHaveBeenCalledWith('dark', false);
        });

        it('remembers the preferred provider as local when logging in without an explicit provider', async () => {
            configureTestBed(true);
            const promise = service.login({ username: 'fox', password: 'pw' });

            const req = httpMock.expectOne(`${environment.apiUrl}/user/login`);
            req.flush({ access_token: 'at', refresh_token: 'rt', user });
            await promise;

            expect(localStorage.getItem('preferred_auth_provider')).toBe('local');
        });
    });

    describe('logout', () => {
        it('clears tokens, resets auth state and navigates to login', async () => {
            configureTestBed(false);
            sessionStorage.setItem('access_token', 'at');
            sessionStorage.setItem('refresh_token', 'rt');

            service.logout();

            expect(sessionStorage.getItem('access_token')).toBeNull();
            expect(sessionStorage.getItem('refresh_token')).toBeNull();
            expect(service.isLoggedIn()).toBe(false);
            expect(service.currentUser()).toBeNull();
            expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);

            // clearAuthState() defers cache-clearing to a setTimeout(0) to dodge a
            // circular dependency; drain it here so it doesn't fire mid-way
            // through a later, unrelated test.
            await tick();
        });
    });

    describe('refreshToken', () => {
        it('stores new tokens on success', async () => {
            sessionStorage.setItem('refresh_token', 'rt-old');
            configureTestBed(false);

            const emissions: unknown[] = [];
            service.refreshToken().subscribe((v) => emissions.push(v));

            const req = httpMock.expectOne(`${environment.apiUrl}/user/refresh`);
            expect(req.request.method).toBe('POST');
            expect(req.request.body).toEqual({ refresh_token: 'rt-old' });
            req.flush({ access_token: 'at-new', refresh_token: 'rt-new' });

            expect(sessionStorage.getItem('access_token')).toBe('at-new');
            expect(sessionStorage.getItem('refresh_token')).toBe('rt-new');
            expect(emissions.length).toBe(1);
        });

        it('logs out and errors when there is no refresh token', async () => {
            configureTestBed(false);
            let gotError: unknown;

            await new Promise<void>((resolve) => {
                service.refreshToken().subscribe({
                    error: (err) => {
                        gotError = err;
                        resolve();
                    },
                });
            });

            expect((gotError as Error).message).toBe('No refresh token available');
            expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);
            await tick();
        });

        it('logs out and rethrows on a failed refresh request', async () => {
            sessionStorage.setItem('refresh_token', 'rt-old');
            configureTestBed(false);
            let gotError: unknown;

            const donePromise = new Promise<void>((resolve) => {
                service.refreshToken().subscribe({
                    error: (err) => {
                        gotError = err;
                        resolve();
                    },
                });
            });

            const req = httpMock.expectOne(`${environment.apiUrl}/user/refresh`);
            req.flush({ message: 'expired' }, { status: 401, statusText: 'Unauthorized' });
            await donePromise;

            expect(gotError).toBeTruthy();
            expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);
            await tick();
        });
    });

    describe('profile and account HTTP methods', () => {
        it('fetches the user profile', () => {
            configureTestBed(false);
            service.getUserProfile().subscribe((u) => expect(u.id).toBe('u1'));

            const req = httpMock.expectOne(`${environment.apiUrl}/user/profile`);
            expect(req.request.method).toBe('GET');
            req.flush({
                id: 'u1',
                username: 'fox',
                email: 'fox@example.com',
                created_at: '',
                updated_at: '',
            });
        });

        it('updates the profile and refreshes auth state', () => {
            configureTestBed(false);
            service.updateProfile({ first_name: 'Fox' }).subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/user/profile`);
            expect(req.request.method).toBe('PUT');
            req.flush({
                id: 'u1',
                username: 'fox',
                email: 'fox@example.com',
                first_name: 'Fox',
                created_at: '',
                updated_at: '',
            });

            expect(service.currentUser()?.first_name).toBe('Fox');
        });

        it('updates the password', () => {
            configureTestBed(false);
            service.updatePassword({ current_password: 'old', new_password: 'new' }).subscribe((r) =>
                expect(r.message).toBe('ok'),
            );

            const req = httpMock.expectOne(`${environment.apiUrl}/user/password`);
            expect(req.request.method).toBe('PUT');
            req.flush({ message: 'ok' });
        });

        it('deletes the account and clears auth state', async () => {
            configureTestBed(false);
            sessionStorage.setItem('access_token', 'at');
            service.deleteAccount().subscribe();

            const req = httpMock.expectOne(`${environment.apiUrl}/user/delete`);
            expect(req.request.method).toBe('DELETE');
            req.flush({ message: 'deleted' });

            expect(sessionStorage.getItem('access_token')).toBeNull();
            expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);
            await tick();
        });

        it('fetches usage stats with the default period', () => {
            configureTestBed(false);
            service.getUserUsageStats().subscribe((stats) => expect(stats.chats).toBe(3));

            const req = httpMock.expectOne(
                (r) => r.url === `${environment.apiUrl}/user/usage` && r.params.get('period') === 'month',
            );
            expect(req.request.method).toBe('GET');
            req.flush({ chats: 3, messages: 10, input_tokens: 1, output_tokens: 2 });
        });

        it('fetches usage stats with an explicit period', () => {
            configureTestBed(false);
            service.getUserUsageStats('week').subscribe();

            const req = httpMock.expectOne(
                (r) => r.url === `${environment.apiUrl}/user/usage` && r.params.get('period') === 'week',
            );
            req.flush({ chats: 0, messages: 0, input_tokens: 0, output_tokens: 0 });
        });
    });

    describe('cognito flows', () => {
        const user = {
            id: 'u1',
            username: 'fox',
            email: 'fox@example.com',
            created_at: '',
            updated_at: '',
        };

        function flushProfile() {
            const req = httpMock.expectOne(`${environment.apiUrl}/user/profile`);
            expect(req.request.method).toBe('GET');
            req.flush(user);
        }

        it('rejects cognito calls when the provider is unavailable', async () => {
            configureTestBed(false);
            await expect(service.loginWithCognito({ username: 'fox', password: 'pw' })).rejects.toThrow(
                'Cognito authentication is disabled',
            );
        });

        it('logs in via cognito and completes the sign-in with a backend profile fetch', async () => {
            configureTestBed(true);
            externalAuth.signIn.mockResolvedValue({ isSignedIn: true, needsNewPassword: false });
            externalAuth.fetchSession.mockResolvedValue({ accessToken: 'ext-access', idToken: 'ext-id' });

            const promise = service.loginWithCognito({ username: 'fox', password: 'pw' });
            await tick();
            flushProfile();
            await promise;

            expect(sessionStorage.getItem('access_token')).toBe('ext-id');
            expect(sessionStorage.getItem('external_access_token')).toBe('ext-access');
            expect(service.isLoggedIn()).toBe(true);
        });

        it('throws a named NEW_PASSWORD_REQUIRED error when cognito demands a password change', async () => {
            configureTestBed(true);
            externalAuth.signIn.mockResolvedValue({ isSignedIn: false, needsNewPassword: true });

            await expect(service.loginWithCognito({ username: 'fox', password: 'pw' })).rejects.toMatchObject({
                name: 'NEW_PASSWORD_REQUIRED',
            });
        });

        it('throws a generic error when cognito sign-in fails outright', async () => {
            configureTestBed(true);
            externalAuth.signIn.mockResolvedValue({ isSignedIn: false, needsNewPassword: false });

            await expect(service.loginWithCognito({ username: 'fox', password: 'pw' })).rejects.toThrow('Login failed');
        });

        it('completes a new-password challenge and signs the user in', async () => {
            configureTestBed(true);
            externalAuth.completeNewPassword.mockResolvedValue({ isSignedIn: true, needsNewPassword: false });
            externalAuth.fetchSession.mockResolvedValue({ accessToken: 'a', idToken: 'i' });

            const promise = service.completeNewPasswordChallenge('NewPass123!');
            await tick();
            flushProfile();
            await promise;

            expect(externalAuth.completeNewPassword).toHaveBeenCalledWith('NewPass123!');
            expect(service.isLoggedIn()).toBe(true);
        });

        it('throws when the new-password challenge does not sign the user in', async () => {
            configureTestBed(true);
            externalAuth.completeNewPassword.mockResolvedValue({ isSignedIn: false, needsNewPassword: false });

            await expect(service.completeNewPasswordChallenge('pw')).rejects.toThrow('Failed to set new password');
        });

        it('registers via cognito, auto-logs in and persists name attributes', async () => {
            configureTestBed(true);
            externalAuth.signUp.mockResolvedValue({ needsConfirmation: false });
            externalAuth.signIn.mockResolvedValue({ isSignedIn: true, needsNewPassword: false });
            externalAuth.fetchSession.mockResolvedValue({ accessToken: 'a', idToken: 'i' });

            const promise = service.registerWithCognito({
                username: 'fox',
                email: 'fox@example.com',
                password: 'pw',
                first_name: 'Fox',
                last_name: 'Vale',
            });

            await tick();
            flushProfile();
            await tick();

            const updateReq = httpMock.expectOne(`${environment.apiUrl}/user/profile`);
            expect(updateReq.request.method).toBe('PUT');
            expect(updateReq.request.body).toEqual({ first_name: 'Fox', last_name: 'Vale' });
            updateReq.flush(user);

            await promise;

            expect(externalAuth.signUp).toHaveBeenCalledWith('fox', 'pw', {
                email: 'fox@example.com',
                given_name: 'Fox',
                family_name: 'Vale',
            });
        });

        it('signals EMAIL_VERIFICATION_REQUIRED without logging in when confirmation is needed', async () => {
            configureTestBed(true);
            externalAuth.signUp.mockResolvedValue({ needsConfirmation: true });

            await expect(
                service.registerWithCognito({ username: 'fox', email: 'fox@example.com', password: 'pw' }),
            ).rejects.toThrow('EMAIL_VERIFICATION_REQUIRED');
            expect(externalAuth.signIn).not.toHaveBeenCalled();
        });

        it('signs out externally and clears auth state', async () => {
            configureTestBed(true);
            sessionStorage.setItem('access_token', 'at');
            externalAuth.signOut.mockResolvedValue(undefined);

            await service.logoutExternal();

            expect(externalAuth.signOut).toHaveBeenCalled();
            expect(sessionStorage.getItem('access_token')).toBeNull();
            expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);
            await tick();
        });

        it('falls back to local logout when cognito is unavailable', async () => {
            configureTestBed(false);
            await service.logoutExternal();
            expect(router.navigate).toHaveBeenCalledWith(['/auth/login']);
            await tick();
        });

        it('still clears local state when the external sign-out call throws', async () => {
            configureTestBed(true);
            sessionStorage.setItem('access_token', 'at');
            externalAuth.signOut.mockRejectedValue(new Error('network down'));

            await service.logoutExternal();

            expect(logger.error).toHaveBeenCalledWith('External sign-out error', expect.any(Error));
            expect(sessionStorage.getItem('access_token')).toBeNull();
            await tick();
        });

        it('delegates confirmation/reset helpers to the external provider', async () => {
            configureTestBed(true);
            await service.confirmSignUpWithCode('fox', '123456');
            expect(externalAuth.confirmSignUp).toHaveBeenCalledWith('fox', '123456');

            await service.resendVerificationCode('fox');
            expect(externalAuth.resendSignUpCode).toHaveBeenCalledWith('fox');

            await service.confirmForgotPassword('fox', '123456', 'newPw');
            expect(externalAuth.confirmPasswordReset).toHaveBeenCalledWith('fox', '123456', 'newPw');
        });

        it('swallows forgotPassword errors to avoid user enumeration', async () => {
            configureTestBed(true);
            externalAuth.requestPasswordReset.mockRejectedValue(new Error('no such user'));

            await expect(service.forgotPassword('ghost')).resolves.toBeUndefined();
            expect(externalAuth.requestPasswordReset).toHaveBeenCalledWith('ghost');
        });
    });

    describe('syncExternalTokens', () => {
        it('does nothing when cognito is unavailable', () => {
            configureTestBed(false);
            service.syncExternalTokens({ accessToken: 'a', idToken: 'i' });
            expect(sessionStorage.getItem('access_token')).toBeNull();
        });

        it('does nothing when there is no idToken', () => {
            configureTestBed(true);
            service.syncExternalTokens({ accessToken: 'a' });
            expect(sessionStorage.getItem('access_token')).toBeNull();
        });

        it('persists tokens when both are present', () => {
            configureTestBed(true);
            service.syncExternalTokens({ accessToken: 'a', idToken: 'i' });
            expect(sessionStorage.getItem('access_token')).toBe('i');
            expect(sessionStorage.getItem('external_access_token')).toBe('a');
        });

        it('logs and swallows an unexpected persistence failure', () => {
            configureTestBed(true);
            themeService.setTheme.mockImplementation(() => {
                throw new Error('should not be reached');
            });
            // accessToken missing triggers persistExternalTokens' internal throw,
            // which syncExternalTokens catches and logs rather than propagating.
            service.syncExternalTokens({ idToken: 'i' } as { idToken: string; accessToken?: string });
            expect(logger.error).toHaveBeenCalled();
        });
    });

    describe('hasExternalSession', () => {
        it('is false when cognito is unavailable even if a token exists', () => {
            sessionStorage.setItem('external_access_token', 'x');
            configureTestBed(false);
            expect(service.hasExternalSession()).toBe(false);
        });

        it('is true when cognito is available and a token is stored', () => {
            sessionStorage.setItem('external_access_token', 'x');
            configureTestBed(true);
            expect(service.hasExternalSession()).toBe(true);
        });

        it('is false when cognito is available but no token is stored', () => {
            configureTestBed(true);
            expect(service.hasExternalSession()).toBe(false);
        });
    });

    describe('resolveAuthProvider via login()/register()', () => {
        it('throws when an explicit unavailable provider is requested', async () => {
            configureTestBed(false);
            await expect(service.login({ username: 'fox', password: 'pw' }, 'cognito')).rejects.toThrow(
                'Authentication provider "cognito" is not enabled in local mode',
            );
        });

        it('routes to cognito when explicitly requested and available', async () => {
            configureTestBed(true);
            externalAuth.signIn.mockResolvedValue({ isSignedIn: true, needsNewPassword: false });
            externalAuth.fetchSession.mockResolvedValue({ accessToken: 'a', idToken: 'i' });

            const promise = service.login({ username: 'fox', password: 'pw' }, 'cognito');
            await tick();
            flushProfileFor(httpMock);
            await promise;

            expect(service.isLoggedIn()).toBe(true);
        });
    });
});

function flushProfileFor(httpMock: HttpTestingController) {
    const req = httpMock.expectOne(`${environment.apiUrl}/user/profile`);
    req.flush({
        id: 'u1',
        username: 'fox',
        email: 'fox@example.com',
        created_at: '',
        updated_at: '',
    });
}
