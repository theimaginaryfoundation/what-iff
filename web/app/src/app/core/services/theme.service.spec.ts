import type { Mock, MockedObject } from "vitest";
import { TestBed } from '@angular/core/testing';
import { provideZonelessChangeDetection } from '@angular/core';
import { DOCUMENT } from '@angular/common';
import { of } from 'rxjs';

import { THEME_STORAGE_KEY, ThemeService } from './theme.service';
import { UserPreferencesService } from './user-preferences.service';
import { AuthService } from './auth.service';

interface FakeMediaQueryList {
    matches: boolean;
    addEventListener: Mock;
    removeEventListener: Mock;
    trigger(matches: boolean): void;
}

function makeFakeMediaQuery(matches: boolean): FakeMediaQueryList {
    let listener: ((event: MediaQueryListEvent) => void) | null = null;
    const addEventListener = vi.fn().mockName('addEventListener').mockImplementation((_event: string, fn: (event: MediaQueryListEvent) => void) => {
        listener = fn;
    });
    const removeEventListener = vi.fn().mockName('removeEventListener');
    return {
        matches,
        addEventListener,
        removeEventListener,
        trigger(next: boolean) {
            this.matches = next;
            listener?.({ matches: next } as MediaQueryListEvent);
        },
    };
}

describe('ThemeService', () => {
    let mockUserPrefs: Pick<MockedObject<UserPreferencesService>, 'getUserPreferences' | 'updateUserPreferences'>;
    let mockAuth: Pick<MockedObject<AuthService>, 'isLoggedIn'>;
    let fakeDocument: Document;
    let fakeRoot: HTMLElement;
    let fakeMediaQuery: FakeMediaQueryList;
    let originalMatchMedia: typeof window.matchMedia | undefined;

    function configureTestBed(): void {
        TestBed.configureTestingModule({
            providers: [
                provideZonelessChangeDetection(),
                { provide: UserPreferencesService, useValue: mockUserPrefs },
                { provide: AuthService, useValue: mockAuth },
                { provide: DOCUMENT, useValue: fakeDocument },
            ],
        });
    }

    beforeEach(() => {
        fakeRoot = document.createElement('html');
        fakeDocument = { documentElement: fakeRoot } as unknown as Document;
        fakeMediaQuery = makeFakeMediaQuery(false);

        originalMatchMedia = window.matchMedia;
        (window as any).matchMedia = vi.fn().mockName('matchMedia').mockReturnValue(fakeMediaQuery);

        mockUserPrefs = {
            getUserPreferences: vi.fn().mockName("UserPreferencesService.getUserPreferences"),
            updateUserPreferences: vi.fn().mockName("UserPreferencesService.updateUserPreferences")
        } as unknown as Pick<MockedObject<UserPreferencesService>, 'getUserPreferences' | 'updateUserPreferences'>;
        mockUserPrefs.getUserPreferences.mockReturnValue(of({ theme: 'light' } as any));
        mockUserPrefs.updateUserPreferences.mockReturnValue(of({ theme: 'light' } as any));

        mockAuth = {
            isLoggedIn: vi.fn().mockName("AuthService.isLoggedIn")
        } as unknown as Pick<MockedObject<AuthService>, 'isLoggedIn'>;
        mockAuth.isLoggedIn.mockReturnValue(false);

        localStorage.clear();
    });

    afterEach(() => {
        localStorage.clear();
        if (originalMatchMedia) {
            (window as any).matchMedia = originalMatchMedia;
        }
        else {
            delete (window as any).matchMedia;
        }
    });

    describe('initialization', () => {
        it('defaults to system mode and persists it when storage is empty', () => {
            configureTestBed();
            const service = TestBed.inject(ThemeService);

            expect(service.mode()).toBe('system');
            expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('system');
        });

        it('hydrates from a stored light value', () => {
            localStorage.setItem(THEME_STORAGE_KEY, 'light');
            configureTestBed();
            const service = TestBed.inject(ThemeService);

            expect(service.mode()).toBe('light');
            expect(service.theme()).toBe('light');
        });

        it('hydrates from a stored dark value', () => {
            localStorage.setItem(THEME_STORAGE_KEY, 'dark');
            configureTestBed();
            const service = TestBed.inject(ThemeService);

            expect(service.mode()).toBe('dark');
            expect(service.theme()).toBe('dark');
        });

        it('hydrates from a stored system value', () => {
            localStorage.setItem(THEME_STORAGE_KEY, 'system');
            configureTestBed();
            const service = TestBed.inject(ThemeService);

            expect(service.mode()).toBe('system');
        });
    });

    describe('document application', () => {
        it('applies the resolved theme to <html data-theme>', async () => {
            localStorage.setItem(THEME_STORAGE_KEY, 'dark');
            configureTestBed();
            TestBed.inject(ThemeService);

            await new Promise((r) => setTimeout(r, 0));

            expect(fakeRoot.getAttribute('data-theme')).toBe('dark');
        });

        it('removes the legacy `dark` class when applying a theme', async () => {
            fakeRoot.classList.add('dark');
            configureTestBed();
            const service = TestBed.inject(ThemeService);
            service.setTheme('light', false);

            await new Promise((r) => setTimeout(r, 0));

            expect(fakeRoot.classList.contains('dark')).toBe(false);
            expect(fakeRoot.getAttribute('data-theme')).toBe('light');
        });
    });

    describe('setTheme', () => {
        it('persists the chosen mode to localStorage', () => {
            configureTestBed();
            const service = TestBed.inject(ThemeService);

            service.setTheme('dark', false);

            expect(localStorage.getItem(THEME_STORAGE_KEY)).toBe('dark');
            expect(service.mode()).toBe('dark');
            expect(service.theme()).toBe('dark');
        });

        it('skips backend sync when syncToBackend is false', async () => {
            configureTestBed();
            mockAuth.isLoggedIn.mockReturnValue(true);
            const service = TestBed.inject(ThemeService);

            service.setTheme('dark', false);
            await new Promise((r) => setTimeout(r, 0));

            expect(mockUserPrefs.getUserPreferences).not.toHaveBeenCalled();
            expect(mockUserPrefs.updateUserPreferences).not.toHaveBeenCalled();
        });

        it('skips backend sync when the user is not logged in', async () => {
            configureTestBed();
            mockAuth.isLoggedIn.mockReturnValue(false);
            const service = TestBed.inject(ThemeService);

            service.setTheme('dark', true);
            await new Promise((r) => setTimeout(r, 0));

            expect(mockUserPrefs.updateUserPreferences).not.toHaveBeenCalled();
        });

        it('persists the selected mode to the backend', async () => {
            fakeMediaQuery.matches = true;
            configureTestBed();
            mockAuth.isLoggedIn.mockReturnValue(true);
            mockUserPrefs.getUserPreferences.mockReturnValue(of({ theme: 'light' } as any));

            const service = TestBed.inject(ThemeService);
            service.setTheme('system', true);

            await new Promise((r) => setTimeout(r, 0));

            expect(mockUserPrefs.updateUserPreferences).toHaveBeenCalledWith(expect.objectContaining({ theme: 'system' }));
        });
    });

    describe('system mode reactivity', () => {
        it('flips the resolved theme when the OS preference changes', async () => {
            configureTestBed();
            const service = TestBed.inject(ThemeService);
            service.setTheme('system', false);

            await new Promise((r) => setTimeout(r, 0));
            expect(service.theme()).toBe('light');

            fakeMediaQuery.trigger(true);

            await new Promise((r) => setTimeout(r, 0));
            expect(service.theme()).toBe('dark');
            expect(fakeRoot.getAttribute('data-theme')).toBe('dark');
        });

        it('ignores OS changes when an explicit mode is set', async () => {
            configureTestBed();
            const service = TestBed.inject(ThemeService);
            service.setTheme('light', false);

            await new Promise((r) => setTimeout(r, 0));
            expect(service.theme()).toBe('light');

            fakeMediaQuery.trigger(true);

            await new Promise((r) => setTimeout(r, 0));
            expect(service.theme()).toBe('light');
        });
    });

    describe('clearThemeFromStorage', () => {
        it('removes the persisted preference', () => {
            localStorage.setItem(THEME_STORAGE_KEY, 'dark');
            configureTestBed();
            const service = TestBed.inject(ThemeService);

            service.clearThemeFromStorage();

            expect(localStorage.getItem(THEME_STORAGE_KEY)).toBeNull();
        });
    });

    describe('initializeThemeEarly', () => {
        it('applies the stored mode synchronously to <html>', () => {
            localStorage.setItem(THEME_STORAGE_KEY, 'dark');
            ThemeService.initializeThemeEarly();
            expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
        });

        it('falls back to OS preference when no value is stored', () => {
            fakeMediaQuery.matches = true;
            ThemeService.initializeThemeEarly();
            expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
        });

        it('removes the legacy `dark` class', () => {
            document.documentElement.classList.add('dark');
            localStorage.setItem(THEME_STORAGE_KEY, 'light');
            ThemeService.initializeThemeEarly();
            expect(document.documentElement.classList.contains('dark')).toBe(false);
        });
    });
});
