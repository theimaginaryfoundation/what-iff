import { Injectable, inject, signal, Injector } from '@angular/core';
import { HttpClient, HttpErrorResponse } from '@angular/common/http';
import { Router } from '@angular/router';
import { Observable, BehaviorSubject, throwError, firstValueFrom } from 'rxjs';
import { tap, catchError } from 'rxjs/operators';
import { environment } from '../../../environments/environment';
import { ExternalAuthProvider, ExternalSessionTokens } from '../auth/external-auth.provider';
import {
  UserRegisterRequest,
  UserLoginRequest,
  UserResponse,
  LoginResponse,
  RefreshTokenRequest,
  RefreshTokenResponse,
  UpdateUserRequest,
  UpdatePasswordRequest,
  UsageStats,
  UsagePeriod
} from '../models/user.model';
import { LoggerService } from './logger.service';
import { ThemeService, THEME_STORAGE_KEY } from './theme.service';
import { getPasswordPolicyDescription } from '../utils/password-policy';
import { apiErrorMessage } from '../utils/api-error.helpers';

export type AuthProvider = 'local' | 'cognito';
export type AuthMode = 'local' | 'cognito' | 'hybrid';

const PREFERRED_AUTH_PROVIDER_KEY = 'preferred_auth_provider';
const ALLOWED_AUTH_MODES: readonly AuthMode[] = ['local', 'cognito', 'hybrid'];
const ALLOWED_AUTH_PROVIDERS: readonly AuthProvider[] = ['local', 'cognito'];

function resolveAuthMode(value: unknown): AuthMode {
  if (typeof value === 'string') {
    const normalized = value.toLowerCase();
    if ((ALLOWED_AUTH_MODES as readonly string[]).includes(normalized)) {
      return normalized as AuthMode;
    }
  }

  console.error(`[Auth] Invalid auth mode "${String(value)}" provided. Falling back to 'local'.`);
  return 'local';
}

@Injectable({
  providedIn: 'root'
})
export class AuthService {
  private http = inject(HttpClient);
  private router = inject(Router);
  private logger = inject(LoggerService);
  private injector = inject(Injector);
  private externalAuth = inject(ExternalAuthProvider);
  private apiUrl = `${environment.apiUrl}/user`;
  private readonly authMode: AuthMode = resolveAuthMode(environment.authMode);
  private readonly localEnabled = this.authMode === 'local' || this.authMode === 'hybrid';
  private readonly cognitoProviderAvailable = this.externalAuth.available;
  private static cachedPreferenceStorage: Storage | null | undefined;
  private readonly preferenceStorage = AuthService.resolvePreferenceStorage();
  private static readonly PASSWORD_POLICY_MESSAGE = getPasswordPolicyDescription(environment.passwordPolicy);
  private static readonly NEW_PASSWORD_REQUIRED = 'NEW_PASSWORD_REQUIRED';

  private currentUserSubject = new BehaviorSubject<UserResponse | null>(null);
  public currentUser$ = this.currentUserSubject.asObservable();

  // Signal for reactive UI updates
  public isLoggedIn = signal<boolean>(false);
  public currentUser = signal<UserResponse | null>(null);

  constructor() {
    // Initialize auth state without making HTTP calls to avoid circular dependency
    this.initializeAuthState();
  }

  private initializeAuthState(): void {
    // Only check token existence, don't make HTTP calls in constructor
    const accessToken = this.getAccessToken();
    if (accessToken) {
      // Set basic auth state without user profile verification
      this.isLoggedIn.set(true);
      // Defer profile loading until needed
      this.loadUserProfile();
    }
  }

  private loadUserProfile(): void {
    // Use setTimeout to break the circular dependency by deferring the HTTP call
    setTimeout(() => {
      this.getUserProfile().subscribe({
        next: (user) => {
          this.setAuthState(user);
        },
        error: () => {
          this.clearAuthState();
        }
      });
    }, 0);
  }

  // Public method to manually check auth state when needed
  public checkAuthState(): void {
    const accessToken = this.getAccessToken();
    if (accessToken) {
      this.getUserProfile().subscribe({
        next: (user) => {
          this.setAuthState(user);
        },
        error: () => {
          this.clearAuthState();
        }
      });
    }
  }

