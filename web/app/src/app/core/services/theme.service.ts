import { Injectable, Signal, computed, effect, inject, signal } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { firstValueFrom } from 'rxjs';

import { UserPreferencesService } from './user-preferences.service';
import { AuthService } from './auth.service';
import {
  THEME_STORAGE_KEY,
  ThemeMode,
  ResolvedTheme,
  applyThemeAttribute,
  clearStoredThemeMode,
  readStoredThemeMode,
  resolveTheme,
  writeStoredThemeMode,
} from './theme.helpers';

/**
 * Backwards-compatible alias for the resolved theme. Existing callers use
 * `Theme` to type form values and DOM lookups; new code should prefer
 * `ThemeMode` (which adds `'system'`) for user-facing preferences.
 */
export type Theme = ResolvedTheme;

export { THEME_STORAGE_KEY };
export type { ThemeMode };

/**
 * ThemeService owns the user's color-scheme preference and applies the
 * resolved scheme to <html data-theme="...">. The work is split into pure
 * helpers in `theme.helpers.ts`; this class is a thin Angular wrapper that
 * adds dependency injection, signal reactivity, and backend persistence.
 */
@Injectable({
  providedIn: 'root',
})
export class ThemeService {
  private document = inject(DOCUMENT);
  private userPreferencesService = inject(UserPreferencesService);
  private authService = inject(AuthService);

  /** User-selected mode; `'system'` defers to the OS preference. */
  public mode = signal<ThemeMode>('system');

  /** OS-level preference, kept in sync with `(prefers-color-scheme: dark)`. */
  private prefersDark = signal<boolean>(this.readPrefersDark());

  /** Resolved theme actually applied to the document. */
  public theme: Signal<ResolvedTheme> = computed(() =>
    resolveTheme(this.mode(), this.prefersDark()),
  );

  private mediaQuery: MediaQueryList | null = null;
  private mediaListener: ((event: MediaQueryListEvent) => void) | null = null;

  constructor() {
    this.subscribeToSystemPreference();

    effect(() => {
      applyThemeAttribute(this.document, this.theme());
    });

    this.initializeTheme();
  }

  /** Hydrates `mode` from localStorage. Defaults to `'system'` when nothing is stored. */
  public initializeTheme(): void {
    const stored = readStoredThemeMode(this.getStorage());
    if (stored) {
      this.mode.set(stored);
    } else {
      this.mode.set('system');
      writeStoredThemeMode(this.getStorage(), 'system');
    }
  }

  /** Updates the user's mode and (optionally) persists it to the backend. */
  setTheme(mode: ThemeMode, syncToBackend: boolean = true): void {
    this.mode.set(mode);
    writeStoredThemeMode(this.getStorage(), mode);

    if (syncToBackend && this.authService.isLoggedIn()) {
      this.syncThemeModeToBackend(mode).catch((error) => {
        console.warn('Failed to sync theme to backend:', error);
      });
    }
  }

  /** Drops the stored preference; used during sign-out by AuthService. */
  clearThemeFromStorage(): void {
    clearStoredThemeMode(this.getStorage());
  }

  /**
   * Pre-bootstrap entry point invoked from `main.ts`. Mirrors the logic in
   * `initializeTheme()` + the constructor effect using only the static helpers
   * so the document is themed before Angular's renderer is ready.
   */
  static initializeThemeEarly(): void {
    if (typeof window === 'undefined' || !window.document) {
      return;
    }

    const storage = typeof window.localStorage === 'undefined' ? null : window.localStorage;
    const mode = readStoredThemeMode(storage) ?? 'system';

    const prefersDark =
      typeof window.matchMedia === 'function'
        ? window.matchMedia('(prefers-color-scheme: dark)').matches
        : false;

    applyThemeAttribute(window.document, resolveTheme(mode, prefersDark));
  }

  private readPrefersDark(): boolean {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return false;
    }
    return window.matchMedia('(prefers-color-scheme: dark)').matches;
  }

  private subscribeToSystemPreference(): void {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') {
      return;
    }
    this.mediaQuery = window.matchMedia('(prefers-color-scheme: dark)');
    this.mediaListener = (event) => this.prefersDark.set(event.matches);
    this.mediaQuery.addEventListener('change', this.mediaListener);
  }

  private getStorage(): Storage | null {
    if (typeof window === 'undefined' || !window.localStorage) {
      return null;
    }
    return window.localStorage;
  }

  private async syncThemeModeToBackend(mode: ThemeMode): Promise<void> {
    try {
      const currentPreferences = await firstValueFrom(
        this.userPreferencesService.getUserPreferences(),
      );

      const updatedPreferences = {
        ...currentPreferences,
        theme: mode,
      };

      await firstValueFrom(
        this.userPreferencesService.updateUserPreferences(updatedPreferences),
      );
    } catch (error) {
      console.error('Failed to sync theme to backend:', error);
      throw error;
    }
  }
}
