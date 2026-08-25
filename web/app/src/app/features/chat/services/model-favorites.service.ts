import { InjectionToken, Injectable, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { Subject, firstValueFrom, from } from 'rxjs';
import { debounceTime, switchMap } from 'rxjs/operators';

import { UserPreferencesService } from '../../../core/services/user-preferences.service';

/**
 * Shown when a sync fails. Deliberately fixed copy rather than the server's message:
 * an HttpErrorResponse's own `message` is developer-facing plumbing text
 * ("Http failure response for <url>: 0 Unknown Error"), and nothing useful reaches the
 * user from a failed star. The underlying error goes to the console instead.
 */
const FAVORITES_SYNC_ERROR = 'Could not save favorites. Please try again.';

/**
 * Debounce before a starred/unstarred change reaches the API. Injectable so tests can
 * drive the sync without waiting on a real timer.
 */
export const MODEL_FAVORITES_SYNC_DEBOUNCE_MS = new InjectionToken<number>(
  'Model favorites sync debounce (ms)',
  { providedIn: 'root', factory: () => 500 },
);

/** localStorage key used while favorites were a client-only, per-personality concern. */
const LEGACY_STORAGE_KEY = 'modelFavorites_v1';

/**
 * Tracks the user's favorite models for the model picker.
 *
 * Favorites are a server-owned account preference (`favorite_model_ids`), not
 * device state, so this service holds no persistent local copy: it mirrors
 * `UserPreferencesService` and writes back through it. Keeping a second copy in
 * localStorage would mean a second thing to invalidate on logout, which is
 * exactly the bug that let one user's favorites leak into the next user's picker
 * on a shared browser.
 *
 * Toggles apply to the local signal immediately and sync on a debounce, so
 * starring several models in a row is one request rather than one per click.
 */
@Injectable({ providedIn: 'root' })
export class ModelFavoritesService {
  private readonly preferences = inject(UserPreferencesService);
  private readonly syncDebounceMs = inject(MODEL_FAVORITES_SYNC_DEBOUNCE_MS);

  private readonly store = signal<ReadonlySet<string>>(new Set<string>());

  /**
   * Last list the server confirmed, and the target a failed write rolls back to.
   * Tracked separately from `store`, which also holds not-yet-synced toggles.
   */
  private confirmed: readonly string[] = [];

  /**
   * Set when a sync fails, so the picker can explain why stars it just showed have
   * reverted. Cleared on the next successful write.
   */
  readonly error = signal<string | null>(null);

  /** Emits the full desired list; debounced so rapid toggles collapse into one write. */
  private readonly pendingSync = new Subject<string[]>();

  constructor() {
    // Favorites now live on the server, so a stale local copy is only a liability.
    this.forgetLegacyStorage();

    this.preferences.preferences$.pipe(takeUntilDestroyed()).subscribe(prefs => {
      this.confirmed = prefs?.favorite_model_ids ?? [];
      this.store.set(new Set(this.confirmed));
    });

    this.pendingSync
      .pipe(
        debounceTime(this.syncDebounceMs),
        // switchMap so a newer list supersedes an in-flight write rather than racing it.
        switchMap(ids => from(this.persist(ids))),
        takeUntilDestroyed(),
      )
      .subscribe();
  }

  /** Favorite model ids. Reads the backing signal, so it tracks inside computed/effect. */
  favoriteIds(): ReadonlySet<string> {
    return this.store();
  }

  isFavorite(modelId: string): boolean {
    return this.store().has(modelId);
  }

  toggle(modelId: string): void {
    const next = new Set(this.store());
    if (!next.delete(modelId)) {
      next.add(modelId);
    }
    this.store.set(next);
    this.pendingSync.next([...next]);
  }

  /** Drops in-memory favorites. Called on logout so the next user starts clean. */
  clearCache(): void {
    this.store.set(new Set<string>());
  }

  /**
   * Reads the current preferences before writing, because the endpoint replaces the
   * record rather than patching it — sending a partial object would clobber unrelated
   * fields. Mirrors the read-modify-write ThemeService already uses.
   *
   * On failure the optimistic state is rolled back to the last confirmed list, following
   * `ThreadListService.optimisticPatch`. Leaving a lit star in place would have the UI
   * assert something the server never accepted, and the user would discover it much later
   * with nothing connecting it to the click that caused it.
   *
   * Writes are last-write-wins across tabs. `switchMap` on the sync pipeline only orders
   * writes within one tab; two tabs starring different models will each read, merge into
   * their own snapshot, and write, so the later request overwrites the earlier one's
   * additions. Accepted deliberately: the preferences endpoint has no version or ETag to
   * check against, and adding optimistic concurrency for a star is disproportionate. The
   * losing tab reconciles on its next successful preferences load.
   */
  private async persist(favoriteModelIds: string[]): Promise<void> {
    try {
      const current = await firstValueFrom(this.preferences.getUserPreferences());
      await firstValueFrom(
        this.preferences.updateUserPreferences({ ...current, favorite_model_ids: favoriteModelIds }),
      );
      this.confirmed = favoriteModelIds;
      this.error.set(null);
    } catch (error) {
      // Roll back to the last confirmed list. With the debounce this can revert more than
      // one star at once, which is intended: the batch is what failed, so partial-success
      // state would be a fiction.
      this.store.set(new Set(this.confirmed));
      this.error.set(FAVORITES_SYNC_ERROR);
      console.error('[ModelFavorites] Failed to sync favorites', error);
    }
  }

  private forgetLegacyStorage(): void {
    try {
      localStorage.removeItem(LEGACY_STORAGE_KEY);
    } catch {
      // Storage may be unavailable (private mode); nothing to clean up in that case.
    }
  }
}