  private registerLocal(registerData: UserRegisterRequest): Observable<LoginResponse> {
    return this.http.post<LoginResponse>(`${this.apiUrl}/register`, registerData)
      .pipe(
        tap(response => {
          this.storeTokens(response.access_token, response.refresh_token);
          this.setAuthState(response.user);
        }),
        catchError((error) => this.handleError(error))
      );
  }

  private loginLocal(loginData: UserLoginRequest): Observable<LoginResponse> {
    return this.http.post<LoginResponse>(`${this.apiUrl}/login`, loginData)
      .pipe(
        tap(response => {
          this.storeTokens(response.access_token, response.refresh_token);
          this.setAuthState(response.user);
        }),
        catchError((error) => this.handleError(error))
      );
  }

  logout(): void {
    this.clearAuthState();
    this.router.navigate(['/auth/login']);
  }

  refreshToken(): Observable<RefreshTokenResponse> {
    if (!this.localEnabled) {
      return throwError(() => new Error('Local authentication is disabled'));
    }

    const refreshToken = this.getRefreshToken();
    if (!refreshToken) {
      this.logout();
      return throwError(() => new Error('No refresh token available'));
    }

    const refreshData: RefreshTokenRequest = { refresh_token: refreshToken };
    return this.http.post<RefreshTokenResponse>(`${this.apiUrl}/refresh`, refreshData)
      .pipe(
        tap(response => {
          this.storeTokens(response.access_token, response.refresh_token);
        }),
        catchError(error => {
          this.logout();
          return throwError(() => error);
        })
      );
  }

  getUserProfile(): Observable<UserResponse> {
    return this.http.get<UserResponse>(`${this.apiUrl}/profile`)
      .pipe(catchError((error) => this.handleError(error)));
  }

  updateProfile(updateData: UpdateUserRequest): Observable<UserResponse> {
    return this.http.put<UserResponse>(`${this.apiUrl}/profile`, updateData)
      .pipe(
        tap(user => {
          this.setAuthState(user);
        }),
        catchError((error) => this.handleError(error))
      );
  }

  updatePassword(passwordData: UpdatePasswordRequest): Observable<{message: string}> {
    return this.http.put<{message: string}>(`${this.apiUrl}/password`, passwordData)
      .pipe(catchError((error) => this.handleError(error)));
  }

  deleteAccount(): Observable<{message: string}> {
    return this.http.delete<{message: string}>(`${this.apiUrl}/delete`)
      .pipe(
        tap(() => {
          this.clearAuthState();
          this.router.navigate(['/auth/login']);
        }),
        catchError((error) => this.handleError(error))
      );
  }

  getUserUsageStats(period: UsagePeriod = 'month'): Observable<UsageStats> {
    const params = { period };
    return this.http.get<UsageStats>(`${this.apiUrl}/usage`, { params })
      .pipe(catchError((error) => this.handleError(error)));
  }

  // =============================================================================
  // Authentication Provider Helpers
  // =============================================================================

  getAvailableAuthProviders(): AuthProvider[] {
    const providers: AuthProvider[] = [];
    if (this.localEnabled) {
      providers.push('local');
    }
    if (this.cognitoProviderAvailable) {
      providers.push('cognito');
    }
    return providers;
  }

  getPreferredAuthProvider(): AuthProvider {
    const availableProviders = this.getAvailableAuthProviders();
    const storedRaw = this.preferenceStorage?.getItem(PREFERRED_AUTH_PROVIDER_KEY);
    const normalizedStored =
      typeof storedRaw === 'string' && (ALLOWED_AUTH_PROVIDERS as readonly string[]).includes(storedRaw)
        ? (storedRaw as AuthProvider)
        : null;

    if (normalizedStored && availableProviders.includes(normalizedStored)) {
      return normalizedStored;
    }

    if (availableProviders.includes('local')) {
      return 'local';
    }

    if (availableProviders.includes('cognito')) {
      return 'cognito';
    }

    return 'local';
  }

