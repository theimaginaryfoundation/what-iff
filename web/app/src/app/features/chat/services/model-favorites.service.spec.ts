import type { Mock } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { BehaviorSubject, Observable, of, throwError } from 'rxjs';

import { ModelFavoritesService, MODEL_FAVORITES_SYNC_DEBOUNCE_MS } from './model-favorites.service';
import { UserPreferencesService } from '../../../core/services/user-preferences.service';
import { UserPreferences } from '../../../core/models/user.model';

function prefsFixture(favorites: string[] = []): UserPreferences {
    return {
        id: 'prefs-1',
        user_id: 'user-1',
        default_model_id: 'model-default',
        theme: 'dark',
        favorite_model_ids: favorites,
    };
}

/**
 * Lets a debounced write run to completion. The debounce is overridden to 0 below, so one
 * macrotask releases it, after which the promise chain inside the write still needs the
 * microtask queue to drain. The app is zoneless, so fakeAsync/tick are unavailable here.
 */
async function flushPendingWrites(): Promise<void> {
    await new Promise(resolve => setTimeout(resolve, 0));
    for (let i = 0; i < 5; i++) {
        await Promise.resolve();
    }
}

describe('ModelFavoritesService', () => {
    let preferences$: BehaviorSubject<UserPreferences | null>;
    let updateSpy: Mock;
    let service: ModelFavoritesService;

    beforeEach(() => {
        preferences$ = new BehaviorSubject<UserPreferences | null>(null);
        updateSpy = vi.fn().mockName('updateUserPreferences').mockImplementation((prefs: UserPreferences) => of(prefs));

        const preferencesStub: Partial<UserPreferencesService> = {
            preferences$: preferences$.asObservable(),
            getUserPreferences: () => of(preferences$.value ?? prefsFixture()),
            updateUserPreferences: updateSpy as unknown as UserPreferencesService['updateUserPreferences'],
        };

        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                { provide: UserPreferencesService, useValue: preferencesStub },
                { provide: MODEL_FAVORITES_SYNC_DEBOUNCE_MS, useValue: 0 },
            ],
        });

        service = TestBed.inject(ModelFavoritesService);
    });

    it('hydrates from the loaded user preferences', () => {
        preferences$.next(prefsFixture(['m1', 'm2']));

        expect(service.isFavorite('m1')).toBe(true);
        expect([...service.favoriteIds()].sort()).toEqual(['m1', 'm2']);
    });

    it('treats a missing favorites field as no favorites', () => {
        preferences$.next({ ...prefsFixture(), favorite_model_ids: undefined });

        expect(service.favoriteIds().size).toBe(0);
    });

    it('applies a toggle to local state immediately, before any request', () => {
        preferences$.next(prefsFixture([]));

        service.toggle('m1');

        expect(service.isFavorite('m1')).toBe(true);
        expect(updateSpy).not.toHaveBeenCalled();
    });

    it('collapses rapid toggles into a single write carrying the final list', async () => {
        preferences$.next(prefsFixture([]));

        service.toggle('m1');
        service.toggle('m2');
        service.toggle('m3');
        await flushPendingWrites();

        expect(updateSpy).toHaveBeenCalledTimes(1);
        const sent = vi.mocked(updateSpy).mock.lastCall![0] as UserPreferences;
        expect([...(sent.favorite_model_ids ?? [])].sort()).toEqual(['m1', 'm2', 'm3']);
    });

    it('sends the whole preferences object so unrelated fields are not clobbered', async () => {
        preferences$.next(prefsFixture([]));

        service.toggle('m1');
        await flushPendingWrites();

        const sent = vi.mocked(updateSpy).mock.lastCall![0] as UserPreferences;
        expect(sent.default_model_id).toBe('model-default');
        expect(sent.theme).toBe('dark');
    });

    it('removes a favorite and writes the remaining list', async () => {
        preferences$.next(prefsFixture(['m1', 'm2']));

        service.toggle('m1');
        await flushPendingWrites();

        expect(service.isFavorite('m1')).toBe(false);
        const sent = vi.mocked(updateSpy).mock.lastCall![0] as UserPreferences;
        expect(sent.favorite_model_ids).toEqual(['m2']);
    });

    it('sends an empty array when the last favorite is removed', async () => {
        preferences$.next(prefsFixture(['m1']));

        service.toggle('m1');
        await flushPendingWrites();

        const sent = vi.mocked(updateSpy).mock.lastCall![0] as UserPreferences;
        expect(sent.favorite_model_ids).toEqual([]);
    });

    it('rolls back to the last confirmed list when the write fails', async () => {
        preferences$.next(prefsFixture(['m1']));
        updateSpy.mockReturnValue(throwError(() => new Error('network down')));

        service.toggle('m2');
        expect(service.isFavorite('m2')).toBe(true); // optimistic, before the write
        await flushPendingWrites();

        expect(service.isFavorite('m2')).toBe(false);
        expect([...service.favoriteIds()]).toEqual(['m1']);
    });

    it('surfaces fixed copy rather than the raw error when the write fails', async () => {
        // Never the error's own message: an HttpErrorResponse carries developer-facing text
        // ("Http failure response for <url>: 0 Unknown Error"), which is not useful to a user
        // who just clicked a star. The detail goes to the console instead.
        preferences$.next(prefsFixture([]));
        updateSpy.mockReturnValue(throwError(() => new Error('Http failure response for /api: 0 Unknown Error')));

        service.toggle('m1');
        await flushPendingWrites();

        expect(service.error()).toBe('Could not save favorites. Please try again.');
    });

    it('uses the same copy when the failure carries no message at all', async () => {
        preferences$.next(prefsFixture([]));
        updateSpy.mockReturnValue(throwError(() => ({})));

        service.toggle('m1');
        await flushPendingWrites();

        expect(service.error()).toBe('Could not save favorites. Please try again.');
    });

    it('reverts every star from a failed batch, not just the last one', async () => {
        // The debounce means one failed write can own several toggles. Reverting the whole
        // batch is intended: partial-success state would be a fiction.
        preferences$.next(prefsFixture([]));
        updateSpy.mockReturnValue(throwError(() => new Error('nope')));

        service.toggle('m1');
        service.toggle('m2');
        await flushPendingWrites();

        expect(service.favoriteIds().size).toBe(0);
    });

    it('clears a previous error once a write succeeds', async () => {
        preferences$.next(prefsFixture([]));
        updateSpy.mockReturnValue(throwError(() => new Error('nope')));
        service.toggle('m1');
        await flushPendingWrites();
        expect(service.error()).not.toBeNull();

        updateSpy.mockImplementation((prefs: UserPreferences) => of(prefs));
        service.toggle('m2');
        await flushPendingWrites();

        expect(service.error()).toBeNull();
    });

    it('is not blocked by an earlier write that never settles', async () => {
        // switchMap drops the in-flight inner observable when a newer list arrives, so a hung
        // request cannot wedge the pipeline: the next toggle still reaches the API. Without
        // that, one stalled request would silently stop favorites syncing for the session.
        preferences$.next(prefsFixture([]));
        updateSpy.mockReturnValue(new Observable<UserPreferences>(() => {
            // never emits, never errors, never completes
        }));

        service.toggle('m1');
        await flushPendingWrites();
        expect(updateSpy).toHaveBeenCalledTimes(1);

        updateSpy.mockImplementation((prefs: UserPreferences) => of(prefs));
        service.toggle('m2');
        await flushPendingWrites();

        expect(updateSpy).toHaveBeenCalledTimes(2);
        const sent = vi.mocked(updateSpy).mock.lastCall![0] as UserPreferences;
        expect([...(sent.favorite_model_ids ?? [])].sort()).toEqual(['m1', 'm2']);
    });

    it('clears in-memory favorites on clearCache so the next user starts empty', () => {
        preferences$.next(prefsFixture(['m1']));
        expect(service.isFavorite('m1')).toBe(true);

        service.clearCache();

        expect(service.favoriteIds().size).toBe(0);
    });

    it('drops the legacy localStorage key rather than leaving stale data behind', () => {
        // The key predates server-side favorites; a leftover copy is what leaked one
        // user's picker state into the next user's session on a shared browser.
        expect(localStorage.getItem('modelFavorites_v1')).toBeNull();
    });
});
