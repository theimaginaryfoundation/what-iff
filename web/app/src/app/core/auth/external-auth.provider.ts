import { Injectable } from '@angular/core';

/** Tokens returned from an upstream identity provider session. */
export interface ExternalSessionTokens {
  accessToken?: string;
  idToken?: string;
}

/** Result of a sign-up attempt. */
export interface ExternalSignUpResult {
  /** True when the account needs a verification code confirmed before sign-in. */
  needsConfirmation: boolean;
}

/** Result of a sign-in (or new-password) attempt. */
export interface ExternalSignInResult {
  isSignedIn: boolean;
  /** True when the provider requires the user to set a new password to continue. */
  needsNewPassword: boolean;
}

/**
 * ExternalAuthProvider abstracts an upstream identity provider (hosted sign-in,
 * managed sessions, email verification, password reset). The application depends
 * only on this interface; the concrete implementation is supplied via DI.
 *
 * When `available` is false the application uses its built-in username/password
 * authentication and never calls the other methods.
 */
export abstract class ExternalAuthProvider {
  abstract readonly available: boolean;

  /** Configure the provider SDK. Called once during app startup. */
  abstract configure(): void;

  /** Current session tokens, or null when there is no active session. */
  abstract fetchSession(forceRefresh?: boolean): Promise<ExternalSessionTokens | null>;

  abstract signUp(
    username: string,
    password: string,
    attributes: Record<string, string>,
  ): Promise<ExternalSignUpResult>;

  abstract signIn(username: string, password: string): Promise<ExternalSignInResult>;

  abstract completeNewPassword(newPassword: string): Promise<ExternalSignInResult>;

  abstract confirmSignUp(username: string, code: string): Promise<void>;

  abstract resendSignUpCode(username: string): Promise<void>;

  abstract requestPasswordReset(username: string): Promise<void>;

  abstract confirmPasswordReset(username: string, code: string, newPassword: string): Promise<void>;

  abstract updatePassword(oldPassword: string, newPassword: string): Promise<void>;

  abstract updateProfileAttributes(attributes: Record<string, string>): Promise<void>;

  abstract signOut(): Promise<void>;
}

/**
 * Default provider for builds without an upstream identity provider: reports
 * unavailable and performs no external calls. Its methods are never invoked while
 * `available` is false, but they reject clearly if called.
 */
@Injectable()
export class NoopExternalAuthProvider extends ExternalAuthProvider {
  readonly available = false;

  configure(): void {
    // No external provider to configure.
  }

  private unavailable(): Promise<never> {
    return Promise.reject(new Error('External authentication is not available in this build'));
  }

  fetchSession(): Promise<ExternalSessionTokens | null> {
    return Promise.resolve(null);
  }

  signUp(): Promise<ExternalSignUpResult> {
    return this.unavailable();
  }

  signIn(): Promise<ExternalSignInResult> {
    return this.unavailable();
  }

  completeNewPassword(): Promise<ExternalSignInResult> {
    return this.unavailable();
  }

  confirmSignUp(): Promise<void> {
    return this.unavailable();
  }

  resendSignUpCode(): Promise<void> {
    return this.unavailable();
  }

  requestPasswordReset(): Promise<void> {
    return this.unavailable();
  }

  confirmPasswordReset(): Promise<void> {
    return this.unavailable();
  }

  updatePassword(): Promise<void> {
    return this.unavailable();
  }

  updateProfileAttributes(): Promise<void> {
    return this.unavailable();
  }

  signOut(): Promise<void> {
    return Promise.resolve();
  }
}