  setPreferredAuthProvider(provider: AuthProvider): void {
    if (!this.getAvailableAuthProviders().includes(provider)) {
      throw new Error(`Authentication provider "${provider}" is not enabled in ${this.authMode} mode`);
    }
    try {
      this.preferenceStorage?.setItem(PREFERRED_AUTH_PROVIDER_KEY, provider);
    } catch {
      // Ignore storage write issues; preference simply won't persist.
    }
  }

  isCognitoEnabled(): boolean {
    return this.cognitoProviderAvailable;
  }

  isLocalEnabled(): boolean {
    return this.localEnabled;
  }

  private static resolvePreferenceStorage(): Storage | null {
    if (AuthService.cachedPreferenceStorage !== undefined) {
      return AuthService.cachedPreferenceStorage;
    }

    if (typeof window === 'undefined') {
      AuthService.cachedPreferenceStorage = null;
      return AuthService.cachedPreferenceStorage;
    }

    const storages: Storage[] = [];
    if (window.localStorage) {
      storages.push(window.localStorage);
    }
    if (window.sessionStorage) {
      storages.push(window.sessionStorage);
    }

    for (const storage of storages) {
      try {
        const probeKey = '__auth_pref_probe__';
        storage.setItem(probeKey, 'probe');
        storage.removeItem(probeKey);
        AuthService.cachedPreferenceStorage = storage;
        return AuthService.cachedPreferenceStorage;
      } catch {
        continue;
      }
    }

    AuthService.cachedPreferenceStorage = null;
    return AuthService.cachedPreferenceStorage;
  }

  private assertCognitoEnabled(): void {
    if (!this.cognitoProviderAvailable) {
      throw new Error('Cognito authentication is disabled');
    }
  }

  async register(registerData: UserRegisterRequest, provider?: AuthProvider): Promise<void> {
    const resolvedProvider = this.resolveAuthProvider(provider);
    if (!provider && this.getAvailableAuthProviders().includes(resolvedProvider)) {
      this.setPreferredAuthProvider(resolvedProvider);
    }

    if (resolvedProvider === 'cognito') {
      await this.registerWithCognito(registerData);
      return;
    }

    if (!this.localEnabled) {
      throw new Error('Local authentication is disabled');
    }

    await firstValueFrom(this.registerLocal(registerData));
  }

  async login(loginData: UserLoginRequest, provider?: AuthProvider): Promise<void> {
    const resolvedProvider = this.resolveAuthProvider(provider);
    if (!provider && this.getAvailableAuthProviders().includes(resolvedProvider)) {
      this.setPreferredAuthProvider(resolvedProvider);
    }

    if (resolvedProvider === 'cognito') {
      await this.loginWithCognito(loginData);
      return;
    }

    if (!this.localEnabled) {
      throw new Error('Local authentication is disabled');
    }

    await firstValueFrom(this.loginLocal(loginData));
  }

  async logoutPreferred(provider?: AuthProvider): Promise<void> {
    const resolvedProvider = this.resolveAuthProvider(provider);

    if (resolvedProvider === 'cognito' && this.cognitoProviderAvailable) {
      await this.logoutExternal();
      return;
    }

    this.logout();
  }

  private resolveAuthProvider(provider?: AuthProvider): AuthProvider {
    if (provider) {
      if (!this.getAvailableAuthProviders().includes(provider)) {
        throw new Error(`Authentication provider "${provider}" is not enabled in ${this.authMode} mode`);
      }
      return provider;
    }

    return this.getPreferredAuthProvider();
  }

  // =============================================================================
  // External provider authentication
  // =============================================================================

