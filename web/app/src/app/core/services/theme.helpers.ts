/**
 * Pure helpers backing ThemeService.
 *
 * No Angular imports here — these helpers are unit-tested without TestBed and
 * keep ThemeService thin. See `theme.helpers.spec.ts` for behavior contracts.
 */

export type ThemeMode = 'light' | 'dark' | 'system';
export type ResolvedTheme = 'light' | 'dark';

export const THEME_STORAGE_KEY = 'userThemePreference';

/** Maps the OS-level prefers-color-scheme bit onto a concrete theme. */
export function resolveSystemTheme(prefersDark: boolean): ResolvedTheme {
  return prefersDark ? 'dark' : 'light';
}

/** Resolves any ThemeMode (including 'system') to a concrete light/dark theme. */
export function resolveTheme(mode: ThemeMode, prefersDark: boolean): ResolvedTheme {
  if (mode === 'light' || mode === 'dark') {
    return mode;
  }
  return resolveSystemTheme(prefersDark);
}

/**
 * Applies the resolved theme to <html>. Sets the `data-theme` attribute and
 * defensively removes the legacy `dark` class from any earlier theme strategy
 * so styles only key off `data-theme` going forward.
 */
export function applyThemeAttribute(doc: Document, resolved: ResolvedTheme): void {
  const root = doc.documentElement;
  root.setAttribute('data-theme', resolved);
  root.classList.remove('dark');
}

/** Reads a stored ThemeMode from localStorage; null on missing/invalid/unavailable. */
export function readStoredThemeMode(storage: Storage | null): ThemeMode | null {
  if (!storage) {
    return null;
  }
  try {
    const stored = storage.getItem(THEME_STORAGE_KEY);
    if (stored === 'light' || stored === 'dark' || stored === 'system') {
      return stored;
    }
  } catch {
    // Storage may throw in private mode / disabled cookies; treat as missing.
  }
  return null;
}

/** Persists a ThemeMode to localStorage; silently no-ops when storage is unavailable. */
export function writeStoredThemeMode(storage: Storage | null, mode: ThemeMode): void {
  if (!storage) {
    return;
  }
  try {
    storage.setItem(THEME_STORAGE_KEY, mode);
  } catch {
    // Quota or access errors are non-fatal — we only lose persistence.
  }
}

/** Removes the persisted ThemeMode entry; silently no-ops when storage is unavailable. */
export function clearStoredThemeMode(storage: Storage | null): void {
  if (!storage) {
    return;
  }
  try {
    storage.removeItem(THEME_STORAGE_KEY);
  } catch {
    // Same rationale as writeStoredThemeMode.
  }
}
