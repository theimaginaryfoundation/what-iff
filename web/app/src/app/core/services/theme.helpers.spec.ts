import { THEME_STORAGE_KEY, applyThemeAttribute, clearStoredThemeMode, readStoredThemeMode, resolveSystemTheme, resolveTheme, writeStoredThemeMode, } from './theme.helpers';

describe('theme.helpers', () => {
    describe('resolveSystemTheme', () => {
        it('returns dark when the OS prefers dark', () => {
            expect(resolveSystemTheme(true)).toBe('dark');
        });

        it('returns light when the OS does not prefer dark', () => {
            expect(resolveSystemTheme(false)).toBe('light');
        });
    });

    describe('resolveTheme', () => {
        it('returns the explicit mode for light', () => {
            expect(resolveTheme('light', true)).toBe('light');
        });

        it('returns the explicit mode for dark', () => {
            expect(resolveTheme('dark', false)).toBe('dark');
        });

        it('falls back to OS preference when mode is system', () => {
            expect(resolveTheme('system', true)).toBe('dark');
            expect(resolveTheme('system', false)).toBe('light');
        });
    });

    describe('applyThemeAttribute', () => {
        function buildDoc(): {
            doc: Document;
            root: HTMLElement;
        } {
            const root = document.createElement('html');
            const doc = { documentElement: root } as unknown as Document;
            return { doc, root };
        }

        it('sets data-theme to the resolved theme', () => {
            const { doc, root } = buildDoc();
            applyThemeAttribute(doc, 'dark');
            expect(root.getAttribute('data-theme')).toBe('dark');
        });

        it('overwrites any prior data-theme value', () => {
            const { doc, root } = buildDoc();
            root.setAttribute('data-theme', 'dark');
            applyThemeAttribute(doc, 'light');
            expect(root.getAttribute('data-theme')).toBe('light');
        });

        it('removes the legacy `dark` class so styles key off data-theme only', () => {
            const { doc, root } = buildDoc();
            root.classList.add('dark');
            applyThemeAttribute(doc, 'light');
            expect(root.classList.contains('dark')).toBe(false);
        });

        it('also removes the `dark` class when applying dark', () => {
            const { doc, root } = buildDoc();
            root.classList.add('dark');
            applyThemeAttribute(doc, 'dark');
            expect(root.classList.contains('dark')).toBe(false);
            expect(root.getAttribute('data-theme')).toBe('dark');
        });
    });

    describe('storage helpers', () => {
        function makeStorage(initial: Record<string, string> = {}): Storage {
            const store: Record<string, string> = { ...initial };
            return {
                get length() {
                    return Object.keys(store).length;
                },
                clear() {
                    for (const key of Object.keys(store)) {
                        delete store[key];
                    }
                },
                getItem(key: string) {
                    return Object.prototype.hasOwnProperty.call(store, key) ? store[key] : null;
                },
                key(index: number) {
                    return Object.keys(store)[index] ?? null;
                },
                removeItem(key: string) {
                    delete store[key];
                },
                setItem(key: string, value: string) {
                    store[key] = value;
                },
            } satisfies Storage;
        }

        it('reads a valid stored mode', () => {
            const storage = makeStorage({ [THEME_STORAGE_KEY]: 'dark' });
            expect(readStoredThemeMode(storage)).toBe('dark');
        });

        it('reads `system` as a valid mode', () => {
            const storage = makeStorage({ [THEME_STORAGE_KEY]: 'system' });
            expect(readStoredThemeMode(storage)).toBe('system');
        });

        it('returns null for unknown stored values', () => {
            const storage = makeStorage({ [THEME_STORAGE_KEY]: 'sepia' });
            expect(readStoredThemeMode(storage)).toBeNull();
        });

        it('returns null when no value is stored', () => {
            const storage = makeStorage();
            expect(readStoredThemeMode(storage)).toBeNull();
        });

        it('returns null when storage is unavailable', () => {
            expect(readStoredThemeMode(null)).toBeNull();
        });

        it('returns null when storage throws', () => {
            const storage = {
                getItem() {
                    throw new Error('access denied');
                },
            } as unknown as Storage;
            expect(readStoredThemeMode(storage)).toBeNull();
        });

        it('writes a mode to storage', () => {
            const storage = makeStorage();
            writeStoredThemeMode(storage, 'system');
            expect(storage.getItem(THEME_STORAGE_KEY)).toBe('system');
        });

        it('does not throw when storage is unavailable on write', () => {
            expect(() => writeStoredThemeMode(null, 'dark')).not.toThrow();
        });

        it('does not throw when setItem fails', () => {
            const storage = {
                setItem() {
                    throw new Error('quota exceeded');
                },
            } as unknown as Storage;
            expect(() => writeStoredThemeMode(storage, 'dark')).not.toThrow();
        });

        it('clears the stored mode', () => {
            const storage = makeStorage({ [THEME_STORAGE_KEY]: 'dark' });
            clearStoredThemeMode(storage);
            expect(storage.getItem(THEME_STORAGE_KEY)).toBeNull();
        });

        it('does not throw when clearing on unavailable storage', () => {
            expect(() => clearStoredThemeMode(null)).not.toThrow();
        });
    });
});