  async registerWithCognito(registerData: UserRegisterRequest): Promise<void> {
    this.assertCognitoEnabled();

    const attributes: Record<string, string> = { email: registerData.email };
    if (registerData.first_name) {
      attributes['given_name'] = registerData.first_name;
    }
    if (registerData.last_name) {
      attributes['family_name'] = registerData.last_name;
    }

    const result = await this.externalAuth.signUp(registerData.username, registerData.password, attributes);
    if (result.needsConfirmation) {
      // Email verification required - throw special signal for the component to handle.
      throw new Error('EMAIL_VERIFICATION_REQUIRED');
    }

    // Auto-login after registration if no verification needed.
    await this.loginWithCognito({ username: registerData.username, password: registerData.password });

    // Persist profile details in our backend (the provider may not allow name writes).
    const profileUpdate: UpdateUserRequest = {};
    if (registerData.first_name) {
      profileUpdate.first_name = registerData.first_name;
    }
    if (registerData.last_name) {
      profileUpdate.last_name = registerData.last_name;
    }
    if (Object.keys(profileUpdate).length > 0) {
      try {
        await firstValueFrom(this.updateProfile(profileUpdate));
      } catch (profileError) {
        console.warn('[Auth] Failed to store profile details after signup', profileError);
      }
    }
  }

  async loginWithCognito(loginData: UserLoginRequest): Promise<void> {
    this.assertCognitoEnabled();

    const result = await this.externalAuth.signIn(loginData.username, loginData.password);
    if (result.needsNewPassword) {
      const error = new Error(AuthService.NEW_PASSWORD_REQUIRED);
      error.name = AuthService.NEW_PASSWORD_REQUIRED;
      throw error;
    }
    if (!result.isSignedIn) {
      throw new Error('Login failed');
    }
    await this.completeExternalSignIn('after login');
  }

  async completeNewPasswordChallenge(newPassword: string): Promise<void> {
    this.assertCognitoEnabled();

    const result = await this.externalAuth.completeNewPassword(newPassword);
    if (!result.isSignedIn) {
      throw new Error('Failed to set new password');
    }
    await this.completeExternalSignIn('after password update');
  }

  // Persists the current provider session and loads the backend profile (which
  // triggers JIT provisioning).
  private async completeExternalSignIn(context: string): Promise<void> {
    const tokens = await this.externalAuth.fetchSession(true);
    this.persistExternalTokens(tokens, context);
    const userProfile = await firstValueFrom(this.getUserProfile());
    this.setAuthState(userProfile);
  }

  async logoutExternal(): Promise<void> {
    if (!this.cognitoProviderAvailable) {
      this.logout();
      return;
    }

    try {
      await this.externalAuth.signOut();
    } catch (error) {
      this.logger.error('External sign-out error', error);
    } finally {
      this.clearAuthState();
      this.router.navigate(['/auth/login']);
    }
  }

  async confirmSignUpWithCode(username: string, code: string): Promise<void> {
    this.assertCognitoEnabled();
    await this.externalAuth.confirmSignUp(username, code);
  }

  async resendVerificationCode(username: string): Promise<void> {
    this.assertCognitoEnabled();
    await this.externalAuth.resendSignUpCode(username);
  }

  async forgotPassword(username: string): Promise<void> {
    this.assertCognitoEnabled();
    try {
      await this.externalAuth.requestPasswordReset(username);
    } catch {
      // Do not reveal whether the account exists (prevents user enumeration).
    }
  }

  async confirmForgotPassword(username: string, confirmationCode: string, newPassword: string): Promise<void> {
    this.assertCognitoEnabled();
    await this.externalAuth.confirmPasswordReset(username, confirmationCode, newPassword);
  }

  // =============================================================================
  // Legacy JWT Authentication Methods (for parallel auth during transition)
  // =============================================================================

  getAccessToken(): string | null {
    return sessionStorage.getItem('access_token');
  }

  getRefreshToken(): string | null {
    return sessionStorage.getItem('refresh_token');
  }

  hasExternalSession(): boolean {
    return this.cognitoProviderAvailable && sessionStorage.getItem('external_access_token') !== null;
  }

  syncExternalTokens(tokens: ExternalSessionTokens | null | undefined, context = 'token sync'): void {
    if (!this.cognitoProviderAvailable) {
      return;
    }

    if (!tokens?.idToken) {
      return;
    }

    try {
      this.persistExternalTokens(tokens, context);
    } catch (error) {
      this.logger.error('Failed to persist external session tokens', error);
    }
  }

  private storeTokens(accessToken: string, refreshToken?: string): void {
    sessionStorage.setItem('access_token', accessToken);

    if (refreshToken) {
      sessionStorage.setItem('refresh_token', refreshToken);
    } else {
      sessionStorage.removeItem('refresh_token');
    }

    // Clear any cached external session token unless explicitly set later.
    sessionStorage.removeItem('external_access_token');
  }

  private clearTokens(): void {
    sessionStorage.removeItem('access_token');
    sessionStorage.removeItem('refresh_token');
    sessionStorage.removeItem('external_access_token');
  }

  private persistExternalTokens(tokens: ExternalSessionTokens | null | undefined, context: string): void {
    if (!this.cognitoProviderAvailable) {
      this.logger.error('External token persistence attempted while unavailable', context);
      throw new Error('External authentication is not available');
    }

    if (!tokens?.accessToken) {
      this.logger.error(`External access token missing ${context}`);
      throw new Error('Authentication failed. Please try signing in again.');
    }

    if (!tokens.idToken) {
      this.logger.error(`External ID token missing ${context}`);
      throw new Error('Authentication failed. Please try signing in again.');
    }

    this.storeTokens(tokens.idToken, undefined);

    // Preserve the raw access token for future use if needed.
    sessionStorage.setItem('external_access_token', tokens.accessToken);
  }

  private setAuthState(user: UserResponse): void {
    this.currentUserSubject.next(user);
    this.currentUser.set(user);
    this.isLoggedIn.set(true);

    // Apply theme from user object if present
    if (user.theme && (user.theme === 'light' || user.theme === 'dark')) {
      try {
        const themeService = this.injector.get(ThemeService);
        themeService.setTheme(user.theme, false);
      } catch (error) {
        console.warn('Failed to apply theme from user object:', error);
      }
    }
  }

  private clearAuthState(): void {
    this.clearTokens();
    this.currentUserSubject.next(null);
    this.currentUser.set(null);
    this.isLoggedIn.set(false);

    try {
      localStorage.removeItem('lastChatId');
    } catch (error) {
      // Ignore if localStorage is not available
    }

    // Clear all service caches to prevent stale data on next login
    // Use setTimeout to avoid circular dependency issues during initialization
    setTimeout(() => {
      try {
        // Dynamically import services to avoid circular dependency
        Promise.all([
          // Clears any state the send gate holds around chat.
          import('../../features/chat/services/chat-send-gate').then(({ ChatSendGate }) => {
            const chatSendGate = this.injector.get(ChatSendGate);
            chatSendGate.clearCache();
          }),
          import('./chat.service').then(({ ChatService }) => {
            const chatService = this.injector.get(ChatService);
            chatService.clearCache();
          }),
          import('./message.service').then(({ MessageService }) => {
            const messageService = this.injector.get(MessageService);
            messageService.clearCache();
          }),
          import('./draft-message.service').then(({ DraftMessageService }) => {
            const draftMessageService = this.injector.get(DraftMessageService);
            draftMessageService.clearCache();
          }),
          import('./user-preferences.service').then(({ UserPreferencesService }) => {
            const userPreferencesService = this.injector.get(UserPreferencesService);
            userPreferencesService.clearCache();
          }),
          import('./model.service').then(({ ModelService }) => {
            const modelService = this.injector.get(ModelService);
            modelService.clearCache();
          }),
          import('./job.service').then(({ JobService }) => {
            const jobService = this.injector.get(JobService);
            jobService.clearCache();
          }),
          import('../../features/chat/services/model-favorites.service').then(({ ModelFavoritesService }) => {
            const modelFavoritesService = this.injector.get(ModelFavoritesService);
            modelFavoritesService.clearCache();
          })
        ]).catch(() => {
          // Ignore if services can't be loaded
        });
      } catch (error) {
        // Ignore if services are not available
      }
    }, 0);

    // Clear theme preference from localStorage on logout
    try {
      if (typeof window !== 'undefined' && window.localStorage) {
        localStorage.removeItem(THEME_STORAGE_KEY);
      }
    } catch (error) {
      // Ignore localStorage errors
    }
  }

  private handleError = (error: HttpErrorResponse): Observable<never> => {
    const errorMessage = apiErrorMessage(error, 'An error occurred');

    this.logger.error('Auth service error', error);
    return throwError(() => new Error(errorMessage));
  }
}
